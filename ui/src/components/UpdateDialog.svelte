<script lang="ts">
  import { onMount } from 'svelte'
  import { parseNotes } from '../lib/notes'
  import { closeUpdateDialog, updates } from '../lib/updates.svelte'

  // The dialog shows one of two things: a version that is out (with what it brings
  // and how to update), or what changed in the version that was just installed.
  let dialog: HTMLDialogElement
  const d = $derived(updates.dialog!)
  const MAX = 8 // releases shown; the rest are on GitHub
  const shown = $derived(d.releases.slice(0, MAX))
  const top = $derived(d.releases[0])
  let copied = $state(false)

  const date = (iso: string) => {
    const t = Date.parse(iso)
    return Number.isNaN(t) ? '' : new Date(t).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText('update')
      copied = true
      setTimeout(() => (copied = false), 2000)
    } catch {
      // No clipboard (plain http): the command is shown, it can be typed.
    }
  }

  onMount(() => {
    dialog.showModal()
    // The first thing that takes focus should be the button that closes it, not a link.
    dialog.querySelector<HTMLElement>('.primary')?.focus()
  })
</script>

<dialog
  bind:this={dialog}
  class="dialog updates"
  aria-labelledby="update-title"
  oncancel={(e) => {
    e.preventDefault()
    closeUpdateDialog()
  }}
>
  <div class="head">
    {#if d.kind === 'new'}
      <h2 id="update-title">Homie {top.version} is available</h2>
      <p>You are running {updates.current}.</p>
    {:else}
      <h2 id="update-title">Homie was updated to {updates.current}</h2>
      <p>This is what changed since {d.from}.</p>
    {/if}
  </div>

  <div class="notes">
    {#each shown as r (r.version)}
      <section>
        <h3>
          <a href={r.url} target="_blank" rel="noopener noreferrer">{r.version}</a>
          {#if date(r.date)}<span>{date(r.date)}</span>{/if}
        </h3>
        {#each parseNotes(r.notes) as s, i (i)}
          <div class="change">
            {#if s.title}<h4>{s.title}</h4>{/if}
            {#each s.blocks as b, j (j)}
              {#if b.kind === 'p'}
                <p>{b.text}</p>
              {:else}
                <ul>
                  {#each b.items as item, k (k)}<li>{item}</li>{/each}
                </ul>
              {/if}
            {/each}
          </div>
        {:else}
          <p class="empty">No description for this version.</p>
        {/each}
      </section>
    {/each}
    {#if d.releases.length > MAX}
      <p class="more">…and {d.releases.length - MAX} older {d.releases.length - MAX === 1 ? 'version' : 'versions'}: see them on GitHub.</p>
    {/if}
  </div>

  {#if d.kind === 'new'}
    <div class="how">
      <span>To update, run this in the Homie container:</span>
      <code>update</code>
      <button type="button" onclick={copy}>{copied ? 'Copied' : 'Copy'}</button>
    </div>
  {/if}

  <div class="dialog-actions">
    <a class="btn" href={top.url} target="_blank" rel="noopener noreferrer">View on GitHub</a>
    <button type="button" class="primary" onclick={closeUpdateDialog}>Close</button>
  </div>
</dialog>

<style>
  .updates {
    width: min(640px, calc(100vw - 32px));
    max-height: calc(100vh - 48px);
    overflow: hidden;
  }
  .updates[open] {
    display: flex;
    flex-direction: column;
  }
  .head {
    padding: 20px 20px 4px;
  }
  .head p {
    margin: 4px 0 0;
    color: var(--muted);
  }
  .notes {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 8px 20px;
    scrollbar-width: thin;
  }
  section + section {
    margin-top: 18px;
    padding-top: 14px;
    border-top: 1px solid var(--line);
  }
  h3 {
    display: flex;
    align-items: baseline;
    gap: 10px;
    margin: 6px 0 8px;
    font-size: 1rem;
  }
  h3 a {
    color: var(--accent);
    text-decoration: none;
  }
  h3 a:hover {
    text-decoration: underline;
  }
  h3 span {
    font-weight: 400;
    font-size: 0.85rem;
    color: var(--muted);
  }
  .change + .change {
    margin-top: 10px;
  }
  h4 {
    margin: 0 0 3px;
    font-size: 0.95rem;
  }
  p {
    margin: 0 0 6px;
    line-height: 1.45;
  }
  ul {
    margin: 0 0 6px;
    padding-left: 20px;
  }
  .empty,
  .more {
    color: var(--muted);
  }
  .how {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    margin: 4px 20px 0;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: 8px;
  }
  .how code {
    padding: 2px 8px;
    border-radius: 5px;
    background: color-mix(in srgb, currentColor 12%, transparent);
  }
  .dialog-actions {
    padding: 14px 20px 20px;
  }
  .btn {
    display: inline-flex;
    align-items: center;
    padding: 0 14px;
  }
</style>
