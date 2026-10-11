export type Theme = 'auto' | 'light' | 'dark'
export type Density = 'compact' | 'comfortable'

export interface Item {
  id: string
  name: string
  url: string
  icon: string
  description: string
  check: boolean
  iconInset: boolean
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
