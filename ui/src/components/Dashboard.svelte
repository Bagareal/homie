<script lang="ts">
  import { app } from '../lib/state.svelte'
  import type { Item } from '../lib/types'
  import Shelf from './Shelf.svelte'
  import ItemDialog from './ItemDialog.svelte'
  import GroupDialog from './GroupDialog.svelte'

  let itemEdit = $state<{ groupId: string; item: Item | null } | null>(null)
  let groupEdit = $state<{ id: string | null } | null>(null)
  let search: HTMLInputElement | undefined = $state()

  const cfg = $derived(app.config!)
  const q = $derived(app.query.trim().toLowerCase())
  const visible = $derived(
    cfg.groups
      .map((group) => ({
        group,
        items: q ? group.items.filter((it) => `${it.name} ${it.description} ${it.url}`.toLowerCase().includes(q)) : group.items,
      }))
      .filter((v) => !q || v.items.length > 0),
  )

  const summary = $derived.by(() => {
    let up = 0
    let down = 0
    for (const g of cfg.groups)
      for (const it of g.items) {
        const s = it.check ? app.status[it.id] : undefined
        if (s) s.up ? up++ : down++
      }
    if (up + down === 0) return ''
    const parts = [`${up} online`]
    if (down) parts.push(down === 1 ? '1 unreachable' : `${down} unreachable`)
    return parts.join(', ')
  })
  const anyDown = $derived(summary.includes('unreachable'))

  function onWindowKey(e: KeyboardEvent) {
    if (app.editing || itemEdit || groupEdit) return
    const t = e.target as HTMLElement
    if (e.key === '/' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName)) {
      e.preventDefault()
      search?.focus()
    }
  }
  function onSearchKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      app.query = ''
      search?.blur()
    } else if (e.key === 'Enter') {
      const first = visible[0]?.items[0]
      if (first) window.open(first.url, cfg.settings.openInNewTab ? '_blank' : '_self', 'noopener')
    }
  }
  function toggleEdit() {
    app.editing = !app.editing
    app.query = ''
  }
</script>

<svelte:window onkeydown={onWindowKey} />

<header class="bar">
  <h1>{cfg.title || 'Homie'}</h1>
  {#if !app.editing}
    <input
      bind:this={search}
      bind:value={app.query}
      onkeydown={onSearchKey}
      class="search"
      type="search"
      placeholder="Search (press /)"
      aria-label="Search services"
    />
  {/if}
  {#if summary}
    <p class="summary" class:alert={anyDown}>{summary}</p>
  {/if}
  <nav class="actions">
    <button type="button" class:primary={app.editing} onclick={toggleEdit}>
      {app.editing ? 'Done editing' : 'Edit'}
    </button>
    <a class="btn" href="#/settings">Settings</a>
  </nav>
</header>

{#if app.editing && cfg.groups.length > 0}
  <p class="edit-hint">Drag tiles and group labels to reorder them. Click an item to edit it.</p>
{/if}

<main class="shelves" class:editing={app.editing}>
  {#if cfg.groups.length === 0}
    <div class="empty">
      <p>The dashboard is empty. Create a group, then add your services to it.</p>
      <button
        type="button"
        class="primary"
        onclick={() => {
          app.editing = true
          groupEdit = { id: null }
        }}>Add group</button
      >
    </div>
  {:else if visible.length === 0}
    <p class="empty">No service matches “{app.query}”.</p>
  {:else}
    {#each visible as v (v.group.id)}
      <Shelf
        group={v.group}
        items={v.items}
        onEditItem={(item) => (itemEdit = { groupId: v.group.id, item })}
        onAddItem={() => (itemEdit = { groupId: v.group.id, item: null })}
        onEditGroup={() => (groupEdit = { id: v.group.id })}
      />
    {/each}
  {/if}
  {#if app.editing && cfg.groups.length > 0}
    <button type="button" class="add-group" onclick={() => (groupEdit = { id: null })}>Add group</button>
  {/if}
</main>

{#if itemEdit}
  <ItemDialog groupId={itemEdit.groupId} item={itemEdit.item} onclose={() => (itemEdit = null)} />
{/if}
{#if groupEdit}
  <GroupDialog groupId={groupEdit.id} onclose={() => (groupEdit = null)} />
{/if}
