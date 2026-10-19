<script lang="ts">
  import { onMount } from 'svelte'
  import { app } from './lib/state.svelte'
  import { auth } from './lib/auth.svelte'
  import Login from './components/Login.svelte'
  import Locked from './components/Locked.svelte'
  import Dashboard from './components/Dashboard.svelte'
  import Settings from './components/Settings.svelte'
  import Toast from './components/Toast.svelte'
  import ConfirmDialog from './components/ConfirmDialog.svelte'
  import UpdateDialog from './components/UpdateDialog.svelte'
  import { loadUpdates, recheckUpdates, updates } from './lib/updates.svelte'

  const routeFromHash = () => (location.hash === '#/settings' ? 'settings' : 'dashboard')
  let route = $state(routeFromHash())
  let loadError = $state('')
  let bgLayer: HTMLDivElement | undefined = $state()

  onMount(() => {
    const onHash = () => (route = routeFromHash())
    window.addEventListener('hashchange', onHash)
    auth.refresh().catch((e: Error) => (loadError = e.message))
    const look = () => document.visibilityState === 'visible' && auth.state === 'ok' && void recheckUpdates()
    document.addEventListener('visibilitychange', look)
    const timer = setInterval(look, 30 * 60 * 1000)
    return () => {
      window.removeEventListener('hashchange', onHash)
      document.removeEventListener('visibilitychange', look)
      clearInterval(timer)
    }
  })

  // The dashboard is read once signed in (and again after a lock or a new sign-in).
  let loading = false
  $effect(() => {
    if (auth.state !== 'ok' || app.config || loading) return
    loading = true
    app
      .load()
      .then(() => loadUpdates())
      .catch((e: Error) => {
        if (auth.state === 'ok') loadError = e.message
      })
      .finally(() => (loading = false))
  })

  // Theme, density, title, and custom colors follow the settings.
  $effect(() => {
    const cfg = app.config
    if (!cfg) return
    const root = document.documentElement
    if (cfg.settings.theme === 'auto') delete root.dataset.theme
    else root.dataset.theme = cfg.settings.theme
    root.dataset.density = cfg.settings.density
    root.dataset.layout = 'grid'
    root.style.setProperty('--bar-opacity', String(cfg.settings.barOpacity ?? 100))
    root.style.setProperty('--tile-border-w', `${cfg.settings.tileBorderWidth ?? 1}px`)
    root.style.setProperty('--group-border-w', `${cfg.settings.groupBorderWidth ?? 1}px`)
    root.style.setProperty('--card-opacity', String(cfg.settings.cardOpacity ?? 40))
    // The look of the widgets: their own setting, else the tiles'.
    const wb = cfg.settings.widgetBorderWidth
    root.style.setProperty('--widget-opacity', String(cfg.settings.widgetOpacity ?? 100))
    root.style.setProperty('--widget-border-w', wb !== undefined ? `${wb}px` : 'var(--tile-border-w, 1px)')
    root.style.setProperty('--widget-color', cfg.settings.widgetColor ?? 'var(--tile)')
    root.style.setProperty('--widget-text', cfg.settings.widgetTextColor ?? 'var(--tile-text, var(--text))')
    root.style.setProperty('--widget-border', cfg.settings.widgetBorderColor ?? 'var(--tile-border, var(--line))')
    root.style.setProperty('--widget-bar', cfg.settings.widgetBarColor ?? 'var(--accent)')
    document.title = cfg.title || 'Homie'

    const vars: [string, string | undefined][] = [
      ['--bg', cfg.settings.backgroundColor],
      ['--tile', cfg.settings.tileColor],
      ['--accent', cfg.settings.accentColor],
      ['--card-color', cfg.settings.cardColor],
      ['--tile-hover', cfg.settings.hoverColor],
      ['--shelf', cfg.settings.panelColor],
      ['--tile-text', cfg.settings.tileTextColor],
      ['--tile-text-hover', cfg.settings.tileTextHoverColor],
      ['--tile-border', cfg.settings.tileBorderColor],
      ['--group-border', cfg.settings.groupBorderColor],
      ['--glow-shadow', cfg.settings.glowColor && `color-mix(in srgb, ${cfg.settings.glowColor} 55%, transparent)`],
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

  // Widgets are re-read as often as the quickest one asks, and not at all
  // while the page is hidden or on the Settings page.
  $effect(() => {
    const widgets = app.config?.widgets
    if (route !== 'dashboard' || !widgets?.length) return
    const secs = Math.min(...widgets.map((w) => w.refresh || 5))
    const tick = () => {
      if (!document.hidden) app.refreshWidgets()
    }
    tick()
    const t = setInterval(tick, secs * 1000)
    document.addEventListener('visibilitychange', tick)
    return () => {
      clearInterval(t)
      document.removeEventListener('visibilitychange', tick)
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
{:else if auth.state === 'loading'}
  <main class="state"><p>Loading…</p></main>
{:else if auth.state === 'setup' || auth.state === 'login'}
  <Login />
{:else if auth.state === 'locked'}
  <Locked />
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

{#if updates.dialog}
  {#key updates.dialog}
    <UpdateDialog />
  {/key}
{/if}

<Toast />
