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

In a service's edit dialog, **Search icons** opens a picker with the whole [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) collection (searchable by name, alias, and category, with the light/dark variants listed separately); click one to fill the field. The browser needs to reach `cdn.jsdelivr.net` for this.

You can also type in the Icon field:

- the name of an icon from the [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) collection, for example `proxmox`, `truenas-scale`, `adguard-home`, `jellyfin` (icons that only exist as SVG or WebP are found automatically, or can be written as `name.svg`)
- the full address of an image
- a file placed in `/var/lib/homie/icons/`, for example `/icons/router.png`
- an emoji or a single character

If the field is empty or the icon fails to load, the tile shows the service's initials.

## Service status

The backend periodically runs an active check on each service (interval set in Settings). A service is online if it responds with an HTTP status code below 500: even a login page (401, 403, or a redirect) counts as online, while a 502 or 503 from a reverse proxy with no backend does not. Self-signed certificates are accepted.

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
| GET | `/api/version` | Running version |
