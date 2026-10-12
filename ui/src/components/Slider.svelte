<script lang="ts">
  import { tick } from 'svelte'

  let {
    label,
    value,
    min = 1,
    max = 100,
    unit = '%',
    compact = false,
    oninput,
    oncommit,
  }: {
    label: string
    value: number
    min?: number
    max?: number
    unit?: string
    // Hides the visible label (it stays as the accessible name), for use inline.
    compact?: boolean
    // Called continuously while dragging (live preview) ...
    oninput: (v: number) => void
    // ... and once when the value is final (release, or Enter after typing).
    oncommit: (v: number) => void
  } = $props()

  let live = $state(0)
  $effect(() => {
    live = value
  })

  let editing = $state(false)
  let draft = $state('')
  let box: HTMLInputElement | undefined = $state()

  async function startEdit() {
    draft = String(live)
    editing = true
    await tick()
    box?.select()
  }

  function finishEdit(apply: boolean) {
    if (!editing) return
    editing = false
    const n = Math.round(Number(draft))
    if (!apply || draft.trim() === '' || Number.isNaN(n)) return
    live = Math.min(max, Math.max(min, n))
    oninput(live)
    oncommit(live)
  }

  const uid = $derived(`slider-${label.replace(/\W+/g, '-')}`)
</script>

<div class="slider" class:compact>
  <span class="name" class:sr-only={compact} id={uid}>{label}</span>
  <input
    type="range"
    {min}
    {max}
    value={live}
    aria-labelledby={uid}
    oninput={(e) => {
      live = e.currentTarget.valueAsNumber
      oninput(live)
    }}
    onchange={() => oncommit(live)}
  />
  {#if editing}
    <input
      bind:this={box}
      bind:value={draft}
      class="number"
      type="number"
      {min}
      {max}
      aria-label="{label} ({unit})"
      onkeydown={(e) => {
        if (e.key === 'Enter') finishEdit(true)
        else if (e.key === 'Escape') finishEdit(false)
      }}
      onblur={() => finishEdit(true)}
    />
  {:else}
    <span
      class="value"
      role="button"
      tabindex="0"
      title="Double-click to type a value"
      ondblclick={startEdit}
      onkeydown={(e) => (e.key === 'Enter' || e.key === 'F2') && startEdit()}>{live}{unit}</span
    >
  {/if}
</div>

<style>
  .slider {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .name {
    font-weight: 600;
    min-width: 9.5rem;
  }
  .compact input[type='range'] {
    flex: none;
    width: 90px;
    min-width: 0;
  }
  .compact .name {
    min-width: 0;
  }
  .compact {
    flex-wrap: nowrap;
    gap: 6px;
  }
  input[type='range'] {
    flex: 1;
    min-width: 120px;
    max-width: 260px;
    padding: 0;
  }
  .value {
    min-width: 4.5ch;
    padding: 3px 6px;
    border-radius: 4px;
    color: var(--muted);
    text-align: right;
    cursor: text;
    user-select: none;
  }
  .value:hover {
    background: var(--tile);
  }
  .number {
    width: 6.5ch;
    padding: 3px 6px;
    text-align: right;
  }
</style>
