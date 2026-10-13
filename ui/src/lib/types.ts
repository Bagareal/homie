export type Theme = 'auto' | 'light' | 'dark'
export type Density = 'compact' | 'comfortable'
export type Layout = 'list' | 'grid'

export interface Item {
  id: string
  name: string
  url: string
  icon: string
  description: string
  check: boolean
}

export interface Group {
  id: string
  name: string
  items: Item[]
}

// Everything that decides how the dashboard looks: what a saved theme holds.
export interface Appearance {
  theme: Theme
  density: Density
  backgroundColor?: string
  tileColor?: string
  accentColor?: string
  backgroundImage?: string
  backgroundOpacity: number
  layout: Layout
  barOpacity: number
  cardColor?: string
  hoverColor?: string
  panelColor?: string
  tileTextColor?: string
  tileTextHoverColor?: string
  tileBorderColor?: string
  groupBorderColor?: string
  glowColor?: string
  tileBorderWidth?: number
  groupBorderWidth?: number
  cardOpacity: number
  // The look every widget has unless it sets its own.
  widgetColor?: string
  widgetTextColor?: string
  widgetBorderColor?: string
  widgetBarColor?: string
  widgetOpacity: number
  widgetBorderWidth?: number
}

export interface Settings extends Appearance {
  checkInterval: number
  openInNewTab: boolean
  // Widgets collapse in a window narrower than this many px (0 or unset = never).
  widgetCollapseBelow?: number
  // Do not ask GitHub whether a new version exists.
  noUpdateCheck?: boolean
}

// How one widget looks when it differs from the global widget look.
export interface WidgetStyle {
  color?: string
  textColor?: string
  borderColor?: string
  barColor?: string
  opacity?: number
  borderWidth?: number
}

// A saved look: the appearance settings plus the style of each widget (by id).
export interface SavedTheme {
  id: string
  name: string
  appearance: Appearance
  widgets?: Record<string, WidgetStyle>
}

export type WidgetKind = 'glances' | 'truenas' | 'nut' | 'proxmox'
export type RowType = 'cpu' | 'mem' | 'swap' | 'load' | 'uptime' | 'temp' | 'disk' | 'network' | 'version' | 'pool' | 'drive' | 'alerts' | 'apps' | 'status' | 'charge' | 'runtime' | 'upsload' | 'power' | 'voltage' | 'model' | 'var' | 'clients' | 'client' | 'nodes' | 'lxc' | 'vm' | 'guest' | 'storage' | 'backup' | 'quorum' | 'tasks'

// One line of a widget. `key` picks a disk (mount point), sensor (label),
// network card, pool or drive (name); empty = all of them, or the CPU sensor /
// the physical cards / the hottest drive.
export interface Row {
  id: string
  type: RowType
  key?: string
  name?: string
  // cpu rows: the load and/or a temperature, side by side.
  load?: boolean
  temp?: boolean
  // client rows (UPS): what the user says the machine is.
  role?: 'primary' | 'secondary'
  // Shown when the widget is collapsed (up to 4 per widget).
  collapsed?: boolean
  // Where the value comes from: the widget's own source, or its extra Glances source
  // (of a Proxmox cluster, `node` says which node's Glances).
  source?: 'glances' | 'nut'
  node?: string
}

// An extra source of a widget (Glances). Like the main one, its password is write-only.
export interface Extra {
  url: string
  username?: string
  password?: string
  hasPassword?: boolean
  clearPassword?: boolean
  // Proxmox cluster: a Glances address per node.
  nodes?: Record<string, string>
}

export interface Widget extends WidgetStyle {
  id: string
  name: string
  kind: WidgetKind
  url: string
  username?: string
  // Write-only: the server never sends it back, only hasPassword.
  password?: string
  hasPassword?: boolean
  clearPassword?: boolean
  glances?: Extra
  // The extra UPS source: a NUT server (nut://host[:port][/ups]), nothing secret.
  nut?: { url: string }
  rows: Row[]
  refresh: number
  // Size in groups: cols wide, heightUnits high; or, with precise, height in px (0 = fit the content).
  cols: number
  heightUnits: number
  precise?: boolean
  height: number
}

export interface Config {
  title: string
  settings: Settings
  groups: Group[]
  widgets: Widget[]
  // Ids of the groups and widgets, in the order they appear.
  order: string[]
  themes: SavedTheme[]
  // The id of the theme applied last ("builtin:…" for the ones that ship).
  activeTheme?: string
}

export interface Usage {
  percent: number
  used: number
  total: number
}

export interface HostData {
  cpu?: number
  mem?: Usage
  swap?: Usage
  load?: { min1: number; min5: number; min15: number; cores: number }
  uptime?: string
  // The CPU temperature picked automatically, and every temperature sensor.
  temp?: { label: string; value: number }
  sensors?: { label: string; value: number }[]
  disks?: (Usage & { mount: string })[]
  network?: { name: string; rx: number; tx: number; virtual?: boolean; bridge?: boolean }[]
  // TrueNAS only.
  version?: string
  hostname?: string
  pools?: {
    name: string
    status: string
    healthy: boolean
    percent: number
    used: number
    total: number
    scan?: { function?: string; state?: string; end?: string; percent?: number; errors?: number }
  }[]
  drives?: { name: string; model?: string; pool?: string; temp?: number }[]
  alerts?: { count: number; level?: string; items?: { level: string; text: string }[] }
  apps?: { running: number; total: number; stopped?: string[] }
  // UPS (NUT) only: the one that was read, all the ones the server has, its variables
  // (battery.charge, ups.status, ...) and the addresses of the connected machines.
  ups?: string
  upsList?: string[]
  vars?: Record<string, string>
  clients?: string[]
  // Proxmox VE only.
  cluster?: { name: string; quorate: boolean; nodes: number; online: number }
  nodes?: { name: string; online: boolean; cpu?: number; cores?: number; mem?: Usage; uptime?: string }[]
  guests?: {
    id: number
    name: string
    type: 'lxc' | 'qemu'
    node: string
    status: string
    cpu: number
    cores?: number
    mem: Usage
    uptime?: string
    template?: boolean
    tags?: string[]
    pool?: string
  }[]
  storage?: { id: string; node: string; type?: string; shared?: boolean; status?: string; percent: number; used: number; total: number }[]
  backup?: { status: 'ok' | 'warnings' | 'failed' | 'running'; end?: string; node?: string; message?: string }
  tasks?: { failed: number; items?: { type: string; node: string; status: string; end: string }[] }
  // The extra Glances source of the widget: the general one and one per node.
  glances?: HostData
  glancesNodes?: Record<string, HostData>
  // The extra UPS source of the widget (same shape as a UPS widget's own data).
  nut?: HostData
}

export interface WidgetData {
  online: boolean
  error?: string
  updatedAt: string
  data: HostData
  // Why a value could not be read, for troubleshooting.
  notes?: Record<string, string>
}

export interface Status {
  up: boolean
  code?: number
  latencyMs: number
  checkedAt: string
  error?: string
}
