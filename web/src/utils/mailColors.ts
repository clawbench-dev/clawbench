/**
 * Colour assignment for the mail panel.
 *
 * The panel used to be monochrome: the only colour in it was the theme's single
 * accent (unread edge, unread badges, the open row), so a mailbox of thirty
 * messages read as one grey block. These helpers spread the theme's *own*
 * status tokens over senders, accounts and attachments — no new colours are
 * introduced, so every one of the 36 themes keeps working and light/dark both
 * stay legible.
 *
 * Sender hues are derived from a stable hash of the address rather than picked
 * at random: the same person must keep the same colour across reloads, devices
 * and sessions, otherwise the colour carries no information at all.
 */

/**
 * Palette a sender/account hue is picked from.
 *
 * Theme tokens only, and deliberately excludes `--color-yellow`: it is the one
 * status token that several light themes keep quite pale, and these hues are
 * used as *text* on an 18% tint of themselves.
 */
export const MAIL_HUES = [
  'var(--color-green)',
  'var(--color-purple)',
  'var(--color-info)',
  'var(--color-orange)',
  'var(--color-red)',
  'var(--accent-color)',
] as const

/**
 * djb2 + the murmur3 finalizer.
 *
 * The avalanche step is not decoration: plain djb2 leaves short, similar
 * addresses (`boss@`, `support@`, `noreply@`) clustered in the low bits, and
 * `% 6` then hands most of an inbox the same colour — the exact monotony this
 * module exists to fix.
 */
function mailHash(key: string): number {
  let h = 5381
  for (let i = 0; i < key.length; i++) {
    h = ((h << 5) + h + key.charCodeAt(i)) >>> 0
  }
  h ^= h >>> 15
  h = Math.imul(h, 2246822507) >>> 0
  h ^= h >>> 13
  h = Math.imul(h, 3266489909) >>> 0
  h ^= h >>> 16
  return h >>> 0
}

/**
 * Deterministic hue for a sender address (or any stable string key such as an
 * account address). Empty keys fall back to the accent so a message with no
 * `From:` header still gets a sane avatar.
 */
export function mailHue(key: string): string {
  const normalized = (key || '').trim().toLowerCase()
  if (!normalized) return MAIL_HUES[MAIL_HUES.length - 1]
  return MAIL_HUES[mailHash(normalized) % MAIL_HUES.length]
}

/**
 * First glyph of the sender, for the avatar circle.
 *
 * `?` rather than an empty string: an avatar with no letter reads as a broken
 * circle, while a question mark reads as "unknown sender".
 */
export function mailInitial(name: string, addr: string): string {
  const base = (name || addr || '').trim()
  if (!base) return '?'
  // Array.from, not charAt: a name may start with an emoji or a surrogate pair.
  return Array.from(base)[0].toUpperCase()
}

/** What kind of file an attachment is, for icon + colour. */
export type MailAttachmentKind = 'image' | 'pdf' | 'archive' | 'sheet' | 'doc' | 'other'

const IMAGE_EXT = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'heic', 'avif', 'ico']
const ARCHIVE_EXT = ['zip', 'rar', '7z', 'tar', 'gz', 'tgz', 'bz2', 'xz']
const SHEET_EXT = ['xls', 'xlsx', 'csv', 'ods', 'numbers']
const DOC_EXT = ['doc', 'docx', 'odt', 'rtf', 'txt', 'md', 'ppt', 'pptx', 'odp', 'key']

/**
 * Classify an attachment by extension, falling back to the declared content
 * type. Extension first because that is what the user sees in the filename;
 * `application/octet-stream` is far too common to be authoritative.
 */
export function mailAttachmentKind(filename: string, contentType = ''): MailAttachmentKind {
  const name = (filename || '').toLowerCase()
  const dot = name.lastIndexOf('.')
  const ext = dot >= 0 ? name.slice(dot + 1) : ''
  const type = (contentType || '').toLowerCase()

  if (IMAGE_EXT.includes(ext) || type.startsWith('image/')) return 'image'
  if (ext === 'pdf' || type === 'application/pdf') return 'pdf'
  if (ARCHIVE_EXT.includes(ext)) return 'archive'
  if (SHEET_EXT.includes(ext)) return 'sheet'
  if (DOC_EXT.includes(ext) || type.startsWith('text/')) return 'doc'
  return 'other'
}
