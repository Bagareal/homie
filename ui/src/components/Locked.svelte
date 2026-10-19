<script lang="ts">
  import { onMount } from 'svelte'
  import { auth } from '../lib/auth.svelte'

  let busy = $state(false)
  let error = $state('')

  async function unlock() {
    if (busy) return
    busy = true
    error = (await auth.unlock()) ?? ''
    busy = false
  }

  // Ask right away; some browsers want a tap first, then the button is there.
  onMount(() => {
    void unlock()
  })
</script>

<main class="locked">
  <div class="card">
    <svg viewBox="0 0 24 24" width="56" height="56" aria-hidden="true">
      <path
        d="M12 11c0 3.5-.5 6-2 8M8.5 20c1.5-2 2-5 2-9a3.5 3.5 0 0 1 7 0c0 1.5 0 3-.4 4.5M5 17c.7-1.5 1-3.5 1-6a6 6 0 0 1 12 0M3 12.5V11a9 9 0 0 1 18 0v1M15 21c.8-1.5 1.2-3 1.5-4.5"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
    </svg>
    <h1>Homie is locked</h1>
    <p>Confirm with your fingerprint, your face or the screen lock of this device.</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button type="button" class="primary" onclick={unlock} disabled={busy}>{busy ? 'Waiting…' : 'Unlock'}</button>
    <button type="button" class="link" onclick={() => auth.logout()}>Sign in with the password instead</button>
  </div>
</main>

<style>
  .locked {
    min-height: 100dvh;
    display: grid;
    place-items: center;
    padding: 24px 16px;
    box-sizing: border-box;
  }
  .card {
    width: min(360px, 100%);
    display: grid;
    justify-items: center;
    gap: 12px;
    padding: 28px 24px 22px;
    text-align: center;
    background: color-mix(in srgb, var(--shelf) 92%, transparent);
    border: 1px solid var(--line);
    border-radius: 10px;
    box-shadow: 0 10px 30px rgb(0 0 0 / 0.25);
    color: var(--text);
  }
  svg {
    color: var(--accent);
  }
  h1 {
    margin: 0;
    font-size: 1.3rem;
  }
  p {
    margin: 0;
    color: var(--muted);
  }
  .error {
    color: var(--down);
  }
  button.primary {
    width: 100%;
    justify-content: center;
    padding: 10px 14px;
    margin-top: 4px;
  }
  button:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .link {
    border: none;
    background: none;
    color: var(--muted);
    text-decoration: underline;
    padding: 4px;
  }
  .link:hover {
    background: none;
    color: var(--text);
  }
</style>
