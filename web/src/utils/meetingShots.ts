/**
 * Shared vocabulary for meeting key frames.
 *
 * The same frame row is drawn on three surfaces — the publisher's live
 * transcript, the record opened from the app, and the record a member opens
 * from the share link — so the label and the image URL must come from one
 * place. Three copies would drift the moment a reason is renamed.
 */
import i18n from '@/i18n'

/** Trigger → i18n key. A closed set on the server; an unknown value is a
 *  frame stored by a newer client, and "画面已更新" is the honest fallback. */
const REASON_KEYS: Record<string, string> = {
  manual: 'meeting.shots.reasonManual',
  file: 'meeting.shots.reasonFile',
  sharer: 'meeting.shots.reasonSharer',
  switch: 'meeting.shots.reasonSwitch',
  speech: 'meeting.shots.reasonSpeech',
  change: 'meeting.shots.reasonChange',
}

/** "打开文件" / "成员共享" — why the frame was taken. */
export function shotReasonLabel(reason: string): string {
  return i18n.global.t(REASON_KEYS[reason] ?? 'meeting.shots.reasonChange')
}

/** The display name of a frame's file reference.
 *
 * `ref` arrives as a path from the publisher and as a bare name from the public
 * record, so both separators are split here — the row must read the same on
 * every surface.
 */
export function shotRefName(ref: string): string {
  return ref ? (ref.split(/[\\/]/).pop() ?? '') : ''
}

/** The reason plus the file's base name, when the trigger was a file.
 *
 * The file reason reads as one sentence — "打开文件 main.ts" — the same shape
 * the transcript's own file event uses, so the two rows in a meeting are not
 * labelled two different ways. Other reasons keep the separator: they pair a
 * cause with a person or a source, not a sentence with a path.
 */
export function shotLabel(reason: string, ref = ''): string {
  const reasonText = shotReasonLabel(reason)
  const name = shotRefName(ref)
  if (!name) return reasonText
  return reason === 'file' ? `${reasonText} ${name}` : `${reasonText} · ${name}`
}

/**
 * Who is asking for the picture.
 *
 * `share` is the public token-scoped endpoint a member's link uses. It follows
 * the record's publication switch, so a closed meeting without that switch
 * answers 404 — correct for a stranger, wrong for the owner.
 *
 * `owner` is the authenticated endpoint: the publisher reading their own
 * record, or the live panel while the meeting runs. It checks project ownership
 * instead of the publication switch, so the pictures the host was just watching
 * in the live transcript do not turn into broken images the moment the room
 * closes.
 */
export type ShotScope = 'share' | 'owner'

/**
 * Thumbnail URL for one stored frame.
 *
 * The server resolves the file from the frame id in both scopes: no project
 * path ever reaches the browser.
 */
export function shotThumbUrl(token: string, id: number, width = 480, scope: ShotScope = 'share'): string {
  if (scope === 'owner') {
    return `/api/live-share/notes/shot/image?token=${encodeURIComponent(token)}&id=${id}&w=${width}`
  }
  return `/api/share/${encodeURIComponent(token)}/notes/shot?id=${id}&w=${width}`
}

/**
 * The image the viewer opens, from the same endpoint pair.
 *
 * There is no "original" URL: the endpoint resizes on demand and clamps to its
 * own maximum, so the widest it will render is what the reader gets. Asking for
 * it explicitly keeps the inline `<img>` a thumbnail (fast, small) while the
 * lightbox shows the frame at a size the text on it can actually be read at.
 */
export function shotFullUrl(token: string, id: number, scope: ShotScope = 'share'): string {
  return shotThumbUrl(token, id, 1600, scope)
}
