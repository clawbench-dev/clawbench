// Agent Team member colour mapping — single source of truth.
//
// CodeBuddy's teamUpdate reports a member colour by NAME (blue/green/…), not a
// hex value. Both the team roster (TeamPanel) and the permission card
// (renderToolDetail) colour members from the same palette, so the mapping lives
// here to keep them in agreement — a member must look the same in both places.

const MEMBER_COLOR_TOKENS: Record<string, string> = {
  blue: 'var(--color-info, #3b82f6)',
  green: 'var(--color-green, #16a34a)',
  red: 'var(--color-red, #ef4444)',
  yellow: 'var(--color-orange, #eab308)',
  orange: 'var(--color-orange, #f97316)',
  purple: 'var(--color-purple, #a855f7)',
  pink: '#ec4899',
  cyan: '#06b6d4',
}

/** CSS colour for a wire colour name; falls back to the secondary text token. */
export function teamMemberColorVar(color?: string | null): string {
  return MEMBER_COLOR_TOKENS[color ?? ''] ?? 'var(--text-secondary, #495057)'
}
