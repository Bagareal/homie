// New versions of Homie. The server reads GitHub (see /api/releases); this decides
// what to tell the user, once: a modal when a new version is out, and another one,
// after an update, saying what changed. Both are remembered per device in this
// browser. A permanent reminder lives in Settings.

import * as api from './api'
import { compareVersions } from './notes'

const SEEN = 'homie.seenVersion' // the version this device last ran
const NOTIFIED = 'homie.notifiedVersion' // the newest version this device was told about

function read(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}
function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Not remembered: the modal may come back next time.
  }
}

export const updates = $state({
  current: '',
  enabled: false,
  releases: [] as api.Release[],
  checkedAt: '',
  error: '',
  loaded: false,
  // The modal on screen: a new version is out ("new"), or Homie was just updated ("done").
  dialog: null as null | { kind: 'new' | 'done'; releases: api.Release[]; from?: string },
})

// The releases newer than the one running, newest first.
export const newer = () => updates.releases.filter((r) => compareVersions(r.version, updates.current) > 0)

let lastLoad = 0

// `announce`: also decide whether a modal comes up. Only the first look, at load, does:
// a later look just updates the button in the top bar, it never interrupts what you are doing.
export async function loadUpdates(refresh = false, announce = !refresh) {
  try {
    const r = await api.getReleases(refresh)
    updates.current = r.current
    updates.enabled = r.enabled
    updates.releases = r.releases ?? []
    updates.checkedAt = r.checkedAt ?? ''
    updates.error = r.error ?? ''
    lastLoad = Date.now()
  } catch {
    // The server did not answer: nothing to announce.
    return
  } finally {
    updates.loaded = true
  }
  if (announce) showNext()
}

// A page left open (or an installed app in the background) learns about a new version
// when it is looked at again, or every half hour.
export async function recheckUpdates() {
  if (Date.now() - lastLoad < 20 * 60 * 1000) return
  await loadUpdates(false, false)
}

// Decides which modal, if any, comes next. Called on load and after one is closed.
export function showNext() {
  if (updates.dialog || compareVersions(updates.current, 'v0.0.0') <= 0) return
  const seen = read(SEEN)
  if (!seen) {
    write(SEEN, updates.current) // the first time on this device: nothing to compare with
  } else if (compareVersions(updates.current, seen) > 0) {
    const done = updates.releases.filter((r) => compareVersions(r.version, seen) > 0 && compareVersions(r.version, updates.current) <= 0)
    if (updates.enabled && done.length > 0) {
      updates.dialog = { kind: 'done', releases: done, from: seen }
      return
    }
    write(SEEN, updates.current)
  }
  const fresh = newer()
  if (updates.enabled && fresh.length > 0 && compareVersions(fresh[0].version, read(NOTIFIED)) > 0) {
    updates.dialog = { kind: 'new', releases: fresh }
  }
}

export function closeUpdateDialog() {
  const d = updates.dialog
  if (!d) return
  if (d.kind === 'done') write(SEEN, updates.current)
  else write(NOTIFIED, d.releases[0].version)
  updates.dialog = null
  showNext()
}
