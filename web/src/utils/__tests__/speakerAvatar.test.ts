import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * The speaker avatar is the only thing that tells two participants apart in a
 * transcript read back later, so its mapping has to be a pure function of the
 * nickname: same name → same icon on every reload and every device, different
 * names → different icons.
 *
 * The DiceBear runtime is stubbed (it is a dynamic import of ~61KB plus a style
 * JSON); what is under test here is the mapping, not the renderer.
 */
const generate = vi.hoisted(() => vi.fn(async (seed: string, style: string) => `<svg data-seed="${seed}" data-style="${style}"/>`))

vi.mock('@/utils/lazyAvatar', () => ({
  generateAvatarSvgWithStyle: generate,
}))

import {
  SPEAKER_AVATAR_STYLE,
  _resetSpeakerAvatars,
  cachedSpeakerAvatar,
  speakerAvatarDataUri,
  speakerAvatarSeed,
} from '@/utils/speakerAvatar'

beforeEach(() => {
  generate.mockClear()
  _resetSpeakerAvatars()
})

describe('speakerAvatarSeed', () => {
  it('folds the spellings of one nickname onto a single seed', () => {
    // The same person arrives with different whitespace/case depending on which
    // channel announced them; a second icon for one human is worse than none.
    expect(speakerAvatarSeed(' Alex ')).toBe('alex')
    expect(speakerAvatarSeed('访客  2')).toBe('访客 2')
    expect(speakerAvatarSeed('王工')).toBe('王工')
  })

  it('treats composed and decomposed unicode as the same name', () => {
    expect(speakerAvatarSeed('Jose\u0301')).toBe(speakerAvatarSeed('José'))
  })

  it('keeps different names different', () => {
    const names = ['我', '小明', '小李', '访客 2', 'Alex', 'Sarah']
    const seeds = new Set(names.map(speakerAvatarSeed))
    expect(seeds.size).toBe(names.length)
  })
})

describe('speakerAvatarDataUri', () => {
  it('pins the style instead of rolling one per call', async () => {
    // A random style (what the group-chat shuffle does) re-rolls on reload, so
    // the same participant would change face between sessions.
    await speakerAvatarDataUri('小明')
    expect(generate).toHaveBeenCalledWith('小明', SPEAKER_AVATAR_STYLE)
  })

  it('generates once per nickname and serves the rest from the cache', async () => {
    const first = await speakerAvatarDataUri('小明')
    const second = await speakerAvatarDataUri(' 小明 ')
    expect(second).toBe(first)
    expect(generate).toHaveBeenCalledTimes(1)
    expect(cachedSpeakerAvatar('小明')).toBe(first)
  })

  it('shares one in-flight generation between concurrent callers', async () => {
    // A transcript renders every line at once; 40 rows must not start 40 jobs.
    await Promise.all([speakerAvatarDataUri('小明'), speakerAvatarDataUri('小明'), speakerAvatarDataUri('小明')])
    expect(generate).toHaveBeenCalledTimes(1)
  })

  it('gives different names different icons', async () => {
    const a = await speakerAvatarDataUri('小明')
    const b = await speakerAvatarDataUri('小李')
    expect(a).not.toBe(b)
  })

  it('returns a data URI, not raw markup', async () => {
    const uri = await speakerAvatarDataUri('小明')
    expect(uri.startsWith('data:image/svg+xml')).toBe(true)
    expect(uri).not.toContain('<svg')
  })
})
