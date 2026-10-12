// The searchable list of the dashboard-icons collection, built from the
// collection's own file list and metadata (names, aliases, categories).
import { CDN } from './icons'

export type IconEntry = {
  name: string
  // What goes in a service's Icon field: the bare name, or "name.svg" /
  // "name.webp" for icons that have no PNG.
  value: string
  src: string
  variant: 'dark' | 'light' | ''
  // Lowercased text the search looks in.
  text: string
}

type Meta = Record<string, { aliases?: string[]; categories?: string[] }>
type Tree = { png?: string[]; svg?: string[]; webp?: string[] }

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

async function build(): Promise<IconEntry[]> {
  const [tree, meta] = await Promise.all([
    fetchJSON<Tree>(`${CDN}/tree.json`),
    // Only adds search terms, so searching by name still works without it.
    fetchJSON<Meta>(`${CDN}/metadata.json`).catch(() => ({}) as Meta),
  ])
  const names = (files: string[] | undefined, ext: string) => new Set((files ?? []).map((f) => f.slice(0, -(ext.length + 1))))
  const png = names(tree.png, 'png')
  const svg = names(tree.svg, 'svg')
  const webp = names(tree.webp, 'webp')

  const entries: IconEntry[] = []
  for (const name of new Set([...png, ...svg, ...webp])) {
    const ext = png.has(name) ? 'png' : svg.has(name) ? 'svg' : 'webp'
    const variant = name.endsWith('-dark') ? 'dark' : name.endsWith('-light') ? 'light' : ''
    const info = meta[name] ?? meta[name.replace(/-(dark|light)$/, '')]
    entries.push({
      name,
      value: ext === 'png' ? name : `${name}.${ext}`,
      src: `${CDN}/${ext}/${name}.${ext}`,
      variant,
      text: [name, name.replace(/-/g, ' '), ...(info?.aliases ?? []), ...(info?.categories ?? [])].join(' ').toLowerCase(),
    })
  }
  return entries.sort((a, b) => a.name.localeCompare(b.name))
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
