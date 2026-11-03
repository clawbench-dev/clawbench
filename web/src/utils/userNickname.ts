import { GROUP_USER_TARGET_NAME } from '@/utils/groupRouting'

/**
 * Resolve the account nickname from the `user` config section.
 *
 * Empty means "not configured". The fallback literal is ALSO treated as not
 * configured: it is applied where the name is used (group routing, role
 * labels), never stored, so pre-filling it would make every unconfigured
 * account look like the same person — and in a meeting the publisher could not
 * tell those viewers apart.
 */
export function resolveUserNickname(user: unknown): string {
  const v = user && typeof user === 'object' ? (user as Record<string, unknown>).nickname : undefined
  const name = typeof v === 'string' ? v.trim() : ''
  return name === GROUP_USER_TARGET_NAME ? '' : name
}
