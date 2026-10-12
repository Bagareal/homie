// Phone-only view state: which tab is showing (services or widgets) and which
// groups are folded. Both live in this browser only, like the unsaved-edit
// draft: they describe how this device likes to look at the dashboard, not the
// dashboard itself.

const NARROW = '(max-width: 640px)'
const TAB_KEY = 'homie.mobileTab'
const FOLD_KEY = 'homie.foldedGroups'

export type MobileTab = 'services' | 'widgets'

function read<T>(key: string, fallback: T, valid: (v: unknown) => v is T): T {
  try {
    const raw = localStorage.getItem(key)
    if (raw === null) return fallback
    const v = JSON.parse(raw)
    return valid(v) ? v : fallback
  } catch {
    return fallback
  }
}

function write(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Storage unavailable: the view just won't be remembered.
  }
}

export const mobile = $state({
  narrow: false,
  tab: read<MobileTab>(TAB_KEY, 'services', (v): v is MobileTab => v === 'services' || v === 'widgets'),
  folded: read<string[]>(FOLD_KEY, [], (v): v is string[] => Array.isArray(v) && v.every((x) => typeof x === 'string')),
})

try {
  const mq = window.matchMedia(NARROW)
  mobile.narrow = mq.matches
  mq.addEventListener('change', (e) => (mobile.narrow = e.matches))
} catch {
  // No matchMedia: stay on the desktop view.
}

export function setTab(tab: MobileTab) {
  mobile.tab = tab
  write(TAB_KEY, tab)
}

export function toggleFold(groupId: string) {
  mobile.folded = mobile.folded.includes(groupId) ? mobile.folded.filter((id) => id !== groupId) : [...mobile.folded, groupId]
  write(FOLD_KEY, mobile.folded)
}
