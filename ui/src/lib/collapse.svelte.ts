// Collapsed widgets. Whether a widget is collapsed is a matter of this device and
// window, not of the dashboard: a big screen shows them open, a small one closed.
// A widget follows the window (collapsed below the width set in Settings) until
// it is collapsed or expanded by hand, which this browser then remembers.

import { mobile } from './mobile.svelte'

const KEY = 'homie.collapsedWidgets'

function load(): Record<string, boolean> {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '{}')
    if (v && typeof v === 'object' && !Array.isArray(v)) return v as Record<string, boolean>
  } catch {
    // Storage unavailable: nothing is remembered.
  }
  return {}
}

export const view = $state({ width: typeof window === 'undefined' ? 0 : window.innerWidth, manual: load() })

try {
  window.addEventListener('resize', () => (view.width = window.innerWidth))
} catch {
  // No window: the width stays as it was.
}

function save() {
  try {
    localStorage.setItem(KEY, JSON.stringify(view.manual))
  } catch {
    // Not remembered.
  }
}

// The phone has its own Widgets tab, so nothing collapses there.
export function isCollapsed(id: string, below: number | undefined): boolean {
  if (mobile.narrow) return false
  const mine = view.manual[id]
  if (mine !== undefined) return mine
  return (below ?? 0) > 0 && view.width < (below ?? 0)
}

export function setCollapsed(id: string, collapsed: boolean) {
  view.manual = { ...view.manual, [id]: collapsed }
  save()
}

// Collapses every widget, or, when they are all collapsed already, opens them.
export function toggleAll(ids: string[], below: number | undefined) {
  const collapse = ids.some((id) => !isCollapsed(id, below))
  view.manual = { ...view.manual, ...Object.fromEntries(ids.map((id) => [id, collapse])) }
  save()
}
