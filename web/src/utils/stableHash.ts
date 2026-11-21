/**
 * FNV-1a 32-bit hash.
 *
 * Shared by anything that must map a name to a stable value across devices and
 * reloads (meeting avatars, avatar background colours). Kept dependency-free and
 * deliberately simple: the output only needs to be stable, not cryptographic.
 */
export function fnv1a32(input: string): number {
  let h = 0x811c9dc5
  for (const ch of input) {
    h ^= ch.codePointAt(0) ?? 0
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return h >>> 0
}
