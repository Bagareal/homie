<script lang="ts">
  import { tick } from 'svelte'

  let {
    label,
    value,
    cssVar,
    theme,
    onpreview,
    oncommit,
  }: {
    label: string
    // The custom color, or undefined when the theme's own default is in use.
    value: string | undefined
    // The CSS variable that holds the theme default, read to show its hex.
    cssVar: string
    // Only here so the default is re-read when the theme changes.
    theme: string
    onpreview: (hex: string | undefined) => void
    oncommit: () => void
  } = $props()

  const HEX = /^#[0-9a-f]{6}$/

  function normalize(raw: string): string | null {
    let v = raw.trim().toLowerCase()
    if (!v.startsWith('#')) v = `#${v}`
    return HEX.test(v) ? v : null
  }

  let fallback = $state('#000000')
  $effect(() => {
    void value
    void theme
    void cssVar
    tick().then(() => {
      const v = normalize(getComputedStyle(document.documentElement).getPropertyValue(cssVar))
      if (v) fallback = v
    })
  })

  const shown = $derived(value ?? fallback)
  let text = $state('')
  let invalid = $state(false)
  $effect(() => {
    text = shown
    invalid = false
  })

  const uid = $derived(`color-${label.replace(/\W+/g, '-')}`)
</script>

<div class="color">
  <span class="name" id={uid}>{label}</span>
  <input
    type="color"
    value={shown}
    aria-labelledby={uid}
    oninput={(e) => onpreview(e.currentTarget.value)}
    onchange={oncommit}
  />
  <input
    class="hex"
    class:invalid
    type="text"
    spellcheck="false"
    autocomplete="off"
    maxlength="7"
    aria-label="{label} (hex code)"
    bind:value={text}
    oninput={() => {
      const v = normalize(text)
      invalid = v === null
      if (v) onpreview(v)
    }}
    onchange={() => {
      if (normalize(text)) oncommit()
      else {
        text = shown
        invalid = false
      }
    }}
  />
  {#if value === undefined}
    <span class="tag">default</span>
  {:else}
    <button
      type="button"
      onclick={() => {
        onpreview(undefined)
        oncommit()
      }}>Reset</button
    >
  {/if}
</div>

<style>
  .color {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .name {
    font-weight: 600;
    min-width: 9.5rem;
  }
  input[type='color'] {
    width: 40px;
    height: 30px;
    padding: 2px;
  }
  .hex {
    width: 9ch;
    font-family: ui-monospace, 'Cascadia Mono', Consolas, monospace;
    font-size: 0.9rem;
    padding: 5px 8px;
  }
  .hex.invalid {
    border-color: var(--down);
  }
  .tag {
    color: var(--muted);
    font-size: 0.85rem;
  }
</style>
