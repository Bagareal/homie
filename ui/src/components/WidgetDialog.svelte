<script lang="ts">
  import { onMount } from 'svelte'
  import * as api from '../lib/api'
  import { bytes } from '../lib/format'
  import { app, uid } from '../lib/state.svelte'
  import { STYLE_KEYS } from '../lib/themes'
  import { mobile } from '../lib/mobile.svelte'
  import { groupHeight, groupWidth } from '../lib/widgetSize'
  import type { HostData, Row, RowType, Widget, WidgetData, WidgetKind } from '../lib/types'
  import ColorField from './ColorField.svelte'
  import Slider from './Slider.svelte'
  import AddressInput from './AddressInput.svelte'
  import { withDefaultPort } from '../lib/address'
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
    version: 'Version',
    pool: 'Pool',
    drive: 'Drive temperature',
    alerts: 'Alerts',
    apps: 'Apps',
    status: 'UPS status',
    charge: 'Battery charge',
    runtime: 'Runtime left',
    upsload: 'UPS load',
    power: 'Power',
    voltage: 'Voltage',
    model: 'UPS model',
    var: 'Other value',
    clients: 'Connected clients',
    client: 'Client',
    nodes: 'Nodes',
    lxc: 'LXC containers',
    vm: 'Virtual machines',
    guest: 'A container or VM',
    storage: 'Storage',
    backup: 'Last backup',
    quorum: 'Cluster quorum',
    tasks: 'Failed tasks',
  }

  const defaultRows = (kind: WidgetKind): Row[] =>
    kind === 'proxmox'
      ? [
          { id: uid(), type: 'cpu', load: true },
          { id: uid() + 'm', type: 'mem' },
          { id: uid() + 'l', type: 'lxc' },
          { id: uid() + 'v', type: 'vm' },
          { id: uid() + 's', type: 'storage' },
        ]
      : kind === 'nut'
      ? [
          { id: uid(), type: 'status' },
          { id: uid() + 'c', type: 'charge' },
          { id: uid() + 'r', type: 'runtime' },
          { id: uid() + 'l', type: 'upsload' },
        ]
      : kind === 'truenas'
      ? [
          { id: uid(), type: 'pool' },
          { id: uid() + 'a', type: 'alerts' },
          { id: uid() + 'u', type: 'uptime' },
        ]
      : [
          { id: uid(), type: 'cpu', load: true },
          { id: uid() + 'm', type: 'mem' },
        ]

  // svelte-ignore state_referenced_locally
  const saved = widgetId ? app.widget(widgetId) : undefined
  const blank: Widget = {
    id: '',
    name: '',
    kind: 'glances',
    url: '',
    username: '',
    rows: defaultRows('glances'),
    refresh: 5,
    cols: 1,
    heightUnits: 1,
    precise: false,
    height: 0,
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
  // What the extra Glances source reported at the last test: the general address and each node's.
  let foundExtra = $state<{ glances?: api.ProbeResult; nodes?: Record<string, api.ProbeResult> } | null>(null)
  // What the extra UPS source reported at the last test.
  let foundNut = $state<api.ProbeResult | null>(null)

  type Tab = 'source' | 'values' | 'size' | 'look'
  const tabs: [Tab, string][] = [
    ['source', 'Source'],
    ['values', 'Values'],
    ['size', 'Size'],
    ['look', 'Appearance'],
  ]
  // svelte-ignore state_referenced_locally
  let tab = $state<Tab>(saved ? 'values' : 'source')

  // The preview is shrunk to fit its column when the widget is wider than it.
  let stageW = $state(0)
  let naturalH = $state(0)
  // The size in pixels: counted in groups unless the height is set precisely.
  const SIZE_LOCKED = 'Not available on mobile: changing the size here would break the desktop view.'
  const widthPx = $derived(groupWidth(w.cols))
  const heightPx = $derived(w.precise ? w.height : groupHeight(w.heightUnits)) // 0 = fit the content
  const fit = $derived(stageW > 0 ? Math.min(1, (stageW - 30) / widthPx) : 1)

  // The size tab draws the widget next to a group card of the real dashboard,
  // on a strip as wide as the page, all at the same scale.
  let vw = $state(0)
  let diagramW = $state(0)
  const page = $derived.by(() => {
    void [tab, vw]
    const shelf = document.querySelector<HTMLElement>('.shelf')
    return {
      width: document.querySelector<HTMLElement>('.scroller')?.clientWidth ?? vw,
      card: { w: shelf?.offsetWidth ?? 562, h: shelf?.offsetHeight ?? 200 },
    }
  })
  const widgetH = $derived(heightPx > 0 ? heightPx : naturalH)
  // The strip has 8 px of padding on each side; the rest stands for the page width.
  const scale = $derived(page.width > 0 ? Math.max(0, diagramW - 16) / page.width : 0)

  function fail(on: Tab, message: string) {
    tab = on
    error = message
  }

  const nas = $derived(w.kind === 'truenas')
  const ups = $derived(w.kind === 'nut')
  const pve = $derived(w.kind === 'proxmox')
  const secretName = $derived(nas ? 'API key' : pve ? 'token secret' : 'password')
  // Ticked: on save, this widget's look becomes the look of every widget.
  let shareLook = $state(false)
  const global = $derived(app.config!.settings)
  const dirty = $derived(JSON.stringify(w) !== initialJson || shareLook)

  // A new widget chooses its source first; changing it starts over.
  function setKind(kind: WidgetKind) {
    w.kind = kind
    w.rows = defaultRows(kind)
    w.password = ''
    w.glances = undefined
    w.nut = undefined
    found = null
    foundExtra = null
    foundNut = null
    testResult = null
  }
  const addressChanged = $derived(!!saved?.hasPassword && (normalizeUrl(w.url) !== saved.url || (w.username ?? '').trim() !== (saved.username ?? '')))

  // TrueNAS is only ever reached over https (it revokes API keys sent without TLS).
  // The port each program listens on, used when the address has none (TrueNAS: 443, from https).
  const DEFAULT_PORT: Record<WidgetKind, string> = { glances: '61208', proxmox: '8006', truenas: '', nut: '3493' }
  const defaultPort = $derived(DEFAULT_PORT[w.kind])

  function normalizeUrl(raw: string): string {
    const v = raw.trim()
    const full = v && !/^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? `${w.kind === 'truenas' || w.kind === 'proxmox' ? 'https' : w.kind === 'nut' ? 'nut' : 'http'}://${v}` : v
    return withDefaultPort(full, DEFAULT_PORT[w.kind])
  }

  // What the editor previews until the host has been read.
  const sampleNAS: WidgetData = {
    online: true,
    updatedAt: '',
    data: {
      version: 'TrueNAS-25.10.1',
      uptime: '12 days, 3:45:10',
      cpu: 12,
      temp: { label: 'CPU (average)', value: 46 },
      sensors: [{ label: 'CPU (average)', value: 46 }],
      mem: { percent: 48, used: 15 * 2 ** 30, total: 32 * 2 ** 30 },
      load: { min1: 0.52, min5: 0.38, min15: 0.3, cores: 8 },
      pools: [
        { name: 'tank', status: 'ONLINE', healthy: true, percent: 62, used: 8.4 * 2 ** 40, total: 13.6 * 2 ** 40 },
        { name: 'backup', status: 'ONLINE', healthy: true, percent: 93, used: 3.7 * 2 ** 40, total: 4 * 2 ** 40 },
      ],
      drives: [{ name: 'sda', model: 'WDC', pool: 'tank', temp: 38 }],
      alerts: { count: 0 },
    },
  }
  const sampleGlances: WidgetData = {
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
  const sampleUPS: WidgetData = {
    online: true,
    updatedAt: '',
    data: {
      ups: 'myups',
      vars: {
        'ups.status': 'OL CHRG',
        'ups.mfr': 'APC',
        'ups.model': 'Back-UPS ES 700G',
        'battery.charge': '100',
        'battery.runtime': '2100',
        'ups.load': '23',
        'ups.realpower.nominal': '405',
        'input.voltage': '229.0',
        'input.voltage.nominal': '230',
        'output.voltage': '229.0',
      },
      clients: ['127.0.0.1', '192.168.1.20'],
    },
  }
  const samplePVE: WidgetData = {
    online: true,
    updatedAt: '',
    data: {
      version: '8.2.4',
      cpu: 12,
      mem: { percent: 48, used: 15 * 2 ** 30, total: 32 * 2 ** 30 },
      nodes: [{ name: 'pve', online: true, cpu: 12, cores: 8, mem: { percent: 48, used: 15 * 2 ** 30, total: 32 * 2 ** 30 }, uptime: '12 days, 3:45:10' }],
      guests: [
        { id: 100, name: 'web', type: 'lxc', node: 'pve', status: 'running', cpu: 2, mem: { percent: 20, used: 100 * 2 ** 20, total: 512 * 2 ** 20 } },
        { id: 101, name: 'db', type: 'lxc', node: 'pve', status: 'stopped', cpu: 0, mem: { percent: 0, used: 0, total: 2 ** 30 } },
        { id: 200, name: 'nas', type: 'qemu', node: 'pve', status: 'running', cpu: 8, mem: { percent: 60, used: 2.4 * 2 ** 30, total: 4 * 2 ** 30 } },
      ],
      storage: [
        { id: 'local', node: 'pve', status: 'available', percent: 39, used: 39 * 2 ** 30, total: 100 * 2 ** 30 },
        { id: 'local-lvm', node: 'pve', status: 'available', percent: 62, used: 620 * 2 ** 30, total: 1000 * 2 ** 30 },
      ],
      backup: { status: 'ok', end: new Date(Date.now() - 5 * 3600 * 1000).toISOString(), node: 'pve' },
      tasks: { failed: 0 },
      glances: { temp: { label: 'Package id 0', value: 48 }, sensors: [{ label: 'Package id 0', value: 48 }] },
    },
  }
  const sample = $derived(pve ? samplePVE : ups ? sampleUPS : nas ? sampleNAS : sampleGlances)
  const live = $derived(widgetId ? app.widgetData[widgetId] : undefined)
  const previewData = $derived<WidgetData>(
    found
      ? {
          online: true,
          updatedAt: '',
          // What the extra Glances source reported, so its rows preview with real values too.
          data: {
            ...found,
            glances: foundExtra?.glances?.data,
            nut: foundNut?.data,
            glancesNodes: Object.fromEntries(Object.entries(foundExtra?.nodes ?? {}).filter(([, p]) => p.data).map(([n, p]) => [n, p.data!])),
          },
        }
      : live?.online && !addressChanged ? live : sample,
  )
  const previewIsSample = $derived(previewData === sample)

  // Where each node's Glances is, as typed so far.
  const extraNodes = $derived(Object.fromEntries(Object.entries(w.glances?.nodes ?? {}).filter(([, u]) => u.trim())))

  const glancesPayload = () => {
    const g = w.glances
    return g
      ? { url: normalizeHttp(g.url), username: (g.username ?? '').trim(), password: g.password ?? '', nodes: Object.fromEntries(Object.entries(extraNodes).map(([n, u]) => [n, normalizeHttp(u)])) }
      : undefined
  }

  // The Test button of the Glances section: it tests only Glances, and the result
  // stays under it.
  let testingGlances = $state(false)
  let glancesError = $state('')
  async function testGlances() {
    const g = glancesPayload()
    if (!g) return
    foundExtra = null
    glancesError = ''
    if (!g.url && Object.keys(g.nodes ?? {}).length === 0) {
      glancesError = 'Enter the Glances address first.'
      return
    }
    testingGlances = true
    try {
      const r = await api.testWidget({ id: widgetId ?? undefined, kind: w.kind, url: normalizeUrl(w.url), username: (w.username ?? '').trim(), password: w.password ?? '', only: 'glances', glances: g })
      foundExtra = r.extra ?? null
    } catch (e) {
      glancesError = (e as Error).message
    } finally {
      testingGlances = false
    }
  }

  // The Test button of the main source. `both` (an existing widget, on opening)
  // reads Glances too, so its values can be picked right away.
  async function test(silent = false, both = false) {
    error = ''
    if (!silent) testResult = null
    const url = normalizeUrl(w.url)
    if (!url) return void (silent || (testResult = { ok: false, text: 'Enter the address first.' }))
    testing = true
    try {
      const r = await api.testWidget({
        id: widgetId ?? undefined,
        kind: w.kind,
        url,
        username: (w.username ?? '').trim(),
        password: w.password ?? '',
        glances: both ? glancesPayload() : undefined,
        nut: both && w.nut?.url.trim() ? { url: normalizeNut(w.nut.url) } : undefined,
      })
      if (both) {
        foundExtra = r.extra ?? null
        foundNut = r.extra?.nut ?? null
      }
      if (r.ok) {
        found = r.data ?? null
        testResult = { ok: true, text: `Connected${r.version ? ` (${ups ? 'UPS: ' : nas || pve ? '' : 'Glances '}${r.version})` : ''}` }
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
    if (saved) test(true, true)
  })

  async function requestClose() {
    if (!dirty) return dialog.close()
    const choice = await app.confirmUnsaved('widget')
    if (choice === 'discard') dialog.close()
    else if (choice === 'save') form.requestSubmit()
  }

  // ---- the extra UPS source

  const normalizeNut = (raw: string) => {
    const v = (raw ?? '').trim()
    return withDefaultPort(v && !/^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? `nut://${v}` : v, '3493')
  }

  async function toggleNut(on: boolean) {
    if (on) {
      w.nut = { url: '' }
      return
    }
    const mine = w.rows.filter((r) => r.source === 'nut').length
    if (mine > 0) {
      const ok = await app.confirm({
        title: 'Turn off the UPS',
        message: `This also removes the ${mine} value${mine === 1 ? '' : 's'} read from the UPS.`,
        confirmLabel: 'Turn off',
        danger: true,
      })
      if (!ok) return
    }
    w.rows = w.rows.filter((r) => r.source !== 'nut')
    w.nut = undefined
    foundNut = null
  }

  // The Test button of the UPS section: it tests only the UPS.
  let testingNut = $state(false)
  let nutError = $state('')
  async function testNut() {
    foundNut = null
    nutError = ''
    const url = normalizeNut(w.nut?.url ?? '')
    if (!url) {
      nutError = 'Enter the UPS address first.'
      return
    }
    testingNut = true
    try {
      const r = await api.testWidget({ kind: w.kind, url: normalizeUrl(w.url), username: '', password: '', only: 'nut', nut: { url } })
      foundNut = r.extra?.nut ?? null
    } catch (e) {
      nutError = (e as Error).message
    } finally {
      testingNut = false
    }
  }

  // ---- the extra Glances source

  function normalizeHttp(raw: string): string {
    const v = (raw ?? '').trim()
    return withDefaultPort(v && !/^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? `http://${v}` : v, '61208')
  }

  async function toggleExtra(on: boolean) {
    if (on) {
      w.glances = { url: '', username: '', password: '', nodes: {} }
      return
    }
    const mine = w.rows.filter((r) => r.source === 'glances').length
    if (mine > 0) {
      const ok = await app.confirm({
        title: 'Turn off Glances',
        message: `This also removes the ${mine} value${mine === 1 ? '' : 's'} read from Glances.`,
        confirmLabel: 'Turn off',
        danger: true,
      })
      if (!ok) return
    }
    w.rows = w.rows.filter((r) => r.source !== 'glances')
    w.glances = undefined
    foundExtra = null
  }

  function setNodeUrl(node: string, url: string) {
    if (!w.glances) return
    w.glances.nodes = { ...(w.glances.nodes ?? {}), [node]: url }
  }

  // The nodes of a cluster: the ones found, plus any that already have an address.
  const nodeNames = $derived([...new Set([...(found?.nodes ?? []).map((n) => n.name), ...Object.keys(w.glances?.nodes ?? {})])].sort())
  const poolNames = $derived([...new Set((found?.guests ?? []).map((g) => g.pool).filter((p): p is string => !!p))].sort())
  const tagNames = $derived([...new Set((found?.guests ?? []).flatMap((g) => g.tags ?? []))].sort())
  const storeIds = $derived([...new Set((found?.storage ?? []).map((s) => s.id))])

  // What a row can pick from: the main source's data, or the extra Glances source's.
  function srcData(r: Row): HostData | null | undefined {
    if (r.source === 'nut') return foundNut?.data
    if (r.source !== 'glances') return found
    return r.node ? foundExtra?.nodes?.[r.node]?.data : foundExtra?.glances?.data
  }
  // One entry per Glances server that answered: the general one and each node's.
  const glancesTargets = $derived.by(() => {
    const out: { node: string; label: string; data: HostData }[] = []
    if (foundExtra?.glances?.data) out.push({ node: '', label: 'Glances', data: foundExtra.glances.data })
    for (const [node, p] of Object.entries(foundExtra?.nodes ?? {})) if (p.data) out.push({ node, label: `Glances · ${node}`, data: p.data })
    return out
  })

  // ---- rows

  const diskLabel = (d: { mount: string; used: number; total: number }) => `${d.mount} · ${bytes(d.used)} of ${bytes(d.total)}`
  const tempLabel = (t: { label: string; value: number }) => `${t.label} · ${Math.round(t.value)} °C`

  // The "add" menu's values are "type" or "type:key".
  function addRow(value: string) {
    if (!value) return
    let source: Row['source']
    let node: string | undefined
    if (value.startsWith('nut/')) {
      source = 'nut'
      value = value.slice('nut/'.length)
    } else if (value.startsWith('glances')) {
      const slash = value.indexOf('/')
      const head = value.slice(0, slash)
      source = 'glances'
      node = head.startsWith('glances@') ? head.slice('glances@'.length) : undefined
      value = value.slice(slash + 1)
    }
    const at = value.indexOf(':')
    const type = (at < 0 ? value : value.slice(0, at)) as RowType
    const row: Row = { id: uid(), type }
    if (source) row.source = source
    if (node) row.node = node
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
    if (!w.name.trim()) return fail('source', 'Enter a name for the widget.')
    try {
      const u = new URL(url)
      if (ups ? u.protocol !== 'nut:' : u.protocol !== 'http:' && u.protocol !== 'https:') throw new Error()
      if (pve && !(w.username ?? '').includes('@')) return fail('source', 'The API token ID looks like user@realm!name, for example root@pam!homie.')
      if (pve && !(w.username ?? '').includes('!')) return fail('source', 'The API token ID looks like user@realm!name, for example root@pam!homie.')
      if (nas && u.protocol !== 'https:') return fail('source', 'A TrueNAS address must start with https:// (TrueNAS revokes an API key sent over plain http).')
    } catch {
      return fail('source', `Enter a valid address, for example ${ups ? 'nut://192.168.1.10' : nas ? 'https://truenas.local' : 'http://192.168.1.10:61208'}.`)
    }
    if (w.rows.length === 0) return fail('values', 'Add at least one value to show.')
    if (w.rows.some((r) => r.type === 'client' && !(r.key ?? '').trim())) return fail('values', 'A client needs its address (the connected ones are listed once the connection is tested).')
    // The extra source: addresses tidied up; with nothing to read it is simply off.
    let glances: Widget['glances']
    if (w.glances) {
      const g = $state.snapshot(w.glances)
      const nodes = Object.fromEntries(Object.entries(g.nodes ?? {}).filter(([, u]) => u.trim()).map(([n, u]) => [n, normalizeHttp(u)]))
      const gurl = normalizeHttp(g.url)
      for (const raw of [gurl, ...Object.values(nodes)]) {
        try {
          if (raw && !['http:', 'https:'].includes(new URL(raw).protocol)) throw new Error()
        } catch {
          return fail('source', `The Glances address “${raw}” is not valid, for example http://192.168.1.10:61208.`)
        }
      }
      if (gurl || Object.keys(nodes).length) glances = { ...g, url: gurl, username: (g.username ?? '').trim(), nodes }
    }
    let nut: Widget['nut']
    if (w.nut && w.nut.url.trim()) {
      const nurl = normalizeNut(w.nut.url)
      try {
        if (new URL(nurl).protocol !== 'nut:') throw new Error()
      } catch {
        return fail('source', `The UPS address “${nurl}” is not valid, for example nut://192.168.1.10.`)
      }
      nut = { url: nurl }
    }
    const out: Widget = {
      ...$state.snapshot(w),
      glances,
      nut,
      id: saved?.id ?? uid(),
      name: w.name.trim(),
      url,
      username: (w.username ?? '').trim(),
      // Changing the address or username drops the stored password on the server.
      clearPassword: !!w.clearPassword || (addressChanged && !w.password),
      rows: w.rows.filter((r) => (r.source !== 'glances' || glances) && (r.source !== 'nut' || nut)).map((r) => ({ ...$state.snapshot(r), name: (r.name ?? '').trim() })),
    }
    if (shareLook) {
      // Its own settings move to Settings → Widgets; the widget then simply follows them.
      const style = Object.fromEntries(STYLE_KEYS.map((k) => [k, out[k]]).filter(([, v]) => v !== undefined && v !== ''))
      for (const k of STYLE_KEYS) delete out[k]
      app.upsertWidget(out)
      app.shareWidgetStyle(style)
    } else app.upsertWidget(out)
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
  bind:innerWidth={vw}
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

    <div class="body">
      <aside class="stage" bind:clientWidth={stageW} aria-label="Preview">
        <div class="scaler" style:width="{widthPx * fit}px" style:height="{naturalH * fit}px">
          <div class="natural" bind:offsetHeight={naturalH} style:width="{widthPx}px" style:transform="scale({fit})">
            <WidgetView widget={w} data={previewData} preview />
          </div>
        </div>
        {#if fit < 1}<small>Shown at {Math.round(fit * 100)}% to fit.</small>{/if}
        <small>Preview only: its height grows to show every value. On the dashboard the widget keeps the size set in the Size tab.</small>
        {#if previewIsSample}<small>Sample values. Test the connection to see your host's.</small>{/if}
      </aside>

      <div class="main">
        <div class="tabs" role="tablist" aria-label="Widget sections">
          {#each tabs as [id, label] (id)}
            <button type="button" role="tab" class:on={tab === id} aria-selected={tab === id} onclick={() => (tab = id)}>{label}</button>
          {/each}
        </div>
        <div class="panel">
    {#if tab === 'source'}
    <fieldset>
      {#if !saved}
        <label>
          Type
          <select value={w.kind} onchange={(e) => setKind(e.currentTarget.value as WidgetKind)}>
            <option value="proxmox">Proxmox VE (a node or a cluster)</option>
            <option value="glances">Glances (any Linux host)</option>
            <option value="truenas">TrueNAS (25.04 or later)</option>
            <option value="nut">UPS (NUT server)</option>
          </select>
        </label>
      {/if}
      <label>
        Name
        <!-- svelte-ignore a11y_autofocus -->
        <input bind:value={w.name} maxlength="60" placeholder={ups ? 'UPS' : nas ? 'TrueNAS' : pve ? 'Proxmox' : 'Server'} autofocus />
      </label>
      <label>
        {ups ? 'NUT server address' : nas ? 'TrueNAS address' : pve ? 'Proxmox VE address' : 'Glances address'}
        <AddressInput bind:value={w.url} placeholder={ups ? 'nut://192.168.1.10' : nas ? 'https://truenas.local' : pve ? 'https://192.168.1.10' : 'http://192.168.1.10'} port={defaultPort || '443'} />
        {#if pve}
          <small>Any node of the cluster (or the single node), port 8006: it answers for the whole cluster. The certificate is not checked, so Proxmox's own self-signed one works.</small>
        {:else if ups}
          <small>The machine running <code>upsd</code>, as <code>nut://host</code> (port 3493) or <code>nut://host:port</code>. With more than one UPS add its name, <code>nut://host/myups</code>; otherwise the first one is used. Reading needs no password.</small>
        {:else if nas}
          <small>The NAS itself, starting with https:// (for example its IP address), not an address behind a reverse proxy: a proxy that talks plain http to TrueNAS makes it revoke the key. The certificate is not checked, so a self-signed one works.</small>
        {:else}
          <small>The host must run Glances in web server mode (<code>glances -w</code>, port 61208 by default).</small>
        {/if}
      </label>
      {#if !ups}
      <div class="two">
        <label>
          {pve ? 'API token ID' : 'Username'} <small>{pve ? '' : nas ? '(only if the login is refused)' : '(optional)'}</small>
          <input bind:value={w.username} maxlength="100" autocomplete="off" placeholder={pve ? 'root@pam!homie' : ''} />
        </label>
        <label>
          {nas ? 'API key' : pve ? 'API token secret' : 'Password'} {#if !nas && !pve}<small>(optional)</small>{/if}
          <input
            type="password"
            bind:value={w.password}
            maxlength="200"
            autocomplete="new-password"
            placeholder={saved?.hasPassword && !addressChanged ? '(saved, leave blank to keep)' : ''}
          />
        </label>
      </div>
      {/if}
      {#if saved?.hasPassword && !ups}
        {#if addressChanged}
          <small>Changing the address or {pve ? 'token ID' : 'username'} removes the saved {secretName}: enter it again if the host needs one.</small>
        {:else}
          <label class="check"><input type="checkbox" bind:checked={w.clearPassword} /> Remove the saved {secretName}</label>
        {/if}
      {/if}
      {#if pve}
        <small>Create the token in Proxmox: Datacenter → Permissions → API Tokens → Add, for a user that has only the <code>PVEAuditor</code> role on <code>/</code> (with “Privilege Separation” on, give the role to the token itself too). The secret is shown once; it is stored on the Homie server and never sent back to the browser.</small>
      {:else if ups}
        <small>The server must accept connections from the Homie machine: set <code>LISTEN 0.0.0.0 3493</code> in <code>upsd.conf</code> and allow the port in the firewall.</small>
      {:else if nas}
        <small>Create the key in TrueNAS: user menu at the top right → My API Keys → Add API Key, linked to a user that ideally has only the Readonly Admin role. It is stored on the Homie server and never sent back to the browser.</small>
      {:else}
        <small>Glances only supports basic authentication, which is sent unencrypted over plain http. Use it on a trusted network.</small>
      {/if}
      <div class="test">
        <button type="button" onclick={() => test()} disabled={testing}>{testing ? 'Testing…' : w.glances ? `Test ${pve ? 'Proxmox' : nas ? 'TrueNAS' : 'UPS'} connection` : 'Test connection'}</button>
        {#if testResult}<span class:ok={testResult.ok} class:bad={!testResult.ok} role="status">{testResult.text}</span>{/if}
      </div>
      {#if w.kind !== 'glances'}
        <div class="extra">
          <label class="check">
            <input type="checkbox" checked={!!w.glances} onchange={(e) => toggleExtra(e.currentTarget.checked)} />
            Also read this machine's Glances (temperatures, disks, network…)
          </label>
          {#if w.glances}
            <label>
              Glances address
              <AddressInput bind:value={w.glances.url} placeholder="http://192.168.1.10" port="61208" />
              <small>{pve && nodeNames.length > 1 ? 'The node you are connected to, or leave empty and give one per node below.' : 'The machine itself, running Glances in web server mode (glances -w, port 61208). Then press Test connection: the result is shown under these fields.'}</small>
            </label>
            <div class="two">
              <label>
                Username <small>(optional)</small>
                <input bind:value={w.glances.username} maxlength="100" autocomplete="off" />
              </label>
              <label>
                Password <small>(optional)</small>
                <input type="password" bind:value={w.glances.password} maxlength="200" autocomplete="new-password" placeholder={saved?.glances?.hasPassword ? '(saved, leave blank to keep)' : ''} />
              </label>
            </div>
            {#if saved?.glances?.hasPassword}
              <label class="check"><input type="checkbox" bind:checked={w.glances.clearPassword} /> Remove the saved Glances password</label>
            {/if}
            {#if pve && nodeNames.length > 1}
              <small>A cluster has one Glances per node: give each node's address (the same login is used for all of them). Leave a node empty to skip it.</small>
              {#each nodeNames as n (n)}
                <label class="node">
                  {n}
                  <input value={w.glances.nodes?.[n] ?? ''} oninput={(e) => setNodeUrl(n, e.currentTarget.value)} placeholder="http://… (optional)" inputmode="url" />
                </label>
              {/each}
            {:else if pve && !found}
              <small>With a cluster, test the connection first: each node's Glances address can then be entered here.</small>
            {/if}
            <small>Glances only supports basic authentication, which is sent unencrypted over plain http. Use it on a trusted network.</small>
            <div class="test">
              <button type="button" onclick={testGlances} disabled={testingGlances}>{testingGlances ? 'Testing…' : 'Test Glances'}</button>
              {#if glancesError}<span class="bad" role="status">{glancesError}</span>{/if}
            </div>
            {#if foundExtra}
              <ul class="probe">
                {#if foundExtra.glances}
                  <li class:ok={foundExtra.glances.ok} class:bad={!foundExtra.glances.ok}>{foundExtra.glances.ok ? `Glances: connected (${foundExtra.glances.version})` : `Glances: ${foundExtra.glances.error}`}</li>
                {/if}
                {#each Object.entries(foundExtra.nodes ?? {}) as [n, p] (n)}
                  <li class:ok={p.ok} class:bad={!p.ok}>{n}: {p.ok ? `connected (${p.version})` : p.error}</li>
                {/each}
              </ul>
            {/if}
          {/if}
        </div>
      {/if}
      {#if w.kind !== 'nut'}
        <div class="extra">
          <label class="check">
            <input type="checkbox" checked={!!w.nut} onchange={(e) => toggleNut(e.currentTarget.checked)} />
            Also read this machine's UPS (NUT server)
          </label>
          {#if w.nut}
            <label>
              NUT server address
              <AddressInput bind:value={w.nut.url} placeholder="nut://192.168.1.10" port="3493" />
              <small>The machine running <code>upsd</code>, as <code>nut://host</code> (port 3493) or <code>nut://host:port</code>; add the UPS name, <code>nut://host/myups</code>, if it has several. Reading needs no password. Press Test UPS: the result is shown under it.</small>
            </label>
            <div class="test">
              <button type="button" onclick={testNut} disabled={testingNut}>{testingNut ? 'Testing…' : 'Test UPS'}</button>
              {#if nutError}<span class="bad" role="status">{nutError}</span>{/if}
            </div>
            {#if foundNut}
              <ul class="probe">
                <li class:ok={foundNut.ok} class:bad={!foundNut.ok}>{foundNut.ok ? `UPS: connected (${foundNut.version})` : `UPS: ${foundNut.error}`}</li>
              </ul>
            {/if}
          {/if}
        </div>
      {/if}
    </fieldset>
    {:else if tab === 'values'}
    <fieldset>
      <small>Drag to reorder (or use the arrows). {pve ? 'Guests, storages and nodes' : ups ? 'Clients and other values' : nas ? 'Pools and drives' : 'Disks, sensors and network cards'} can be chosen once the connection has been tested.</small>
      <ul class="rows">
        {#each w.rows as r, i (r.id)}
          {@const sd = srcData(r)}
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
                {#if r.source === 'glances'}<span class="tag">Glances{r.node ? ` · ${r.node}` : ''}</span>{:else if r.source === 'nut'}<span class="tag">UPS</span>{/if}
                {#if r.type === 'cpu' && !(pve && !r.source)}
                  <label class="check"><input type="checkbox" checked={r.load} onchange={(e) => setCpu(r, 'load', e.currentTarget.checked)} /> Load</label>
                  <label class="check"><input type="checkbox" checked={r.temp} onchange={(e) => setCpu(r, 'temp', e.currentTarget.checked)} /> Temperature</label>
                {/if}
                <span class="spacer"></span>
                <button type="button" class="icon-btn" aria-label="Move up" disabled={i === 0} onclick={() => move(i, i - 1)}>▲</button>
                <button type="button" class="icon-btn" aria-label="Move down" disabled={i === w.rows.length - 1} onclick={() => move(i, i + 1)}>▼</button>
                <button type="button" class="icon-btn danger" aria-label="Remove" onclick={() => w.rows.splice(i, 1)}>✕</button>
              </div>
              <div class="rfields">
                {#if pve && !r.source && (r.type === 'cpu' || r.type === 'mem' || r.type === 'uptime')}
                  {@render keyOptions(r, 'Every node', [...(r.type === 'uptime' ? [] : [{ value: '*', label: 'The whole cluster' }]), ...nodeNames.map((n) => ({ value: n, label: `Node ${n}` }))])}
                {:else if r.type === 'lxc' || r.type === 'vm'}
                  {@render keyOptions(r, 'Everything', [
                    ...nodeNames.map((n) => ({ value: `node:${n}`, label: `Node ${n}` })),
                    ...poolNames.map((p) => ({ value: `pool:${p}`, label: `Pool ${p}` })),
                    ...tagNames.map((t) => ({ value: `tag:${t}`, label: `Tag ${t}` })),
                  ])}
                {:else if r.type === 'guest'}
                  {@render keyOptions(r, 'Pick one…', (found?.guests ?? []).filter((g) => !g.template).map((g) => ({ value: String(g.id), label: `${g.id} · ${g.name} (${g.type === 'lxc' ? 'LXC' : 'VM'}, ${g.node})` })))}
                {:else if r.type === 'storage'}
                  {@render keyOptions(r, 'All storages', storeIds.map((s) => ({ value: s, label: s })))}
                {:else if r.type === 'temp' || (r.type === 'cpu' && r.temp)}
                  {@render keyOptions(r, 'CPU (automatic)', (sd?.sensors ?? []).map((s) => ({ value: s.label, label: tempLabel(s) })))}
                {:else if r.type === 'disk'}
                  {@render keyOptions(r, 'All disks', (sd?.disks ?? []).map((d) => ({ value: d.mount, label: diskLabel(d) })))}
                {:else if r.type === 'pool'}
                  {@render keyOptions(r, 'All pools', (found?.pools ?? []).map((p) => ({ value: p.name, label: `${p.name} · ${p.status}` })))}
                {:else if r.type === 'drive'}
                  {@render keyOptions(r, 'Hottest drive', (found?.drives ?? []).map((d) => ({ value: d.name, label: `${d.name}${d.model ? ` · ${d.model}` : ''}${d.temp !== undefined ? ` · ${Math.round(d.temp)} °C` : ''}` })))}
                {:else if r.type === 'network'}
                  {@render keyOptions(r, 'All physical cards (total)', (sd?.network ?? []).map((n) => ({ value: n.name, label: n.virtual ? `${n.name} (virtual)` : n.bridge ? `${n.name} (bridge)` : n.name })))}
                {:else if r.type === 'voltage'}
                  <select value={r.key === 'output' ? 'output' : 'input'} onchange={(e) => (r.key = e.currentTarget.value === 'output' ? 'output' : undefined)} aria-label="Which voltage">
                    <option value="input">Input (mains)</option>
                    <option value="output">Output</option>
                  </select>
                {:else if r.type === 'var'}
                  {@render keyOptions(r, 'Pick a value…', Object.entries(sd?.vars ?? {}).map(([k, v]) => ({ value: k, label: `${k} · ${v}` })))}
                {:else if r.type === 'client'}
                  <input bind:value={r.key} maxlength="100" list="clients-{r.id}" placeholder="Address, e.g. 192.168.1.20" aria-label="Address of the client" />
                  <datalist id="clients-{r.id}">
                    {#each sd?.clients ?? [] as c (c)}<option value={c}></option>{/each}
                  </datalist>
                  <select bind:value={r.role} aria-label="Role of the client">
                    <option value={undefined}>Role not set</option>
                    <option value="primary">Primary</option>
                    <option value="secondary">Secondary</option>
                  </select>
                {/if}
                <input
                  bind:value={r.name}
                  maxlength="60"
                  placeholder={r.key || typeLabels[r.type]}
                  aria-label="Name shown for this value"
                  disabled={(r.type === 'disk' || r.type === 'pool' || r.type === 'var') && !r.key}
                  title={(r.type === 'disk' || r.type === 'pool' || r.type === 'var') && !r.key ? 'Pick one to give it a name' : ''}
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
        {#if pve}
          <option value="nodes">Nodes (state, CPU, memory)</option>
          <option value="cpu">CPU</option>
          <option value="mem">Memory</option>
          <option value="uptime">Uptime</option>
          <option value="lxc">LXC containers</option>
          <option value="vm">Virtual machines</option>
          <option value="storage">Storage</option>
          <option value="backup">Last backup</option>
          <option value="quorum">Cluster quorum</option>
          <option value="tasks">Failed tasks (24 h)</option>
          <option value="version">Proxmox version</option>
          {#if found?.guests?.length}
            <optgroup label="A container or VM">
              {#each found.guests.filter((g) => !g.template) as g (g.id)}<option value="guest:{g.id}">{g.id} · {g.name} ({g.type === 'lxc' ? 'LXC' : 'VM'})</option>{/each}
            </optgroup>
          {/if}
          {#if found?.storage?.length}
            <optgroup label="Detected storages">
              {#each storeIds as s (s)}<option value="storage:{s}">{s}</option>{/each}
            </optgroup>
          {/if}
        {:else if ups}
          <option value="status">UPS status</option>
          <option value="charge">Battery charge</option>
          <option value="runtime">Runtime left</option>
          <option value="upsload">UPS load</option>
          <option value="power">Power</option>
          <option value="voltage">Input voltage</option>
          <option value="voltage:output">Output voltage</option>
          <option value="model">UPS model</option>
          <option value="clients">Connected clients (count)</option>
          <option value="client">A client machine…</option>
          {#if found?.clients?.length}
            <optgroup label="Detected clients">
              {#each found.clients as c (c)}<option value="client:{c}">{c}</option>{/each}
            </optgroup>
          {/if}
          {#if found?.vars}
            <optgroup label="Other values of the UPS">
              {#each Object.entries(found.vars) as [k, v] (k)}<option value="var:{k}">{k} · {v}</option>{/each}
            </optgroup>
          {/if}
        {:else if nas}
          <option value="pool">All pools</option>
          <option value="alerts">Alerts</option>
          <option value="apps">Apps</option>
          <option value="drive">Hottest drive</option>
          <option value="cpu">CPU</option>
          <option value="mem">Memory</option>
          <option value="load">Load average</option>
          <option value="uptime">Uptime</option>
          <option value="version">TrueNAS version</option>
          {#if found?.pools?.length}
            <optgroup label="Detected pools">
              {#each found.pools as p (p.name)}<option value="pool:{p.name}">{p.name} · {p.status}</option>{/each}
            </optgroup>
          {/if}
          {#if found?.drives?.length}
            <optgroup label="Detected drives">
              {#each found.drives as d (d.name)}<option value="drive:{d.name}">{d.name}{d.model ? ` · ${d.model}` : ''}</option>{/each}
            </optgroup>
          {/if}
        {:else}
          <option value="cpu">CPU</option>
          <option value="mem">Memory</option>
          <option value="swap">Swap</option>
          <option value="load">Load average</option>
          <option value="uptime">Uptime</option>
          <option value="temp">Temperature (CPU)</option>
          <option value="disk">All disks</option>
          <option value="network">Network (all physical cards)</option>
        {/if}
        {#if foundNut?.data}
          <optgroup label="UPS (NUT)">
            <option value="nut/status">UPS status</option>
            <option value="nut/charge">Battery charge</option>
            <option value="nut/runtime">Runtime left</option>
            <option value="nut/upsload">UPS load</option>
            <option value="nut/power">Power</option>
            <option value="nut/voltage">Input voltage</option>
            <option value="nut/voltage:output">Output voltage</option>
            <option value="nut/model">UPS model</option>
            <option value="nut/clients">Connected clients (count)</option>
            {#each foundNut.data.clients ?? [] as c (c)}<option value="nut/client:{c}">Client {c}</option>{/each}
            {#each Object.entries(foundNut.data.vars ?? {}) as [k, v] (k)}<option value="nut/var:{k}">{k} · {v}</option>{/each}
          </optgroup>
        {/if}
        {#each glancesTargets as t (t.node)}
          {@const p = t.node ? `glances@${t.node}/` : 'glances/'}
          <optgroup label={t.label}>
            <option value="{p}cpu">CPU</option>
            <option value="{p}temp">CPU temperature</option>
            <option value="{p}mem">Memory</option>
            <option value="{p}swap">Swap</option>
            <option value="{p}load">Load average</option>
            <option value="{p}uptime">Uptime</option>
            <option value="{p}disk">All disks</option>
            <option value="{p}network">Network (all physical cards)</option>
            {#each t.data.sensors ?? [] as s (s.label)}<option value="{p}temp:{s.label}">{tempLabel(s)}</option>{/each}
            {#each t.data.disks ?? [] as d (d.mount)}<option value="{p}disk:{d.mount}">{diskLabel(d)}</option>{/each}
            {#each t.data.network ?? [] as n (n.name)}<option value="{p}network:{n.name}">{n.name}</option>{/each}
          </optgroup>
        {/each}
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
    {:else if tab === 'size'}
    <fieldset>
      <!-- On a phone the size cannot be set: it would only break the desktop view. -->
      <div class="size-lock" class:locked={mobile.narrow} title={mobile.narrow ? SIZE_LOCKED : undefined}>
      <div class="size-controls" inert={mobile.narrow}>
      <Slider label="Width (groups)" unit="" min={1} max={3} value={w.cols} oninput={(v) => (w.cols = v)} oncommit={noop} />
      {#if w.precise}
        <Slider label="Height (0 = fit content)" unit="px" min={0} max={1000} value={w.height} oninput={(v) => (w.height = v)} oncommit={noop} />
      {:else}
        <Slider label="Height (groups)" unit="" min={1} max={3} value={w.heightUnits} oninput={(v) => (w.heightUnits = v)} oncommit={noop} />
      {/if}
      <label class="check" title="Set the height in pixels, for a widget that must not scroll or leave a gap">
        <input
          type="checkbox"
          checked={w.precise}
          onchange={(e) => {
            w.precise = e.currentTarget.checked
            // Start from the size it has now.
            if (w.precise) w.height = Math.round(groupHeight(w.heightUnits))
          }}
        />
        Precise height
      </label>
      </div>
      </div>
      {#if mobile.narrow}<small>{SIZE_LOCKED}</small>{/if}
      <div class="diagram" bind:clientWidth={diagramW} style:height="{Math.max(widgetH, page.card.h) * scale + 16}px" style:gap="{20 * scale}px" aria-hidden="true">
        <div
          class="d-widget"
          style:width="{widthPx * scale}px"
          style:height="{widgetH * scale}px"
          style:background="color-mix(in srgb, {w.color ?? 'var(--widget-color)'} {w.opacity ?? global.widgetOpacity ?? 100}%, transparent)"
          style:border="{Math.max(1, (w.borderWidth ?? global.widgetBorderWidth ?? global.tileBorderWidth ?? 1) * scale)}px solid {w.borderColor ?? 'var(--widget-border)'}"
        >widget</div>
        <div class="d-card" style:width="{page.card.w * scale}px" style:height="{page.card.h * scale}px">group card</div>
      </div>
      <small>
        To scale, on a page {page.width} px wide: this widget is {Math.round(widthPx)} × {Math.round(widgetH)} px, a group card {page.card.w} × {page.card.h} px.
        Drag the widget wherever you like on the dashboard while editing, even between two groups.
      </small>
    </fieldset>
    {:else}
    <fieldset>
      <small>“Default” means the look every widget shares (Settings → Appearance → Widgets). Set a value here only to make this widget different.</small>
      <ColorField label="Background" cssVar="--widget-color" {theme} value={w.color} onpreview={(v) => (w.color = v)} oncommit={noop} />
      <div class="with-reset">
        <Slider label="Background opacity" min={1} value={w.opacity ?? global.widgetOpacity ?? 100} oninput={(v) => (w.opacity = v)} oncommit={noop} />
        {#if w.opacity !== undefined}<button type="button" onclick={() => (w.opacity = undefined)}>Reset</button>{:else}<span class="tag">default</span>{/if}
      </div>
      <ColorField label="Text" cssVar="--widget-text" {theme} value={w.textColor} onpreview={(v) => (w.textColor = v)} oncommit={noop} />
      <ColorField label="Border" cssVar="--widget-border" {theme} value={w.borderColor} onpreview={(v) => (w.borderColor = v)} oncommit={noop} />
      <div class="with-reset">
        <Slider label="Border width" unit="px" min={0} max={8} value={w.borderWidth ?? global.widgetBorderWidth ?? global.tileBorderWidth ?? 1} oninput={(v) => (w.borderWidth = v)} oncommit={noop} />
        {#if w.borderWidth !== undefined}<button type="button" onclick={() => (w.borderWidth = undefined)}>Reset</button>{:else}<span class="tag">default</span>{/if}
      </div>
      <ColorField label="Bars" cssVar="--widget-bar" {theme} value={w.barColor} onpreview={(v) => (w.barColor = v)} oncommit={noop} />
      <label class="check">
        <input type="checkbox" bind:checked={shareLook} />
        Use this look for all widgets
      </label>
      <small>When you save, the values set on this widget become the shared look in Settings, and the other widgets lose their own.</small>
    </fieldset>
    {/if}
        </div>
      </div>
    </div>

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
    width: min(960px, calc(100vw - 32px));
    height: min(800px, calc(100vh - 32px));
    overflow: hidden;
  }
  .dialog.wide form {
    height: 100%;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 18px 20px;
  }
  .body {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: minmax(0, 340px) minmax(0, 1fr);
    gap: 18px;
  }
  .stage {
    align-self: start;
    max-height: 100%;
    overflow: auto;
    display: grid;
    gap: 6px;
    justify-items: center;
    padding: 14px;
    border: 1px dashed var(--line);
    border-radius: 8px;
  }
  .scaler {
    max-width: 100%;
  }
  .natural {
    transform-origin: top left;
  }
  .main {
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .panel {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 16px 6px 4px 0;
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
    padding: 8px 14px;
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
  .diagram {
    position: relative;
    display: flex;
    align-items: flex-start;
    padding: 8px;
    margin-top: 4px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg);
    overflow: hidden;
  }
  .d-card,
  .d-widget {
    display: grid;
    place-items: center;
    overflow: hidden;
    color: var(--muted);
    font-size: 0.7rem;
  }
  .d-card {
    flex: none;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: color-mix(in srgb, var(--shelf) 60%, transparent);
  }
  .d-widget {
    flex: none;
    border-radius: 3px;
    box-sizing: border-box;
  }
  fieldset {
    display: grid;
    gap: 12px;
    margin: 0;
    padding: 0;
    border: none;
  }
  .size-controls {
    display: grid;
    gap: 12px;
  }
  /* Greyed out where the size cannot be set; the wrapper (not the inert controls
     inside it) carries the hover text. */
  .size-lock.locked .size-controls {
    opacity: 0.45;
    filter: grayscale(1);
    cursor: not-allowed;
  }
  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  .extra {
    display: grid;
    gap: 10px;
    padding-top: 12px;
    border-top: 1px solid color-mix(in srgb, currentColor 15%, transparent);
  }
  .node {
    display: grid;
    grid-template-columns: minmax(70px, 120px) 1fr;
    align-items: center;
    gap: 10px;
  }
  .probe {
    margin: 0;
    padding: 0;
    list-style: none;
    display: grid;
    gap: 4px;
    font-size: 0.85rem;
  }
  .probe .ok {
    color: var(--up);
  }
  .probe .bad {
    color: var(--down);
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
  .with-reset {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .tag {
    color: var(--muted);
    font-size: 0.85rem;
  }
  .add {
    justify-self: start;
    max-width: 100%;
  }
  @media (max-width: 760px) {
    .body {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: minmax(0, 34%) minmax(0, 1fr);
    }
    .stage {
      max-height: 100%;
    }
  }
  @media (max-width: 520px) {
    .two {
      grid-template-columns: 1fr;
    }
  }
</style>
