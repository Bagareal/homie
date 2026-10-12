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

export type WidgetKind = 'glances' | 'truenas'
export type RowType = 'cpu' | 'mem' | 'swap' | 'load' | 'uptime' | 'temp' | 'disk' | 'network' | 'version' | 'pool' | 'drive' | 'alerts' | 'apps'

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
  rows: Row[]
  refresh: number
  width: number
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
