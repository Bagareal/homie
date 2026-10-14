# Proxmox VE widgets

A Proxmox widget shows the state of a Proxmox VE node or of a whole cluster: how many containers (LXC) and virtual machines are running, the load of the nodes, the storages, the last backup, the cluster quorum and the tasks that failed. Together with the optional Glances source it can also show the CPU temperature, disks and network of the nodes.

## How it works

- The Homie **server** (not your browser) asks the Proxmox API (port **8006**) with an **API token**. Homie's container must be able to reach that port. The certificate is not checked, so Proxmox's own self-signed one works.
- One request to `/cluster/resources` lists every node, container, VM and storage of the **whole cluster**, so you connect to any single node and still see all of them. A single, standalone node works the same way.
- The token only needs to read: give it the **PVEAuditor** role. The token secret is stored on the Homie server and is never sent back to the browser or included in exported backups.

## 1. Create a read-only API token

In the Proxmox web interface:

1. **Datacenter → Permissions → Users → Add**: create a user such as `homie` in the *Proxmox VE authentication server* realm (`homie@pve`). It needs no password.
2. **Datacenter → Permissions → Add → User Permission**: path `/`, user `homie@pve`, role **PVEAuditor**, *Propagate* ticked.
3. **Datacenter → Permissions → API Tokens → Add**: user `homie@pve`, token ID `homie`. The secret is shown **once**: copy it.

   If you leave **Privilege Separation** ticked (the default), the token has no permissions of its own: add the same permission for the token too (**Add → API Token Permission**, path `/`, token `homie@pve!homie`, role PVEAuditor). Unticking it makes the token inherit the user's permissions.

The **token ID** you give Homie is `homie@pve!homie` (`user@realm!name`) and the **secret** is the long value shown at creation.

## 2. Add the widget in Homie

**Edit → Add widget → Proxmox VE**, enter the address (`https://192.168.1.10:8006`), the token ID and the secret, then press **Test connection**. Homie reads the whole cluster and then lists nodes, containers, VMs, pools, tags and storages in the menus.

Values you can show:

| Row | What it shows |
| --- | --- |
| Nodes | one line per node with its CPU and memory, or `offline` in red |
| CPU / Memory / Uptime | of every node, of one node, or (CPU and memory) of the whole cluster |
| LXC containers / Virtual machines | running out of total (templates are not counted); hover to see the ones that are not running. Can be limited to one node, one pool or one tag |
| A container or VM | one guest: state, CPU and memory (it keeps working if it moves to another node) |
| Storage | used / total of every storage, or of one; a shared storage is listed once, a local one per node; an unavailable storage is red |
| Last backup | result and age of the latest backup job (OK, warnings, failed in red, running) |
| Cluster quorum | whether the cluster has quorum and how many nodes are online; `Standalone` for a single node |
| Failed tasks | tasks that failed in the last 24 hours; hover for the list |
| Proxmox version | |

### Which address to enter

In the editor, enter `https://192.168.1.10` and leave **Port** empty: the default 8006 is used (type another number there if you changed it). The stored address is then `https://192.168.1.10:8006`. The scheme is **https** and the host can be an IP or a name. If you normally open Proxmox through a reverse proxy by name, that name points to the proxy and the widget will usually only work through it on 443; the node's own IP with `:8006` is the most reliable. The rules for every widget are collected in [widgets-glances.md](widgets-glances.md#which-address-to-enter).

## Temperatures, disks and network (Glances)

The Proxmox API does not report temperatures. The same widget can read them from **Glances** running on the node: in the Source tab tick **Also read this machine's Glances**, enter its address (`http://192.168.1.10:61208`: **http**, not https, and with the port) and press **Test Glances**. The widget has two Test buttons, one in each section (*Test Proxmox connection* and *Test Glances*), so you always know what has been checked and what has not; the result stays under the button you pressed. The menu then offers a group of Glances values (CPU, temperature, memory, swap, load, disks, network) that you mix freely with the Proxmox ones. Setting up Glances on the host is explained in [widgets-glances.md](widgets-glances.md).

**Cluster:** every node has its own Glances. After testing the connection, the nodes found are listed under the Glances address, each with its own address field (the same login is used for all). The menu then has one Glances group per node, and each row remembers which node it reads.

If Glances cannot be reached, only its rows show `n/a` (hover for the reason); the Proxmox rows keep working.

## The editor

- The preview on the left **grows to show every value** instead of scrolling. On the dashboard a widget is as tall as its values need, unless you tick *Fixed height* in the Size tab (then it scrolls if it has more rows than fit).
- Press **Test Proxmox connection** after entering the address and the token, then **Test Glances** after the Glances address.

## Troubleshooting

- *authentication failed*: the token ID must be written exactly `user@realm!name` and the secret must be the one shown at creation; a token that was deleted or expired no longer works.
- *permission denied* or a list that is empty / missing nodes: the role is not on `/` with propagation, or the token has Privilege Separation without its own permission (see step 1).
- *the address redirects to https://…*: you wrote `http://`; Proxmox VE speaks only https (Homie now corrects a typed `http://` by itself).
- *cannot reach the server*: the address must be a Proxmox node on port 8006 and reachable from the Homie container. Prefer the node's address to one behind a reverse proxy.
- *Last backup* says `None yet`: Proxmox keeps a limited history of tasks; it only shows jobs of the recent past.
- Not tested on a real cluster: the cluster rows were built from the Proxmox documentation and checked against a simulated three-node cluster. If something looks wrong on yours, the raw answers of `/cluster/resources` and `/cluster/status` (`pvesh get /cluster/resources --output-format json`) show what Proxmox really reports.
