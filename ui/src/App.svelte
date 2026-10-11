<script lang="ts">
  import { onMount } from 'svelte'
  import { app } from './lib/state.svelte'
  import Dashboard from './components/Dashboard.svelte'
  import Settings from './components/Settings.svelte'
  import Toast from './components/Toast.svelte'

  const routeFromHash = () => (location.hash === '#/settings' ? 'settings' : 'dashboard')
  let route = $state(routeFromHash())
  let loadError = $state('')

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
    document.title = cfg.title || 'Homie'

    const vars: [string, string | undefined][] = [
      ['--bg', cfg.settings.backgroundColor],
      ['--tile', cfg.settings.tileColor],
      ['--accent', cfg.settings.accentColor],
    ]
    for (const [name, value] of vars) {
      if (value) root.style.setProperty(name, value)
      else root.style.removeProperty(name)
    }

    if (cfg.settings.backgroundImage) root.style.setProperty('--bg-image', `url("backgrounds/${cfg.settings.backgroundImage}")`)
    else root.style.removeProperty('--bg-image')
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

<Toast />
