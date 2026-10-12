// A single unsaved-edit draft, kept in localStorage so a refresh or a closed
// tab doesn't lose a half-filled dialog. Only one dialog is open at a time,
// so one slot is enough.

export type ItemFields = {
  name: string
  url: string
  icon: string
  description: string
  check: boolean
  target: string
}

export type Draft =
  | { kind: 'item'; groupId: string; itemId: string | null; fields: ItemFields }
  | { kind: 'group'; groupId: string | null; name: string }

const KEY = 'homie.draft'

export function loadDraft(): Draft | null {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return null
    const d = JSON.parse(raw)
    if (d?.kind === 'item' && typeof d.groupId === 'string' && d.fields && typeof d.fields.name === 'string') return d
    if (d?.kind === 'group' && typeof d.name === 'string') return d
  } catch {
    // Storage blocked or the value is corrupt: behave as if there is no draft.
  }
  return null
}

export function saveDraft(d: Draft) {
  try {
    localStorage.setItem(KEY, JSON.stringify(d))
  } catch {
    // Storage unavailable: drafts are a convenience, not a requirement.
  }
}

export function clearDraft() {
  try {
    localStorage.removeItem(KEY)
  } catch {
    // See saveDraft.
  }
}
