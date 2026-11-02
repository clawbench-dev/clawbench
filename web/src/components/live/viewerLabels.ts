import type { LiveViewerInfo } from '@/composables/useLiveShare'

/**
 * Display name per viewer, with a `#2` suffix for duplicates.
 *
 * Nicknames are not unique (the server does not reserve them): two people may
 * both be "Alex". Everything is keyed by viewerId, so the suffix only affects
 * what is shown, never who is who.
 *
 * Shared by the participant list and the member-screen card: the card names the
 * person whose picture is on air, and it must say the same thing the list does
 * when two viewers picked the same nickname.
 */
export function viewerLabels(viewers: LiveViewerInfo[]): Record<string, string> {
  const counts = new Map<string, number>()
  const out: Record<string, string> = {}
  for (const v of viewers) {
    const base = v.name || v.id
    const n = (counts.get(base) ?? 0) + 1
    counts.set(base, n)
    out[v.id] = n > 1 ? `${base} #${n}` : base
  }
  return out
}

/** The label for one viewer, falling back to the raw id for someone who left. */
export function viewerLabel(viewers: LiveViewerInfo[], id: string): string {
  if (!id) return ''
  return viewerLabels(viewers)[id] || id
}
