<script lang="ts">
  import { onMount } from 'svelte'
  import * as api from '../lib/api'
  import { barHeight } from '../lib/barHeight'
  import { app } from '../lib/state.svelte'
  import { isBuiltin, isModified } from '../lib/themes'
  import type { Config, Settings } from '../lib/types'
  import ColorField from './ColorField.svelte'
  import Slider from './Slider.svelte'
  import { view } from '../lib/collapse.svelte'
  import { loadUpdates, newer, updates } from '../lib/updates.svelte'

  const s = $derived(app.config!.settings)
  // svelte-ignore state_referenced_locally
  let title = $state(app.config!.title)
  let version = $state('')
  let fileInput: HTMLInputElement | undefined = $state()
  let bgInput: HTMLInputElement | undefined = $state()
  let tab = $state<'appearance' | 'general' | 'backup'>('appearance')

  onMount(() => {
    api.getVersion().then((v) => (version = v.version)).catch(() => {})
    if (!updates.loaded) loadUpdates()
  })

  // What Settings says about versions: the reminder that stays until Homie is updated.
  const fresh = $derived(newer())
  let checking = $state(false)
  async function checkNow() {
    checking = true
    await loadUpdates(true)
    checking = false
  }
  async function setCheck(on: boolean) {
    set('noUpdateCheck', !on)
    await app.save('Settings saved')
    await loadUpdates(true)
  }

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

  // ---- themes

  let themeInput: HTMLInputElement | undefined = $state()
  const activeTheme = $derived(app.config!.activeTheme)
  const modified = $derived(isModified(app.config!, activeTheme))
  const swatches = $derived([
    ...app.builtinThemes.map((t) => ({ id: t.id, name: t.name, bg: t.colors.backgroundColor, tile: t.colors.tileColor, accent: t.colors.accentColor })),
    ...app.config!.themes.map((t) => ({ id: t.id, name: t.name, bg: t.appearance.backgroundColor, tile: t.appearance.tileColor, accent: t.appearance.accentColor })),
  ])

  async function saveAs() {
    const name = await app.prompt({
      title: 'Save theme',
      message: 'Save the current look (colors, layout, background image, and each widget’s look) under a name.',
      placeholder: 'My theme',
      confirmLabel: 'Save',
    })
    if (name) app.saveCurrentTheme(name)
  }

  async function rename(id: string, current: string) {
    const name = await app.prompt({ title: 'Rename theme', message: 'New name for the theme.', value: current, confirmLabel: 'Rename' })
    if (name) app.renameTheme(id, name)
  }

  async function removeTheme(id: string, name: string) {
    const ok = await app.confirm({
      title: 'Delete theme',
      message: `Delete the theme “${name}”? The dashboard keeps its current look.`,
      confirmLabel: 'Delete',
      danger: true,
    })
    if (ok) app.deleteTheme(id)
  }

  async function importTheme(e: Event) {
    const file = (e.currentTarget as HTMLInputElement).files?.[0]
    if (themeInput) themeInput.value = ''
    if (!file) return
    try {
      app.importTheme(await file.text())
    } catch (err) {
      app.notify(`Import failed: ${(err as Error).message}`, true)
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
      <h2>Themes</h2>
      <ul class="themes">
        {#each swatches as t (t.id)}
          {@const active = t.id === activeTheme}
          <li class:active>
            <span class="swatch" style:background={t.bg ?? 'var(--bg)'} aria-hidden="true">
              <i style:background={t.tile ?? 'var(--tile)'}></i><i style:background={t.accent ?? 'var(--accent)'}></i>
            </span>
            <span class="tname">
              {t.name}
              {#if isBuiltin(t.id)}<small>built-in</small>{/if}
              {#if active}<small class="on">{modified ? 'active, modified' : 'active'}</small>{/if}
            </span>
            <span class="tbtns">
              {#if !active || modified}<button type="button" class:primary={!active} onclick={() => app.applyTheme(t.id)}>Apply</button>{/if}
              {#if active && modified && !isBuiltin(t.id)}<button type="button" onclick={() => app.updateTheme(t.id)}>Update</button>{/if}
              {#if !isBuiltin(t.id)}<button type="button" onclick={() => rename(t.id, t.name)}>Rename</button>{/if}
              <button type="button" onclick={() => app.exportTheme(t.id)}>Export</button>
              {#if !isBuiltin(t.id)}<button type="button" class="danger" onclick={() => removeTheme(t.id, t.name)}>Delete</button>{/if}
            </span>
          </li>
        {/each}
      </ul>
      <div class="row">
        <button type="button" onclick={saveAs}>Save current as…</button>
        <button type="button" onclick={() => themeInput?.click()}>Import theme…</button>
        <input bind:this={themeInput} type="file" accept="application/json,.json" hidden onchange={importTheme} />
      </div>
      <p class="hint">A theme saves everything on this page: colors, layout, density, the background image, and the look of each widget. “Update” overwrites the active theme with your changes; “Save current as…” makes a new one. A theme file does not include the background image.</p>
    </section>

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
      <h2>Widgets</h2>
      <ColorField label="Background" cssVar="--widget-color" theme={s.theme} value={s.widgetColor} onpreview={(v) => preview('widgetColor', v)} oncommit={commit} />
      <Slider label="Background opacity" value={s.widgetOpacity} oninput={(v) => preview('widgetOpacity', v)} oncommit={commit} />
      <ColorField label="Text" cssVar="--widget-text" theme={s.theme} value={s.widgetTextColor} onpreview={(v) => preview('widgetTextColor', v)} oncommit={commit} />
      <ColorField label="Border" cssVar="--widget-border" theme={s.theme} value={s.widgetBorderColor} onpreview={(v) => preview('widgetBorderColor', v)} oncommit={commit} />
      <Slider label="Border width" unit="px" min={0} max={8} value={s.widgetBorderWidth ?? s.tileBorderWidth ?? 1} oninput={(v) => preview('widgetBorderWidth', v)} oncommit={commit} />
      <ColorField label="Bars" cssVar="--widget-bar" theme={s.theme} value={s.widgetBarColor} onpreview={(v) => preview('widgetBarColor', v)} oncommit={commit} />
      <p class="hint">The look every widget has, unless a widget sets its own in its editor. Without a custom color a widget looks like a tile. In a widget's editor, “Use this look for all widgets” copies that widget's look here.</p>
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
      <label class="check">
        <input type="checkbox" checked={!s.noUpdateCheck} onchange={(e) => setCheck(e.currentTarget.checked)} />
        Tell me when a new version of Homie is out
      </label>
      {#if !s.noUpdateCheck}
        <small>
          {#if !updates.loaded}
            Checking…
          {:else if updates.error && fresh.length === 0}
            Could not check for a new version: {updates.error}
          {:else if fresh.length > 0}
            <strong class="update">Homie {fresh[0].version} is available</strong> (you have {updates.current}).
            <a href={fresh[0].url} target="_blank" rel="noopener noreferrer">What's new</a>. To update, run <code>update</code> in the Homie container.
          {:else if updates.enabled}
            Homie {updates.current} is up to date.
          {:else}
            This is a development build: updates are not checked.
          {/if}
          {#if updates.enabled}
            <button type="button" class="link" onclick={checkNow} disabled={checking}>{checking ? 'Checking…' : 'Check now'}</button>
          {/if}
        </small>
      {/if}
      <Slider label="Collapse widgets under" unit="px" min={0} max={3000} value={s.widgetCollapseBelow ?? 0} oninput={(v) => preview('widgetCollapseBelow', v)} oncommit={commit} />
      <small>
        Widgets collapse when the window is narrower than this (0 = never). This window is {view.width} px wide. Each widget has a button to collapse it, and the one in the top bar collapses them all; what you choose by hand is remembered on this device. A collapsed widget shows up to four values (choose them in the widget's Values tab).
      </small>
    </section>
  {:else}
    <section>
      <h2>Configuration backup</h2>
      <p class="hint">The file contains groups, services, widgets, and settings (widget passwords are never exported, so enter them again after importing on another install). Importing it replaces the current configuration.</p>
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
  .update {
    color: var(--accent);
  }
  .link {
    margin-left: 8px;
    padding: 0;
    border: none;
    background: none;
    color: var(--accent);
    font: inherit;
    cursor: pointer;
    text-decoration: underline;
  }
  .link:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .themes {
    display: grid;
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .themes li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 12px;
    padding: 8px 10px;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 6px;
  }
  .themes li.active {
    border-color: var(--accent);
  }
  .swatch {
    display: flex;
    gap: 3px;
    align-items: center;
    flex: none;
    padding: 5px;
    border: 1px solid var(--line);
    border-radius: 5px;
  }
  .swatch i {
    width: 14px;
    height: 22px;
    border-radius: 3px;
  }
  .tname {
    flex: 1;
    min-width: 8ch;
    font-weight: 600;
  }
  .tname small {
    margin-left: 6px;
    color: var(--muted);
    font-weight: 400;
  }
  .tname small.on {
    color: var(--accent);
  }
  .tbtns {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .tbtns button {
    padding: 3px 10px;
    font-size: 0.9rem;
  }
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
