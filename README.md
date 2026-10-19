# Homie

A homelab dashboard: all your services in one place, organized into groups and managed entirely from the web interface. Add, edit, delete, and reorder services and groups by dragging them, without touching any configuration files.

Designed to run in an LXC container on Proxmox VE and to be used on a local network (or through a VPN). It has a **login** (user name and password, remembered by the browser, with an optional fingerprint or face lock for the phone app): see [docs/security.md](docs/security.md).

## Installing on Proxmox

From the Proxmox host shell:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/Bagareal/homie/main/install.sh)"
```

The script creates an unprivileged Debian LXC (defaults: 1 core, 512 MB of RAM, 4 GB of disk, DHCP on vmbr0, start on boot), installs the latest release after verifying its checksum, and starts the service. At the end it prints the dashboard address, reachable on port 8080.

Answering "n" to the first question lets you choose the ID, hostname, resources, storage, bridge, and a static IP.

## Updating

From the LXC console (or after `pct enter <ID>`), run:

```bash
update
```

Or from the Proxmox host, without entering the container:

```bash
pct exec <ID> -- update
```

It downloads the latest release and restarts the service; your configuration is left untouched. The first time you open Homie after updating from a version without a login, it asks you to create the account: do it right away. It installs the latest published GitHub Release, so a change only reaches you once a release has been published; if it says you are already up to date right after one appeared, try again in a minute.

## Where the data lives

| Path | Contents |
| --- | --- |
| `/var/lib/homie/config.json` | Groups, services, widgets (with their passwords and API keys), themes, and settings |
| `/var/lib/homie/backgrounds/` | Uploaded background images |
| `/var/lib/homie/icons/` | Local icons, reachable as `/icons/name.png` |
| `/var/lib/homie/icon-cache/` | Icons downloaded from the collections, kept so they never need to be fetched again (safe to delete) |
| `/usr/local/bin/homie` | The program |
| `/etc/systemd/system/homie.service` | The service (runs as the `homie` system user) |

The configuration can also be exported and imported from the Settings page; the exported file leaves out widget passwords and API keys (enter them again after importing on another install) and background images. A PBS backup of the LXC includes everything, so keep such backups private: `config.json` holds the widget credentials.

## Icons

In a service's edit dialog, **Search icons** opens a picker with the [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) and [selfh.st](https://selfh.st/icons) collections (searchable by name, alias, and category, with the light/dark variants listed separately); click one to fill the field. Homie downloads each icon itself the first time it is shown (from GitHub, falling back to jsDelivr) and keeps it in its data folder, so your browser never needs to reach a CDN.

You can also type in the Icon field:

- the name of an icon from the [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) collection, for example `proxmox`, `truenas-scale`, `adguard-home`, `jellyfin` (a bare name is looked up in dashboard-icons first, then in selfh.st, and icons that only exist as SVG or WebP are found automatically; `selfhst:name` forces selfh.st)
- the full address of an image
- a file placed in `/var/lib/homie/icons/`, for example `/icons/router.png`
- an emoji or a single character

If the field is empty or the icon fails to load, the tile shows the service's initials.

## Service status

The backend periodically runs an active check on each service (interval set in Settings). A service is online if it responds with an HTTP status code below 500: even a login page (401, 403, or a redirect) counts as online, while a 502 or 503 from a reverse proxy with no backend does not. Self-signed certificates are accepted.

## Widgets

A widget is a standalone block, separate from the groups, that shows live numbers from one of your hosts. Six kinds exist so far, each with its own setup guide:

- **Glances**: CPU, memory, swap, load, uptime, temperatures, disks and network traffic of any Linux host (a Proxmox node, for example). Guide: [docs/widgets-glances.md](docs/widgets-glances.md) (installing Glances and keeping it running).
- **Proxmox VE**: LXC and VM counts, single guests, nodes, storages, last backup, cluster quorum and failed tasks, for one node or a whole cluster. Guide: [docs/widgets-proxmox.md](docs/widgets-proxmox.md) (creating a read-only API token).
- **Proxmox Backup Server**: datastore space, how long ago each VM/container was last backed up, the latest backup, failed tasks and the server's load. Guide: [docs/widgets-pbs.md](docs/widgets-pbs.md).
- **TrueNAS**: pools, alerts, apps, drive temperatures, CPU, memory, uptime. Guide: [docs/widgets-truenas.md](docs/widgets-truenas.md) (creating a read-only user and an API key, and what to avoid).
- **Home Assistant**: the sensors you choose (temperatures, power, doors…) and, if you want, switches for lights, plugs and scenes. Guide: [docs/widgets-homeassistant.md](docs/widgets-homeassistant.md) (creating the access token, and what can and cannot be switched).
- **UPS (NUT)**: status, battery charge, runtime, load, power, voltage and which machines are connected, each marked primary or secondary. Guide: [docs/widgets-nut.md](docs/widgets-nut.md) (letting `upsd` accept Homie, and naming the clients).

A Proxmox, TrueNAS or UPS widget can also read the same machine's **Glances** (tick "Also read this machine's Glances" in the Source tab): temperatures, disks and network then sit in the same block, one widget per machine. On a cluster, each node can have its own Glances address. In the same way a Glances, Proxmox or TrueNAS widget can read the machine's **UPS** ("Also read this machine's UPS"), so a machine that runs NUT and does other things too needs one widget only.

Use **Edit → Add widget**, pick the type, fill in the address and press "Test connection" (each source of a widget, the main one, Glances and the UPS, has its own Test button, so you can see which one has been checked): Homie then lists what the host offers (disks, sensors, network cards, pools, drives), so you choose from a menu instead of typing names. Each value is a row you can rename ("HDD esterno" instead of `/mnt/hdd-esterno`), reorder by dragging, or remove. A CPU row can show the load, a temperature, or both side by side (the bar follows the load, or the temperature when it is alone).

The editor has four tabs (Source, Values, Size, Appearance) with the preview always in view (it grows to show every value; on the dashboard the widget keeps the size you chose); In edit mode the "Add group" and "Add widget" buttons are in the top bar. A widget is one group card wide and as tall as its values need (tick *Fixed height* in the Size tab to set the height in pixels), and you choose its colors (background with opacity, text, border, bars). Where the widgets go next to the groups is a setting (*Settings → Appearance → Theme and layout → Widgets*): **Masonry** mixes widgets and groups, each block going to the shortest column; **Column** keeps the widgets stacked in a column of their own, on the left or the right; **Strip** puts them, compact, in a row above the groups, and a click opens one in a small window with all its values. While editing, drag a widget to reorder it (in Masonry also between the groups); on a phone the widgets have their own tab.

**Collapsing widgets.** Every widget has a small arrow in its header that collapses it to a single line with up to four values (each with its name in small print underneath); the button "Collapse widgets" in the top bar collapses or opens them all. In the widget's Values tab, tick "Collapsed" on the values to keep (none ticked: the first four). Settings → General can collapse them automatically when the window is narrower than a width you choose, so a laptop screen starts compact while a big monitor shows everything. What you collapse or open by hand is remembered per device (in the browser), not in the shared configuration, and never applies on a phone, which has its own Widgets tab.

Homie's server reads the hosts, not your browser, so a host only has to be reachable from Homie's container. Each address has a Port field next to it with the default in grey (Glances 61208, Proxmox VE 8006, UPS 3493): leave it empty, or type your own if you changed it. Each address needs its scheme (Glances `http://`, Proxmox VE `https://`, UPS `nut://`), and a name that goes through a reverse proxy usually does not reach the host's own ports: see [Which address to enter](docs/widgets-glances.md#which-address-to-enter). Passwords and API keys are stored only on the server: they are never sent back to the browser, are not included in exported backups, and changing a widget's address or username removes them.

## Login

Homie asks for a user name and a password (created the first time you open it), and keeps a device signed in for 30 days after its last use. Forgot it: run `homie-reset-password` in the container. On a phone you can add a biometric lock to the installed app. Everything is explained in [docs/security.md](docs/security.md).

## Updates

Homie tells you when a new version is out. The server asks GitHub, from the container (the same place `update` downloads from; your browser never does), at most every few hours, and a window opens **once per version** with what changed and how to update (run `update` in the Homie container). A small dot on the **Settings** button, and a line in Settings → General, remind you until you have updated. The first time you open Homie after an update, a window shows what changed in the versions you skipped. What each device has already seen is remembered in its browser.

The text comes from the release notes on GitHub, which are built from the commit messages since the previous version (see `scripts/release-notes.sh`, run by the release workflow): the first line of a commit is a change's title and the rest its description. Turn the check off in Settings → General ("Tell me when a new version of Homie is out") if the container should not contact GitHub; it also does nothing in a development build. If GitHub cannot be reached, nothing is shown and nothing breaks.

## Appearance

The Appearance section in Settings lets you customize:

- the theme (light, dark, or automatic) and the tile density
- where the widgets go next to the groups: *Masonry*, *Column* (left or right) or *Strip*, see Widgets above. Each group is a card, and the cards sit side by side (up to three columns, fewer on a narrower window).
- colors: page background, tile background, accent (used for group titles and drag indicators), and the group card color. Each color has a "Reset" button to return to the theme's default.
- a background image (JPEG or PNG) with its own opacity
- the opacity of the top bar and of the group cards

### Themes

Everything on the Appearance page (colors, theme mode, layout, density, the background image, opacities) plus the look of each widget can be saved as a **theme**: *Settings → Appearance → Themes → Save current as…*. Apply, rename, update or delete your themes from the same list, and switch between them with one click. The active theme shows "modified" when you have changed something since applying it; *Update* saves the changes into it.

Three themes ship with Homie (near-black graphite with white and light-grey text, differing only in the accent): **Mono**, **Neon green** and **Purple**. They only set the colors, so your layout, density and background image stay as they are, and widgets go back to following the theme. They cannot be overwritten, but you can apply one and save it under your own name.

*Export* writes a theme to a small `.json` file and *Import theme…* reads one (the background image is not part of the file). Background images are kept on the server for as long as the current look or any saved theme uses them.

### Widget look

Under *Appearance → Widgets* you set the look every widget shares (background with opacity, text, border, bars); by default a widget looks like a tile. A widget can override any of it in its own editor, and "Use this look for all widgets" copies one widget's look into the shared one.

## Installing as an app (PWA)

Homie can be installed like an app, with its own icon and a full-screen window. Browsers only allow this over **HTTPS**, so open it through a reverse proxy with a certificate (for example Nginx Proxy Manager) rather than `http://<ip>:8080`, then use "Install" in the browser menu (Chrome, Edge) or "Add to Home Screen" (Safari). On iPhone and iPad "Add to Home Screen" also works over plain HTTP.

The service worker only caches the interface's build files. The dashboard and service status always come from the server, and if the server cannot be reached a short "unreachable" page is shown.

## Project structure

```
cmd/homie/              program entry point
internal/config/        data model, validation, persistence
internal/status/        periodic service checks
internal/updates/       finds the released versions on GitHub (new-version window)
internal/glances/       reads host metrics from the Glances API (widgets)
internal/truenas/       reads a TrueNAS system over its JSON-RPC WebSocket API (widgets)
internal/nut/           reads a UPS from a NUT server (widgets)
internal/proxmox/       reads a Proxmox VE node or cluster through its API (widgets)
internal/pbs/           reads a Proxmox Backup Server through its API (widgets)
internal/homeassistant/ reads and switches Home Assistant entities through its REST API (widgets)
internal/auth/          login: account, sessions, passkeys (biometric lock), password recovery
internal/server/        HTTP API and pages
internal/webui/         compiled interface, embedded in the binary
ui/                     interface (Svelte 5, TypeScript, Vite)
docs/                   widget setup guides, and how the login works
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

GitHub Actions builds the interface and the binaries and creates the release with the files `homie-linux-amd64`, `homie-linux-arm64`, and `SHA256SUMS`, which are the ones downloaded by the installer and the updater. The release's text (its changelog) is written from the commit messages since the previous tag by `scripts/release-notes.sh`, and is what Homie shows when a new version is out: write each commit as a first line that works as a title, then a short description of what changed for the user.

## API

All of these need a signed-in session (the cookie set by `/api/auth/login`); `/api/auth/*` is the login itself (status, setup, login, logout, password, devices, passkeys).

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/config` | Full configuration |
| PUT | `/api/config` | Replaces the configuration (validated) |
| GET | `/api/status` | Latest status of every checked service |
| GET | `/api/widgets` | Latest readings of every widget (cached for half the refresh interval) |
| POST | `/api/widgets/test` | Checks a widget's address (and login) from the editor |
| POST | `/api/widgets/{id}/command` | Switches a Home Assistant row that was given a switch (`{row, action}`) |
| GET | `/api/version` | Running version |
| GET | `/api/releases` | Released versions with their notes (read from GitHub by the server, cached for hours; `?refresh=1` asks again, at most once a minute) |

## Credits

Homie does not ship any icons. The icon picker and the icon names use two community collections, which Homie downloads from GitHub onto your own server when an icon is first shown:

- [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) by Homarr Labs and its contributors
- [selfh.st/icons](https://github.com/selfhst/icons) by selfh.st

The icons are the logos of the projects they depict and remain the property and trademarks of their respective owners; each collection has its own license, which applies to what you download from it.
