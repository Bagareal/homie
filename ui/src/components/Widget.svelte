<script lang="ts">
  import { app } from '../lib/state.svelte'
  import { ago, bytes, duration, percent, rate } from '../lib/format'
  import type { HostData, Row, RowType, Widget, WidgetData } from '../lib/types'
  import EditIcon from './EditIcon.svelte'
  import { isCollapsed, setCollapsed } from '../lib/collapse.svelte'
  import { arrange } from '../lib/arrange.svelte'

  let {
    widget,
    data,
    onedit,
    preview = false,
    collapsed: previewCollapsed = false,
    mode = 'normal',
    onopen,
  }: {
    widget: Widget
    data: WidgetData | undefined
    onedit?: () => void
    // Rendered inside the editor: not draggable or clickable.
    preview?: boolean
    // In the editor's preview: show the collapsed view.
    collapsed?: boolean
    // In the strip a widget is a compact tile (always the collapsed view) that opens
    // its full view on a click; "pop" is that full view.
    mode?: 'normal' | 'strip' | 'pop'
    onopen?: (el: HTMLElement) => void
  } = $props()

  let drop = $state<'before' | 'after' | null>(null)
  let vertical = $state(false)
  const editable = $derived(app.editing && !preview)
  // Collapsed: only a few values, side by side. Never while editing.
  const folded = $derived(mode === 'strip' ? true : mode === 'pop' ? false : preview ? previewCollapsed : editable ? false : isCollapsed(widget.id, app.config?.settings.widgetCollapseBelow))

  const style = $derived(
    [
      // One group card wide; as tall as its values need unless a height is set.
      '--w-width:var(--card-w)',
      // (not in the strip, where a tile is as tall as its few values)
      mode === 'normal' && widget.precise && widget.height > 0 ? `--w-height:${widget.height}px` : '',
      // Only what this widget sets itself; the rest follows the global widget look.
      widget.color ? `--w-color:${widget.color}` : '',
      widget.opacity !== undefined ? `--w-opacity:${widget.opacity}` : '',
      widget.textColor ? `--w-text:${widget.textColor}` : '',
      widget.borderColor ? `--w-border:${widget.borderColor}` : '',
      widget.borderWidth !== undefined ? `--w-border-w:${widget.borderWidth}px` : '',
      widget.barColor ? `--w-bar:${widget.barColor}` : '',
    ]
      .filter(Boolean)
      .join(';'),
  )

  type Line = { key: string; label: string; value: string; pct?: number; hot?: boolean; bad?: boolean; title?: string }

  const labels: Record<RowType, string> = {
    cpu: 'CPU',
    mem: 'Memory',
    swap: 'Swap',
    load: 'Load',
    uptime: 'Uptime',
    temp: 'Temperature',
    disk: 'Disk',
    network: 'Network',
    version: 'Version',
    pool: 'Pool',
    drive: 'Drive',
    alerts: 'Alerts',
    apps: 'Apps',
    status: 'Status',
    charge: 'Battery',
    runtime: 'Runtime',
    upsload: 'Load',
    power: 'Power',
    voltage: 'Input voltage',
    model: 'UPS',
    var: 'Value',
    clients: 'Clients',
    client: 'Client',
    nodes: 'Nodes',
    lxc: 'LXC',
    vm: 'VMs',
    guest: 'Guest',
    storage: 'Storage',
    backup: 'Backup',
    quorum: 'Quorum',
    tasks: 'Failed tasks',
    datastore: 'Datastore',
    group: 'Backup group',
  }

  // ups.status is a list of flags: OL OB LB CHRG DISCHRG RB ...
  const STATUS: Record<string, string> = {
    OL: 'Online',
    OB: 'On battery',
    LB: 'Low battery',
    HB: 'High battery',
    CHRG: 'Charging',
    DISCHRG: 'Discharging',
    RB: 'Replace battery',
    OVER: 'Overloaded',
    TRIM: 'Trimming',
    BOOST: 'Boosting',
    BYPASS: 'Bypass',
    CAL: 'Calibrating',
    OFF: 'Off',
    ALARM: 'Alarm',
    FSD: 'Shutting down',
  }
  const BAD_STATUS = ['OB', 'LB', 'RB', 'OVER', 'ALARM', 'FSD', 'OFF']

  // The same address however the server or the user wrote it.
  const sameHost = (a: string) => {
    const v = a.trim().toLowerCase().replace(/^::ffff:/, '')
    return v === '::1' || v === 'localhost' ? '127.0.0.1' : v
  }

  // A temperature has no natural 100%: the bar fills to 100 °C and warns from 80.
  const tempLine = (key: string, label: string, t: { label: string; value: number }): Line => ({
    key,
    label,
    value: `${Math.round(t.value)} °C`,
    pct: t.value,
    hot: t.value >= 80,
    title: t.label,
  })

  function sensorFor(d: HostData, key: string | undefined) {
    if (!key) return d.temp
    return d.sensors?.find((s) => s.label.toLowerCase() === key.toLowerCase())
  }

  // Proxmox: the guests a row counts. The key is empty (all), node:x, pool:x or tag:x.
  function guestsFor(d: HostData, type: 'lxc' | 'qemu', key: string | undefined) {
    const [what, ...rest] = (key ?? '').split(':')
    const v = rest.join(':')
    return (d.guests ?? []).filter(
      (g) =>
        g.type === type &&
        !g.template &&
        (!key || (what === 'node' ? g.node === v : what === 'pool' ? g.pool === v : what === 'tag' ? !!g.tags?.includes(v) : true)),
    )
  }

  // Proxmox: a CPU, memory or uptime row is about one node, every node, or the whole cluster.
  function nodeLines(r: Row, d: HostData, label: string): Line[] {
    const all = d.nodes ?? []
    const total = r.key === '*' && r.type !== 'uptime'
    const picked = total ? [] : r.key && r.key !== '*' ? all.filter((n) => n.name === r.key) : all
    const na = (l = label): Line => ({ key: r.id, label: l, value: 'n/a' })
    if (total) {
      if (r.type === 'cpu') return d.cpu === undefined ? [na()] : [{ key: r.id, label, value: percent(d.cpu), pct: d.cpu, hot: d.cpu >= 90, title: 'Average of the nodes that are online' }]
      return d.mem ? [{ key: r.id, label, value: `${bytes(d.mem.used)} / ${bytes(d.mem.total)}`, pct: d.mem.percent, hot: d.mem.percent >= 90, title: 'Total of the nodes that are online' }] : [na()]
    }
    if (!picked.length) return [na()]
    return picked.map((n): Line => {
      const l = picked.length > 1 ? n.name : r.key && r.key !== '*' && !r.name ? `${label} · ${n.name}` : label
      const key = `${r.id}:${n.name}`
      if (!n.online) return { key, label: l, value: 'offline', bad: true, title: `${n.name} is not online` }
      if (r.type === 'cpu') return { key, label: l, value: percent(n.cpu ?? 0), pct: n.cpu ?? 0, hot: (n.cpu ?? 0) >= 90, title: n.cores ? `${n.name}, ${n.cores} cores` : n.name }
      if (r.type === 'mem') return n.mem ? { key, label: l, value: `${bytes(n.mem.used)} / ${bytes(n.mem.total)}`, pct: n.mem.percent, hot: n.mem.percent >= 90, title: n.name } : na(l)
      return n.uptime ? { key, label: l, value: n.uptime, title: n.name } : na(l)
    })
  }

  // One or more lines per row (a disk row without a disk expands to all of them).
  const lines = $derived.by((): Line[] => {
    if (!data?.online) return []
    const out: Line[] = []
    for (const r of widget.rows) {
      const label = r.name || labels[r.type]
      // Rows of the extra Glances source read their own data (a cluster has one per node).
      const fromGlances = r.source === 'glances'
      const fromNUT = r.source === 'nut'
      const d: HostData | undefined = fromNUT ? data.data.nut : fromGlances ? (r.node ? data.data.glancesNodes?.[r.node] : data.data.glances) : data.data
      const note = (type: string) =>
        (fromNUT ? data?.notes?.nut : fromGlances ? data?.notes?.[r.node ? `glances:${r.node}` : 'glances'] : undefined) ?? data?.notes?.[type]
      // The reading groups are not always named like the rows: say why a value is missing.
      const reason = widget.kind === 'pbs' && ['cpu', 'mem', 'uptime'].includes(r.type) ? 'host' : r.type === 'backup' ? 'tasks' : r.type === 'quorum' ? 'cluster' : r.type === 'mem' ? 'memory' : r.type
      const na = (): Line => ({ key: r.id, label, value: 'n/a', title: note(reason) })
      if (!d) {
        out.push(na())
        continue
      }
      if (widget.kind === 'proxmox' && !fromGlances && !fromNUT && (r.type === 'cpu' || r.type === 'mem' || r.type === 'uptime')) {
        out.push(...nodeLines(r, d, label))
        continue
      }
      switch (r.type) {
        case 'cpu': {
          const t = r.temp ? sensorFor(d, r.key) : undefined
          const load = r.load ? d.cpu : undefined
          // Show what is there; n/a only when nothing is.
          if (load === undefined && !t) {
            out.push(na())
            break
          }
          const parts = [t ? `${Math.round(t.value)} °C` : '', load !== undefined ? percent(load) : ''].filter(Boolean)
          out.push({
            key: r.id,
            label,
            value: parts.join('   '),
            // The bar follows the load; with the temperature alone it follows that.
            pct: load !== undefined ? load : t?.value,
            hot: load !== undefined ? load >= 90 : (t?.value ?? 0) >= 80,
            title: t ? t.label : undefined,
          })
          break
        }
        case 'mem':
        case 'swap': {
          const u = d[r.type]
          if (!u) out.push(na())
          else if (u.total === 0) out.push({ key: r.id, label, value: 'none' })
          else out.push({ key: r.id, label, value: `${bytes(u.used)} / ${bytes(u.total)}`, pct: u.percent, hot: u.percent >= 90 })
          break
        }
        case 'load':
          out.push(
            d.load
              ? {
                  key: r.id,
                  label,
                  value: [d.load.min1, d.load.min5, d.load.min15].map((n) => n.toFixed(2)).join(' · '),
                  title: `Load average over 1, 5 and 15 minutes (${d.load.cores} cores)`,
                }
              : na(),
          )
          break
        case 'uptime':
          out.push(d.uptime ? { key: r.id, label, value: d.uptime } : na())
          break
        case 'temp': {
          const t = sensorFor(d, r.key)
          out.push(t ? tempLine(r.id, r.name || (r.key ? r.key : labels.temp), t) : na())
          break
        }
        case 'disk': {
          const disks = r.key ? d.disks?.filter((x) => x.mount === r.key) : d.disks
          if (!disks?.length) out.push({ key: r.id, label: r.name || r.key || labels.disk, value: 'n/a' })
          else
            for (const x of disks)
              out.push({
                key: `${r.id}:${x.mount}`,
                label: r.key ? r.name || x.mount : x.mount,
                value: `${bytes(x.used)} / ${bytes(x.total)}`,
                pct: x.percent,
                hot: x.percent >= 90,
                title: x.mount,
              })
          break
        }
        case 'network': {
          // A total counts the physical cards, or the bridges when no card is listed.
          const physical = d.network?.filter((n) => !n.virtual && !n.bridge)
          const nics = r.key ? d.network?.filter((n) => n.name === r.key) : physical?.length ? physical : d.network?.filter((n) => n.bridge)
          const l = r.name || r.key || labels.network
          if (!nics?.length) out.push({ key: r.id, label: l, value: 'n/a' })
          else {
            const rx = nics.reduce((a, n) => a + n.rx, 0)
            const tx = nics.reduce((a, n) => a + n.tx, 0)
            out.push({ key: r.id, label: l, value: `↓ ${rate(rx)}   ↑ ${rate(tx)}` })
          }
          break
        }
        case 'version':
          out.push(d.version ? { key: r.id, label, value: d.version.replace(/^TrueNAS-(SCALE-)?/, ''), title: d.version } : na())
          break
        case 'pool': {
          const pools = r.key ? d.pools?.filter((p) => p.name === r.key) : d.pools
          if (!pools?.length) out.push({ key: r.id, label: r.name || r.key || labels.pool, value: 'n/a' })
          else
            for (const p of pools) {
              const s = p.scan
              const scrub = s?.function
                ? `${s.function === 'SCRUB' ? 'Last scrub' : s.function}: ${(s.state ?? '').toLowerCase()}${s.end ? ` on ${new Date(s.end).toLocaleDateString()}` : ''}, ${s.errors ?? 0} errors`
                : 'No scrub yet'
              // The temperature next to the capacity: the drive picked, else the hottest of the pool.
              const mine = r.temp ? (d.drives ?? []).filter((x) => x.pool === p.name && x.temp !== undefined) : []
              const drive = mine.find((x) => x.name === r.drive) ?? mine.reduce<(typeof mine)[number] | undefined>((a, x) => (!a || x.temp! > a.temp! ? x : a), undefined)
              out.push({
                key: `${r.id}:${p.name}`,
                label: r.key ? r.name || p.name : p.name,
                value: `${p.healthy ? '' : `${p.status} · `}${bytes(p.used)} / ${bytes(p.total)}${drive ? `   ${Math.round(drive.temp!)} °C` : ''}`,
                pct: p.percent,
                hot: !p.healthy || p.percent >= 90,
                bad: !p.healthy,
                title: `${p.status}. ${scrub}${mine.length ? `. Drives: ${mine.map((x) => `${x.name} ${Math.round(x.temp!)} °C`).join(', ')}` : ''}`,
              })
            }
          break
        }
        case 'drive': {
          const warm = (d.drives ?? []).filter((x) => x.temp !== undefined)
          const pick = r.key ? d.drives?.find((x) => x.name === r.key) : warm.reduce<(typeof warm)[number] | undefined>((a, x) => (!a || x.temp! > a.temp! ? x : a), undefined)
          const l = r.name || r.key || 'Hottest drive'
          if (pick?.temp === undefined) out.push({ key: r.id, label: l, value: 'n/a' })
          else out.push({ key: r.id, label: l, value: `${Math.round(pick.temp)} °C`, pct: pick.temp, hot: pick.temp >= 55, title: `${pick.name}${pick.model ? ` (${pick.model})` : ''}` })
          break
        }
        case 'status': {
          const raw = d.vars?.['ups.status']
          if (raw === undefined) {
            out.push(na())
            break
          }
          const flags = raw.split(/\s+/).filter(Boolean)
          out.push({
            key: r.id,
            label,
            // Charging next to Online is the normal case: say the most important flags first.
            value: flags.map((f) => STATUS[f] ?? f).join(' · ') || 'Unknown',
            bad: flags.some((f) => BAD_STATUS.includes(f)),
            title: raw,
          })
          break
        }
        case 'charge': {
          const c = Number(d.vars?.['battery.charge'])
          if (d.vars?.['battery.charge'] === undefined || Number.isNaN(c)) out.push(na())
          else {
            const bv = d.vars['battery.voltage']
            out.push({ key: r.id, label, value: `${Math.round(c)}%`, pct: c, hot: c <= 20, title: bv ? `Battery voltage ${bv} V` : undefined })
          }
          break
        }
        case 'runtime': {
          const s = Number(d.vars?.['battery.runtime'])
          out.push(d.vars?.['battery.runtime'] === undefined || Number.isNaN(s) ? na() : { key: r.id, label, value: duration(s), bad: s < 300, title: 'Time left on battery at the current load' })
          break
        }
        case 'upsload': {
          const l = Number(d.vars?.['ups.load'])
          out.push(d.vars?.['ups.load'] === undefined || Number.isNaN(l) ? na() : { key: r.id, label, value: `${Math.round(l)}%`, pct: l, hot: l >= 90 })
          break
        }
        case 'power': {
          const real = Number(d.vars?.['ups.realpower'])
          const nominal = Number(d.vars?.['ups.realpower.nominal'])
          const l = Number(d.vars?.['ups.load'])
          if (d.vars?.['ups.realpower'] !== undefined && !Number.isNaN(real)) out.push({ key: r.id, label, value: `${Math.round(real)} W` })
          else if (nominal > 0 && !Number.isNaN(l)) out.push({ key: r.id, label, value: `~${Math.round((nominal * l) / 100)} W`, title: `Estimated: ${Math.round(l)}% of ${nominal} W` })
          else out.push(na())
          break
        }
        case 'voltage': {
          const side = r.key === 'output' ? 'output' : 'input'
          const v = d.vars?.[`${side}.voltage`]
          const nominal = d.vars?.[`${side}.voltage.nominal`]
          out.push(v === undefined ? na() : { key: r.id, label: r.name || (side === 'output' ? 'Output voltage' : 'Input voltage'), value: `${Math.round(Number(v))} V`, title: nominal ? `Nominal ${nominal} V` : undefined })
          break
        }
        case 'model': {
          const v = d.vars ?? {}
          const mfr = v['ups.mfr'] ?? v['device.mfr']
          const text = v['ups.model'] ?? v['device.model']
          out.push(text ? { key: r.id, label, value: text, title: [mfr, v['ups.serial'] ? `serial ${v['ups.serial']}` : ''].filter(Boolean).join(', ') } : na())
          break
        }
        case 'var': {
          const v = r.key ? d.vars?.[r.key] : undefined
          out.push(v === undefined ? { key: r.id, label: r.name || r.key || labels.var, value: 'n/a' } : { key: r.id, label: r.name || r.key!, value: v })
          break
        }
        case 'clients': {
          const list = d.clients ?? []
          out.push({ key: r.id, label, value: String(list.length), title: list.length ? list.join(String.fromCharCode(10)) : 'No machine is connected' })
          break
        }
        case 'client': {
          const want = sameHost(r.key ?? '')
          const here = (d.clients ?? []).some((c) => sameHost(c) === want)
          const role = r.role === 'primary' ? 'Primary' : r.role === 'secondary' ? 'Secondary' : ''
          out.push({
            key: r.id,
            label: r.name || r.key || labels.client,
            value: [role, here ? 'connected' : 'not connected'].filter(Boolean).join(' · '),
            bad: !here,
            title: r.key,
          })
          break
        }
        case 'nodes': {
          const nodes = d.nodes ?? []
          if (!nodes.length) out.push(na())
          for (const n of nodes)
            out.push(
              n.online
                ? { key: `${r.id}:${n.name}`, label: n.name, value: `CPU ${percent(n.cpu ?? 0)} · RAM ${percent(n.mem?.percent ?? 0)}`, pct: n.cpu ?? 0, hot: (n.cpu ?? 0) >= 90, title: n.uptime ? `Up ${n.uptime}` : undefined }
                : { key: `${r.id}:${n.name}`, label: n.name, value: 'offline', bad: true, title: `${n.name} is not online` },
            )
          break
        }
        case 'lxc':
        case 'vm': {
          if (!d.guests) {
            out.push(na())
            break
          }
          const mine = guestsFor(d, r.type === 'lxc' ? 'lxc' : 'qemu', r.key)
          const running = mine.filter((g) => g.status === 'running')
          const stopped = mine.filter((g) => g.status !== 'running')
          const scope = r.key ? ` · ${r.key.slice(r.key.indexOf(':') + 1)}` : ''
          out.push({
            key: r.id,
            label: r.name || `${labels[r.type]}${scope}`,
            value: `${running.length} / ${mine.length}`,
            title: stopped.length ? `Not running: ${stopped.map((g) => `${g.name} (${g.id})`).join(', ')}` : mine.length ? 'All running' : 'None',
          })
          break
        }
        case 'guest': {
          const g = d.guests?.find((x) => String(x.id) === r.key)
          if (!g) {
            out.push({ key: r.id, label: r.name || r.key || labels.guest, value: 'n/a', title: note('guest') })
            break
          }
          const on = g.status === 'running'
          out.push({
            key: r.id,
            label: r.name || g.name,
            value: on ? `CPU ${percent(g.cpu)} · ${bytes(g.mem.used)}` : g.status === 'stopped' ? 'Stopped' : g.status,
            pct: on ? g.cpu : undefined,
            hot: on && g.cpu >= 90,
            title: `${g.type === 'lxc' ? 'LXC' : 'VM'} ${g.id} on ${g.node}${g.uptime ? `, up ${g.uptime}` : ''}${g.tags?.length ? `, tags: ${g.tags.join(', ')}` : ''}`,
          })
          break
        }
        case 'storage': {
          const all = d.storage ?? []
          const picked = r.key ? all.filter((s) => s.id === r.key) : all
          if (!picked.length) out.push({ key: r.id, label: r.name || r.key || labels.storage, value: 'n/a', title: note('storage') })
          for (const s of picked) {
            // The same name on several nodes: say which one.
            const dup = all.filter((x) => x.id === s.id).length > 1
            const l = r.key && r.name ? r.name : dup ? `${s.id} · ${s.node}` : s.id
            const key = `${r.id}:${s.id}:${s.node}`
            const kind = `${s.type ?? 'storage'}${s.shared ? ', shared' : ''}`
            if (s.status && s.status !== 'available') out.push({ key, label: l, value: 'unavailable', bad: true, title: `${kind}: ${s.status}` })
            else if (!s.total) out.push({ key, label: l, value: 'n/a', title: kind })
            else out.push({ key, label: l, value: `${bytes(s.used)} / ${bytes(s.total)}`, pct: s.percent, hot: s.percent >= 90, title: kind })
          }
          break
        }
        case 'backup': {
          const b = d.backup
          if (data.notes?.tasks) out.push(na())
          else if (!b) out.push({ key: r.id, label, value: 'None yet', title: 'No backup job has run recently' })
          else {
            const when = ago(b.end)
            const text = { ok: 'OK', warnings: 'Warnings', failed: 'Failed', running: 'Running' }[b.status]
            out.push({
              key: r.id,
              label,
              value: b.status === 'running' ? 'Running' : `${text}${when ? ` · ${when}` : ''}`,
              bad: b.status === 'failed',
              title: [b.node, b.message].filter(Boolean).join(': ') || undefined,
            })
          }
          break
        }
        case 'datastore': {
          const all = d.datastores ?? []
          const picked = r.key ? all.filter((s) => s.name === r.key) : all
          if (!picked.length) out.push({ key: r.id, label: r.name || r.key || labels.datastore, value: 'n/a', title: note('usage') })
          for (const s of picked) {
            const l = r.key && r.name ? r.name : s.name
            const key = `${r.id}:${s.name}`
            if (s.error) out.push({ key, label: l, value: 'error', bad: true, title: s.error })
            else {
              const full = s.fullAt ? new Date(s.fullAt) : undefined
              out.push({
                key,
                label: l,
                value: `${bytes(s.used)} / ${bytes(s.total)}`,
                pct: s.percent,
                hot: s.percent >= 90,
                title: full ? `Expected to be full on ${full.toLocaleDateString()} at the current growth` : undefined,
              })
            }
          }
          break
        }
        case 'group': {
          if (!d.groups) {
            out.push({ key: r.id, label: r.name || r.key || labels.group, value: 'n/a', title: note('groups') })
            break
          }
          const picked = r.key ? d.groups.filter((g) => `${g.store}/${g.type}/${g.id}` === r.key) : d.groups
          if (!picked.length) out.push({ key: r.id, label: r.name || r.key || labels.group, value: r.key ? 'not found' : 'None', title: r.key ? 'No backup group has this name' : 'No backup yet' })
          const stores = new Set(d.groups.map((g) => g.store)).size
          for (const g of picked) {
            const when = ago(g.last)
            // A group not backed up for two days has probably been forgotten.
            const stale = !g.last || Date.now() - Date.parse(g.last) > 2 * 86400 * 1000 || Number.isNaN(Date.parse(g.last))
            const name = `${g.type}/${g.id}${g.comment ? ` · ${g.comment}` : ''}`
            out.push({
              key: `${r.id}:${g.store}/${g.type}/${g.id}`,
              label: r.key && r.name ? r.name : stores > 1 ? `${g.store} · ${name}` : name,
              value: when || 'never',
              bad: stale,
              title: `${g.store}/${g.type}/${g.id}: ${g.count} backup${g.count === 1 ? '' : 's'}${stale ? ' (the latest is more than two days old)' : ''}`,
            })
          }
          break
        }
        case 'quorum': {
          const c = d.cluster
          if (data.notes?.cluster) out.push(na())
          else if (!c) out.push({ key: r.id, label, value: 'Standalone', title: 'This node is not part of a cluster' })
          else
            out.push({
              key: r.id,
              label,
              value: `${c.quorate ? 'Quorate' : 'No quorum'} · ${c.online} / ${c.nodes} nodes online`,
              bad: !c.quorate || c.online < c.nodes,
              title: c.name ? `Cluster ${c.name}` : undefined,
            })
          break
        }
        case 'tasks': {
          const t = d.tasks
          if (!t) out.push(na())
          else
            out.push({
              key: r.id,
              label,
              value: t.failed === 0 ? 'None in 24 h' : `${t.failed} in 24 h`,
              bad: t.failed > 0,
              title: (t.items ?? []).map((i) => `${i.type} on ${i.node}: ${i.status}`).join(String.fromCharCode(10)) || undefined,
            })
          break
        }
        case 'apps': {
          const a = d.apps
          if (!a) out.push(na())
          else
            out.push({
              key: r.id,
              label,
              value: `${a.running} / ${a.total}`,
              title: a.stopped?.length ? `Not running: ${a.stopped.join(', ')}` : 'All apps are running',
            })
          break
        }
        case 'alerts': {
          const a = d.alerts
          if (!a) out.push(na())
          else {
            const serious = a.count > 0 && ['ERROR', 'CRITICAL', 'ALERT', 'EMERGENCY'].includes(a.level ?? '')
            out.push({
              key: r.id,
              label,
              value: a.count === 0 ? 'None' : `${a.count} · ${(a.level ?? '').toLowerCase()}`,
              bad: serious,
              title: (a.items ?? []).map((i) => `${i.level}: ${i.text}`).join('\n'),
            })
          }
          break
        }
      }
    }
    return out
  })

  // The collapsed view: the first line of each value ticked "Collapsed view" (up to
  // four), or of the first four values when none is.
  const brief = $derived.by(() => {
    const ticked = widget.rows.filter((r) => r.collapsed)
    const picked = (ticked.length ? ticked : widget.rows).slice(0, 4)
    return picked.flatMap((r) => lines.find((l) => l.key === r.id || l.key.startsWith(`${r.id}:`)) ?? [])
  })

  function dragStart(e: DragEvent) {
    app.drag = { kind: 'widget', id: widget.id }
    e.dataTransfer?.setData('text/plain', widget.name)
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function dragEnd() {
    app.drag = null
    drop = null
  }
  function dragOver(e: DragEvent) {
    const d = app.drag
    if ((d?.kind !== 'widget' && d?.kind !== 'group') || d.id === widget.id) return
    // Groups have a zone of their own in the column and strip layouts.
    if (arrange.separate && d.kind === 'group') return
    e.preventDefault()
    e.stopPropagation()
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    // Drop markers: top/bottom when the widgets are stacked, left/right in the strip.
    vertical = arrange.widgetsVertical
    drop = (vertical ? e.clientY < r.top + r.height / 2 : e.clientX < r.left + r.width / 2) ? 'before' : 'after'
  }
  function dragLeave(e: DragEvent) {
    if (!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) drop = null
  }
  function onDrop(e: DragEvent) {
    const d = app.drag
    if ((d?.kind !== 'widget' && d?.kind !== 'group') || (arrange.separate && d.kind === 'group')) return
    e.preventDefault()
    e.stopPropagation()
    const after = drop === 'after'
    drop = null
    app.drag = null
    app.moveBlock(d.id, widget.id, after)
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onedit?.()
    }
  }
</script>

{#snippet body()}
  <div class="head">
    <h3>{widget.name}</h3>
    {#if data}
      <span class="dot" class:online={data.online} title={data.online ? 'Online' : 'Unreachable'}></span>
    {/if}
    {#if !preview && !editable && mode === 'normal'}
      <button type="button" class="fold" aria-label={folded ? 'Expand the widget' : 'Collapse the widget'} aria-expanded={!folded} title={folded ? 'Expand' : 'Collapse'} onclick={() => setCollapsed(widget.id, !folded)}>
        <svg viewBox="0 0 12 12" width="12" height="12" aria-hidden="true"><path d={folded ? 'M2 4l4 4 4-4' : 'M2 8l4-4 4 4'} fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </button>
    {/if}
  </div>
  {#if !data}
    <p class="note">Loading…</p>
  {:else if !data.online}
    <p class="note down">Unreachable: {data.error}</p>
  {:else if folded && mode === 'strip'}
    <!-- In the strip the chosen values are short rows, so a long value never wraps. -->
    <dl class="mini">
      {#each brief as c (c.key)}
        <div class="mrow" class:bad={c.bad} title={c.title ? `${c.label}: ${c.title}` : c.label}>
          <dt>{c.label}</dt>
          <dd>{c.value}</dd>
        </div>
      {/each}
    </dl>
  {:else if folded}
    <div class="brief">
      {#each brief as c (c.key)}
        <div class="cell" class:bad={c.bad} title={c.title ? `${c.label}: ${c.title}` : c.label}>
          <b>{c.value}</b>
          <span>{c.label}</span>
        </div>
      {/each}
    </div>
  {:else}
    <dl>
      {#each lines as r (r.key)}
        <div class="row" title={r.title}>
          <dt>{r.label}</dt>
          <dd class:bad={r.bad}>{r.value}</dd>
          {#if r.pct !== undefined}
            <div class="meter" role="presentation"><span class:hot={r.hot} style:width="{Math.min(100, Math.max(0, r.pct))}%"></span></div>
          {/if}
        </div>
      {/each}
    </dl>
  {/if}
{/snippet}

{#if editable}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="widget editing"
    class:dragging={app.drag?.kind === 'widget' && app.drag.id === widget.id}
    class:vertical
    class:stripedit={mode === 'strip'}
    class:drop-before={drop === 'before'}
    class:drop-after={drop === 'after'}
    role="button"
    tabindex="0"
    draggable="true"
    {style}
    title="Drag to move, click to edit"
    ondragstart={dragStart}
    ondragend={dragEnd}
    ondragover={dragOver}
    ondragleave={dragLeave}
    ondrop={onDrop}
    onclick={onedit}
    onkeydown={onKey}
  >
    {@render body()}
    <EditIcon />
  </div>
{:else if mode === 'strip'}
  <div
    class="widget strip folded"
    {style}
    aria-label={widget.name}
    role="button"
    tabindex="0"
    title="Click to see all the values"
    onclick={(e) => onopen?.(e.currentTarget)}
    onkeydown={(e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        onopen?.(e.currentTarget)
      }
    }}
  >
    {@render body()}
  </div>
{:else}
  <section class="widget" class:preview class:folded class:pop={mode === 'pop'} {style} aria-label={widget.name}>
    {@render body()}
  </section>
{/if}

<style>
  /* In the grid layout a widget fills its column, like a group card. */
  :global(:root[data-layout='grid']) .widget:not(.preview) {
    width: 100%;
  }

  /* On a phone a widget is as wide as the screen and as high as its content: the
     size chosen for the desktop view does not apply (and the editor says so). */
  @media (max-width: 640px) {
    .widget:not(.preview) {
      width: 100%;
      height: auto;
    }
  }

  .widget {
    position: relative;
    flex: none;
    box-sizing: border-box;
    width: min(var(--w-width), 100%);
    height: var(--w-height, auto);
    overflow: auto;
    scrollbar-width: thin;
    padding: 12px 14px;
    background: color-mix(in srgb, var(--w-color, var(--widget-color)) calc(var(--w-opacity, var(--widget-opacity)) * 1%), transparent);
    border: var(--w-border-w, var(--widget-border-w)) solid var(--w-border, var(--widget-border));
    border-radius: 10px;
    color: var(--w-text, var(--widget-text));
    text-align: left;
  }
  /* In the editor the preview starts at the size chosen and grows to show every
     value instead of scrolling. On the dashboard the size is kept. */
  .widget.preview {
    height: auto;
    min-height: var(--w-height, 0);
    overflow: visible;
  }
  .widget.folded {
    height: auto;
    min-height: 0;
  }
  /* A collapsed widget is a label first: its title takes the accent color. */
  .widget.folded h3 {
    color: var(--accent);
  }
  /* In the strip a widget is a small tile you click; its full view is a .pop. */
  .widget.strip {
    cursor: pointer;
    overflow: hidden;
  }
  .widget.strip:hover,
  .widget.strip:focus-visible {
    border-color: var(--accent);
    outline: none;
  }
  .widget.pop {
    box-shadow: 0 10px 30px rgb(0 0 0 / 0.45);
  }
  .fold {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    padding: 0;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: inherit;
    opacity: 0.6;
    cursor: pointer;
  }
  .fold:hover {
    opacity: 1;
    background: color-mix(in srgb, currentColor 12%, transparent);
  }
  .brief {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: 8px 14px;
  }
  .cell {
    display: grid;
    min-width: 0;
    text-align: center;
  }
  .cell b {
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .cell span {
    max-width: 9em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.65rem;
    opacity: 0.6;
  }
  .cell.bad b {
    color: var(--down);
  }
  .mini {
    margin: 0;
    display: grid;
    gap: 5px;
  }
  .mrow {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 10px;
    min-width: 0;
    font-size: 0.8rem;
  }
  .mrow dt {
    flex: 0 1 auto;
    min-width: 2em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    opacity: 0.65;
  }
  .mrow dd {
    margin: 0;
    flex: 0 0 auto;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .mrow.bad dd {
    color: var(--down);
  }
  .widget.editing {
    cursor: grab;
  }
  /* The pencil sits in the corner: keep the last value of a strip tile clear of it. */
  .widget.stripedit {
    padding-bottom: 30px;
  }
  .widget.dragging {
    opacity: 0.4;
  }
  .widget.drop-before {
    box-shadow: -4px 0 0 var(--accent);
  }
  .widget.drop-after {
    box-shadow: 4px 0 0 var(--accent);
  }
  .widget.vertical.drop-before {
    box-shadow: 0 -4px 0 var(--accent);
  }
  .widget.vertical.drop-after {
    box-shadow: 0 4px 0 var(--accent);
  }
  .widget :global(.edit-icon) {
    position: absolute;
    right: 8px;
    bottom: 8px;
    opacity: 0.6;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
  }
  h3 {
    margin: 0;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.85rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--down);
  }
  .dot.online {
    background: var(--up);
  }
  dl {
    margin: 0;
    display: grid;
    gap: 8px;
  }
  .row {
    display: grid;
    grid-template-columns: auto 1fr;
    column-gap: 12px;
    row-gap: 3px;
    align-items: baseline;
  }
  dt {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.85rem;
    opacity: 0.7;
  }
  dd {
    margin: 0;
    min-width: 0;
    text-align: right;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  dd.bad {
    color: var(--down);
  }
  .meter {
    grid-column: 1 / -1;
    height: 5px;
    border-radius: 3px;
    background: color-mix(in srgb, currentColor 15%, transparent);
    overflow: hidden;
  }
  .meter span {
    display: block;
    height: 100%;
    border-radius: 3px;
    background: var(--w-bar, var(--widget-bar));
    transition: width 0.4s ease;
  }
  .meter span.hot {
    background: var(--down);
  }
  .note {
    margin: 0;
    opacity: 0.7;
  }
  .note.down {
    color: var(--down);
    opacity: 1;
  }
  @media (prefers-reduced-motion: reduce) {
    .meter span {
      transition: none;
    }
  }
</style>
