<script lang="ts">
  import { onMount } from 'svelte'
  import * as api from '../lib/api'
  import { auth } from '../lib/auth.svelte'
  import { app } from '../lib/state.svelte'
  import { biometricAvailable, createPasskey, passkeyError, usePasskey } from '../lib/webauthn'

  let devices = $state<api.Device[]>([])
  let passkeys = $state<api.PasskeyInfo[]>([])
  let loaded = $state(false)
  let loadError = $state('')
  let canBiometric = $state(false)
  let busy = $state(false)
  let lockError = $state('')
  // svelte-ignore state_referenced_locally
  let after = $state(auth.lockAfter)

  let current = $state('')
  let next = $state('')
  let again = $state('')
  let pwError = $state('')

  const afters = [
    [60, 'After 1 minute'],
    [300, 'After 5 minutes'],
    [900, 'After 15 minutes'],
  ] as const

  async function load() {
    try {
      const r = await api.authDevices()
      devices = r.devices
      passkeys = r.passkeys
      loadError = ''
    } catch (e) {
      loadError = (e as Error).message
    } finally {
      loaded = true
    }
  }

  onMount(() => {
    void load()
    void biometricAvailable().then((ok) => (canBiometric = ok))
  })

  const when = (iso: string) => {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
  }

  async function changePassword(e: SubmitEvent) {
    e.preventDefault()
    pwError = ''
    if (next.length < 10) return void (pwError = 'The new password needs at least 10 characters.')
    if (next !== again) return void (pwError = 'The two new passwords are not the same.')
    busy = true
    try {
      await api.authChangePassword(current, next)
      current = next = again = ''
      app.notify('Password changed. The other devices were signed out.')
      await load()
    } catch (err) {
      pwError = (err as Error).message
    } finally {
      busy = false
    }
  }

  // Makes a passkey on this device and turns the lock on. A device that already has one
  // (it was signed out and back in) just proves it and takes it again.
  async function turnOn() {
    lockError = ''
    busy = true
    try {
      let status: api.AuthStatus
      try {
        const opts = await api.passkeyRegisterBegin()
        status = await api.passkeyRegisterFinish(await createPasskey(opts), auth.device || 'This device', after)
      } catch (e) {
        if ((e as { name?: string })?.name !== 'InvalidStateError') throw e
        const opts = await api.passkeyAssertBegin(true)
        status = await api.passkeyAssertFinish(await usePasskey(opts), true, after)
      }
      auth.apply(status)
      app.notify('Biometric lock is on for this device')
      await load()
    } catch (e) {
      // The browser's own errors (a DOMException) are explained; the server's are shown as they are.
      lockError = e instanceof DOMException ? passkeyError(e) : (e as Error).message
    } finally {
      busy = false
    }
  }

  async function turnOff() {
    busy = true
    try {
      auth.apply(await api.lockOff())
      app.notify('Biometric lock is off for this device')
      await load()
    } catch (e) {
      lockError = (e as Error).message
    } finally {
      busy = false
    }
  }

  async function setAfter(sec: number) {
    after = sec
    if (!auth.biometric) return
    try {
      auth.apply(await api.lockAfter(sec))
      app.notify('Saved')
    } catch (e) {
      lockError = (e as Error).message
    }
  }

  async function signOutDevice(d: api.Device) {
    const ok = await app.confirm({ title: 'Sign out', message: `Sign out “${d.label}”? It will have to sign in again.`, confirmLabel: 'Sign out', danger: true })
    if (!ok) return
    try {
      await api.authRevoke(d.id)
      await load()
    } catch (e) {
      app.notify((e as Error).message, true)
    }
  }

  async function signOutOthers() {
    const ok = await app.confirm({ title: 'Sign out the other devices', message: 'Every device but this one will have to sign in again.', confirmLabel: 'Sign out', danger: true })
    if (!ok) return
    try {
      await api.authRevokeOthers()
      await load()
    } catch (e) {
      app.notify((e as Error).message, true)
    }
  }

  async function removePasskey(p: api.PasskeyInfo) {
    const ok = await app.confirm({
      title: 'Remove passkey',
      message: `Remove “${p.name}”? ${p.used ? `The ${p.used} device${p.used === 1 ? '' : 's'} locked with it will no longer ask for the biometric.` : ''}`.trim(),
      confirmLabel: 'Remove',
      danger: true,
    })
    if (!ok) return
    try {
      await api.passkeyRemove(p.id)
      await auth.refresh()
      await load()
    } catch (e) {
      app.notify((e as Error).message, true)
    }
  }

  async function signOut() {
    await auth.logout()
  }
</script>

<section>
  <h2>Account</h2>
  <p class="who">Signed in as <strong>{auth.username}</strong>{auth.device ? ` · ${auth.device}` : ''}.</p>
  <form class="pw" onsubmit={changePassword} novalidate>
    <input type="text" name="username" value={auth.username} autocomplete="username" hidden readonly />
    <label>
      Current password
      <input type="password" bind:value={current} autocomplete="current-password" maxlength="200" />
    </label>
    <label>
      New password
      <input type="password" bind:value={next} autocomplete="new-password" maxlength="200" />
    </label>
    <label>
      Repeat the new password
      <input type="password" bind:value={again} autocomplete="new-password" maxlength="200" />
    </label>
    {#if pwError}<small class="bad" role="alert">{pwError}</small>{/if}
    <div class="row">
      <button type="submit" disabled={busy || !current || !next}>Change password</button>
      <button type="button" onclick={signOut}>Sign out</button>
    </div>
  </form>
  <p class="hint">At least 10 characters. Changing the password signs out every other device. If you forget it, run <code>homie-reset-password</code> in the Homie container and create the account again.</p>
</section>

<section>
  <h2>Biometric lock</h2>
  {#if !auth.secure}
    <p class="hint">The biometric lock needs Homie to be opened over https (for example through your reverse proxy) so the device allows it. Open Homie from its https address on this device to turn it on.</p>
  {:else if !canBiometric && !auth.biometric}
    <p class="hint">This device has nothing Homie can use to check it is you: set up a fingerprint, a face or a screen lock in its system settings first.</p>
  {:else}
    <p class="hint">On this device only: Homie asks for the fingerprint, face or screen lock each time you come back to it after it was out of sight for the time below. Other devices are not affected.</p>
    <label>
      Lock
      <select value={after} onchange={(e) => setAfter(Number(e.currentTarget.value))}>
        {#each afters as [value, label] (value)}<option {value}>{label}</option>{/each}
      </select>
    </label>
    <div class="row">
      {#if auth.biometric}
        <button type="button" onclick={turnOff} disabled={busy}>Turn off on this device</button>
      {:else}
        <button type="button" class="primary" onclick={turnOn} disabled={busy}>{busy ? 'Waiting…' : 'Turn on for this device'}</button>
      {/if}
    </div>
    <small class={auth.biometric ? 'ok' : ''}>{auth.biometric ? 'The lock is on for this device.' : 'The lock is off for this device.'}</small>
    {#if lockError}<small class="bad" role="alert">{lockError}</small>{/if}
  {/if}
</section>

<section>
  <h2>Signed-in devices</h2>
  {#if loadError}
    <small class="bad">{loadError}</small>
  {:else if !loaded}
    <small>Loading…</small>
  {:else}
    <ul class="list">
      {#each devices as d (d.id)}
        <li>
          <span class="what">
            {d.label}
            {#if d.current}<small class="ok">this device</small>{/if}
            {#if d.lock}<small>biometric lock</small>{/if}
            <small>last used {when(d.lastUsed)}</small>
          </span>
          {#if !d.current}<button type="button" class="danger" onclick={() => signOutDevice(d)}>Sign out</button>{/if}
        </li>
      {/each}
    </ul>
    {#if devices.length > 1}
      <div class="row"><button type="button" onclick={signOutOthers}>Sign out the other devices</button></div>
    {/if}
    <p class="hint">A device stays signed in for 30 days after it was last used.</p>
  {/if}
</section>

{#if passkeys.length}
  <section>
    <h2>Passkeys</h2>
    <ul class="list">
      {#each passkeys as p (p.id)}
        <li>
          <span class="what">
            {p.name}
            <small>made {when(p.created)}{p.used ? ` · locks ${p.used} device${p.used === 1 ? '' : 's'}` : ''}</small>
          </span>
          <button type="button" class="danger" onclick={() => removePasskey(p)}>Remove</button>
        </li>
      {/each}
    </ul>
    <p class="hint">A passkey stays on the device that made it; Homie only keeps its public half. Passkeys work at the address where they were made.</p>
  </section>
{/if}

<style>
  .who {
    margin: 0;
  }
  .pw {
    display: grid;
    gap: 12px;
  }
  .list {
    display: grid;
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .list li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 8px 10px;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 6px;
  }
  .what {
    display: grid;
    gap: 1px;
    min-width: 0;
    font-weight: 600;
  }
  small {
    color: var(--muted);
    font-weight: 400;
  }
  small.ok {
    color: var(--up);
  }
  small.bad {
    color: var(--down);
  }
  button:disabled {
    opacity: 0.6;
    cursor: default;
  }
</style>
