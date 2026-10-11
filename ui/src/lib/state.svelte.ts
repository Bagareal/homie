import * as api from './api'
import type { Config, Group, Item, Status } from './types'

export type DragData = { kind: 'item'; id: string; groupId: string } | { kind: 'group'; id: string }

// crypto.randomUUID requires HTTPS: not available on a plain-http LAN.
export function uid(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

class AppState {
  config = $state<Config | null>(null)
  status = $state<Record<string, Status>>({})
  editing = $state(false)
  query = $state('')
  drag = $state<DragData | null>(null)
  toast = $state<{ text: string; error: boolean; key: number } | null>(null)
  #toastTimer: ReturnType<typeof setTimeout> | undefined

  async load() {
    this.config = await api.getConfig()
  }

  async save(message = 'Changes saved') {
    if (!this.config) return
    try {
      await api.putConfig($state.snapshot(this.config) as Config)
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

  notify(text: string, error = false) {
    clearTimeout(this.#toastTimer)
    this.toast = { text, error, key: Date.now() }
    this.#toastTimer = setTimeout(() => (this.toast = null), error ? 6000 : 2200)
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

function order(cfg: Config): string {
  return cfg.groups.map((g) => g.id + ':' + g.items.map((i) => i.id).join(',')).join('|')
}

export const app = new AppState()
