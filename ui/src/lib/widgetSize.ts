// Widgets are sized in groups: 1, 2 or 3 group cards wide, and 1, 2 or 3 rows of
// tiles high. The same sums are in app.css (--card-w, --card-head) for the page;
// these are for the editor, which draws the widget in pixels.
const CARD_PAD = 16 // the padding of a group card, as in app.css
const CARD_GAP = 20 // the space between cards

function metrics() {
  const cs = getComputedStyle(document.documentElement)
  return {
    tile: parseFloat(cs.getPropertyValue('--tile-min')) || 120,
    gap: parseFloat(cs.getPropertyValue('--gap')) || 16,
    border: parseFloat(cs.getPropertyValue('--group-border-w')) || 1,
    rem: parseFloat(cs.fontSize) || 16,
  }
}

// One group card: three tiles, its padding and its border.
export function cardWidth(): number {
  const m = metrics()
  return 3 * m.tile + 2 * m.gap + 2 * CARD_PAD + 2 * m.border
}

export function groupWidth(cols: number): number {
  return cols * cardWidth() + (cols - 1) * CARD_GAP
}

// A group card with `rows` rows of tiles: the rows, the padding, the border and
// the title above them.
export function groupHeight(rows: number): number {
  const m = metrics()
  return rows * m.tile + (rows - 1) * m.gap + 2 * CARD_PAD + 2 * m.border + 0.85 * m.rem * 1.2 + 10
}
