#!/usr/bin/env bash
# Homie - install and update inside the container (Debian).
# Run by install.sh on first install and by homie-update for updates.
# The configuration in /var/lib/homie is never touched.
set -Eeuo pipefail

REPO="${HOMIE_REPO:-Bagareal/homie}"
BRANCH="${HOMIE_BRANCH:-main}"
BIN=/usr/local/bin/homie
DATA=/var/lib/homie

die() { echo "error: $*" >&2; exit 1; }
[[ $EUID -eq 0 ]] || die "run this script as root."

case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

if ! command -v curl >/dev/null || ! command -v sha256sum >/dev/null; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq curl ca-certificates coreutils >/dev/null
fi

# Patch the container's own Debian packages (security fixes), so one `update`
# keeps everything current. Plain `upgrade` only: it never removes a package and
# never moves to a new Debian release. A failure here is reported, not fatal:
# Homie itself must still update.
os_upgrade() {
  command -v apt-get >/dev/null || return 0
  echo "Updating the system packages..."
  export DEBIAN_FRONTEND=noninteractive
  if apt-get update -qq && apt-get -y -qq -o Dpkg::Options::=--force-confold upgrade >/dev/null; then
    apt-get -y -qq autoclean >/dev/null || true
    echo "System packages are up to date."
  else
    echo "warning: the system package update failed; Homie is not affected." >&2
  fi
}

OLD="$([[ -x $BIN ]] && $BIN --version || echo "none")"

# Download the latest release and verify its checksum.
BASE="${HOMIE_RELEASE_BASE:-https://github.com/${REPO}/releases/latest/download}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
curl -fsSL -o "$TMP/homie-linux-${ARCH}" "${BASE}/homie-linux-${ARCH}" || die "release download failed"
curl -fsSL -o "$TMP/SHA256SUMS" "${BASE}/SHA256SUMS" || die "checksum download failed"
(cd "$TMP" && grep " homie-linux-${ARCH}\$" SHA256SUMS | sha256sum -c --quiet -) || die "invalid checksum, installation aborted"

NEW="$(chmod +x "$TMP/homie-linux-${ARCH}" && "$TMP/homie-linux-${ARCH}" --version)"
if [[ "$OLD" == "$NEW" ]]; then
  echo "Homie is already at the latest version (${NEW})."
  os_upgrade
  exit 0
fi

# Dedicated system user, with no shell or interactive home.
id homie >/dev/null 2>&1 || useradd --system --home-dir "$DATA" --shell /usr/sbin/nologin homie
install -d -o homie -g homie -m 750 "$DATA" "$DATA/icons"
install -m 755 "$TMP/homie-linux-${ARCH}" "$BIN"

cat >/etc/systemd/system/homie.service <<'EOF'
[Unit]
Description=Homie - homelab dashboard
After=network-online.target
Wants=network-online.target

[Service]
User=homie
Group=homie
Environment=HOMIE_ADDR=:8080
Environment=HOMIE_DATA=/var/lib/homie
ExecStart=/usr/local/bin/homie
Restart=on-failure
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF

# Update command inside the container.
cat >/usr/local/bin/homie-update <<EOF
#!/usr/bin/env bash
set -euo pipefail
curl -fsSL "https://raw.githubusercontent.com/${REPO}/${BRANCH}/scripts/setup.sh" | HOMIE_REPO="${REPO}" HOMIE_BRANCH="${BRANCH}" bash -s
EOF
chmod 755 /usr/local/bin/homie-update
# /usr/local/bin isn't on PATH in a bare `pct enter`/`pct exec` shell; /usr/sbin is.
ln -sf /usr/local/bin/homie-update /usr/sbin/update

# Forgot the password: forget the account, then create a new one in the browser.
cat >/usr/local/bin/homie-reset-password <<EOF
#!/usr/bin/env bash
set -euo pipefail
HOMIE_DATA="${DATA}" exec "${BIN}" reset-password
EOF
chmod 755 /usr/local/bin/homie-reset-password
ln -sf /usr/local/bin/homie-reset-password /usr/sbin/homie-reset-password

systemctl daemon-reload
systemctl enable homie >/dev/null 2>&1
systemctl restart homie

if [[ "$OLD" == "none" ]]; then
  echo "Homie ${NEW} installed."
else
  echo "Homie updated from ${OLD} to ${NEW}."
  os_upgrade
fi
