// Keeps --bar-h equal to the top bar's height (it wraps on narrow screens), so
// the scrolling content below knows where the bar ends.
export function barHeight(node: HTMLElement) {
  const update = () => document.documentElement.style.setProperty('--bar-h', `${node.offsetHeight}px`)
  const ro = new ResizeObserver(update)
  ro.observe(node)
  update()
  return { destroy: () => ro.disconnect() }
}
