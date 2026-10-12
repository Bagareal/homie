<script lang="ts">
  import { app } from '../lib/state.svelte'
  import { bytes, percent, rate } from '../lib/format'
  import type { Widget, WidgetData, WidgetMetric } from '../lib/types'
  import EditIcon from './EditIcon.svelte'

  let {
    widget,
    data,
    onedit,
    preview = false,
  }: {
    widget: Widget
    data: WidgetData | undefined
    onedit?: () => void
    // Rendered inside the editor: not draggable or clickable.
    preview?: boolean
  } = $props()

  let drop = $state<'before' | 'after' | null>(null)
  const editable = $derived(app.editing && !preview)

  const style = $derived(
    [
      `--w-width:${widget.width}px`,
      widget.height > 0 ? `--w-height:${widget.height}px` : '',
      `--w-bg:color-mix(in srgb, ${widget.color ?? 'var(--tile)'} ${widget.opacity}%, transparent)`,
      widget.textColor ? `--w-text:${widget.textColor}` : '',
      widget.borderColor ? `--w-border:${widget.borderColor}` : '',
      `--w-border-w:${widget.borderWidth ?? 1}px`,
      widget.barColor ? `--w-bar:${widget.barColor}` : '',
    ]
      .filter(Boolean)
      .join(';'),
  )

  const labels: Record<WidgetMetric, string> = {
    cpu: 'CPU',
    mem: 'Memory',
    swap: 'Swap',
    load: 'Load',
    uptime: 'Uptime',
    temp: 'Temperature',
    disks: 'Disks',
    network: 'Network',
  }

  type Row = { key: string; label: string; value: string; pct?: number; title?: string }

  // One row per value; the disks metric expands into one row per disk.
  const rows = $derived.by((): Row[] => {
    if (!data?.online) return []
    const d = data.data
    const out: Row[] = []
    for (const m of widget.metrics) {
      const missing = (): Row => ({ key: m, label: labels[m], value: 'n/a' })
      switch (m) {
        case 'cpu':
          out.push(d.cpu === undefined ? missing() : { key: m, label: labels[m], value: percent(d.cpu), pct: d.cpu })
          break
        case 'mem':
        case 'swap': {
          const u = d[m]
          if (!u) out.push(missing())
          else if (u.total === 0) out.push({ key: m, label: labels[m], value: 'none' })
          else out.push({ key: m, label: labels[m], value: `${bytes(u.used)} / ${bytes(u.total)}`, pct: u.percent })
          break
        }
        case 'load':
          out.push(
            d.load
              ? {
                  key: m,
                  label: labels[m],
                  value: [d.load.min1, d.load.min5, d.load.min15].map((n) => n.toFixed(2)).join(' · '),
                  title: `Load average over 1, 5 and 15 minutes (${d.load.cores} cores)`,
                }
              : missing(),
          )
          break
        case 'uptime':
          out.push(d.uptime ? { key: m, label: labels[m], value: d.uptime } : missing())
          break
        case 'temp':
          out.push(d.temp ? { key: m, label: labels[m], value: `${Math.round(d.temp.value)} °C`, title: d.temp.label } : missing())
          break
        case 'disks':
          if (!d.disks?.length) out.push(missing())
          else
            for (const disk of d.disks)
              out.push({ key: `disk:${disk.mount}`, label: disk.mount, value: `${bytes(disk.used)} / ${bytes(disk.total)}`, pct: disk.percent })
          break
        case 'network':
          out.push(d.network ? { key: m, label: labels[m], value: `↓ ${rate(d.network.rx)}   ↑ ${rate(d.network.tx)}` } : missing())
          break
      }
    }
    return out
  })

  function dragStart(e: DragEvent) {
    app.drag = { kind: 'widget', id: widget.id }
    e.dataTransfer?.setData('text/plain', widget.name)
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function dragEnd() {
    app.drag = null
    drop = null
  }
  function dragOver(e: DragEvent) {
    if (app.drag?.kind !== 'widget' || app.drag.id === widget.id) return
    e.preventDefault()
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    drop = e.clientX < r.left + r.width / 2 ? 'before' : 'after'
  }
  function dragLeave(e: DragEvent) {
    if (!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) drop = null
  }
  function onDrop(e: DragEvent) {
    const d = app.drag
    if (d?.kind !== 'widget') return
    e.preventDefault()
    const after = drop === 'after'
    drop = null
    app.drag = null
    app.moveWidget(d.id, widget.id, after)
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onedit?.()
    }
  }
</script>

{#snippet body()}
  <div class="head">
    <h3>{widget.name}</h3>
    {#if data}
      <span class="dot" class:online={data.online} title={data.online ? 'Online' : 'Unreachable'}></span>
    {/if}
  </div>
  {#if !data}
    <p class="note">Loading…</p>
  {:else if !data.online}
    <p class="note down">Unreachable: {data.error}</p>
  {:else}
    <dl>
      {#each rows as r (r.key)}
        <div class="row" title={r.title}>
          <dt>{r.label}</dt>
          <dd>{r.value}</dd>
          {#if r.pct !== undefined}
            <div class="meter" role="presentation"><span class:hot={r.pct >= 90} style:width="{Math.min(100, Math.max(0, r.pct))}%"></span></div>
          {/if}
        </div>
      {/each}
    </dl>
  {/if}
{/snippet}

{#if editable}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="widget editing"
    class:dragging={app.drag?.kind === 'widget' && app.drag.id === widget.id}
    class:drop-before={drop === 'before'}
    class:drop-after={drop === 'after'}
    role="button"
    tabindex="0"
    draggable="true"
    {style}
    title="Drag to move, click to edit"
    ondragstart={dragStart}
    ondragend={dragEnd}
    ondragover={dragOver}
    ondragleave={dragLeave}
    ondrop={onDrop}
    onclick={onedit}
    onkeydown={onKey}
  >
    {@render body()}
    <EditIcon />
  </div>
{:else}
  <section class="widget" class:preview {style} aria-label={widget.name}>
    {@render body()}
  </section>
{/if}

<style>
  .widget {
    position: relative;
    flex: none;
    box-sizing: border-box;
    width: min(var(--w-width), 100%);
    height: var(--w-height, auto);
    overflow: auto;
    scrollbar-width: thin;
    padding: 12px 14px;
    background: var(--w-bg);
    border: var(--w-border-w) solid var(--w-border, var(--tile-border, var(--line)));
    border-radius: 10px;
    color: var(--w-text, var(--tile-text, var(--text)));
    text-align: left;
  }
  .widget.editing {
    cursor: grab;
  }
  .widget.dragging {
    opacity: 0.4;
  }
  .widget.drop-before {
    box-shadow: -4px 0 0 var(--accent);
  }
  .widget.drop-after {
    box-shadow: 4px 0 0 var(--accent);
  }
  .widget :global(.edit-icon) {
    position: absolute;
    right: 8px;
    bottom: 8px;
    opacity: 0.6;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
  }
  h3 {
    margin: 0;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.85rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--down);
  }
  .dot.online {
    background: var(--up);
  }
  dl {
    margin: 0;
    display: grid;
    gap: 8px;
  }
  .row {
    display: grid;
    grid-template-columns: auto 1fr;
    column-gap: 12px;
    row-gap: 3px;
    align-items: baseline;
  }
  dt {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.85rem;
    opacity: 0.7;
  }
  dd {
    margin: 0;
    min-width: 0;
    text-align: right;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .meter {
    grid-column: 1 / -1;
    height: 5px;
    border-radius: 3px;
    background: color-mix(in srgb, currentColor 15%, transparent);
    overflow: hidden;
  }
  .meter span {
    display: block;
    height: 100%;
    border-radius: 3px;
    background: var(--w-bar, var(--accent));
    transition: width 0.4s ease;
  }
  .meter span.hot {
    background: var(--down);
  }
  .note {
    margin: 0;
    opacity: 0.7;
  }
  .note.down {
    color: var(--down);
    opacity: 1;
  }
  @media (prefers-reduced-motion: reduce) {
    .meter span {
      transition: none;
    }
  }
</style>
