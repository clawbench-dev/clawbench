/**
 * Deterministic avatar for a meeting participant.
 *
 * Every speaker in a meeting — the publisher, each viewer, and the same person
 * in both the transcript and the chat — is drawn from their nickname alone, so
 * the icon is stable across reloads, devices and both surfaces.
 *
 *   normalize(nickname)  ->  seed
 *   seed                 ->  style  (hash of the seed, from a curated set)
 *   (style, seed)        ->  DiceBear SVG  ->  data URI
 *
 * Two hashes, not one: the style is picked from the nickname as well, so a
 * meeting is not eight variations of one drawing — the point of the character
 * subset is that colleagues look like different characters.
 *
 * Both steps are pure functions of the nickname, using the same policy as group
 * member avatars (`avatarPolicy.autoAvatarStyle`). Regenerating either surface
 * therefore produces the same face, never a fresh random roll.
 *
 * The seed is the FULL nickname, not its first letter — 小明 / 小李 must not
 * collide the way a monogram would.
 *
 * The SVG is cached as a data URI keyed by the seed: a transcript re-renders
 * every line on each new segment, and regenerating 40 avatars per segment would
 * be pointless work.
 */
import { generateAvatarSvgWithStyle } from '@/utils/lazyAvatar'
import { svgToDataUri } from '@/utils/svgDataUri'
import { autoAvatarStyle, type AvatarStyle } from '@/utils/avatarPolicy'

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

/**
 * The style a nickname maps to.
 *
 * Same nickname → same style on every device, and neighbouring nicknames do
 * not walk the list in order (FNV-1a scatters them), so a meeting of five
 * people drawn from five adjacent names does not get five neighbours in the
 * style list. The policy is shared with group members, so both automatic
 * surfaces draw from the same character subset.
 */
export function speakerAvatarStyle(name: string): AvatarStyle {
  return autoAvatarStyle(speakerAvatarSeed(name))
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
  const job = generateAvatarSvgWithStyle(seed, speakerAvatarStyle(seed))
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
