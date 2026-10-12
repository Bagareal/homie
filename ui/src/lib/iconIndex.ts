// The searchable list of icons: the dashboard-icons collection (built from its
// own file list and metadata) plus the selfh.st collection, which has about
// 1,800 more self-hosted apps (e.g. yubal) that dashboard-icons doesn't.
import { CDN, SELFHST_CDN } from './icons'

export type IconEntry = {
  name: string
  // What goes in a service's Icon field: the bare name, "name.svg" for icons
  // that have no PNG, or "selfhst:name" for the selfh.st collection.
  value: string
  src: string
  source: 'dashboard-icons' | 'selfh.st'
  variant: 'dark' | 'light' | ''
  // Lowercased text the search looks in.
  text: string
}

type Meta = Record<string, { aliases?: string[]; categories?: string[] }>
type Tree = { png?: string[]; svg?: string[]; webp?: string[] }
type SelfhstItem = { Name: string; Reference: string; Tags?: string; Category?: string; Light?: string; Dark?: string }

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

const variantOf = (name: string) => (name.endsWith('-dark') ? 'dark' : name.endsWith('-light') ? 'light' : '')

async function build(): Promise<IconEntry[]> {
  const [tree, meta, selfhst] = await Promise.all([
    fetchJSON<Tree>(`${CDN}/index/tree.json`),
    // These two only add icons and search terms, so the picker still works
    // (with fewer results) if either can't be loaded.
    fetchJSON<Meta>(`${CDN}/index/metadata.json`).catch(() => ({}) as Meta),
    fetchJSON<SelfhstItem[]>(`${SELFHST_CDN}/index/index.json`).catch(() => [] as SelfhstItem[]),
  ])
  const names = (files: string[] | undefined, ext: string) => new Set((files ?? []).map((f) => f.slice(0, -(ext.length + 1))))
  const png = names(tree.png, 'png')
  const svg = names(tree.svg, 'svg')
  const webp = names(tree.webp, 'webp')

  const entries = new Map<string, IconEntry>()
  for (const name of new Set([...png, ...svg, ...webp])) {
    const ext = png.has(name) ? 'png' : svg.has(name) ? 'svg' : 'webp'
    const info = meta[name] ?? meta[name.replace(/-(dark|light)$/, '')]
    entries.set(name, {
      name,
      value: ext === 'png' ? name : `${name}.${ext}`,
      src: `${CDN}/${ext}/${name}.${ext}`,
      source: 'dashboard-icons',
      variant: variantOf(name),
      text: [name, name.replace(/-/g, ' '), ...(info?.aliases ?? []), ...(info?.categories ?? [])].join(' ').toLowerCase(),
    })
  }

  // selfh.st: only what dashboard-icons doesn't already have. Every selfh.st
  // icon has a PNG; "Light" / "Dark" say whether -light / -dark versions exist.
  for (const item of selfhst) {
    const ref = item.Reference
    if (!ref || entries.has(ref)) continue
    const text = [ref, item.Name, ref.replace(/-/g, ' '), item.Tags, item.Category].join(' ').toLowerCase()
    const add = (name: string) => {
      if (entries.has(name)) return
      entries.set(name, {
        name,
        value: `selfhst:${name}`,
        src: `${SELFHST_CDN}/png/${name}.png`,
        source: 'selfh.st',
        variant: variantOf(name),
        text,
      })
    }
    add(ref)
    if (item.Light === 'Yes') add(`${ref}-light`)
    if (item.Dark === 'Yes') add(`${ref}-dark`)
  }
  return [...entries.values()].sort((a, b) => a.name.localeCompare(b.name))
}

let loading: Promise<IconEntry[]> | null = null

// Downloaded once, the first time the picker opens.
export function loadIconIndex(): Promise<IconEntry[]> {
  loading ??= build().catch((e) => {
    loading = null
    throw e
  })
  return loading
}

export function searchIcons(all: IconEntry[], query: string): IconEntry[] {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return all
  const q = terms.join(' ')
  const scored: [number, IconEntry][] = []
  for (const e of all) {
    if (!terms.every((t) => e.text.includes(t))) continue
    const n = e.name.replace(/-/g, ' ')
    scored.push([e.name === q ? 0 : n.startsWith(q) ? 1 : n.includes(q) ? 2 : 3, e])
  }
  return scored.sort((a, b) => a[0] - b[0]).map(([, e]) => e)
}
