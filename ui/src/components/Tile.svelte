<script lang="ts">
  import { app } from '../lib/state.svelte'
  import type { Item } from '../lib/types'
  import Icon from './Icon.svelte'
  import EditIcon from './EditIcon.svelte'

  let { item, groupId, onedit }: { item: Item; groupId: string; onedit: () => void } = $props()

  let drop = $state<'before' | 'after' | null>(null)
  const st = $derived(item.check ? app.status[item.id] : undefined)
  const led = $derived(!item.check ? 'off' : !st ? 'unknown' : st.up ? 'up' : 'down')
  const checkedAgo = $derived.by(() => {
    if (!st) return ''
    const secs = Math.max(0, Math.round((Date.now() - new Date(st.checkedAt).getTime()) / 1000))
    if (secs < 90) return ' (just now)'
    const mins = Math.round(secs / 60)
    return ` (${mins} min ago)`
  })
  const ledLabel = $derived.by(() => {
    if (led === 'off') return 'Status check disabled'
    if (led === 'unknown') return 'Waiting for the first check'
    if (led === 'up') return `Online, responded in ${st!.latencyMs} ms${checkedAgo}`
    return `Unreachable: ${st!.error ?? `HTTP ${st!.code}`}${checkedAgo}`
  })
  const host = $derived.by(() => {
    try {
      return new URL(item.url).host
    } catch {
      return item.url
    }
  })
  const newTab = $derived(app.config?.settings.openInNewTab ?? true)
  const linkTitle = $derived(item.description || host)

  function dragStart(e: DragEvent) {
    app.drag = { kind: 'item', id: item.id, groupId }
    e.dataTransfer?.setData('text/plain', item.name)
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function dragEnd() {
    app.drag = null
    drop = null
  }
  function dragOver(e: DragEvent) {
    if (app.drag?.kind !== 'item' || app.drag.id === item.id) return
    e.preventDefault()
    e.stopPropagation()
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    drop = e.clientX < r.left + r.width / 2 ? 'before' : 'after'
  }
  function dragLeave(e: DragEvent) {
    const el = e.currentTarget as HTMLElement
    if (!el.contains(e.relatedTarget as Node)) drop = null
  }
  function onDrop(e: DragEvent) {
    const d = app.drag
    if (d?.kind !== 'item') return
    e.preventDefault()
    e.stopPropagation()
    const after = drop === 'after'
    drop = null
    app.drag = null
    app.moveItem(d.groupId, d.id, groupId, item.id, after)
  }
</script>

{#snippet body()}
  <Icon icon={item.icon} name={item.name} inset={item.iconInset} />
  <span class="text">
    <span class="name">{item.name}</span>
  </span>
  {#if led !== 'off'}
    <span class="led {led}" title={ledLabel}></span>
    <span class="sr-only">{ledLabel}</span>
  {/if}
{/snippet}

{#if app.editing}
  <button
    type="button"
    class="tile editing"
    class:dragging={app.drag?.kind === 'item' && app.drag.id === item.id}
    class:drop-before={drop === 'before'}
    class:drop-after={drop === 'after'}
    draggable="true"
    ondragstart={dragStart}
    ondragend={dragEnd}
    ondragover={dragOver}
    ondragleave={dragLeave}
    ondrop={onDrop}
    onclick={onedit}
    title="Drag to move, click to edit"
  >
    {@render body()}
    <EditIcon />
  </button>
{:else}
  <a class="tile" href={item.url} target={newTab ? '_blank' : undefined} rel="noopener noreferrer" title={linkTitle}>
    {@render body()}
  </a>
{/if}
