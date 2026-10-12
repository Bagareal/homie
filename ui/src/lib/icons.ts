// An icon can be:
// - a name from the dashboard-icons collection (e.g. "proxmox", "truenas-scale", "adguard-home"),
//   optionally with its format ("dagster.svg") when it isn't available as a PNG
// - an image address (https://...) or a local file (/icons/name.png)
// - a character or an emoji
// If empty or it fails to load, the tile shows the service's initials.
export const CDN = 'https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons'

// Addresses to try, in order, until one loads. A bare name is tried as a PNG
// first, then SVG and WebP: a third of the collection has no PNG at all.
export function iconSources(icon: string): string[] {
  const v = icon.trim()
  if (!v) return []
  if (/^https?:\/\//i.test(v)) return [v]
  if (v.startsWith('/')) return [v.slice(1)] // relativo, per il funzionamento dietro proxy
  const withExt = /^([a-z0-9][a-z0-9-]*)\.(png|svg|webp)$/i.exec(v)
  if (withExt) {
    const ext = withExt[2].toLowerCase()
    return [`${CDN}/${ext}/${withExt[1].toLowerCase()}.${ext}`]
  }
  if (/^[a-z0-9][a-z0-9-]*$/i.test(v)) {
    const name = v.toLowerCase()
    return ['png', 'svg', 'webp'].map((ext) => `${CDN}/${ext}/${name}.${ext}`)
  }
  return []
}

export function initials(name: string): string {
  const words = name.trim().split(/[\s._-]+/).filter(Boolean)
  if (words.length === 0) return '?'
  if (words.length === 1) return [...words[0]].slice(0, 2).join('').toUpperCase()
  return (words[0][0] + words[1][0]).toUpperCase()
}
