/**
 * Deterministic avatar for a meeting participant.
 *
 * Every speaker in a meeting — the publisher, each viewer, and the same person
 * in both the transcript and the chat — is drawn from their nickname alone, so
 * the icon is stable across reloads, devices and both surfaces.
 *
 *   normalize(nickname)  ->  seed  ->  DiceBear `identicon`  ->  data URI
 *
 * One pinned style for everybody, deliberately NOT the random style that
 * `generateAvatarSvg` picks for the group-chat "shuffle": a random pick re-rolls
 * on every load (the same person changes face) and mixing robot faces with
 * geometric marks reads as noise at 20dp. `identicon` is flat, survives 20dp,
 * and — the reason it wins over a letter monogram — its seed is the FULL
 * nickname, so 小明 / 小李 never collide the way 小 / 小 would.
 *
 * The SVG is cached as a data URI keyed by the seed: a transcript re-renders
 * every line on each new segment, and regenerating 40 avatars per segment would
 * be pointless work.
 */
import { generateAvatarSvgWithStyle, type AvatarStyle } from '@/utils/lazyAvatar'
import { svgToDataUri } from '@/utils/svgDataUri'

/** The one style used for every speaker. See the module comment. */
export const SPEAKER_AVATAR_STYLE: AvatarStyle = 'identicon'

/**
 * The seed a nickname maps to.
 *
 * Normalizing matters for identity, not cosmetics: the same person can arrive
 * as `"Alex"`, `"alex "` or `"访客  2"` depending on which channel announced
 * them, and a second icon for the same human is worse than none. Case folding
 * is applied to ASCII only in effect (CJK has no case), which is exactly the
 * behaviour we want.
 */
export function speakerAvatarSeed(name: string): string {
  return name.normalize('NFC').trim().replace(/\s+/g, ' ').toLowerCase()
}

/** seed -> data URI, filled once the DiceBear chunk has produced it. */
const uris = new Map<string, string>()
/** seed -> in-flight generation, so N rows asking for one name generate once. */
const inflight = new Map<string, Promise<string>>()

/** The avatar if it has already been generated, otherwise `undefined`. */
export function cachedSpeakerAvatar(name: string): string | undefined {
  return uris.get(speakerAvatarSeed(name))
}

/**
 * Resolves the data URI for a nickname, generating it at most once.
 *
 * Rejects when the DiceBear chunk cannot load (offline / stale build); callers
 * fall back to the initial rather than leaving a hole in the layout.
 */
export function speakerAvatarDataUri(name: string): Promise<string> {
  const seed = speakerAvatarSeed(name)
  const hit = uris.get(seed)
  if (hit) return Promise.resolve(hit)
  const pending = inflight.get(seed)
  if (pending) return pending
  const job = generateAvatarSvgWithStyle(seed, SPEAKER_AVATAR_STYLE)
    .then((svg) => {
      const uri = svgToDataUri(svg)
      uris.set(seed, uri)
      return uri
    })
    .finally(() => {
      inflight.delete(seed)
    })
  inflight.set(seed, job)
  return job
}

/** Test seam. */
export function _resetSpeakerAvatars(): void {
  uris.clear()
  inflight.clear()
}
