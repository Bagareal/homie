<script lang="ts">
  import { onMount } from 'svelte'
  import * as api from '../lib/api'
  import { bytes } from '../lib/format'
  import { app, uid } from '../lib/state.svelte'
  import type { HostData, Row, RowType, Widget, WidgetData } from '../lib/types'
  import ColorField from './ColorField.svelte'
  import Slider from './Slider.svelte'
  import WidgetView from './Widget.svelte'

  let { widgetId, onclose }: { widgetId: string | null; onclose: () => void } = $props()

  let dialog: HTMLDialogElement
  let form: HTMLFormElement

  const typeLabels: Record<RowType, string> = {
    cpu: 'CPU',
    mem: 'Memory',
    swap: 'Swap',
    load: 'Load average',
    uptime: 'Uptime',
    temp: 'Temperature',
    disk: 'Disk',
    network: 'Network',
  }

  // svelte-ignore state_referenced_locally
  const saved = widgetId ? app.widget(widgetId) : undefined
  const blank: Widget = {
    id: '',
    name: '',
    kind: 'glances',
    url: '',
    username: '',
    rows: [
      { id: uid(), type: 'cpu', load: true },
      { id: uid() + 'm', type: 'mem' },
    ],
    refresh: 5,
    width: 300,
    height: 0,
    opacity: 100,
  }
  // Edited locally until saved; the password is never prefilled.
  const initial: Widget = { ...blank, ...structuredClone($state.snapshot(saved) ?? {}), password: '', clearPassword: false }
  let w = $state<Widget>(structuredClone(initial))
  const initialJson = JSON.stringify(initial)
  let error = $state('')
  let testing = $state(false)
  let testResult = $state<{ ok: boolean; text: string } | null>(null)
  // What the host reported at the last successful test: used to pick disks,
  // sensors and network cards, and to preview the widget with real values.
  let found = $state<HostData | null>(null)

  const dirty = $derived(JSON.stringify(w) !== initialJson)
  const addressChanged = $derived(!!saved?.hasPassword && (normalizeUrl(w.url) !== saved.url || (w.username ?? '').trim() !== (saved.username ?? '')))

  function normalizeUrl(raw: string): string {
    const v = raw.trim()
    return v && !/^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? `http://${v}` : v
  }

  // What the editor previews until the host has been read.
  const sample: WidgetData = {
    online: true,
    updatedAt: '',
    data: {
      cpu: 23,
      mem: { percent: 61, used: 19.5 * 2 ** 30, total: 32 * 2 ** 30 },
      swap: { percent: 4, used: 0.1 * 2 ** 30, total: 2 * 2 ** 30 },
      load: { min1: 0.52, min5: 0.38, min15: 0.3, cores: 8 },
      uptime: '12 days, 3:45:10',
      temp: { label: 'Package id 0', value: 48 },
      sensors: [{ label: 'Package id 0', value: 48 }],
      disks: [
        { mount: '/', percent: 39, used: 194 * 2 ** 30, total: 498 * 2 ** 30 },
        { mount: '/mnt/data', percent: 92, used: 920 * 2 ** 30, total: 1000 * 2 ** 30 },
      ],
      network: [{ name: 'eth0', rx: 1.4 * 2 ** 20, tx: 310 * 2 ** 10 }],
    },
  }
  const live = $derived(widgetId ? app.widgetData[widgetId] : undefined)
  const previewData = $derived<WidgetData>(
    found ? { online: true, updatedAt: '', data: found } : live?.online && !addressChanged ? live : sample,
  )
  const previewIsSample = $derived(previewData === sample)

  async function test(silent = false) {
    error = ''
    if (!silent) testResult = null
    const url = normalizeUrl(w.url)
    if (!url) return void (silent || (testResult = { ok: false, text: 'Enter the address first.' }))
    testing = true
    try {
      const r = await api.testWidget({ id: widgetId ?? undefined, url, username: (w.username ?? '').trim(), password: w.password ?? '' })
      if (r.ok) {
        found = r.data ?? null
        testResult = { ok: true, text: `Connected${r.version ? ` (Glances ${r.version})` : ''}` }
      } else if (!silent) testResult = { ok: false, text: r.error ?? 'Failed' }
    } catch (e) {
      if (!silent) testResult = { ok: false, text: (e as Error).message }
    } finally {
      testing = false
    }
  }

  // An existing widget: list what its host offers right away.
  onMount(() => {
    dialog.showModal()
    if (saved) test(true)
  })

  async function requestClose() {
    if (!dirty) return dialog.close()
    const choice = await app.confirmUnsaved('widget')
    if (choice === 'discard') dialog.close()
    else if (choice === 'save') form.requestSubmit()
  }

  // ---- rows

  const diskLabel = (d: { mount: string; used: number; total: number }) => `${d.mount} · ${bytes(d.used)} of ${bytes(d.total)}`
  const tempLabel = (t: { label: string; value: number }) => `${t.label} · ${Math.round(t.value)} °C`

  // The "add" menu's values are "type" or "type:key".
  function addRow(value: string) {
    if (!value) return
    const at = value.indexOf(':')
    const type = (at < 0 ? value : value.slice(0, at)) as RowType
    const row: Row = { id: uid(), type }
    if (type === 'cpu') row.load = true
    if (at >= 0) row.key = value.slice(at + 1)
    w.rows.push(row)
  }

  function setCpu(r: Row, part: 'load' | 'temp', on: boolean) {
    r[part] = on
    // A CPU row always shows something.
    if (!r.load && !r.temp) r[part === 'load' ? 'temp' : 'load'] = true
  }

  function move(i: number, to: number) {
    if (to < 0 || to >= w.rows.length || i === to) return
    const [r] = w.rows.splice(i, 1)
    w.rows.splice(to, 0, r)
  }

  let dragId = $state<string | null>(null)
  let overId = $state<string | null>(null)
  function rowDrop(targetId: string) {
    const from = w.rows.findIndex((r) => r.id === dragId)
    const to = w.rows.findIndex((r) => r.id === targetId)
    dragId = overId = null
    move(from, to)
  }

  function submit(e: SubmitEvent) {
    e.preventDefault()
    const url = normalizeUrl(w.url)
    if (!w.name.trim()) return void (error = 'Enter a name for the widget.')
    try {
      const u = new URL(url)
      if (u.protocol !== 'http:' && u.protocol !== 'https:') throw new Error()
    } catch {
      return void (error = 'Enter a valid address, for example http://192.168.1.10:61208.')
    }
    if (w.rows.length === 0) return void (error = 'Add at least one value to show.')
    const out: Widget = {
      ...$state.snapshot(w),
      id: saved?.id ?? uid(),
      name: w.name.trim(),
      url,
      username: (w.username ?? '').trim(),
      // Changing the address or username drops the stored password on the server.
      clearPassword: !!w.clearPassword || (addressChanged && !w.password),
      rows: w.rows.map((r) => ({ ...$state.snapshot(r), name: (r.name ?? '').trim() })),
    }
    app.upsertWidget(out)
    dialog.close()
  }

  async function remove() {
    if (!saved) return
    const ok = await app.confirm({
      title: 'Remove widget',
      message: `Remove “${saved.name}” from the dashboard?`,
      confirmLabel: 'Remove',
      danger: true,
    })
    if (ok) {
      app.deleteWidget(saved.id)
      dialog.close()
    }
  }

  const theme = $derived(app.config!.settings.theme)
  const noop = () => {}
</script>

<svelte:window
  onbeforeunload={(e) => {
    if (dirty) e.preventDefault()
  }}
/>

{#snippet keyOptions(r: Row, auto: string, items: { value: string; label: string }[])}
  <select bind:value={r.key} aria-label="{typeLabels[r.type]} to show">
    <option value={undefined}>{auto}</option>
    {#if r.key && !items.some((i) => i.value === r.key)}
      <option value={r.key}>{r.key}{found ? ' (not detected)' : ''}</option>
    {/if}
    {#each items as i (i.value)}
      <option value={i.value}>{i.label}</option>
    {/each}
  </select>
{/snippet}

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
  bind:this={dialog}
  class="dialog wide"
  onclose={onclose}
  oncancel={(e) => {
    e.preventDefault()
    requestClose()
  }}
  onclick={(e) => e.target === dialog && requestClose()}
>
  <form bind:this={form} onsubmit={submit} novalidate>
    <h2>{saved ? 'Edit widget' : 'New widget'}</h2>

    <div class="preview">
      <WidgetView widget={w} data={previewData} preview />
      {#if previewIsSample}<small>Preview with sample values. Test the connection to see your host's.</small>{/if}
    </div>

    <fieldset>
      <legend>Source</legend>
      <label>
        Name
        <!-- svelte-ignore a11y_autofocus -->
        <input bind:value={w.name} maxlength="60" placeholder="Proxmox" autofocus />
      </label>
      <label>
        Glances address
        <input bind:value={w.url} placeholder="http://192.168.1.10:61208" inputmode="url" />
        <small>The host must run Glances in web server mode (<code>glances -w</code>, port 61208 by default).</small>
      </label>
      <div class="two">
        <label>
          Username <small>(optional)</small>
          <input bind:value={w.username} maxlength="100" autocomplete="off" />
        </label>
        <label>
          Password <small>(optional)</small>
          <input
            type="password"
            bind:value={w.password}
            maxlength="200"
            autocomplete="new-password"
            placeholder={saved?.hasPassword && !addressChanged ? '(saved, leave blank to keep)' : ''}
          />
        </label>
      </div>
      {#if saved?.hasPassword}
        {#if addressChanged}
          <small>Changing the address or username removes the saved password: enter it again if the host needs one.</small>
        {:else}
          <label class="check"><input type="checkbox" bind:checked={w.clearPassword} /> Remove the saved password</label>
        {/if}
      {/if}
      <small>Glances only supports basic authentication, which is sent unencrypted over plain http. Use it on a trusted network.</small>
      <div class="test">
        <button type="button" onclick={() => test()} disabled={testing}>{testing ? 'Testing…' : 'Test connection'}</button>
        {#if testResult}<span class:ok={testResult.ok} class:bad={!testResult.ok} role="status">{testResult.text}</span>{/if}
      </div>
    </fieldset>

    <fieldset>
      <legend>Values</legend>
      <small>Drag to reorder (or use the arrows). Disks, sensors and network cards can be chosen once the connection has been tested.</small>
      <ul class="rows">
        {#each w.rows as r, i (r.id)}
          <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
          <li
            class:dragging={dragId === r.id}
            class:over={overId === r.id && dragId !== r.id}
            ondragover={(e) => {
              if (!dragId) return
              e.preventDefault()
              overId = r.id
            }}
            ondrop={(e) => {
              e.preventDefault()
              rowDrop(r.id)
            }}
          >
            <span
              class="grip"
              role="img"
              aria-label="Drag to reorder"
              title="Drag to reorder"
              draggable="true"
              ondragstart={(e) => {
                dragId = r.id
                e.dataTransfer?.setData('text/plain', r.id)
                const li = (e.currentTarget as HTMLElement).closest('li')
                if (li && e.dataTransfer) e.dataTransfer.setDragImage(li, 10, 10)
              }}
              ondragend={() => (dragId = overId = null)}>⋮⋮</span
            >
            <div class="rbody">
              <div class="rhead">
                <strong>{typeLabels[r.type]}</strong>
                {#if r.type === 'cpu'}
                  <label class="check"><input type="checkbox" checked={r.load} onchange={(e) => setCpu(r, 'load', e.currentTarget.checked)} /> Load</label>
                  <label class="check"><input type="checkbox" checked={r.temp} onchange={(e) => setCpu(r, 'temp', e.currentTarget.checked)} /> Temperature</label>
                {/if}
                <span class="spacer"></span>
                <button type="button" class="icon-btn" aria-label="Move up" disabled={i === 0} onclick={() => move(i, i - 1)}>▲</button>
                <button type="button" class="icon-btn" aria-label="Move down" disabled={i === w.rows.length - 1} onclick={() => move(i, i + 1)}>▼</button>
                <button type="button" class="icon-btn danger" aria-label="Remove" onclick={() => w.rows.splice(i, 1)}>✕</button>
              </div>
              <div class="rfields">
                {#if r.type === 'temp' || (r.type === 'cpu' && r.temp)}
                  {@render keyOptions(r, 'CPU (automatic)', (found?.sensors ?? []).map((s) => ({ value: s.label, label: tempLabel(s) })))}
                {:else if r.type === 'disk'}
                  {@render keyOptions(r, 'All disks', (found?.disks ?? []).map((d) => ({ value: d.mount, label: diskLabel(d) })))}
                {:else if r.type === 'network'}
                  {@render keyOptions(r, 'All physical cards (total)', (found?.network ?? []).map((n) => ({ value: n.name, label: n.virtual ? `${n.name} (virtual)` : n.bridge ? `${n.name} (bridge)` : n.name })))}
                {/if}
                <input
                  bind:value={r.name}
                  maxlength="60"
                  placeholder={r.key || typeLabels[r.type]}
                  aria-label="Name shown for this value"
                  disabled={r.type === 'disk' && !r.key}
                  title={r.type === 'disk' && !r.key ? 'Pick one disk to give it a name' : ''}
                />
              </div>
            </div>
          </li>
        {/each}
      </ul>
      <select
        class="add"
        aria-label="Add a value"
        onchange={(e) => {
          addRow(e.currentTarget.value)
          e.currentTarget.value = ''
        }}
      >
        <option value="">+ Add a value…</option>
        <option value="cpu">CPU</option>
        <option value="mem">Memory</option>
        <option value="swap">Swap</option>
        <option value="load">Load average</option>
        <option value="uptime">Uptime</option>
        <option value="temp">Temperature (CPU)</option>
        <option value="disk">All disks</option>
        <option value="network">Network (all physical cards)</option>
        {#if found?.disks?.length}
          <optgroup label="Detected disks">
            {#each found.disks as d (d.mount)}<option value="disk:{d.mount}">{diskLabel(d)}</option>{/each}
          </optgroup>
        {/if}
        {#if found?.sensors?.length}
          <optgroup label="Detected sensors">
            {#each found.sensors as s (s.label)}<option value="temp:{s.label}">{tempLabel(s)}</option>{/each}
          </optgroup>
        {/if}
        {#if found?.network?.length}
          <optgroup label="Detected network cards">
            {#each found.network as n (n.name)}<option value="network:{n.name}">{n.virtual ? `${n.name} (virtual)` : n.bridge ? `${n.name} (bridge)` : n.name}</option>{/each}
          </optgroup>
        {/if}
      </select>
      <Slider label="Refresh every" unit="s" min={2} max={300} value={w.refresh} oninput={(v) => (w.refresh = v)} oncommit={noop} />
    </fieldset>

    <fieldset>
      <legend>Size</legend>
      <Slider label="Width" unit="px" min={120} max={1200} value={w.width} oninput={(v) => (w.width = v)} oncommit={noop} />
      <Slider label="Height (0 = fit content)" unit="px" min={0} max={1000} value={w.height} oninput={(v) => (w.height = v)} oncommit={noop} />
      <small>Drag the widget wherever you like on the dashboard while editing, even between two groups.</small>
    </fieldset>

    <fieldset>
      <legend>Appearance</legend>
      <ColorField label="Background" cssVar="--tile" {theme} value={w.color} onpreview={(v) => (w.color = v)} oncommit={noop} />
      <Slider label="Background opacity" min={1} value={w.opacity} oninput={(v) => (w.opacity = v)} oncommit={noop} />
      <ColorField label="Text" cssVar="--text" {theme} value={w.textColor} onpreview={(v) => (w.textColor = v)} oncommit={noop} />
      <ColorField label="Border" cssVar="--line" {theme} value={w.borderColor} onpreview={(v) => (w.borderColor = v)} oncommit={noop} />
      <Slider label="Border width" unit="px" min={0} max={8} value={w.borderWidth ?? 1} oninput={(v) => (w.borderWidth = v)} oncommit={noop} />
      <ColorField label="Bars" cssVar="--accent" {theme} value={w.barColor} onpreview={(v) => (w.barColor = v)} oncommit={noop} />
    </fieldset>

    {#if error}<p class="form-error" role="alert">{error}</p>{/if}

    <div class="dialog-actions">
      {#if saved}<button type="button" class="danger" onclick={remove}>Delete</button>{/if}
      <button type="button" onclick={requestClose}>Cancel</button>
      <button type="submit" class="primary">{saved ? 'Save changes' : 'Add widget'}</button>
    </div>
  </form>
</dialog>

<style>
  .dialog.wide {
    width: min(620px, calc(100vw - 32px));
    max-height: calc(100vh - 32px);
    overflow-y: auto;
  }
  fieldset {
    display: grid;
    gap: 12px;
    margin: 0;
    padding: 0;
    border: none;
  }
  legend {
    padding: 0 0 8px;
    color: var(--accent);
    font-size: 0.85rem;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
  .preview {
    display: grid;
    gap: 6px;
    justify-items: center;
    padding: 14px;
    overflow: hidden;
    border: 1px dashed var(--line);
    border-radius: 8px;
  }
  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  .test {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .ok {
    color: var(--up);
    font-weight: 600;
  }
  .bad {
    color: var(--down);
    font-weight: 600;
  }

  .rows {
    display: grid;
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .rows li {
    display: flex;
    gap: 8px;
    align-items: flex-start;
    padding: 8px 10px;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 6px;
  }
  .rows li.dragging {
    opacity: 0.4;
  }
  .rows li.over {
    box-shadow: 0 -3px 0 var(--accent);
  }
  .grip {
    flex: none;
    padding: 4px 2px;
    color: var(--muted);
    cursor: grab;
    letter-spacing: -0.15em;
    user-select: none;
  }
  .rbody {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 6px;
  }
  .rhead {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 14px;
  }
  .spacer {
    flex: 1;
  }
  .rfields {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 8px;
  }
  .rfields select,
  .rfields input {
    width: 100%;
    padding: 5px 8px;
  }
  .icon-btn {
    padding: 2px 8px;
    font-size: 0.8rem;
  }
  .icon-btn:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .add {
    justify-self: start;
    max-width: 100%;
  }
  @media (max-width: 520px) {
    .two {
      grid-template-columns: 1fr;
    }
  }
</style>
