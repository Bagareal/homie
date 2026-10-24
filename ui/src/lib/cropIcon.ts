// Trims the empty margin around an icon before it is uploaded, so an icon drawn
// small inside a big canvas fills its tile like the ones from the collections.
// Works in the browser: the picture is drawn on a canvas, the box around the
// visible pixels is measured, and the file is rewritten to that box (a PNG is
// cropped, an SVG gets a tighter viewBox, so it stays a vector).
// If anything is unusual (no margin, unreadable, unknown size) the original file
// is returned untouched.

const SCAN = 512 // longest side of the canvas used to measure the margin
const ALPHA = 8 // pixels fainter than this count as empty
const SKIP = 0.97 // already filling this much of both sides: leave it alone

interface Box {
  x0: number
  y0: number
  x1: number
  y1: number
}

function load(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = () => reject(new Error('could not read the image'))
    img.src = url
  })
}

// Box (in canvas pixels, 0..1 fractions) around the visible pixels, or null when
// the picture is empty or already fills the frame.
function visibleBox(img: HTMLImageElement, w: number, h: number): Box | null {
  const canvas = document.createElement('canvas')
  const s = Math.min(1, SCAN / Math.max(w, h))
  const cw = Math.max(1, Math.round(w * s))
  const ch = Math.max(1, Math.round(h * s))
  canvas.width = cw
  canvas.height = ch
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  if (!ctx) return null
  ctx.drawImage(img, 0, 0, cw, ch)
  const data = ctx.getImageData(0, 0, cw, ch).data
  let x0 = cw
  let y0 = ch
  let x1 = -1
  let y1 = -1
  for (let y = 0; y < ch; y++) {
    for (let x = 0; x < cw; x++) {
      if (data[(y * cw + x) * 4 + 3] > ALPHA) {
        if (x < x0) x0 = x
        if (x > x1) x1 = x
        if (y < y0) y0 = y
        if (y > y1) y1 = y
      }
    }
  }
  if (x1 < 0) return null
  // one pixel of air, as fractions of the whole picture
  const box = { x0: Math.max(0, x0 - 1) / cw, y0: Math.max(0, y0 - 1) / ch, x1: Math.min(cw, x1 + 2) / cw, y1: Math.min(ch, y1 + 2) / ch }
  if (box.x1 - box.x0 >= SKIP && box.y1 - box.y0 >= SKIP) return null
  return box
}

function num(v: string | null): number {
  const n = parseFloat(v ?? '')
  return Number.isFinite(n) && n > 0 ? n : 0
}

async function cropSVG(file: File): Promise<File> {
  const doc = new DOMParser().parseFromString(await file.text(), 'image/svg+xml')
  const svg = doc.documentElement
  if (svg.localName !== 'svg' || doc.querySelector('parsererror')) return file

  let vb = (svg.getAttribute('viewBox') ?? '').trim().split(/[\s,]+/).map(parseFloat)
  if (vb.length !== 4 || vb.some((n) => !Number.isFinite(n)) || vb[2] <= 0 || vb[3] <= 0) {
    const w = num(svg.getAttribute('width'))
    const h = num(svg.getAttribute('height'))
    if (!w || !h) return file
    vb = [0, 0, w, h]
  }
  const [vx, vy, vw, vh] = vb

  // Draw it at a known size (an SVG with only a viewBox has no natural size).
  const k = SCAN / Math.max(vw, vh)
  const probe = svg.cloneNode(true) as Element
  probe.setAttribute('viewBox', `${vx} ${vy} ${vw} ${vh}`)
  probe.setAttribute('width', String(Math.max(1, Math.round(vw * k))))
  probe.setAttribute('height', String(Math.max(1, Math.round(vh * k))))
  const url = URL.createObjectURL(new Blob([new XMLSerializer().serializeToString(probe)], { type: 'image/svg+xml' }))
  try {
    const img = await load(url)
    const box = visibleBox(img, img.width, img.height)
    if (!box) return file
    const nx = vx + box.x0 * vw
    const ny = vy + box.y0 * vh
    const nw = (box.x1 - box.x0) * vw
    const nh = (box.y1 - box.y0) * vh
    svg.setAttribute('viewBox', `${+nx.toFixed(3)} ${+ny.toFixed(3)} ${+nw.toFixed(3)} ${+nh.toFixed(3)}`)
    svg.removeAttribute('width')
    svg.removeAttribute('height')
    const out = new XMLSerializer().serializeToString(svg)
    return new File([out], file.name, { type: 'image/svg+xml' })
  } finally {
    URL.revokeObjectURL(url)
  }
}

async function cropPNG(file: File): Promise<File> {
  const url = URL.createObjectURL(file)
  try {
    const img = await load(url)
    const w = img.naturalWidth
    const h = img.naturalHeight
    if (!w || !h) return file
    const box = visibleBox(img, w, h)
    if (!box) return file
    const sx = Math.floor(box.x0 * w)
    const sy = Math.floor(box.y0 * h)
    const sw = Math.max(1, Math.ceil(box.x1 * w) - sx)
    const sh = Math.max(1, Math.ceil(box.y1 * h) - sy)
    const canvas = document.createElement('canvas')
    canvas.width = sw
    canvas.height = sh
    const ctx = canvas.getContext('2d')
    if (!ctx) return file
    ctx.drawImage(img, sx, sy, sw, sh, 0, 0, sw, sh)
    const blob = await new Promise<Blob | null>((r) => canvas.toBlob(r, 'image/png'))
    if (!blob || blob.size > 1 << 20) return file
    return new File([blob], file.name, { type: 'image/png' })
  } finally {
    URL.revokeObjectURL(url)
  }
}

export async function cropIcon(file: File): Promise<File> {
  try {
    if (file.type === 'image/svg+xml' || /\.svg$/i.test(file.name)) return await cropSVG(file)
    if (file.type === 'image/png' || /\.png$/i.test(file.name)) return await cropPNG(file)
  } catch {
    // fall through: the server validates the original
  }
  return file
}
