# Proxmox Backup Server widgets

A Backup Server widget shows the state of your backups: the space left in each datastore, how long ago every VM, container or host was last backed up, the result of the latest backup, the tasks that failed and the load of the server itself.

## How it works

- The Homie **server** (not your browser) asks the Backup Server API (port **8007**) with an **API token**. Homie's container must be able to reach that port. The certificate is not checked, so the server's own self-signed one works.
- The token only needs to read: give it the **DatastoreAudit** role on `/datastore` and the **Audit** role on `/system`. The token secret is stored on the Homie server and is never sent back to the browser or included in exported backups.
- Unlike Proxmox VE, the Backup Server is a single machine, not a cluster.

## 1. Create a read-only API token

In the Backup Server web interface:

1. **Configuration → Access Control → User Management → Add**: create a user such as `homie` in the *Proxmox Backup authentication server* realm (`homie@pbs`).
2. **Configuration → Access Control → API Token → Add**: user `homie@pbs`, token name `homie`. The secret is shown **once**: copy it.
3. **Configuration → Access Control → Permissions → Add → User Permission** (and again as **API Token Permission**, because a token does not inherit the user's permissions by default):
   - path `/datastore`, role **DatastoreAudit**, *Propagate* ticked;
   - path `/system`, role **Audit**, *Propagate* ticked.

The **token ID** you give Homie is `homie@pbs!homie` (`user@realm!name`) and the **secret** is the long value shown at creation.

## 2. Add the widget in Homie

**Edit → Add widget → Proxmox Backup Server**, enter the address (`https://192.168.1.20:8007`), the token ID and the secret, then press **Test connection**. Homie reads the server and then lists its datastores and backup groups in the menus.

Values you can show:

| Row | What it shows |
| --- | --- |
| Datastores | used / total of every datastore, or of one; red from 90 %. Hover to see when PBS expects it to be full at the current growth |
| Backup groups (last backup) | for every VM (`vm/100`), container (`ct/101`) or host, how long ago it was last backed up. **Red when it is more than two days old**, a sign that a guest fell out of the backup job. Can be limited to one group |
| Last backup | result and age of the latest backup task (OK, warnings, failed in red, running) |
| Failed tasks | tasks that failed in the last 24 hours (backup, verify, garbage collection, prune, sync); hover for the list |
| CPU / Memory / Uptime | of the Backup Server itself |
| Backup Server version | |

With the optional **Glances** source (tick *Also read this machine's Glances*) the widget can also show the temperatures, disks and network of the Backup Server.

## Troubleshooting

- **`authentication failed`**: check the token ID (`user@realm!name`) and the secret.
- **`permission denied`**: the user **and** the token both need the roles above. If the datastores show `n/a` but the version is shown, the token cannot read `/datastore`; if CPU and memory are `n/a`, it cannot read `/system`. Hover a value showing `n/a` for the reason.
- **`the address redirects to https://…`**: use `https://` and port 8007.
- A datastore showing **error** (red) is one the server cannot open, for example an unmounted disk; hover for the message.
