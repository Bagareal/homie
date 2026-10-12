import * as api from './api'
import type { Config, Group, Item, Status, Widget, WidgetData } from './types'

export type DragData = { kind: 'item'; id: string; groupId: string } | { kind: 'group'; id: string } | { kind: 'widget'; id: string }

// crypto.randomUUID requires HTTPS: not available on a plain-http LAN.
export function uid(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

export type Choice<T extends string = string> = { label: string; value: T; kind?: 'primary' | 'danger'; focus?: boolean }
export type Ask = { title: string; message: string; buttons: Choice[]; resolve: (v: string | null) => void }

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

  async load() {
    this.config = withWidgets(await api.getConfig())
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
    }
  }

  notify(text: string, error = false) {
    clearTimeout(this.#toastTimer)
    this.toast = { text, error, key: Date.now() }
    this.#toastTimer = setTimeout(() => (this.toast = null), error ? 6000 : 2200)
  }

  // In-app replacement for the browser's confirm(): resolves with the value of
  // the clicked button, or null when dismissed (Esc).
  choose<T extends string>(o: { title: string; message: string; buttons: Choice<T>[] }): Promise<T | null> {
    this.ask?.resolve(null)
    return new Promise((resolve) => {
      this.ask = { ...o, resolve: resolve as (v: string | null) => void }
    })
  }

  answer(value: string | null) {
    const a = this.ask
    this.ask = null
    a?.resolve(value)
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

  moveGroup(groupId: string, targetId: string, after: boolean) {
    const groups = this.config?.groups
    if (!groups || groupId === targetId) return
    const before = order(this.config!)
    const i = groups.findIndex((g) => g.id === groupId)
    if (i < 0) return
    const [g] = groups.splice(i, 1)
    let j = groups.findIndex((x) => x.id === targetId)
    j = j < 0 ? groups.length : j + (after ? 1 : 0)
    groups.splice(j, 0, g)
    if (order(this.config!) !== before) this.save('Order updated')
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

  moveWidget(widgetId: string, targetId: string, after: boolean) {
    const list = this.config?.widgets
    if (!list || widgetId === targetId) return
    const before = list.map((w) => w.id).join()
    const i = list.findIndex((w) => w.id === widgetId)
    if (i < 0) return
    const [w] = list.splice(i, 1)
    let j = list.findIndex((x) => x.id === targetId)
    j = j < 0 ? list.length : j + (after ? 1 : 0)
    list.splice(j, 0, w)
    if (list.map((x) => x.id).join() !== before) this.save('Order updated')
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
  return cfg
}

function order(cfg: Config): string {
  return cfg.groups.map((g) => g.id + ':' + g.items.map((i) => i.id).join(',')).join('|')
}

export const app = new AppState()
