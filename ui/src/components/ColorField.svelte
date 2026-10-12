<script lang="ts">
  import { tick } from 'svelte'
  import Slider from './Slider.svelte'

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

  // #rrggbb, or #rrggbbaa when the color is not fully opaque.
  const HEX = /^#[0-9a-f]{6}([0-9a-f]{2})?$/

  function normalize(raw: string): string | null {
    let v = raw.trim().toLowerCase()
    if (!v.startsWith('#')) v = `#${v}`
    if (!HEX.test(v)) return null
    return v.length === 9 && v.endsWith('ff') ? v.slice(0, 7) : v
  }

  const alphaOf = (hex: string) => (hex.length === 9 ? Math.round((parseInt(hex.slice(7), 16) / 255) * 100) : 100)

  function compose(rgb: string, pct: number): string {
    if (pct >= 100) return rgb
    return rgb + Math.round((pct / 100) * 255).toString(16).padStart(2, '0')
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
  const rgb = $derived(shown.slice(0, 7))
  const alpha = $derived(alphaOf(shown))
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
    value={rgb}
    aria-labelledby={uid}
    oninput={(e) => onpreview(compose(e.currentTarget.value, alpha))}
    onchange={oncommit}
  />
  <input
    class="hex"
    class:invalid
    type="text"
    spellcheck="false"
    autocomplete="off"
    maxlength="9"
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
  <Slider
    compact
    label="{label} alpha"
    min={0}
    value={alpha}
    oninput={(v) => onpreview(compose(rgb, v))}
    {oncommit}
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
    width: 11ch;
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
