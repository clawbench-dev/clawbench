import { describe, expect, it } from 'vitest'
import { shotFullUrl, shotThumbUrl } from '../meetingShots'

/**
 * Two scopes, two endpoints.
 *
 * The public one (`/api/share/...`) follows the record's publication switch and
 * is what a member's link uses. The owner one is authenticated and must be used
 * by the publisher's own surfaces — pointing them at the public endpoint made
 * every frame 404 the moment the meeting room closed (the room was the only
 * thing bypassing the switch).
 */
describe('meeting shot URLs', () => {
  it('uses the public token path for a member link', () => {
    expect(shotThumbUrl('tok-1', 7)).toBe('/api/share/tok-1/notes/shot?id=7&w=480')
    expect(shotFullUrl('tok-1', 7)).toBe('/api/share/tok-1/notes/shot?id=7&w=1600')
  })

  it('uses the authenticated path for the owner', () => {
    expect(shotThumbUrl('tok-1', 7, 480, 'owner')).toBe(
      '/api/live-share/notes/shot/image?token=tok-1&id=7&w=480',
    )
    expect(shotFullUrl('tok-1', 7, 'owner')).toBe(
      '/api/live-share/notes/shot/image?token=tok-1&id=7&w=1600',
    )
  })

  it('encodes the token on both paths', () => {
    expect(shotThumbUrl('a/b?c', 1)).toContain('/api/share/a%2Fb%3Fc/notes/shot')
    expect(shotThumbUrl('a/b?c', 1, 480, 'owner')).toContain('token=a%2Fb%3Fc')
  })
})
