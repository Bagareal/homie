<script lang="ts">
  import { onMount } from 'svelte'
  import { loadIconIndex, searchIcons, type IconEntry } from '../lib/iconIndex'
  import { listOwnIcons, removeOwnIcon, uploadOwnIcon } from '../lib/api'

  let { current, onpick, onclose }: { current: string; onpick: (value: string) => void; onclose: () => void } = $props()

  const PAGE = 120

  let dialog: HTMLDialogElement
  let all = $state<IconEntry[]>([])
  let status = $state<'loading' | 'ready' | 'error'>('loading')
  let query = $state('')
  let shown = $state(PAGE)

  const results = $derived(searchIcons(all, query))

  // The owner's own icons (uploaded PNG / SVG files).
  let mine = $state<string[]>([])
  let fileInput: HTMLInputElement
  let busy = $state(false)
  let uploadError = $state('')
  const q = $derived(query.trim().toLowerCase())
  const myShown = $derived(q ? mine.filter((n) => n.toLowerCase().includes(q)) : mine)

  async function upload() {
    const file = fileInput.files?.[0]
    fileInput.value = ''
    if (!file) return
    busy = true
    uploadError = ''
    try {
      onpick((await uploadOwnIcon(file)).value)
    } catch (e) {
      uploadError = e instanceof Error ? e.message : 'could not upload the icon'
    } finally {
      busy = false
    }
  }

  async function removeMine(name: string) {
    if (!confirm(`Delete “${name}”? Services using it will show their initials.`)) return
    try {
      await removeOwnIcon(name)
      mine = mine.filter((n) => n !== name)
    } catch (e) {
      uploadError = e instanceof Error ? e.message : 'could not delete the icon'
    }
  }

  function load() {
    status = 'loading'
    loadIconIndex().then(
      (list) => {
        all = list
        status = 'ready'
      },
      () => (status = 'error'),
    )
  }

  onMount(() => {
    dialog.showModal()
    load()
    listOwnIcons().then((l) => (mine = l), () => {})
  })

  $effect(() => {
    void query
    shown = PAGE
  })
</script>

<dialog bind:this={dialog} class="dialog picker" {onclose} onclick={(e) => e.target === dialog && dialog.close()}>
  <div class="picker-body">
    <h2>Search icons</h2>
    <!-- svelte-ignore a11y_autofocus -->
    <input type="search" bind:value={query} placeholder="Search by name, e.g. proxmox, jellyfin, nas…" aria-label="Search icons" autofocus />

    <div class="mine">
      <div class="mine-head">
        <strong>My icons</strong>
        <button type="button" disabled={busy} onclick={() => fileInput.click()}>{busy ? 'Uploading…' : 'Upload PNG or SVG'}</button>
        <input bind:this={fileInput} type="file" accept=".png,.svg,image/png,image/svg+xml" hidden onchange={upload} />
      </div>
      {#if uploadError}<p class="form-error" role="alert">{uploadError}</p>{/if}
      {#if myShown.length > 0}
        <div class="icons own">
          {#each myShown as n (n)}
            <div class="own-cell">
              <button type="button" class="pick" class:on={current === `/icons/${n}`} title={n} onclick={() => onpick(`/icons/${n}`)}>
                <img src={`icons/${encodeURIComponent(n)}`} alt="" />
                <span class="label">{n}</span>
              </button>
              <button type="button" class="del" title="Delete {n}" aria-label="Delete {n}" onclick={() => removeMine(n)}>
                <svg viewBox="0 0 10 10" width="10" height="10" aria-hidden="true"><path d="M1.5 1.5l7 7m0-7l-7 7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" fill="none" /></svg>
              </button>
            </div>
          {/each}
        </div>
      {:else if mine.length === 0}
        <p class="note">Nothing yet. Upload your own icon (up to 1 MB) and it is kept here.</p>
      {/if}
    </div>

    {#if status === 'loading'}
      <p class="note">Loading the icon list…</p>
    {:else if status === 'error'}
      <p class="note">
        Could not load the icon list. Check that Homie can reach cdn.jsdelivr.net (it downloads the icons itself).
        <button type="button" onclick={load}>Try again</button>
      </p>
    {:else if results.length === 0}
      <p class="note">No icon matches “{query}”.</p>
    {:else}
      <p class="note">{results.length === all.length ? `${all.length} icons` : `${results.length} of ${all.length} icons`}</p>
      <div class="results">
        <div class="icons">
          {#each results.slice(0, shown) as e (e.name)}
            <button type="button" class="pick" class:on={e.value === current} title="{e.name} ({e.source})" onclick={() => onpick(e.value)}>
              <img src={e.src} alt="" loading="lazy" />
              <span class="label">{e.name}</span>
              {#if e.variant}<span class="badge">{e.variant}</span>{/if}
            </button>
          {/each}
        </div>
        {#if results.length > shown}
          <button type="button" class="more" onclick={() => (shown += PAGE)}>Show more</button>
        {/if}
      </div>
    {/if}

    <div class="dialog-actions">
      <button type="button" onclick={() => dialog.close()}>Close</button>
    </div>
  </div>
</dialog>

<style>
  .picker {
    width: min(760px, calc(100vw - 32px));
  }
  .picker-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 20px;
    height: min(78vh, 720px);
  }
  h2 {
    margin: 0;
    font-size: 1.3rem;
  }
  .note {
    margin: 0;
    color: var(--muted);
  }
  .results {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
  .icons {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
    gap: 8px;
  }
  .pick {
    position: relative;
    flex-direction: column;
    justify-content: flex-start;
    gap: 6px;
    padding: 10px 6px 8px;
    min-width: 0;
  }
  .pick.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }
  .pick img {
    width: 44px;
    height: 44px;
    object-fit: contain;
  }
  .label {
    max-width: 100%;
    font-size: 0.78rem;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .badge {
    position: absolute;
    top: 4px;
    right: 4px;
    padding: 0 5px;
    border-radius: 3px;
    background: var(--line);
    color: var(--muted);
    font-size: 0.65rem;
    line-height: 1.5;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .mine {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 30%;
    overflow-y: auto;
  }
  .mine-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .own-cell {
    position: relative;
    display: flex;
  }
  .own-cell .pick {
    flex: 1;
  }
  .del {
    position: absolute;
    top: 2px;
    right: 2px;
    width: 20px;
    height: 20px;
    padding: 0;
    justify-content: center;
    border-radius: 4px;
  }
  .more {
    margin: 12px auto 0;
  }
</style>
