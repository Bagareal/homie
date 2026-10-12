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

export const percent = (n: number) => `${n >= 100 ? 100 : n.toFixed(n < 10 ? 1 : 0)}%`
