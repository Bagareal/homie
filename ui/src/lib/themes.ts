import type { Appearance, Config, SavedTheme, Widget, WidgetStyle } from './types'

// What a saved theme holds: every appearance setting ...
export const APPEARANCE_KEYS = [
  'theme',
  'density',
  'layout',
  'widgetLayout',
  'widgetSide',
  'backgroundColor',
  'tileColor',
  'accentColor',
  'backgroundImage',
  'backgroundOpacity',
  'barOpacity',
  'cardColor',
  'hoverColor',
  'panelColor',
  'tileTextColor',
  'tileTextHoverColor',
  'tileBorderColor',
  'groupBorderColor',
  'glowColor',
  'tileBorderWidth',
  'groupBorderWidth',
  'cardOpacity',
  'widgetColor',
  'widgetTextColor',
  'widgetBorderColor',
  'widgetBarColor',
  'widgetOpacity',
  'widgetBorderWidth',
] as const satisfies readonly (keyof Appearance)[]

// ... and the look of each widget.
export const STYLE_KEYS = ['color', 'textColor', 'borderColor', 'barColor', 'opacity', 'borderWidth'] as const satisfies readonly (keyof WidgetStyle)[]

// What a built-in theme sets: the colors and the light/dark mode. It leaves the
// layout, the density, the background image and the opacities as they are.
const COLOR_KEYS = [
  'theme',
  'backgroundColor',
  'tileColor',
  'accentColor',
  'cardColor',
  'hoverColor',
  'panelColor',
  'tileTextColor',
  'tileTextHoverColor',
  'tileBorderColor',
  'groupBorderColor',
  'glowColor',
  'widgetColor',
  'widgetTextColor',
  'widgetBorderColor',
  'widgetBarColor',
] as const satisfies readonly (keyof Appearance)[]

export interface BuiltinTheme {
  id: string
  name: string
  colors: Partial<Appearance>
}

// Near-black graphite with white and light-grey text; only the accent differs.
function graphite(id: string, name: string, accent: string): BuiltinTheme {
  return {
    id: `builtin:${id}`,
    name,
    colors: {
      theme: 'dark',
      backgroundColor: '#121212',
      panelColor: '#1a1a1a',
      cardColor: '#1a1a1a',
      tileColor: '#222222',
      hoverColor: '#2d2d2d',
      tileTextColor: '#e6e6e6',
      tileTextHoverColor: '#ffffff',
      tileBorderColor: '#333333',
      groupBorderColor: '#383838',
      accentColor: accent,
      glowColor: accent,
    },
  }
}

export const BUILTIN_THEMES: BuiltinTheme[] = [
  graphite('mono', 'Mono', '#f2f2f2'),
  graphite('neon-green', 'Neon green', '#39ff14'),
  graphite('purple', 'Purple', '#b026ff'),
]

export const isBuiltin = (id: string | undefined) => !!id?.startsWith('builtin:')

type Bag = Record<string, unknown>

function pick<T extends object>(from: T, keys: readonly string[]): Partial<T> {
  const out: Bag = {}
  for (const k of keys) {
    const v = (from as Bag)[k]
    if (v !== undefined && v !== '') out[k] = v
  }
  return out as Partial<T>
}

export const snapshotAppearance = (settings: Appearance): Appearance => pick(settings, APPEARANCE_KEYS) as Appearance

// The widgets that have a look of their own.
export function snapshotWidgets(widgets: Widget[]): Record<string, WidgetStyle> {
  const out: Record<string, WidgetStyle> = {}
  for (const w of widgets) {
    const st = pick(w, STYLE_KEYS)
    if (Object.keys(st).length > 0) out[w.id] = st
  }
  return out
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

// True when the dashboard no longer looks like the theme that was applied.
export function isModified(cfg: Config, id: string | undefined): boolean {
  if (!id) return true
  const builtin = BUILTIN_THEMES.find((t) => t.id === id)
  if (builtin) {
    const now = pick(cfg.settings, COLOR_KEYS)
    if (!same(now, pick(builtin.colors, COLOR_KEYS))) return true
    return Object.keys(snapshotWidgets(cfg.widgets)).length > 0
  }
  const t = cfg.themes.find((x) => x.id === id)
  if (!t) return true
  if (!same(snapshotAppearance(cfg.settings), pick(t.appearance, APPEARANCE_KEYS))) return true
  // Only widgets that still exist count; a widget the theme does not know must have no style.
  const now = snapshotWidgets(cfg.widgets)
  const then = t.widgets ?? {}
  for (const w of cfg.widgets) if (!same(now[w.id], then[w.id])) return true
  return false
}

function assign(target: object, source: object, keys: readonly string[]) {
  for (const k of keys) {
    const v = (source as Bag)[k]
    if (v === undefined || v === '') delete (target as Bag)[k]
    else (target as Bag)[k] = v
  }
}

export function applyBuiltin(cfg: Config, t: BuiltinTheme) {
  assign(cfg.settings, t.colors, COLOR_KEYS)
  for (const w of cfg.widgets) assign(w, {}, STYLE_KEYS) // widgets follow the theme again
  cfg.activeTheme = t.id
}

export function applyTheme(cfg: Config, t: SavedTheme) {
  assign(cfg.settings, t.appearance, APPEARANCE_KEYS)
  for (const w of cfg.widgets) {
    const st = t.widgets?.[w.id]
    if (st) assign(w, st, STYLE_KEYS) // a widget the theme does not know keeps its look
  }
  cfg.activeTheme = t.id
}

// ---- files

interface ThemeFile {
  homieTheme: 1
  name: string
  appearance: Partial<Appearance>
  widgets?: Record<string, WidgetStyle>
}

// The image is not part of the file (it would not travel with it).
export function toFile(t: SavedTheme | BuiltinTheme): ThemeFile {
  const appearance = 'colors' in t ? { ...t.colors } : { ...snapshotAppearance(t.appearance) }
  delete appearance.backgroundImage
  return { homieTheme: 1, name: t.name, appearance, ...('widgets' in t && t.widgets && Object.keys(t.widgets).length ? { widgets: t.widgets } : {}) }
}

// Reads a theme file; throws a readable message when it is not one.
export function fromFile(text: string, id: string, base: Appearance): SavedTheme {
  let f: unknown
  try {
    f = JSON.parse(text)
  } catch {
    throw new Error('the file is not valid JSON')
  }
  const o = f as Partial<ThemeFile> | null
  if (!o || o.homieTheme !== 1 || typeof o.name !== 'string' || !o.appearance || typeof o.appearance !== 'object') {
    throw new Error('the file is not a Homie theme')
  }
  // Start from the current look so a file that lists only some colors still gives a full theme.
  const appearance = { ...snapshotAppearance(base), ...pick(o.appearance, APPEARANCE_KEYS) } as Appearance
  delete appearance.backgroundImage
  const widgets: Record<string, WidgetStyle> = {}
  for (const [wid, st] of Object.entries(o.widgets ?? {})) {
    if (st && typeof st === 'object') widgets[wid] = pick(st, STYLE_KEYS)
  }
  return { id, name: o.name.trim().slice(0, 60) || 'Imported theme', appearance, widgets }
}
