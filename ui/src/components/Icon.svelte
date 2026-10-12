<script lang="ts">
  import { iconSources, initials } from '../lib/icons'

  let { icon, name }: { icon: string; name: string } = $props()
  // Index of the address being tried; moves on when one fails to load.
  let attempt = $state(0)
  const sources = $derived(iconSources(icon))
  const src = $derived(sources[attempt])
  const glyph = $derived(sources.length === 0 && icon.trim() ? [...icon.trim()].slice(0, 2).join('') : '')

  $effect(() => {
    void icon
    attempt = 0
  })
</script>

<span class="icon" aria-hidden="true">
  {#if src}
    <img {src} alt="" loading="lazy" onerror={() => attempt++} />
  {:else if glyph}
    <span class="glyph">{glyph}</span>
  {:else}
    <span class="initials">{initials(name)}</span>
  {/if}
</span>
