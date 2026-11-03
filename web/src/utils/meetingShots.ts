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

/** The reason plus the file's base name, when the trigger was a file. */
export function shotLabel(reason: string, ref = ''): string {
  const reasonText = shotReasonLabel(reason)
  const name = shotRefName(ref)
  return name ? `${reasonText} · ${name}` : reasonText
}

/**
 * Thumbnail URL for one stored frame.
 *
 * Token-scoped, so it works for the publisher (who has the token in hand) and
 * for a member (for whom the token IS the credential). The server resolves the
 * file from the frame id: no project path ever reaches the browser.
 */
export function shotThumbUrl(token: string, id: number, width = 480): string {
  return `/api/share/${encodeURIComponent(token)}/notes/shot?id=${id}&w=${width}`
}

/**
 * The image the viewer opens, from the same token-scoped endpoint.
 *
 * There is no "original" URL: the endpoint resizes on demand and clamps to its
 * own maximum, so the widest it will render is what the reader gets. Asking for
 * it explicitly keeps the inline `<img>` a thumbnail (fast, small) while the
 * lightbox shows the frame at a size the text on it can actually be read at.
 */
export function shotFullUrl(token: string, id: number): string {
  return shotThumbUrl(token, id, 1600)
}
