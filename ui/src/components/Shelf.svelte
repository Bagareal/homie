<script lang="ts">
  import { app } from '../lib/state.svelte'
  import type { Group, Item } from '../lib/types'
  import Tile from './Tile.svelte'
  import EditIcon from './EditIcon.svelte'

  let {
    group,
    items,
    onEditItem,
    onAddItem,
    onEditGroup,
  }: {
    group: Group
    items: Item[]
    onEditItem: (item: Item) => void
    onAddItem: () => void
    onEditGroup: () => void
  } = $props()

  let groupDrop = $state<'before' | 'after' | null>(null)
  let gridOver = $state(false)

  function tapeDragStart(e: DragEvent) {
    app.drag = { kind: 'group', id: group.id }
    e.dataTransfer?.setData('text/plain', group.name)
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function dragEnd() {
    app.drag = null
    groupDrop = null
  }
  function leaving(e: DragEvent) {
    return !(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)
  }

  function shelfOver(e: DragEvent) {
    if (app.drag?.kind !== 'group' || app.drag.id === group.id) return
    e.preventDefault()
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    groupDrop = e.clientY < r.top + r.height / 2 ? 'before' : 'after'
  }
  function shelfDrop(e: DragEvent) {
    const d = app.drag
    if (d?.kind !== 'group') return
    e.preventDefault()
    const after = groupDrop === 'after'
    const valid = groupDrop !== null
    groupDrop = null
    app.drag = null
    if (valid) app.moveGroup(d.id, group.id, after)
  }

  // Dropping in the group's empty area: the item goes to the end.
  function gridDragOver(e: DragEvent) {
    if (app.drag?.kind !== 'item') return
    e.preventDefault()
    gridOver = true
  }
  function gridDrop(e: DragEvent) {
    const d = app.drag
    if (d?.kind !== 'item') return
    e.preventDefault()
    gridOver = false
    app.drag = null
    app.moveItem(d.groupId, d.id, group.id, null, false)
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<section
  class="shelf"
  class:drop-before={groupDrop === 'before'}
  class:drop-after={groupDrop === 'after'}
  class:dragging={app.drag?.kind === 'group' && app.drag.id === group.id}
  aria-label={group.name}
  ondragover={shelfOver}
  ondragleave={(e) => leaving(e) && (groupDrop = null)}
  ondrop={shelfDrop}
>
  {#if app.editing}
    <button
      type="button"
      class="tape"
      draggable="true"
      ondragstart={tapeDragStart}
      ondragend={dragEnd}
      onclick={onEditGroup}
      title="Drag to move the group, click to rename or delete it"
    >
      <span class="tape-text">{group.name}</span>
      <EditIcon />
    </button>
  {:else}
    <h2 class="tape"><span class="tape-text">{group.name}</span></h2>
  {/if}

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="grid"
    class:over={gridOver}
    ondragover={gridDragOver}
    ondragleave={(e) => leaving(e) && (gridOver = false)}
    ondrop={gridDrop}
  >
    {#each items as item (item.id)}
      <Tile {item} groupId={group.id} onedit={() => onEditItem(item)} />
    {/each}
    {#if app.editing}
      <button type="button" class="tile add" onclick={onAddItem}>Add service</button>
    {:else if items.length === 0}
      <p class="hint">No services in this group.</p>
    {/if}
  </div>
</section>
