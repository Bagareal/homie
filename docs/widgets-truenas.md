# TrueNAS widgets

A TrueNAS widget shows live data from a TrueNAS system: the health and free space of every pool, alerts, apps, drive temperatures, CPU, memory, load, uptime and the version. Homie reads it through the TrueNAS API with an **API key**.

It needs **TrueNAS 25.04 or later**. Homie uses the current JSON-RPC API over a WebSocket (the older REST API is deprecated and removed in TrueNAS 26).

Please read the first section: it explains the one rule that decides whether this works.

## The rule: https, straight to the NAS

TrueNAS **revokes an API key as soon as it is sent over a connection that is not encrypted**. This has two consequences:

- Homie only accepts an `https://` address for TrueNAS and refuses `http://`.
- Do **not** use an address that goes through a reverse proxy (Nginx Proxy Manager, Traefik, Caddy…) that talks plain http to TrueNAS. Homie would send the key over https to the proxy, the proxy would forward it unencrypted to TrueNAS, and TrueNAS would revoke it. Symptom: the first login works, then everything answers `Invalid API key` and `authentication failed`.

Use the NAS's own address, for example `https://192.168.1.50` (its IP, or a name that resolves to it). The certificate is **not verified**, so TrueNAS's default self-signed certificate works. That also means the connection is protected against eavesdropping but not against someone impersonating the NAS: use it on a network you trust.

## 1. Create a user just for Homie

Giving Homie its own read-only user means that if the key ever leaks, it can only read.

In TrueNAS, **Credentials → Users → Add**:

1. **Username**: `homie_svc` (any name will do).
2. **Allow Access**: tick **TrueNAS Access** and set its **Role** to **Readonly Admin**. Leave SMB, Shell and SSH unticked.
3. **Authentication**: leave **Disable Password** ticked (the user never logs in; it only owns the key). If TrueNAS asks for a password, set a long random one that you will never use.
4. Leave the rest as it is and press **Save**.

**Readonly Admin** is enough for everything Homie reads. Homie only ever calls read methods (besides the login itself):

| Value in the widget | TrueNAS API methods |
| --- | --- |
| Pools | `pool.query` |
| Drives and temperatures | `disk.query`, `disk.temperatures` |
| CPU and memory, CPU temperature | `reporting.netdata_get_data` (graphs `cpu`, `memory`, `cputemp`) |
| Alerts | `alert.list` |
| Apps | `app.query` |
| Uptime, load, version | `system.info`, `system.version` |

Do not use `root` or `truenas_admin` for this: a key with full rights is far more than Homie needs.

## 2. Create the API key

1. In the TrueNAS toolbar (top right) open the **user menu → My API Keys**. (Or **Credentials → Users →** the user **→ View API Keys**.)
2. **Add API Key**. Give it a name (for example `Homie`), choose the user from the previous step in **Username**, and leave **Non-expiring** on (or set a date, but then you must replace the key before it expires).
3. **Save**. A window shows the key **once**: press **Copy to Clipboard**. It looks like `3-` followed by 64 characters. The number and the dash are part of the key.

If you lose it, delete the key and create a new one.

## 3. Add the widget in Homie

1. **Edit → Add widget**, type **TrueNAS**.
2. **Address**: `https://` and the NAS's IP address.
3. **API key**: paste the key. Leave **Username** empty; if the login is refused, enter the user's name there (for example `homie_svc`).
4. Press **Test connection**. It should say *Connected* and list your pools and drives.
5. In **Values**, add what you want to see, rename it and drag it in the order you like.

What the rows show:

- **Pool**: one row per pool (or choose one), space used with a bar; if the pool is not ONLINE the row turns red and shows its status. Hover for the last scrub.
- **Alerts**: the number of alerts nobody has dismissed, red when serious.
- **Apps**: running / installed; hover to see which ones are not running.
- **Hottest drive** (or one drive you pick): temperature. TrueNAS refreshes disk temperatures about every five minutes, so they change slowly.
- **CPU**: the usage, the temperature, or both side by side (tick *Load* and/or *Temperature* on the row). The temperature is the average of the cores; you can pick a single core instead. A machine without temperature sensors (a virtual machine, for one) shows `n/a`.
- **Memory, Load average, Uptime, Version.**

## Where the key is kept

The key is stored on the Homie server, in its configuration file, and nowhere else: it is **never sent back to the browser**, never included in exported backups, and changing the widget's address or username removes it. To stop Homie's access at any time, delete the key in TrueNAS (**My API Keys**) or the user.

## Troubleshooting

| Message or symptom | What to check |
| --- | --- |
| `authentication failed`, or `Invalid API key` | See below. |
| `TrueNAS needs an https:// address` | Use `https://` and the NAS's address. |
| `not a TrueNAS 25.04+ system` | The address answers but has no API at `/api/current`: an older TrueNAS, or another device. |
| `TLS connection failed` | The address is probably `http://` or uses the wrong port. |
| `connection failed` | The NAS is unreachable from Homie's container. |
| Memory or CPU shows `n/a` | Hover over it: the tooltip says what TrueNAS answered. The same text is in the `notes` of Homie's `/api/widgets` page. |

**Authentication keeps failing.** Go through these in order:

1. **Is the address the NAS itself?** Not a name that goes through a reverse proxy (see the rule above). If you tried one, the key is probably revoked.
2. **Look at the key list** (**My API Keys**): the **Revoked** column must say *No*. If it says *Yes*, delete the key, create a new one and use it only with the direct address.
3. **Did you copy the whole key**, including the `3-` at the start? Create a new one and use **Copy to Clipboard**.
4. **Does the user have access?** *Credentials → Users →* the user: the **Access** box must say *TrueNAS Access: Readonly Admin*.
5. **Look at the audit log** (*Credentials → Users →* the user *→ See Logs*, or *System → Audit*): a successful key login is listed as *Authentication, Credentials: API Key* with a green tick, so you can tell whether the request reached TrueNAS at all. A key that was revoked or is unknown is rejected without a line under the user.
