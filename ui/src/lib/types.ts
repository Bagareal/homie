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

export interface Settings {
  theme: Theme
  density: Density
  checkInterval: number
  openInNewTab: boolean
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
}

export type WidgetMetric = 'cpu' | 'mem' | 'swap' | 'load' | 'uptime' | 'temp' | 'disks' | 'network'
export type Placement = 'top' | 'bottom'

export interface Widget {
  id: string
  name: string
  kind: 'glances'
  url: string
  username?: string
  // Write-only: the server never sends it back, only hasPassword.
  password?: string
  hasPassword?: boolean
  clearPassword?: boolean
  metrics: WidgetMetric[]
  mounts?: string[]
  interfaces?: string[]
  refresh: number
  placement: Placement
  width: number
  height: number
  color?: string
  textColor?: string
  borderColor?: string
  barColor?: string
  opacity: number
  borderWidth?: number
}

export interface Config {
  title: string
  settings: Settings
  groups: Group[]
  widgets: Widget[]
}

export interface Usage {
  percent: number
  used: number
  total: number
}

export interface WidgetData {
  online: boolean
  error?: string
  updatedAt: string
  failed?: WidgetMetric[]
  data: {
    cpu?: number
    mem?: Usage
    swap?: Usage
    load?: { min1: number; min5: number; min15: number; cores: number }
    uptime?: string
    temp?: { label: string; value: number }
    disks?: (Usage & { mount: string })[]
    network?: { rx: number; tx: number }
  }
}

export interface Status {
  up: boolean
  code?: number
  latencyMs: number
  checkedAt: string
  error?: string
}
