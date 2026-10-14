// Masonry for the dashboard: every group card and widget goes into the column that
// is shortest so far, in the order the user arranged them, so there are no gaps and
// no rows of equal height. The container is a CSS grid whose rows are 1 px high
// (see .shelves.masonry in app.css); each block is placed by hand on the column and
// the rows it takes, from its measured height.
const GAP = 20

type Params = { on: boolean; cols: number }

export function masonry(node: HTMLElement, params: Params) {
  let { on, cols } = params
  let frame = 0
  const watched = new Set<Element>()
  const resizes = new ResizeObserver(schedule)

  function blocks(): HTMLElement[] {
    return Array.from(node.children).filter((c): c is HTMLElement => c instanceof HTMLElement && (c.classList.contains('shelf') || c.classList.contains('widget')))
  }

  function place() {
    frame = 0
    const kids = blocks()
    // Keep watching the blocks that are on the page now, and only those.
    for (const k of kids) if (!watched.has(k)) (watched.add(k), resizes.observe(k))
    for (const w of watched) if (!kids.includes(w as HTMLElement)) (watched.delete(w), resizes.unobserve(w))

    if (!on) {
      for (const k of kids) {
        k.style.gridColumn = ''
        k.style.gridRow = ''
      }
      return
    }
    const heights = new Array<number>(Math.max(1, cols)).fill(0)
    for (const k of kids) {
      // A block keeps its own height (the grid never stretches it), so this is
      // the same before and after it is placed.
      const h = Math.ceil(k.getBoundingClientRect().height)
      let c = 0
      for (let i = 1; i < heights.length; i++) if (heights[i] < heights[c]) c = i
      k.style.gridColumn = String(c + 1)
      k.style.gridRow = `${heights[c] + 1} / span ${Math.max(1, h)}`
      heights[c] += h + GAP
    }
  }

  function schedule() {
    if (!frame) frame = requestAnimationFrame(place)
  }

  // Blocks come and go while editing, or when a search narrows the list.
  const changes = new MutationObserver(schedule)
  changes.observe(node, { childList: true })
  schedule()

  return {
    update(next: Params) {
      on = next.on
      cols = next.cols
      schedule()
    },
    destroy() {
      if (frame) cancelAnimationFrame(frame)
      resizes.disconnect()
      changes.disconnect()
    },
  }
}
