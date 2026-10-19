# Home Assistant widget

Shows the sensors of your [Home Assistant](https://www.home-assistant.io/) in a Homie widget, and lets you switch lights, plugs and scenes from it. You choose, one by one, what the widget shows and what it may switch.

## 1. Create an access token

In Home Assistant: your profile (your name, bottom left) → **Security** → **Long-lived access tokens** → **Create token**. Name it `Homie`. The token is shown **once**: copy it.

The token gives full access to your Home Assistant, whatever you tell Homie to show. Homie keeps it on the server only (in `config.json`, readable by the `homie` user alone), never sends it back to the browser and leaves it out of exported backups. If you stop using Homie, or the token leaks, delete it in the same page.

## 2. Add the widget

**Edit → Add widget → Home Assistant.** Fill in:

- **Address**: where you open Home Assistant, for example `http://192.168.1.10` with the port `8123`. With `https://` the certificate is checked, so the address needs a valid one (a self-signed certificate is refused: use the plain `http://` address on your network instead).
- **Long-lived access token**: the one from step 1.

Press **Test connection**. Homie reads the list of your entities, and the **Values** tab lets you add them: type in the box a part of the name (`power`, `living room`) or of the id (`sensor.`) and pick one. Each entity is a row you can rename, reorder by dragging or remove, like in the other widgets, and tick as *Collapsed* to keep it when the widget is collapsed.

## What is shown

| Entity | Shown as |
| --- | --- |
| Sensors with a number | the value and its unit (`68.2 W`, `21.4 °C`); a `%` also gets a bar |
| Binary sensors | words for the kind of sensor: Open/Closed (doors, windows), Motion/Clear, Wet/Dry, Connected/Disconnected… red when it is the bad side (smoke, moisture, low battery, lost connection) |
| Lights, switches, fans, input booleans, automations | On/Off (a light also says its brightness) |
| Scenes, scripts, buttons | a **Run** button, if you gave the row one |
| Anything else | its state as text |
| Unavailable or unknown | red, with the state |
| An entity that does not exist any more | `not found` (renamed or deleted in Home Assistant) |

The widget re-reads at the interval you set (2–300 seconds).

## Two values in one row

A temperature and a humidity (or any two sensors) can share a row, side by side, like the load and the temperature of a CPU: on the row of the first sensor tick **Second value** and pick the other one. The row shows both (`21.4 °C   55%`), takes its name from the first unless you give it one, turns red if either is unavailable, and its bar follows the one that is a percentage. A row with two values has no switch.

## Switching things

Rows of these kinds can have a **Switch** (a **Run button** for scenes, scripts and buttons): `light`, `switch`, `input_boolean`, `fan`, `automation`, `scene`, `script`, `button`, `input_button`. Tick the box on the row in the Values tab. Without it, the row only shows the value.

What Homie does when you press it:

- The switch moves at once and Home Assistant has the last word a moment later (some devices, a Zigbee lamp for example, answer after a fraction of a second): if the device did not change, the switch goes back.
- The browser only says *which row of which widget* and whether to toggle; the server decides the entity and the service from the saved widget. A request cannot name another entity, so the widget cannot be used to reach the rest of your home.
- **Locks, alarm panels, covers, garage doors, climate and everything else cannot be controlled from Homie**, whatever the configuration says: they are not in the list above and the server refuses them.
- Anyone who is signed in to Homie can press the switches: see [Security](security.md).

## Showing a sensor in another widget

Not yet: the widget has its own rows. A sensor of Home Assistant (a smart plug that measures the power of your server, for example) goes in a Home Assistant widget; a UPS widget keeps showing what the UPS itself reports.

## Troubleshooting

- *the token was refused*: the token was copied with a space, it was deleted in Home Assistant, or it belongs to a user that was disabled.
- *cannot reach the server*: Homie's server (the container) must reach Home Assistant: try `curl http://<address>:8123/api/` from inside it. A name served only to your computer (by a router or `hosts` file) may not resolve in the container.
- *the address redirects to …*: use the address it names. Homie never follows a redirect, so the token cannot be sent somewhere else.
- *the certificate is not trusted*: see the address above.
- A row says `n/a` for a moment after you add an entity: the next reading brings it.
