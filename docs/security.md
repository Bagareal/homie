# Security: login and biometric lock

Homie asks you to sign in. Everything but the page that shows the form (the dashboard data, the API, the icons and the background images) needs a session.

## The account

The first time you open Homie (a new install, or an update from a version without login), it asks you to create the account: a **user name** and a **password** of at least 10 characters. There is one account. Do it right after installing or updating: until it exists, whoever opens the page first creates it.

After you sign in the browser keeps you signed in for **30 days**, renewed every time you use Homie, so you do not type the password again on that device unless you clear its cookies, sign out, or stay away for a month. A different device signs in once with the password.

In **Settings → Security** you can change the password (it signs out every other device), see the signed-in devices and sign one out, and sign out.

### Forgot the password

In the Homie container run:

```bash
homie-reset-password
```

It signs every device out and forgets the account. Open Homie in the browser and create the new one, typing the **code** the command printed (so nobody else on the network can create it first). Your dashboard and widgets are not touched. (Installed before this version? Run `update` first: it adds the command.)

## What protects the account

- The password is stored as a salted **PBKDF2-HMAC-SHA256** hash (600,000 rounds), in `auth.json` in the data folder (mode 600, readable by the `homie` user only), apart from `config.json`: exporting or importing the configuration never touches it.
- The browser keeps a random token in a cookie that scripts cannot read (`HttpOnly`), that is only sent to Homie's own site (`SameSite=Strict`) and, when you open Homie over https, only over https (`Secure`). The server stores only a hash of the token.
- After five wrong passwords in a row Homie makes you wait: 5 seconds, doubling at each further attempt up to 5 minutes. The wait applies to the whole server, not to an address, because behind a proxy every visitor seems to come from it.
- A request that changes something and was started from another website is refused. Homie cannot be shown inside a frame, and the browser is told not to cache the private files.
- Passwords and tokens of widgets (and the Home Assistant token) are never sent to the browser.

Homie is still meant for your own network, or one reached through a VPN such as Tailscale. The login is the second wall, not a reason to put it on the open internet: use https (a reverse proxy) if you open it from outside.

## Biometric lock (phone, tablet, computer)

Optional, **per device**. Where you are signed in with the password, **Settings → Security → Biometric lock → Turn on for this device**. From then on, when you come back to Homie after it was out of sight for the time you chose (1, 5 or 15 minutes), it asks for your fingerprint, face or the screen lock of the device before showing anything. Come back sooner and it does not ask. It is meant for the installed app (PWA) on a phone, but works in any browser.

How it works: the device makes a **passkey** (WebAuthn), a key pair that stays in the device and opens only with the biometric. Homie keeps the public half and, to unlock, asks the device to sign a random challenge. The fingerprint or face never reach Homie. The lock is enforced by the server: until the signature is valid, that session gets nothing from the API, not only a hidden page.

Good to know:

- It needs **https** (open Homie through your reverse proxy): browsers only allow passkeys on a secure address. Plain `http://192.168.x.x` shows the option as unavailable.
- A passkey belongs to the **address** where it was made (the first one fixes it). Opening Homie from another name (another proxy host, or the IP address) does not work with the same passkeys: remove them in Settings → Security, then make them again from the address you want.
- Removing a passkey turns the lock off on the devices that used it. **Sign in with the password instead** on the lock screen always works.
- A device that lost its passkey (reinstalled, new phone) signs in with the password and turns the lock on again.

## Home Assistant switches

See [the widget guide](widgets-homeassistant.md#switching-things). In short: only the rows you gave a switch can be switched, only the kinds of entity listed there, and only by someone signed in.
