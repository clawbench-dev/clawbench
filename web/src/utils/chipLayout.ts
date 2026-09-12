/**
 * Number of chips that fit in a single row inside `availableWidth`, given each
 * chip's measured width and the `gap` (px) between adjacent chips.
 *
 * Returns `chipWidths.length` when `availableWidth` is not yet measurable
 * (0 / negative — e.g. jsdom, or before first layout), so callers degrade to
 * showing every chip instead of hiding them all.
 */
export function computeVisibleChipCount(
  chipWidths: number[],
  availableWidth: number,
  gap: number,
): number {
  if (availableWidth <= 0) return chipWidths.length
  let used = 0
  let count = 0
  for (const width of chipWidths) {
    const next = used + (count > 0 ? gap : 0) + width
    if (next > availableWidth) break
    used = next
    count++
  }
  return count
}
