<script lang="ts">
  import { onMount } from 'svelte'
  import * as api from '../lib/api'
  import { app, uid } from '../lib/state.svelte'
  import type { Widget, WidgetData, WidgetMetric } from '../lib/types'
  import ColorField from './ColorField.svelte'
  import Slider from './Slider.svelte'
  import WidgetView from './Widget.svelte'

  let { widgetId, onclose }: { widgetId: string | null; onclose: () => void } = $props()

  let dialog: HTMLDialogElement
  let form: HTMLFormElement

  const metrics: [WidgetMetric, string][] = [
    ['cpu', 'CPU usage'],
    ['mem', 'Memory'],
    ['swap', 'Swap'],
    ['load', 'Load average'],
    ['uptime', 'Uptime'],
    ['temp', 'Temperature'],
    ['disks', 'Disks'],
    ['network', 'Network traffic'],
  ]

  // svelte-ignore state_referenced_locally
  const saved = widgetId ? app.widget(widgetId) : undefined
  const blank: Widget = {
    id: '',
    name: '',
    kind: 'glances',
    url: '',
    username: '',
    metrics: ['cpu', 'mem', 'disks'],
    refresh: 5,
    placement: 'top',
    width: 300,
    height: 0,
    opacity: 100,
  }
  // Edited locally until saved; password is never prefilled.
  const initial: Widget = { ...blank, ...structuredClone($state.snapshot(saved) ?? {}), password: '', clearPassword: false }
  let w = $state<Widget>(structuredClone(initial))
  const mounts0 = (initial.mounts ?? []).join(', ')
  const interfaces0 = (initial.interfaces ?? []).join(', ')
  let mountsText = $state(mounts0)
  let interfacesText = $state(interfaces0)
  const initialJson = JSON.stringify([initial, mounts0, interfaces0])
  let error = $state('')
  let testing = $state(false)
  let testResult = $state<{ ok: boolean; text: string } | null>(null)

  const dirty = $derived(JSON.stringify([w, mountsText, interfacesText]) !== initialJson)
  const addressChanged = $derived(!!saved?.hasPassword && (normalizeUrl(w.url) !== saved.url || (w.username ?? '').trim() !== (saved.username ?? '')))

  const list = (s: string) => s.split(',').map((x) => x.trim()).filter(Boolean)

  function normalizeUrl(raw: string): string {
    const v = raw.trim()
    return v && !/^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? `http://${v}` : v
  }

  function toggle(m: WidgetMetric, on: boolean) {
    // Keep the order the list is shown in.
    w.metrics = metrics.map(([id]) => id).filter((id) => (id === m ? on : w.metrics.includes(id)))
  }

  // What the editor previews until the host has been read once.
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
      disks: [
        { mount: '/', percent: 39, used: 194 * 2 ** 30, total: 498 * 2 ** 30 },
        { mount: '/mnt/data', percent: 92, used: 920 * 2 ** 30, total: 1000 * 2 ** 30 },
      ],
      network: { rx: 1.4 * 2 ** 20, tx: 310 * 2 ** 10 },
    },
  }
  const live = $derived(widgetId ? app.widgetData[widgetId] : undefined)
  const previewData = $derived(live?.online && !addressChanged ? live : sample)
  const previewWidget = $derived<Widget>({ ...w, metrics: w.metrics })

  async function test() {
    error = ''
    testResult = null
    const url = normalizeUrl(w.url)
    if (!url) return void (testResult = { ok: false, text: 'Enter the address first.' })
    testing = true
    try {
      const r = await api.testWidget({ id: widgetId ?? undefined, url, username: (w.username ?? '').trim(), password: w.password ?? '' })
      testResult = r.ok ? { ok: true, text: `Connected${r.version ? ` (Glances ${r.version})` : ''}` } : { ok: false, text: r.error ?? 'Failed' }
    } catch (e) {
      testResult = { ok: false, text: (e as Error).message }
    } finally {
      testing = false
    }
  }

  async function requestClose() {
    if (!dirty) return dialog.close()
    const choice = await app.confirmUnsaved('widget')
    if (choice === 'discard') dialog.close()
    else if (choice === 'save') form.requestSubmit()
  }

  onMount(() => dialog.showModal())

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
    if (w.metrics.length === 0) return void (error = 'Choose at least one value to show.')
    const out: Widget = {
      ...$state.snapshot(w),
      id: saved?.id ?? uid(),
      name: w.name.trim(),
      url,
      username: (w.username ?? '').trim(),
      // Changing the address or username drops the stored password on the server.
      clearPassword: !!w.clearPassword || (addressChanged && !w.password),
      mounts: list(mountsText),
      interfaces: list(interfacesText),
      sensor: (w.sensor ?? '').trim(),
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
      <WidgetView widget={previewWidget} data={previewData} preview />
      {#if previewData === sample}<small>Preview with sample values.</small>{/if}
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
        <button type="button" onclick={test} disabled={testing}>{testing ? 'Testing…' : 'Test connection'}</button>
        {#if testResult}<span class:ok={testResult.ok} class:bad={!testResult.ok} role="status">{testResult.text}</span>{/if}
      </div>
    </fieldset>

    <fieldset>
      <legend>Show</legend>
      <div class="metrics">
        {#each metrics as [id, label] (id)}
          <label class="check">
            <input type="checkbox" checked={w.metrics.includes(id)} onchange={(e) => toggle(id, e.currentTarget.checked)} />
            {label}
          </label>
        {/each}
      </div>
      {#if w.metrics.includes('disks')}
        <label>
          Only these disks <small>(mount points, comma-separated; empty = all)</small>
          <input bind:value={mountsText} placeholder="/, /mnt/data" />
        </label>
      {/if}
      {#if w.metrics.includes('temp')}
        <label>
          Temperature sensor <small>(its name in Glances; empty = the CPU package)</small>
          <input bind:value={w.sensor} placeholder="Package id 0" />
        </label>
      {/if}
      {#if w.metrics.includes('network')}
        <label>
          Only these network cards <small>(comma-separated; empty = the physical ones)</small>
          <input bind:value={interfacesText} placeholder="eno1" />
        </label>
      {/if}
      <Slider label="Refresh every" unit="s" min={2} max={300} value={w.refresh} oninput={(v) => (w.refresh = v)} oncommit={noop} />
    </fieldset>

    <fieldset>
      <legend>Size and position</legend>
      <Slider label="Width" unit="px" min={120} max={1200} value={w.width} oninput={(v) => (w.width = v)} oncommit={noop} />
      <Slider label="Height (0 = fit content)" unit="px" min={0} max={1000} value={w.height} oninput={(v) => (w.height = v)} oncommit={noop} />
      <label>
        Position
        <select bind:value={w.placement}>
          <option value="top">Above the groups</option>
          <option value="bottom">Below the groups</option>
        </select>
      </label>
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
  .metrics {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
    gap: 8px 12px;
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
  @media (max-width: 520px) {
    .two {
      grid-template-columns: 1fr;
    }
  }
</style>
