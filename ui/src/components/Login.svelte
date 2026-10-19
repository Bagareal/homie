<script lang="ts">
  import { auth } from '../lib/auth.svelte'

  const setup = $derived(auth.state === 'setup')
  let username = $state('')
  let password = $state('')
  let again = $state('')
  let code = $state('')
  let busy = $state(false)
  let error = $state('')

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    if (setup) {
      if (password.length < 10) return void (error = 'The password needs at least 10 characters.')
      if (password !== again) return void (error = 'The two passwords are not the same.')
      if (auth.needCode && !code.trim()) return void (error = 'Enter the code printed by the reset command.')
    }
    busy = true
    try {
      if (setup) await auth.setup(username.trim(), password, code.trim())
      else await auth.login(username.trim(), password)
      password = again = ''
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }
</script>

<main class="login">
  <form onsubmit={submit} novalidate>
    <h1>Homie</h1>
    <p class="lead">
      {#if setup}
        {auth.needCode ? 'The account was reset. Create the new one to open Homie.' : 'Welcome. Create the account that opens Homie.'}
      {:else}
        Sign in to open the dashboard.
      {/if}
    </p>

    <label>
      User name
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={username} name="username" autocomplete="username" autocapitalize="none" spellcheck="false" maxlength="64" required autofocus />
    </label>
    <label>
      Password
      <input type="password" bind:value={password} name="password" autocomplete={setup ? 'new-password' : 'current-password'} maxlength="200" required />
    </label>
    {#if setup}
      <label>
        Repeat the password
        <input type="password" bind:value={again} name="password2" autocomplete="new-password" maxlength="200" required />
      </label>
      <small>At least 10 characters. A few random words are easy to remember and hard to guess.</small>
      {#if auth.needCode}
        <label>
          Code from the reset command
          <input bind:value={code} name="code" autocomplete="off" autocapitalize="characters" spellcheck="false" maxlength="32" />
        </label>
      {/if}
    {/if}

    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button type="submit" class="primary" disabled={busy}>{busy ? 'Please wait…' : setup ? 'Create the account' : 'Sign in'}</button>
    {#if !setup}
      <small class="forgot">Forgot the password? In the Homie container, run <code>homie-reset-password</code>.</small>
    {/if}
  </form>
</main>

<style>
  .login {
    min-height: 100dvh;
    display: grid;
    place-items: center;
    padding: 24px 16px;
    box-sizing: border-box;
  }
  form {
    width: min(380px, 100%);
    display: grid;
    gap: 14px;
    padding: 26px 24px 22px;
    background: color-mix(in srgb, var(--shelf) 92%, transparent);
    border: 1px solid var(--line);
    border-radius: 10px;
    box-shadow: 0 10px 30px rgb(0 0 0 / 0.25);
  }
  h1 {
    margin: 0;
    font-size: 1.5rem;
    letter-spacing: 0.04em;
    color: var(--accent);
  }
  .lead {
    margin: -6px 0 2px;
    color: var(--muted);
  }
  label {
    display: grid;
    gap: 5px;
    font-weight: 600;
  }
  input {
    font-size: 16px; /* no zoom when the field is focused on a phone */
  }
  small {
    color: var(--muted);
  }
  .error {
    margin: 0;
    color: var(--down);
  }
  button.primary {
    justify-content: center;
    padding: 9px 14px;
  }
  button:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .forgot {
    text-align: center;
  }
</style>
