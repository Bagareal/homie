# Homie

A homelab dashboard: all your services in one place, organized into groups and managed entirely from the web interface. Add, edit, delete, and reorder services and groups by dragging them, without touching any configuration files.

Designed to run in an LXC container on Proxmox VE and to be used on a local network only: it has no login.

## Installing on Proxmox

From the Proxmox host shell:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/Bagareal/homie/main/install.sh)"
```

The script creates an unprivileged Debian LXC (defaults: 1 core, 512 MB of RAM, 4 GB of disk, DHCP on vmbr0, start on boot), installs the latest release after verifying its checksum, and starts the service. At the end it prints the dashboard address, reachable on port 8080.

Answering "n" to the first question lets you choose the ID, hostname, resources, storage, bridge, and a static IP.

## Updating

From the Proxmox host:

```bash
pct exec <ID> -- homie-update
```

or run `homie-update` from the LXC console. It downloads the latest release and restarts the service; your configuration is left untouched.

## Where the data lives

| Path | Contents |
| --- | --- |
| `/var/lib/homie/config.json` | Groups, services, and settings |
| `/var/lib/homie/icons/` | Local icons, reachable as `/icons/name.png` |
| `/usr/local/bin/homie` | The program |
| `/etc/systemd/system/homie.service` | The service (runs as the `homie` system user) |

The configuration can also be exported and imported from the Settings page. A PBS backup of the LXC includes it.

## Icons

In a service's edit dialog, **Search icons** opens a picker with the [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) and [selfh.st](https://selfh.st/icons) collections (searchable by name, alias, and category, with the light/dark variants listed separately); click one to fill the field. The browser needs to reach `cdn.jsdelivr.net` for this.

You can also type in the Icon field:

- the name of an icon from the [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) collection, for example `proxmox`, `truenas-scale`, `adguard-home`, `jellyfin` (a bare name is looked up in dashboard-icons first, then in selfh.st, and icons that only exist as SVG or WebP are found automatically; `selfhst:name` forces selfh.st)
- the full address of an image
- a file placed in `/var/lib/homie/icons/`, for example `/icons/router.png`
- an emoji or a single character

If the field is empty or the icon fails to load, the tile shows the service's initials.

## Service status

The backend periodically runs an active check on each service (interval set in Settings). A service is online if it responds with an HTTP status code below 500: even a login page (401, 403, or a redirect) counts as online, while a 502 or 503 from a reverse proxy with no backend does not. Self-signed certificates are accepted.

## Widgets

A widget is a standalone block, separate from the groups, that shows live data from a host running [Glances](https://nicolargo.github.io/glances/). Use **Edit → Add widget**, give it a name and press "Test connection": Homie then lists the disks, temperature sensors and network cards the host reports, so you pick them from a menu instead of typing them. Each value is a row you can rename ("HDD esterno" instead of `/mnt/hdd-esterno`), reorder by dragging, or remove. The CPU row can show the load, a temperature, or both side by side (the bar follows the load, or the temperature when it is alone). Other rows: memory, swap, load average, uptime, one temperature sensor, one disk (or all), network traffic.

The editor has four tabs (Source, Values, Size, Appearance) with the preview always in view; the Size tab draws the widget to scale next to a real group card. In edit mode the "Add group" and "Add widget" buttons are in the top bar. You choose the width and height and the colors (background with opacity, text, border, bars). While editing, drag a widget wherever you like, even between two groups: groups and widgets share one order. In the grid layout a widget flows like a card.

On the host, start Glances in web server mode (it listens on port 61208):

```bash
apt install glances          # or: pipx install glances
glances -w                   # add --password to require one (basic authentication)
```

Homie's server reads the Glances REST API (versions 4 and 3), so the browser never talks to the host and plain-http hosts work behind an HTTPS proxy. Use "Test connection" in the editor to check the address.

A password is stored only on the server: it is never sent back to the browser and is not included in exported backups. Changing the address or username removes it. Glances' basic authentication travels unencrypted over http, so use it on a trusted network.

### TrueNAS widgets

Choose the **TrueNAS** type when adding a widget (TrueNAS 25.04 or later). Homie talks to its JSON-RPC API over a WebSocket (the older REST API is deprecated and removed in TrueNAS 26) using an API key, so it needs an `https://` address: TrueNAS revokes any API key that is sent without TLS, so Homie refuses plain `http://` for this type. The certificate is not verified, so a self-signed one works.

Create the key in TrueNAS from the user menu at the top right (*My API Keys → Add API Key*), linked to a dedicated user that has only the *Readonly Admin* role. Paste it into the widget; it is stored on the Homie server and never sent back to the browser or exported. If the login is refused, also enter the name of the user the key belongs to.

Rows available: every pool (health, space used, last scrub in the tooltip), alerts (the count of undismissed ones, red when serious), apps (running / installed, the stopped ones in the tooltip), the hottest drive or a chosen one, CPU, memory, load average, uptime and the TrueNAS version. "Test connection" lists your pools and drives to pick from.

## Appearance

The Appearance section in Settings lets you customize:

- the theme (light, dark, or automatic) and the tile density
- the layout: *List* stacks the groups in one column, *Grid* shows each group as a card, with the cards side by side
- colors: page background, tile background, accent (used for group titles and drag indicators), and, in the grid layout, the group card color. Each color has a "Reset" button to return to the theme's default.
- a background image (JPEG or PNG) with its own opacity
- the opacity of the top bar and, in the grid layout, of the group cards

## Installing as an app (PWA)

Homie can be installed like an app, with its own icon and a full-screen window. Browsers only allow this over **HTTPS**, so open it through a reverse proxy with a certificate (for example Nginx Proxy Manager) rather than `http://<ip>:8080`, then use "Install" in the browser menu (Chrome, Edge) or "Add to Home Screen" (Safari). On iPhone and iPad "Add to Home Screen" also works over plain HTTP.

The service worker only caches the interface's build files. The dashboard and service status always come from the server, and if the server cannot be reached a short "unreachable" page is shown.

## Project structure

```
cmd/homie/              program entry point
internal/config/        data model, validation, persistence
internal/status/        periodic service checks
internal/glances/       reads host metrics from the Glances API (widgets)
internal/truenas/       reads a TrueNAS system over its JSON-RPC WebSocket API (widgets)
internal/server/        HTTP API and pages
internal/webui/         compiled interface, embedded in the binary
ui/                     interface (Svelte 5, TypeScript, Vite)
scripts/setup.sh        install and update inside the LXC
install.sh              installer for the Proxmox host
```

The build output is a single dependency-free executable, for amd64 and arm64.

## Development

Requires Go 1.22 or later and Node.js 22.

```bash
make dev     # backend on :8080, interface with live reload on :5173
make build   # builds the interface and the binary into bin/homie
make run     # builds and starts with data in ./data
make test    # runs the Go tests
```

## Publishing a release

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions builds the interface and the binaries and creates the release with the files `homie-linux-amd64`, `homie-linux-arm64`, and `SHA256SUMS`, which are the ones downloaded by the installer and the updater.

## API

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/config` | Full configuration |
| PUT | `/api/config` | Replaces the configuration (validated) |
| GET | `/api/status` | Latest status of every checked service |
| GET | `/api/widgets` | Latest readings of every widget (cached for half the refresh interval) |
| POST | `/api/widgets/test` | Checks a Glances address from the editor |
| GET | `/api/version` | Running version |
