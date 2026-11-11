/**
 * Where to set `scrollTop` on the activity list so `item` is fully visible, or null when it already is.
 * Scrolling the list itself (instead of `scrollIntoView`) keeps the page, and therefore the video, still.
 */
export function nextScrollTop(
  view: { scrollTop: number; clientHeight: number },
  item: { offsetTop: number; offsetHeight: number },
  pad = 8,
): number | null {
  const itemBottom = item.offsetTop + item.offsetHeight
  const viewBottom = view.scrollTop + view.clientHeight
  if (item.offsetTop < view.scrollTop) return Math.max(0, item.offsetTop - pad)
  if (itemBottom > viewBottom) return itemBottom - view.clientHeight + pad
  return null
}
