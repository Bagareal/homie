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

export interface Config {
  title: string
  settings: Settings
  groups: Group[]
}

export interface Status {
  up: boolean
  code?: number
  latencyMs: number
  checkedAt: string
  error?: string
}
