<script lang="ts">
  import { onMount } from 'svelte'
  import * as api from '../lib/api'
  import { app } from '../lib/state.svelte'
  import type { Config, Settings } from '../lib/types'

  const s = $derived(app.config!.settings)
  // svelte-ignore state_referenced_locally
  let title = $state(app.config!.title)
  let version = $state('')
  let fileInput: HTMLInputElement
  let bgInput: HTMLInputElement

  onMount(() => {
    api.getVersion().then((v) => (version = v.version)).catch(() => {})
  })

  function set<K extends keyof Settings>(key: K, value: Settings[K]) {
    app.config!.settings[key] = value
    app.save('Settings saved')
  }

  function saveTitle() {
    const t = title.trim()
    if (t === app.config!.title) return
    app.config!.title = t
    app.save('Settings saved')
  }

  function resetColor(key: 'backgroundColor' | 'tileColor' | 'accentColor') {
    set(key, undefined)
  }

  async function uploadBg(e: Event) {
    const file = (e.currentTarget as HTMLInputElement).files?.[0]
    bgInput.value = ''
    if (!file) return
    try {
      await api.uploadBackground(file)
      await app.load()
      app.notify('Background image updated')
    } catch (err) {
      app.notify(`Upload failed: ${(err as Error).message}`, true)
    }
  }

  async function removeBg() {
    try {
      await api.removeBackground()
      await app.load()
      app.notify('Background image removed')
    } catch (err) {
      app.notify(`Could not remove the image: ${(err as Error).message}`, true)
    }
  }

  function exportConfig() {
    const data = JSON.stringify($state.snapshot(app.config), null, 2)
    const a = document.createElement('a')
    a.href = URL.createObjectURL(new Blob([data], { type: 'application/json' }))
    a.download = `homie-config-${new Date().toISOString().slice(0, 10)}.json`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  async function importConfig(e: Event) {
    const file = (e.currentTarget as HTMLInputElement).files?.[0]
    fileInput.value = ''
    if (!file) return
    try {
      // Untyped on purpose: this may be an export from an earlier Homie
      // version (a "blocks" shape, or "groups" with widget/color fields
      // no longer in use) — the backend's own UnmarshalConfig upgrades
      // either shape, so this check only needs to confirm the file looks
      // like SOME Homie export.
      const parsed = JSON.parse(await file.text())
      if (!Array.isArray(parsed?.groups) && !Array.isArray(parsed?.blocks)) {
        throw new Error('the file does not contain a Homie configuration')
      }
      const ok = await app.confirm({
        title: 'Import configuration',
        message: 'The current configuration will be replaced by the one in the file. Continue?',
        confirmLabel: 'Replace',
        danger: true,
      })
      if (!ok) return
      await api.putConfig(parsed as Config)
      await app.load()
      title = app.config!.title
      app.notify('Configuration imported')
    } catch (err) {
      app.notify(`Import failed: ${(err as Error).message}`, true)
    }
  }

  const themes = [
    ['auto', 'Match system'],
    ['light', 'Light'],
    ['dark', 'Dark'],
  ] as const
  const densities = [
    ['comfortable', 'Comfortable'],
    ['compact', 'Compact'],
  ] as const
  const layouts = [
    ['list', 'List'],
    ['grid', 'Grid'],
  ] as const
  const intervals = [
    [15, '15 seconds'],
    [30, '30 seconds'],
    [60, '1 minute'],
    [120, '2 minutes'],
    [300, '5 minutes'],
  ] as const
</script>

<header class="bar">
  <h1>Settings</h1>
  <nav class="actions"><a class="btn" href="#/">Back to dashboard</a></nav>
</header>

<main class="settings">
  <section>
    <h2>Appearance</h2>
    <fieldset>
      <legend>Theme</legend>
      <div class="seg">
        {#each themes as [value, label] (value)}
          <label class:on={s.theme === value}>
            <input type="radio" name="theme" {value} checked={s.theme === value} onchange={() => set('theme', value)} />
            {label}
          </label>
        {/each}
      </div>
    </fieldset>
    <fieldset>
      <legend>Tile density</legend>
      <div class="seg">
        {#each densities as [value, label] (value)}
          <label class:on={s.density === value}>
            <input type="radio" name="density" {value} checked={s.density === value} onchange={() => set('density', value)} />
            {label}
          </label>
        {/each}
      </div>
    </fieldset>
    <fieldset>
      <legend>Layout</legend>
      <div class="seg">
        {#each layouts as [value, label] (value)}
          <label class:on={s.layout === value}>
            <input type="radio" name="layout" {value} checked={s.layout === value} onchange={() => set('layout', value)} />
            {label}
          </label>
        {/each}
      </div>
    </fieldset>
    <p class="hint">List stacks the groups in one column. Grid shows each group as a card, side by side.</p>
    <div class="color-row">
      <label>
        Page background
        <input type="color" value={s.backgroundColor || '#1c2229'} onchange={(e) => set('backgroundColor', e.currentTarget.value)} />
      </label>
      <button type="button" onclick={() => resetColor('backgroundColor')}>Reset</button>
      <label>
        Tile background
        <input type="color" value={s.tileColor || '#2c343e'} onchange={(e) => set('tileColor', e.currentTarget.value)} />
      </label>
      <button type="button" onclick={() => resetColor('tileColor')}>Reset</button>
      <label>
        Accent
        <input type="color" value={s.accentColor || '#e9b949'} onchange={(e) => set('accentColor', e.currentTarget.value)} />
      </label>
      <button type="button" onclick={() => resetColor('accentColor')}>Reset</button>
    </div>
    <p class="hint">Resetting a color goes back to the theme's own default (light or dark, depending on the Theme setting above).</p>

    <div class="row">
      <button type="button" onclick={() => bgInput.click()}>{s.backgroundImage ? 'Replace background image' : 'Upload background image'}</button>
      {#if s.backgroundImage}
        <button type="button" onclick={removeBg}>Remove background image</button>
      {/if}
      <input bind:this={bgInput} type="file" accept="image/png,image/jpeg" hidden onchange={uploadBg} />
    </div>
    <p class="hint">JPEG or PNG, up to 10 MB. Covers the full page; the background color above still shows at the edges and before it loads.</p>

    {#if s.backgroundImage}
      <label class="opacity-row">
        Background image opacity
        <input
          type="range"
          min="1"
          max="100"
          value={s.backgroundOpacity}
          onchange={(e) => set('backgroundOpacity', Number(e.currentTarget.value))}
        />
        <span>{s.backgroundOpacity}%</span>
      </label>
      <p class="hint">Lower it to let the page background color underneath show through.</p>
    {/if}

    <label class="opacity-row">
      Top bar opacity
      <input
        type="range"
        min="1"
        max="100"
        value={s.barOpacity}
        onchange={(e) => set('barOpacity', Number(e.currentTarget.value))}
      />
      <span>{s.barOpacity}%</span>
    </label>
    <p class="hint">Lower it to make the top bar see-through, so the page background shows behind it.</p>
  </section>

  <section>
    <h2>General</h2>
    <label>
      Dashboard title
      <input bind:value={title} onchange={saveTitle} maxlength="100" />
    </label>
    <label>
      Check service status every
      <select value={s.checkInterval} onchange={(e) => set('checkInterval', Number(e.currentTarget.value))}>
        {#each intervals as [value, label] (value)}
          <option {value}>{label}</option>
        {/each}
      </select>
    </label>
    <label class="check">
      <input type="checkbox" checked={s.openInNewTab} onchange={(e) => set('openInNewTab', e.currentTarget.checked)} />
      Open services in a new tab
    </label>
  </section>

  <section>
    <h2>Configuration backup</h2>
    <p class="hint">The file contains groups, services, and settings. Importing it replaces the current configuration.</p>
    <div class="row">
      <button type="button" onclick={exportConfig}>Export configuration</button>
      <button type="button" onclick={() => fileInput.click()}>Import configuration</button>
      <input bind:this={fileInput} type="file" accept="application/json,.json" hidden onchange={importConfig} />
    </div>
  </section>

  {#if version}<p class="version">Homie {version}</p>{/if}
</main>

<style>
  .color-row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    margin-top: 10px;
  }
  .color-row label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 0.9rem;
  }
  .color-row input[type='color'] {
    width: 36px;
    height: 28px;
    padding: 2px;
  }

  .opacity-row {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 10px;
    font-weight: 600;
  }
  .opacity-row input[type='range'] {
    flex: 1;
    max-width: 240px;
  }
  .opacity-row span {
    font-weight: 500;
    color: var(--muted);
    min-width: 3.5ch;
  }
</style>
