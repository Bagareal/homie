<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { app } from '../lib/state.svelte'
  import { barHeight } from '../lib/barHeight'
  import { clearDraft, loadDraft, type ItemFields } from '../lib/draft'
  import type { Item, Placement } from '../lib/types'
  import Shelf from './Shelf.svelte'
  import Widget from './Widget.svelte'
  import WidgetDialog from './WidgetDialog.svelte'
  import ItemDialog from './ItemDialog.svelte'
  import GroupDialog from './GroupDialog.svelte'

  let itemEdit = $state<{ groupId: string; item: Item | null; draft?: ItemFields } | null>(null)
  let groupEdit = $state<{ id: string | null; draft?: string } | null>(null)
  let widgetEdit = $state<{ id: string | null } | null>(null)
  let search: HTMLInputElement | undefined = $state()
  let main: HTMLElement | undefined = $state()

  // A draft left behind by a refresh or a closed tab: offer to pick it back up.
  onMount(async () => {
    const d = loadDraft()
    if (!d) return
    const group = d.groupId ? app.group(d.groupId) : undefined
    const item = d.kind === 'item' && d.itemId ? group?.items.find((x) => x.id === d.itemId) : undefined
    const stale = d.kind === 'item' ? !group || (d.itemId !== null && !item) : d.groupId !== null && !group
    if (stale) return clearDraft()

    const label = d.kind === 'item' ? (d.fields.name || 'a service') : d.name || 'a group'
    const choice = await app.choose({
      title: 'Unsaved changes found',
      message: `Your edits to “${label}” were not saved when the page was closed. Restore them?`,
      buttons: [
        { label: 'Discard', value: 'discard', kind: 'danger' },
        { label: 'Restore', value: 'restore', kind: 'primary', focus: true },
      ],
    })
    if (choice === 'discard') clearDraft()
    else if (choice === 'restore') {
      if (d.kind === 'item') itemEdit = { groupId: d.groupId, item: item ?? null, draft: d.fields }
      else groupEdit = { id: d.groupId, draft: d.name }
    }
  })

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

  // In the grid layout the widgets take the width of the cards' rows (as many
  // cards as fit side by side), so they start at the cards' left edge instead
  // of floating in the middle of the page.
  $effect(() => {
    void [cfg.settings.layout, cfg.groups.length, cfg.widgets.length, app.query, app.editing]
    const el = main
    if (!el) return
    const fit = () => {
      const strips = el.querySelectorAll<HTMLElement>('.widgets')
      const shelf = el.querySelector<HTMLElement>('.shelf')
      if (cfg.settings.layout !== 'grid' || !shelf) return strips.forEach((s) => s.style.removeProperty('width'))
      const cs = getComputedStyle(el)
      const gap = parseFloat(cs.columnGap) || 20
      const avail = el.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight)
      const cols = Math.max(1, Math.min(visible.length, Math.floor((avail + gap) / (shelf.offsetWidth + gap))))
      strips.forEach((s) => (s.style.width = `${cols * shelf.offsetWidth + (cols - 1) * gap}px`))
    }
    const ro = new ResizeObserver(fit)
    ro.observe(el)
    tick().then(fit)
    return () => ro.disconnect()
  })

  function onWindowKey(e: KeyboardEvent) {
    if (app.editing || itemEdit || groupEdit || widgetEdit) return
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

<header class="bar" use:barHeight>
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

<div class="scroller">
{#if app.editing && (cfg.groups.length > 0 || cfg.widgets.length > 0)}
  <p class="edit-hint">Drag tiles, group labels and widgets to reorder them. Click an item to edit it.</p>
{/if}

{#snippet widgets(placement: Placement)}
  {@const list = cfg.widgets.filter((w) => w.placement === placement)}
  {#if list.length > 0 && !q}
    <div class="widgets">
      {#each list as w (w.id)}
        <Widget widget={w} data={app.widgetData[w.id]} onedit={() => (widgetEdit = { id: w.id })} />
      {/each}
    </div>
  {/if}
{/snippet}

<main class="shelves" class:editing={app.editing} bind:this={main}>
  {@render widgets('top')}
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
  {@render widgets('bottom')}
  {#if app.editing}
    <div class="add-row">
      {#if cfg.groups.length > 0}
        <button type="button" class="add-group" onclick={() => (groupEdit = { id: null })}>Add group</button>
      {/if}
      <button type="button" class="add-group" onclick={() => (widgetEdit = { id: null })}>Add widget</button>
    </div>
  {/if}
</main>
</div>

{#if itemEdit}
  <ItemDialog groupId={itemEdit.groupId} item={itemEdit.item} draft={itemEdit.draft} onclose={() => (itemEdit = null)} />
{/if}
{#if widgetEdit}
  {#key widgetEdit}
    <WidgetDialog widgetId={widgetEdit.id} onclose={() => (widgetEdit = null)} />
  {/key}
{/if}
{#if groupEdit}
  <GroupDialog groupId={groupEdit.id} draft={groupEdit.draft} onclose={() => (groupEdit = null)} />
{/if}
