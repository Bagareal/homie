import * as api from './api'
import type { Config, Group, Item, SavedTheme, Status, Widget, WidgetData, WidgetStyle } from './types'
import { applyBuiltin, applyTheme, BUILTIN_THEMES, fromFile, isModified, snapshotAppearance, snapshotWidgets, STYLE_KEYS, toFile, type BuiltinTheme } from './themes'

export type DragData = { kind: 'item'; id: string; groupId: string } | { kind: 'group'; id: string } | { kind: 'widget'; id: string }

// crypto.randomUUID requires HTTPS: not available on a plain-http LAN.
export function uid(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

export type Choice<T extends string = string> = { label: string; value: T; kind?: 'primary' | 'danger'; focus?: boolean }
export type Ask = {
  title: string
  message: string
  buttons: Choice[]
  // A text field above the buttons (see prompt()).
  input?: { value: string; placeholder?: string }
  resolve: (v: string | null) => void
}

class AppState {
  config = $state<Config | null>(null)
  ask = $state.raw<Ask | null>(null)
  status = $state<Record<string, Status>>({})
  widgetData = $state<Record<string, WidgetData>>({})
  editing = $state(false)
  query = $state('')
  drag = $state<DragData | null>(null)
  toast = $state<{ text: string; error: boolean; key: number } | null>(null)
  #toastTimer: ReturnType<typeof setTimeout> | undefined
  #typed = ''

  async load() {
    this.config = withWidgets(await api.getConfig())
  }

  // Forgets everything that was read: the session ended or the device locked.
  reset() {
    this.config = null
    this.widgetData = {}
    this.status = {}
    this.editing = false
    this.ask?.resolve(null)
    this.ask = null
  }

  async save(message = 'Changes saved') {
    if (!this.config) return
    try {
      await api.putConfig($state.snapshot(this.config) as Config)
      this.forgetPasswords()
      this.notify(message)
    } catch (e) {
      this.notify(`Save failed: ${(e as Error).message}`, true)
      // Re-sync the UI with what the server actually saved.
      await this.load().catch(() => {})
    }
  }

  async refreshStatus() {
    try {
      this.status = await api.getStatus()
    } catch {
      // Next tick retries.
    }
  }

  async refreshWidgets() {
    try {
      this.widgetData = await api.getWidgets()
    } catch {
      // Next tick retries.
    }
  }

  // Passwords go to the server once and are not kept in the page afterwards.
  forgetPasswords() {
    for (const w of this.config?.widgets ?? []) {
      if (w.password) w.hasPassword = true
      else if (w.clearPassword) w.hasPassword = false
      w.password = ''
      w.clearPassword = false
      const g = w.glances
      if (g) {
        if (g.password) g.hasPassword = true
        else if (g.clearPassword) g.hasPassword = false
        g.password = ''
        g.clearPassword = false
      }
    }
  }

  notify(text: string, error = false) {
    clearTimeout(this.#toastTimer)
    this.toast = { text, error, key: Date.now() }
    this.#toastTimer = setTimeout(() => (this.toast = null), error ? 6000 : 2200)
  }

  // In-app replacement for the browser's confirm(): resolves with the value of
  // the clicked button, or null when dismissed (Esc).
  choose<T extends string>(o: { title: string; message: string; buttons: Choice<T>[]; input?: Ask['input'] }): Promise<T | null> {
    this.ask?.resolve(null)
    return new Promise((resolve) => {
      this.ask = { ...o, resolve: resolve as (v: string | null) => void }
    })
  }

  answer(value: string | null, typed = '') {
    this.#typed = typed
    const a = this.ask
    this.ask = null
    a?.resolve(value)
  }

  // Asks for a line of text; null when dismissed or left empty.
  async prompt(o: { title: string; message: string; value?: string; placeholder?: string; confirmLabel: string }): Promise<string | null> {
    const r = await this.choose({
      title: o.title,
      message: o.message,
      input: { value: o.value ?? '', placeholder: o.placeholder },
      buttons: [
        { label: 'Cancel', value: 'cancel' },
        { label: o.confirmLabel, value: 'ok', kind: 'primary' },
      ],
    })
    return r === 'ok' ? this.#typed.trim() || null : null
  }

  async confirm(o: { title: string; message: string; confirmLabel: string; danger?: boolean }): Promise<boolean> {
    const r = await this.choose({
      title: o.title,
      message: o.message,
      buttons: [
        { label: 'Cancel', value: 'cancel', focus: true },
        { label: o.confirmLabel, value: 'ok', kind: o.danger ? 'danger' : 'primary' },
      ],
    })
    return r === 'ok'
  }

  async confirmUnsaved(what: string): Promise<'save' | 'discard' | 'keep'> {
    const r = await this.choose({
      title: 'Unsaved changes',
      message: `Save your changes to this ${what} before closing?`,
      buttons: [
        { label: 'Keep editing', value: 'keep', focus: true },
        { label: 'Discard', value: 'discard', kind: 'danger' },
        { label: 'Save', value: 'save', kind: 'primary' },
      ],
    })
    return r ?? 'keep'
  }

  group(id: string): Group | undefined {
    return this.config?.groups.find((g) => g.id === id)
  }

  moveItem(fromId: string, itemId: string, toId: string, targetId: string | null, after: boolean) {
    const from = this.group(fromId)
    const to = this.group(toId)
    if (!from || !to || itemId === targetId) return
    const before = order(this.config!)
    const i = from.items.findIndex((x) => x.id === itemId)
    if (i < 0) return
    const [item] = from.items.splice(i, 1)
    let j = targetId ? to.items.findIndex((x) => x.id === targetId) : -1
    j = j < 0 ? to.items.length : j + (after ? 1 : 0)
    to.items.splice(j, 0, item)
    if (order(this.config!) !== before) this.save('Order updated')
  }

  // Groups and widgets form one sequence. Ids missing from the saved order
  // (just created) go last; ids that no longer exist are dropped.
  blockOrder(): string[] {
    const cfg = this.config
    if (!cfg) return []
    const known = new Set([...cfg.groups.map((g) => g.id), ...cfg.widgets.map((w) => w.id)])
    const out = (cfg.order ?? []).filter((id, i, all) => known.has(id) && all.indexOf(id) === i)
    for (const id of known) if (!out.includes(id)) out.push(id)
    return out
  }

  // Moves a group or widget next to another one.
  moveBlock(id: string, targetId: string, after: boolean) {
    const cfg = this.config
    if (!cfg || id === targetId) return
    const ids = this.blockOrder()
    const before = ids.join()
    const i = ids.indexOf(id)
    if (i < 0) return
    ids.splice(i, 1)
    let j = ids.indexOf(targetId)
    j = j < 0 ? ids.length : j + (after ? 1 : 0)
    ids.splice(j, 0, id)
    if (ids.join() === before) return
    cfg.order = ids
    this.save('Order updated')
  }

  upsertItem(fromId: string, toId: string, data: Item) {
    const from = this.group(fromId)
    const to = this.group(toId)
    if (!to) return
    const i = from ? from.items.findIndex((x) => x.id === data.id) : -1
    if (from && i >= 0) {
      if (fromId === toId) from.items[i] = data
      else {
        from.items.splice(i, 1)
        to.items.push(data)
      }
    } else {
      to.items.push(data)
    }
    this.save()
  }

  deleteItem(groupId: string, itemId: string) {
    const g = this.group(groupId)
    if (!g) return
    g.items = g.items.filter((x) => x.id !== itemId)
    this.save('Service deleted')
  }

  widget(id: string): Widget | undefined {
    return this.config?.widgets.find((w) => w.id === id)
  }

  upsertWidget(data: Widget) {
    if (!this.config) return
    const i = this.config.widgets.findIndex((w) => w.id === data.id)
    if (i >= 0) this.config.widgets[i] = data
    else this.config.widgets.push(data)
    this.save().then(() => this.refreshWidgets())
  }

  deleteWidget(id: string) {
    if (!this.config) return
    this.config.widgets = this.config.widgets.filter((w) => w.id !== id)
    this.save('Widget deleted')
  }

  // ---- themes

  get builtinThemes(): BuiltinTheme[] {
    return BUILTIN_THEMES
  }

  themeModified(): boolean {
    return this.config ? isModified(this.config, this.config.activeTheme) : false
  }

  // Replaces the look with a theme. If the current look is not saved in the
  // theme that was applied last, it is lost, so that is confirmed first.
  async applyTheme(id: string) {
    const cfg = this.config
    if (!cfg) return
    const builtin = BUILTIN_THEMES.find((t) => t.id === id)
    const saved = cfg.themes.find((t) => t.id === id)
    if (!builtin && !saved) return
    if (isModified(cfg, cfg.activeTheme)) {
      const ok = await this.confirm({
        title: 'Replace the current look?',
        message: 'The current look is not saved in a theme and will be lost. Save it first with “Save current as…” to keep it.',
        confirmLabel: 'Replace',
        danger: true,
      })
      if (!ok) return
    }
    if (builtin) applyBuiltin(cfg, builtin)
    else applyTheme(cfg, saved!)
    this.save(`Theme “${builtin?.name ?? saved!.name}” applied`)
  }

  // Saves the current look (appearance and widget styles) under a new name.
  saveCurrentTheme(name: string) {
    const cfg = this.config
    if (!cfg) return
    const t: SavedTheme = { id: uid(), name, appearance: snapshotAppearance(cfg.settings), widgets: snapshotWidgets(cfg.widgets) }
    cfg.themes.push(t)
    cfg.activeTheme = t.id
    this.save(`Theme “${name}” saved`)
  }

  // Overwrites a saved theme with the current look.
  updateTheme(id: string) {
    const cfg = this.config
    const t = cfg?.themes.find((x) => x.id === id)
    if (!cfg || !t) return
    t.appearance = snapshotAppearance(cfg.settings)
    t.widgets = snapshotWidgets(cfg.widgets)
    cfg.activeTheme = id
    this.save(`Theme “${t.name}” updated`)
  }

  renameTheme(id: string, name: string) {
    const t = this.config?.themes.find((x) => x.id === id)
    if (!t) return
    t.name = name
    this.save('Theme renamed')
  }

  deleteTheme(id: string) {
    const cfg = this.config
    if (!cfg) return
    cfg.themes = cfg.themes.filter((t) => t.id !== id)
    if (cfg.activeTheme === id) cfg.activeTheme = undefined
    this.save('Theme deleted')
  }

  exportTheme(id: string) {
    const t = BUILTIN_THEMES.find((x) => x.id === id) ?? this.config?.themes.find((x) => x.id === id)
    if (!t) return
    const a = document.createElement('a')
    a.href = URL.createObjectURL(new Blob([JSON.stringify(toFile(t), null, 2)], { type: 'application/json' }))
    a.download = `homie-theme-${t.name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'theme'}.json`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  importTheme(text: string) {
    const cfg = this.config
    if (!cfg) return
    const t = fromFile(text, uid(), cfg.settings)
    cfg.themes.push(t)
    this.save(`Theme “${t.name}” imported`)
  }

  // Makes one widget's look the look of every widget: its own settings become
  // the global widget look and the others lose theirs.
  shareWidgetStyle(style: WidgetStyle) {
    const cfg = this.config
    if (!cfg) return
    const s = cfg.settings
    if (style.color) s.widgetColor = style.color
    if (style.textColor) s.widgetTextColor = style.textColor
    if (style.borderColor) s.widgetBorderColor = style.borderColor
    if (style.barColor) s.widgetBarColor = style.barColor
    if (style.opacity !== undefined) s.widgetOpacity = style.opacity
    if (style.borderWidth !== undefined) s.widgetBorderWidth = style.borderWidth
    for (const w of cfg.widgets) for (const k of STYLE_KEYS) delete w[k]
    this.save('Widget look applied to all widgets')
  }

  upsertGroup(id: string | null, name: string) {
    if (!this.config) return
    const g = id ? this.group(id) : undefined
    if (g) {
      g.name = name
    } else {
      this.config.groups.push({ id: uid(), name, items: [] })
    }
    this.save()
  }

  deleteGroup(id: string) {
    if (!this.config) return
    this.config.groups = this.config.groups.filter((g) => g.id !== id)
    this.save('Group deleted')
  }
}

// Older servers and imports may omit the list.
function withWidgets(cfg: Config): Config {
  cfg.widgets ??= []
  cfg.order ??= []
  return cfg
}

function order(cfg: Config): string {
  return cfg.groups.map((g) => g.id + ':' + g.items.map((i) => i.id).join(',')).join('|')
}

export const app = new AppState()
