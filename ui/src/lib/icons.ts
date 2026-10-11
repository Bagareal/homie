// An icon can be:
// - a name from the dashboard-icons collection (e.g. "proxmox", "truenas-scale", "adguard-home")
// - an image address (https://...) or a local file (/icons/name.png)
// - a character or an emoji
// If empty or it fails to load, the tile shows the service's initials.
const CDN = 'https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/png'

export function iconSrc(icon: string): string | null {
  const v = icon.trim()
  if (!v) return null
  if (/^https?:\/\//i.test(v)) return v
  if (v.startsWith('/')) return v.slice(1) // relativo, per il funzionamento dietro proxy
  if (/^[a-z0-9][a-z0-9-]*$/i.test(v)) return `${CDN}/${v.toLowerCase()}.png`
  return null
}

export function initials(name: string): string {
  const words = name.trim().split(/[\s._-]+/).filter(Boolean)
  if (words.length === 0) return '?'
  if (words.length === 1) return [...words[0]].slice(0, 2).join('').toUpperCase()
  return (words[0][0] + words[1][0]).toUpperCase()
}
