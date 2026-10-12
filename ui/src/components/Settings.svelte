<script lang="ts">
  import { onMount } from 'svelte'
  import * as api from '../lib/api'
  import { barHeight } from '../lib/barHeight'
  import { app } from '../lib/state.svelte'
  import type { Config, Settings } from '../lib/types'
  import ColorField from './ColorField.svelte'
  import Slider from './Slider.svelte'

  const s = $derived(app.config!.settings)
  // svelte-ignore state_referenced_locally
  let title = $state(app.config!.title)
  let version = $state('')
  let fileInput: HTMLInputElement | undefined = $state()
  let bgInput: HTMLInputElement | undefined = $state()
  let tab = $state<'appearance' | 'general' | 'backup'>('appearance')

  onMount(() => {
    api.getVersion().then((v) => (version = v.version)).catch(() => {})
  })

  function set<K extends keyof Settings>(key: K, value: Settings[K]) {
    app.config!.settings[key] = value
    app.save('Settings saved')
  }

  // Live preview while dragging or typing; the save happens on commit.
  function preview<K extends keyof Settings>(key: K, value: Settings[K]) {
    app.config!.settings[key] = value
  }

  function commit() {
    app.save('Settings saved')
  }

  function saveTitle() {
    const t = title.trim()
    if (t === app.config!.title) return
    app.config!.title = t
    app.save('Settings saved')
  }

  async function uploadBg(e: Event) {
    const file = (e.currentTarget as HTMLInputElement).files?.[0]
    if (bgInput) bgInput.value = ''
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
    if (fileInput) fileInput.value = ''
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

  const tabs = [
    ['appearance', 'Appearance'],
    ['general', 'General'],
    ['backup', 'Backup'],
  ] as const
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

<header class="bar" use:barHeight>
  <h1>Settings</h1>
  <nav class="actions"><a class="btn" href="#/">Back to dashboard</a></nav>
</header>

<div class="scroller">
<main class="settings">
  <div class="tabs" role="tablist" aria-label="Settings sections">
    {#each tabs as [id, label] (id)}
      <button type="button" role="tab" class:on={tab === id} aria-selected={tab === id} onclick={() => (tab = id)}>{label}</button>
    {/each}
  </div>

  <div class="panels">
  {#if tab === 'appearance'}
    <section>
      <h2>Theme and layout</h2>
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
        <p class="hint">List stacks the groups in one column. Grid shows each group as a card, side by side.</p>
      </fieldset>
    </section>

    <section>
      <h2>Colors</h2>
      <ColorField label="Page background" cssVar="--bg" theme={s.theme} value={s.backgroundColor} onpreview={(v) => preview('backgroundColor', v)} oncommit={commit} />
      <ColorField label="Panels" cssVar="--shelf" theme={s.theme} value={s.panelColor} onpreview={(v) => preview('panelColor', v)} oncommit={commit} />
      <ColorField label="Accent" cssVar="--accent" theme={s.theme} value={s.accentColor} onpreview={(v) => preview('accentColor', v)} oncommit={commit} />
      <p class="hint">Panels is the background of the settings sections and dialogs. Accent colors the group titles and the drag markers. Pick a color or type its hex code, for example to copy one from another field. "Default" means the theme's own color (light or dark, depending on Theme above).</p>
    </section>

    <section>
      <h2>Tiles</h2>
      <ColorField label="Background" cssVar="--tile" theme={s.theme} value={s.tileColor} onpreview={(v) => preview('tileColor', v)} oncommit={commit} />
      <ColorField label="Background on hover" cssVar="--tile-hover" theme={s.theme} value={s.hoverColor} onpreview={(v) => preview('hoverColor', v)} oncommit={commit} />
      <ColorField label="Text" cssVar="--text" theme={s.theme} value={s.tileTextColor} onpreview={(v) => preview('tileTextColor', v)} oncommit={commit} />
      <ColorField label="Text on hover" cssVar="--text" theme={s.theme} value={s.tileTextHoverColor} onpreview={(v) => preview('tileTextHoverColor', v)} oncommit={commit} />
      <ColorField label="Border" cssVar="--line" theme={s.theme} value={s.tileBorderColor} onpreview={(v) => preview('tileBorderColor', v)} oncommit={commit} />
      <Slider label="Border width" unit="px" min={0} max={8} value={s.tileBorderWidth ?? 1} oninput={(v) => preview('tileBorderWidth', v)} oncommit={commit} />
      <ColorField label="Hover glow" cssVar="--text" theme={s.theme} value={s.glowColor} onpreview={(v) => preview('glowColor', v)} oncommit={commit} />
      <p class="hint">A border width of 0 hides the border. Without a custom glow color, the glow is a soft shade of the text color.</p>
    </section>

    <section>
      <h2>Groups</h2>
      {#if s.layout === 'grid'}
        <ColorField label="Card background" cssVar="--shelf" theme={s.theme} value={s.cardColor} onpreview={(v) => preview('cardColor', v)} oncommit={commit} />
      {/if}
      <ColorField label="Border" cssVar={s.layout === 'grid' ? '--line' : '--accent'} theme={s.theme} value={s.groupBorderColor} onpreview={(v) => preview('groupBorderColor', v)} oncommit={commit} />
      <Slider label="Border width" unit="px" min={0} max={8} value={s.groupBorderWidth ?? 1} oninput={(v) => preview('groupBorderWidth', v)} oncommit={commit} />
      <p class="hint">The border is the card outline in the grid layout and the line under each group in the list layout. The card background applies to the grid layout only.</p>
    </section>

    <section>
      <h2>Background image</h2>
      <div class="row">
        <button type="button" onclick={() => bgInput?.click()}>{s.backgroundImage ? 'Replace background image' : 'Upload background image'}</button>
        {#if s.backgroundImage}
          <button type="button" onclick={removeBg}>Remove background image</button>
        {/if}
        <input bind:this={bgInput} type="file" accept="image/png,image/jpeg" hidden onchange={uploadBg} />
      </div>
      <p class="hint">JPEG or PNG, up to 10 MB. Covers the full page; the page background color still shows at the edges and before it loads.</p>
      {#if s.backgroundImage}
        <Slider label="Image opacity" value={s.backgroundOpacity} oninput={(v) => preview('backgroundOpacity', v)} oncommit={commit} />
      {/if}
    </section>

    <section>
      <h2>Transparency</h2>
      <Slider label="Top bar opacity" value={s.barOpacity} oninput={(v) => preview('barOpacity', v)} oncommit={commit} />
      {#if s.layout === 'grid'}
        <Slider label="Group card opacity" value={s.cardOpacity} oninput={(v) => preview('cardOpacity', v)} oncommit={commit} />
      {/if}
      <p class="hint">Lower an opacity to let what is behind show through. Double-click a percentage to type an exact value.</p>
    </section>
  {:else if tab === 'general'}
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
  {:else}
    <section>
      <h2>Configuration backup</h2>
      <p class="hint">The file contains groups, services, and settings. Importing it replaces the current configuration.</p>
      <div class="row">
        <button type="button" onclick={exportConfig}>Export configuration</button>
        <button type="button" onclick={() => fileInput?.click()}>Import configuration</button>
        <input bind:this={fileInput} type="file" accept="application/json,.json" hidden onchange={importConfig} />
      </div>
    </section>
  {/if}
  </div>

  {#if version}<p class="version">Homie {version}</p>{/if}
</main>
</div>

<style>
  .tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid var(--line);
  }
  .tabs button {
    border: none;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    background: none;
    padding: 8px 16px;
    margin-bottom: -1px;
    color: var(--muted);
    font-weight: 600;
  }
  .tabs button:hover {
    color: var(--text);
  }
  .tabs button.on {
    color: var(--text);
    border-bottom-color: var(--accent);
  }
</style>
