# Glances widgets

A Glances widget shows live numbers from any Linux host (a Proxmox node, a Raspberry Pi, a server…): CPU, memory, swap, load, uptime, temperatures, disks and network traffic. It reads them from [Glances](https://nicolargo.github.io/glances/), a small monitoring tool that can expose its data over HTTP.

This page covers installing Glances on the host and keeping it running. Adding the widget in Homie is at the end.

## How it works

- Glances runs on the host you want to watch, in *web server mode*, and listens on port **61208**.
- The Homie **server** (not your browser) asks Glances for the numbers every few seconds. So Homie's container must be able to reach the host on that port, and it does not matter whether you open Homie over https through a reverse proxy.
- Homie speaks the Glances REST API version 4 and, as a fallback, version 3. Only the values a widget actually shows are requested.

## 1. Install Glances on the host

On Debian, Ubuntu or Proxmox VE:

```bash
apt update
apt install glances
glances --version
```

Debian 13 (Proxmox VE 9) ships Glances 4, Debian 12 (Proxmox VE 8) ships Glances 3; both work. If the packaged version cannot start the web server, or you want the latest, install it with pipx instead:

```bash
apt install pipx
pipx install 'glances[web]'
```

The program is then `~/.local/bin/glances` (use that full path in the service below).

For temperatures, the host needs its sensor drivers loaded. On physical machines:

```bash
apt install lm-sensors
sensors-detect --auto
sensors            # should list temperatures
```

Virtual machines and containers usually have no sensors; the widget then just has no temperature to show.

## 2. Try it

```bash
glances -w
```

From another machine on the network, check that it answers (replace the address with the host's):

```bash
curl http://192.168.1.20:61208/api/4/status
```

You should get something like `{"version": "4.3.0"}`. Press Ctrl+C to stop it.

## 3. Start it automatically at boot

Create a systemd service so Glances always runs:

```bash
cat > /etc/systemd/system/glances.service <<'EOF'
[Unit]
Description=Glances (web server)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/bin/glances -w --disable-webui
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now glances
systemctl status glances
```

- `--disable-webui` turns off Glances' own web page and keeps only the API, which is all Homie needs.
- If you installed with pipx, replace `/usr/bin/glances` with the real path (`which glances`, or `~/.local/bin/glances`).
- Glances only reads from the system, but it is still a service listening on the network. Keep it on your local network.

## 4. Limit who can reach it (recommended)

By default Glances accepts connections on every network interface. Pick one or both:

- **Listen on one address only**: add `-B 192.168.1.20` (the host's LAN address) to the `ExecStart` line.
- **Firewall**: allow port 61208 only from Homie's container. On Proxmox, in the host's firewall (*Datacenter or node → Firewall*), add an *accept* rule for TCP port 61208 with source set to Homie's IP, and make sure no broader rule allows it.

Glances can also ask for a password (HTTP basic authentication, default user name `glances`; see the [Glances documentation](https://glances.readthedocs.io/en/latest/docs.html) for how to set it). Homie supports it, but basic authentication is sent unencrypted over plain http, so on a home network restricting access with the firewall is usually the better protection.

## 5. Add the widget in Homie

1. **Edit → Add widget**, type **Glances**.
2. Name: whatever you like (for example the host's name). Address: `http://192.168.1.20:61208`. Add the username and password only if you enabled them in Glances.
3. Press **Test connection**. It should say *Connected* and list the disks, temperature sensors and network cards of the host.
4. In **Values**, add the rows you want, rename them, and drag them in the order you like.

A password, if you set one, is stored only on the Homie server: it is never sent back to the browser or included in exported backups, and changing the address or username removes it.

### Notes for Proxmox hosts

- Install Glances on the Proxmox **host**, not inside a VM, to see the host's own numbers.
- Proxmox creates many virtual network interfaces (`tap…`, `veth…`, `fwbr…`, bridges `vmbr…`). The "Network" row sums the physical cards and skips the virtual ones; if Glances lists only a bridge, the bridge is used. You can also pick one interface in the row.
- Some machines report a bogus sensor next to the real ones (for example a Dell `dell_smm` reading 126 °C). The CPU temperature uses the processor package sensor (`Package id 0`, `Tctl`, `Tdie`); you can pick any other sensor by name in the row.
- Glances does not know about Proxmox VMs and containers; for their count you would need the Proxmox API, which Homie does not use yet.

## Troubleshooting

| Message or symptom | What to check |
| --- | --- |
| `connection failed` | The service is not running (`systemctl status glances`), the address or port is wrong, or a firewall blocks Homie's container. Try the `curl` command above from the Homie container. |
| `no response within 5 seconds` | The host is unreachable or very slow. |
| `not a Glances server` | The address answers but is not Glances in web mode. Start it with `-w`. |
| `authentication failed` | The username or password does not match what Glances expects. |
| A value shows `n/a` | The host does not provide it (no sensors, no swap…). |
| CPU or network shows 0 right after starting | Glances needs a few seconds to take its first measurement. |
