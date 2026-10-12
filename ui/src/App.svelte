<script lang="ts">
  import { onMount } from 'svelte'
  import { app } from './lib/state.svelte'
  import Dashboard from './components/Dashboard.svelte'
  import Settings from './components/Settings.svelte'
  import Toast from './components/Toast.svelte'
  import ConfirmDialog from './components/ConfirmDialog.svelte'

  const routeFromHash = () => (location.hash === '#/settings' ? 'settings' : 'dashboard')
  let route = $state(routeFromHash())
  let loadError = $state('')
  let bgLayer: HTMLDivElement | undefined = $state()

  onMount(() => {
    const onHash = () => (route = routeFromHash())
    window.addEventListener('hashchange', onHash)
    app.load().catch((e: Error) => (loadError = e.message))
    return () => window.removeEventListener('hashchange', onHash)
  })

  // Theme, density, title, and custom colors follow the settings.
  $effect(() => {
    const cfg = app.config
    if (!cfg) return
    const root = document.documentElement
    if (cfg.settings.theme === 'auto') delete root.dataset.theme
    else root.dataset.theme = cfg.settings.theme
    root.dataset.density = cfg.settings.density
    root.dataset.layout = cfg.settings.layout ?? 'list'
    root.style.setProperty('--bar-opacity', String(cfg.settings.barOpacity ?? 100))
    root.style.setProperty('--card-opacity', String(cfg.settings.cardOpacity ?? 40))
    document.title = cfg.title || 'Homie'

    const vars: [string, string | undefined][] = [
      ['--bg', cfg.settings.backgroundColor],
      ['--tile', cfg.settings.tileColor],
      ['--accent', cfg.settings.accentColor],
      ['--card-color', cfg.settings.cardColor],
    ]
    for (const [name, value] of vars) {
      if (value) root.style.setProperty(name, value)
      else root.style.removeProperty(name)
    }

    // The image lives on its own layer (not body's background) so its
    // opacity can be lowered without fading the page content above it,
    // leaving the solid background color (body's own background) as the
    // full-opacity base underneath. Set directly on the element's style
    // rather than through a --bg-image custom property: a relative url()
    // inside a custom property can resolve against the wrong base (the
    // stylesheet consuming it, not the document) depending on the
    // browser, which silently 404s the image.
    if (bgLayer) {
      bgLayer.style.backgroundImage = cfg.settings.backgroundImage ? `url("backgrounds/${cfg.settings.backgroundImage}")` : ''
      bgLayer.style.opacity = String((cfg.settings.backgroundOpacity ?? 100) / 100)
    }
  })

  // The backend computes status; here it's just re-read.
  $effect(() => {
    const interval = app.config?.settings.checkInterval
    if (!interval) return
    app.refreshStatus()
    const t = setInterval(() => app.refreshStatus(), Math.min(interval, 30) * 1000)
    return () => clearInterval(t)
  })
</script>

<div class="bg-layer" bind:this={bgLayer} aria-hidden="true"></div>

{#if loadError}
  <main class="state">
    <p>Could not load the dashboard: {loadError}. Check that the homie service is running with <code>systemctl status homie</code>.</p>
  </main>
{:else if !app.config}
  <main class="state"><p>Loading…</p></main>
{:else if route === 'settings'}
  <Settings />
{:else}
  <Dashboard />
{/if}

{#if app.ask}
  {#key app.ask}
    <ConfirmDialog ask={app.ask} />
  {/key}
{/if}

<Toast />
