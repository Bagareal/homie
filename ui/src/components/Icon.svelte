<script lang="ts">
  import { iconSrc, initials } from '../lib/icons'

  let { icon, name, inset = false }: { icon: string; name: string; inset?: boolean } = $props()
  let failed = $state(false)
  const src = $derived(iconSrc(icon))
  const glyph = $derived(!src && icon.trim() ? [...icon.trim()].slice(0, 2).join('') : '')

  $effect(() => {
    void src
    failed = false
  })
</script>

<span class="icon" class:inset aria-hidden="true">
  {#if src && !failed}
    <img {src} alt="" loading="lazy" onerror={() => (failed = true)} />
  {:else if glyph}
    <span class="glyph">{glyph}</span>
  {:else}
    <span class="initials">{initials(name)}</span>
  {/if}
</span>
