# UPS widgets (NUT)

A UPS widget shows the state of a UPS that is managed by [Network UPS Tools](https://networkupstools.org/) (NUT): whether the mains is present, the battery charge, the runtime left, the load, the voltage and which machines are connected to it.

## How it works

- NUT's server, `upsd`, runs on the machine the UPS is plugged into and listens on TCP port **3493**.
- The Homie **server** (not your browser) asks it for the UPS variables every few seconds. Reading needs no password, so there is nothing to store. Homie's container must be able to reach the port.
- The address is `nut://host` (port 3493) or `nut://host:port`. If the server has several UPS units add the name of the one you want, `nut://host/myups`; without a name the first one is used.

## 1. Let `upsd` accept connections from Homie

By default `upsd` only listens on the machine itself. In `/etc/nut/upsd.conf` add:

```
LISTEN 0.0.0.0 3493
```

and restart it (`systemctl restart nut-server`). If the machine has a firewall, allow port 3493 from Homie. To check from another machine:

```bash
upsc myups@192.168.1.10
```

(or, without NUT installed: `printf 'LIST UPS\n' | nc 192.168.1.10 3493`).

## 2. Add the widget in Homie

**Edit → Add widget → UPS (NUT server)**, enter `nut://` and the address of the machine running `upsd`, then press **Test connection**. Homie shows the UPS it found and lists everything that UPS reports and the machines connected to it.

Values you can show:

| Row | What it shows |
| --- | --- |
| UPS status | Online, On battery, Charging, Low battery, Replace battery… (all the flags `ups.status` reports; anything but normal is red) |
| Battery charge | percentage with a bar (red from 20% down) |
| Runtime left | how long the UPS can run on the battery at the current load |
| UPS load | percentage of the UPS capacity in use, with a bar |
| Power | watts, if the UPS reports them; otherwise an estimate (marked `~`) from the load and the nominal power |
| Voltage | the input (mains) or the output voltage |
| UPS model | the model; hover for the maker and the serial number |
| Connected clients | how many machines are connected (hover for their addresses) |
| A client machine | one machine you expect to be connected (see below) |
| Other values | any variable the UPS reports, e.g. `battery.voltage`, `ups.temperature` |

## Which machine is primary and which is secondary

`upsd` lists the addresses of the connected machines but does **not** say which one is the primary (the machine that is connected to the UPS and shuts it down last) and which are secondaries. So you tell Homie:

1. Press **Test connection**, then in the Values tab pick **Detected clients**: one row is added per connected address.
2. In each row choose the **role** (Primary or Secondary) and give it a **name** (for example "Proxmox", "NAS").

The widget then shows `Primary · connected` or `Secondary · connected` for each, and **`not connected` in red** when an expected machine is no longer talking to the UPS server, which usually means it lost power, crashed or can no longer reach the server.

Notes:

- The machine that runs `upsd` usually appears as `127.0.0.1` (that is how it reaches itself); Homie writes `::1` and `localhost` the same way.
- Use the IP address, not a host name: the server reports addresses.
- A client that is shut down on purpose also shows as not connected.

## Troubleshooting

- *cannot reach host:3493 … refused / timed out*: `upsd` is not listening on the network (see step 1) or a firewall blocks the port.
- *the server refuses this machine*: access rules on the NUT side (`ACL`/`ACCEPT` in `upsd.conf`) do not allow Homie's address.
- *the server does not know that UPS name*: the name after the host in the address does not match `upsc -l`.
- *the UPS driver is not running* / *lost contact with the UPS*: the problem is on the NUT machine, between `upsd` and the UPS (USB cable, driver service `nut-driver`).
