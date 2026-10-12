// An icon can be:
// - a name from the dashboard-icons or selfh.st collections (e.g. "proxmox",
//   "truenas-scale", "yubal"), optionally with its format ("dagster.svg") when
//   it isn't available as a PNG
// - "selfhst:name" to pick the selfh.st collection explicitly
// - an image address (https://...) or a local file (/icons/name.png)
// - a character or an emoji
// If empty or it fails to load, the tile shows the service's initials.
//
// The collections are not fetched by the browser: Homie downloads each icon the
// first time it is needed and serves it from its own data folder (relative
// addresses, so it also works behind a proxy under a path).
export const CDN = 'cdn/dashboard-icons'
export const SELFHST_CDN = 'cdn/selfhst'

const FORMATS = ['png', 'svg', 'webp']

// Addresses to try, in order, until one loads. A bare name is tried in
// dashboard-icons first (PNG, then SVG and WebP: a third of that collection has
// no PNG), then in selfh.st, so a name from either site just works.
export function iconSources(icon: string): string[] {
  const v = icon.trim()
  if (!v) return []
  if (/^https?:\/\//i.test(v)) return [v]
  if (v.startsWith('/')) return [v.slice(1)] // relativo, per il funzionamento dietro proxy

  const selfhst = /^selfhst:([a-z0-9][a-z0-9-]*)(?:\.(png|svg|webp))?$/i.exec(v)
  if (selfhst) {
    const name = selfhst[1].toLowerCase()
    const ext = selfhst[2]?.toLowerCase()
    return (ext ? [ext] : FORMATS).map((e) => `${SELFHST_CDN}/${e}/${name}.${e}`)
  }

  const withExt = /^([a-z0-9][a-z0-9-]*)\.(png|svg|webp)$/i.exec(v)
  if (withExt) {
    const ext = withExt[2].toLowerCase()
    return [`${CDN}/${ext}/${withExt[1].toLowerCase()}.${ext}`]
  }

  if (/^[a-z0-9][a-z0-9-]*$/i.test(v)) {
    const name = v.toLowerCase()
    return [CDN, SELFHST_CDN].flatMap((base) => FORMATS.map((ext) => `${base}/${ext}/${name}.${ext}`))
  }
  return []
}

export function initials(name: string): string {
  const words = name.trim().split(/[\s._-]+/).filter(Boolean)
  if (words.length === 0) return '?'
  if (words.length === 1) return [...words[0]].slice(0, 2).join('').toUpperCase()
  return (words[0][0] + words[1][0]).toUpperCase()
}
