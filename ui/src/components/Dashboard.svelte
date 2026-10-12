<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { app } from '../lib/state.svelte'
  import { barHeight } from '../lib/barHeight'
  import { mobile, setTab } from '../lib/mobile.svelte'
  import { clearDraft, loadDraft, type ItemFields } from '../lib/draft'
  import type { Item, Widget as WidgetT } from '../lib/types'
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

  // Groups and widgets in the order the user arranged them. In the list layout
  // neighbouring widgets share a row; in the grid they flow like the cards.
  type Block = { kind: 'group'; v: (typeof visible)[number] } | { kind: 'widget'; w: WidgetT } | { kind: 'strip'; list: WidgetT[] }
  const blocks = $derived.by(() => {
    const byId = new Map(visible.map((v) => [v.group.id, v]))
    const out: Block[] = []
    for (const id of app.blockOrder()) {
      const v = byId.get(id)
      if (v) {
        out.push({ kind: 'group', v })
        continue
      }
      const w = q ? undefined : cfg.widgets.find((x) => x.id === id)
      if (!w) continue
      const last = out[out.length - 1]
      if (cfg.settings.layout === 'grid') out.push({ kind: 'widget', w })
      else if (last?.kind === 'strip') last.list.push(w)
      else out.push({ kind: 'strip', list: [w] })
    }
    return out
  })
  // On a phone the widgets get their own tab so they don't push the services
  // down the page. A search always looks at services.
  const tabbed = $derived(mobile.narrow && cfg.widgets.length > 0)
  const tab = $derived(!tabbed ? 'all' : q ? 'services' : mobile.tab)
  const shown = $derived(tab === 'all' ? blocks : blocks.filter((b) => (b.kind === 'group') === (tab === 'services')))
  const blockKey = (b: Block) => (b.kind === 'group' ? `g${b.v.group.id}` : b.kind === 'widget' ? `w${b.w.id}` : `s${b.list[0].id}`)

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

  // In the grid layout the cards sit on fixed columns inside a block that is
  // centered: as many columns as fit, never more than MAX_COLS, so a wide screen
  // keeps a compact block and a narrower one drops to two columns, then one.
  const MAX_COLS = 3
  $effect(() => {
    void [cfg.settings.layout, cfg.groups.length, cfg.widgets.length, app.query, app.editing]
    const el = main
    const host = el?.parentElement
    if (!el || !host) return
    const fit = () => {
      if (cfg.settings.layout !== 'grid') return el.style.removeProperty('width')
      const cs = getComputedStyle(el)
      const gap = parseFloat(cs.columnGap) || 20
      const pad = parseFloat(cs.paddingLeft) + parseFloat(cs.paddingRight)
      const root = getComputedStyle(document.documentElement)
      const tile = parseFloat(root.getPropertyValue('--tile-min')) || 120
      const tileGap = parseFloat(root.getPropertyValue('--gap')) || 16
      const border = parseFloat(root.getPropertyValue('--group-border-w')) || 1
      // The size of a card: 3 tiles + padding (16 each side, as in app.css) + border.
      // Worked out, not measured, because a card on screen may already be squeezed.
      const card = 3 * tile + 2 * tileGap + 2 * 16 + 2 * border
      const fits = Math.max(1, Math.floor((host.clientWidth - pad + gap) / (card + gap)))
      const wanted = Math.max(1, visible.length + (cfg.widgets.length > 0 && !app.query ? 1 : 0))
      const cols = Math.min(fits, wanted, MAX_COLS)
      el.style.setProperty('--cols', String(cols))
      el.style.width = `${cols * card + (cols - 1) * gap + pad}px`
    }
    const ro = new ResizeObserver(fit)
    ro.observe(host)
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
    {#if app.editing}
      <button type="button" onclick={() => (groupEdit = { id: null })}>Add group</button>
      <button type="button" onclick={() => (widgetEdit = { id: null })}>Add widget</button>
    {/if}
    <button type="button" class:primary={app.editing} onclick={toggleEdit}>
      {app.editing ? 'Done editing' : 'Edit'}
    </button>
    <a class="btn" href="#/settings">Settings</a>
  </nav>
</header>

<div class="scroller">
{#if tabbed}
  <div class="tabs" role="tablist" aria-label="Dashboard view">
    <button type="button" role="tab" aria-selected={tab === 'services'} class:active={tab === 'services'} onclick={() => { app.query = ''; setTab('services') }}>Services</button>
    <button type="button" role="tab" aria-selected={tab === 'widgets'} class:active={tab === 'widgets'} onclick={() => setTab('widgets')}>Widgets</button>
  </div>
{/if}
{#if app.editing && (cfg.groups.length > 0 || cfg.widgets.length > 0)}
  <p class="edit-hint">Drag tiles, group labels and widgets to reorder them. Click an item to edit it.</p>
{/if}


<main class="shelves" class:editing={app.editing} bind:this={main}>
  {#if cfg.groups.length === 0 && cfg.widgets.length === 0}
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
  {:else if q && visible.length === 0}
    <p class="empty">No service matches “{app.query}”.</p>
  {:else if shown.length === 0}
    <p class="empty">{tab === 'widgets' ? 'No widgets yet.' : 'No groups yet.'}</p>
  {:else}
    {#each shown as b (blockKey(b))}
      {#if b.kind === 'group'}
        <Shelf
          group={b.v.group}
          items={b.v.items}
          onEditItem={(item) => (itemEdit = { groupId: b.v.group.id, item })}
          onAddItem={() => (itemEdit = { groupId: b.v.group.id, item: null })}
          onEditGroup={() => (groupEdit = { id: b.v.group.id })}
        />
      {:else if b.kind === 'widget'}
        <Widget widget={b.w} data={app.widgetData[b.w.id]} onedit={() => (widgetEdit = { id: b.w.id })} />
      {:else}
        <div class="widgets">
          {#each b.list as w (w.id)}
            <Widget widget={w} data={app.widgetData[w.id]} onedit={() => (widgetEdit = { id: w.id })} />
          {/each}
        </div>
      {/if}
    {/each}
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
