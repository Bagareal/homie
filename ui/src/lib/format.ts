const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

// 1536 -> "1.5 KB". Whole numbers from 100 up, one decimal below.
export function bytes(n: number): string {
  let i = 0
  while (n >= 1024 && i < UNITS.length - 1) {
    n /= 1024
    i++
  }
  return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)} ${UNITS[i]}`
}

export const rate = (n: number) => `${bytes(n)}/s`

// 4350 -> "1 h 12 min", 90 -> "1 min 30 s".
export function duration(sec: number): string {
  const s = Math.round(sec)
  if (s < 60) return `${s} s`
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (h > 0) return `${h} h ${m} min`
  return s % 60 === 0 || m >= 10 ? `${m} min` : `${m} min ${s % 60} s`
}

export const percent = (n: number) => `${n >= 100 ? 100 : n.toFixed(n < 10 ? 1 : 0)}%`
