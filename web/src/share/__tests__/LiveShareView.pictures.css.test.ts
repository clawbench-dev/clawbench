import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard for the viewer page's second picture.
 *
 * The host's own picture and a member's forwarded screen are SIBLINGS inside
 * `.live-zoom`. `.live-video` used to be a normal in-flow block, so the second
 * <video> was laid out BELOW the first and then clipped by `.live-stage`
 * (`overflow: hidden`). The forwarded track was live and attached, the "view
 * shared" switch worked, and the audience still saw black — measured on a
 * 900×600 stage: the member <video> sat at top=604, entirely outside the clip.
 *
 * jsdom performs no layout, so this contract can only be pinned on the CSS:
 * both pictures must occupy the same absolutely-positioned box.
 */
describe('LiveShareView pictures', () => {
  const FILE = 'src/share/LiveShareView.vue'

  /** The component's own stylesheet, comments stripped (they mention the very
   *  selectors asserted below). */
  function styleBlock(): string {
    const src = readWebFile(FILE)
    const m = src.match(/<style scoped>([\s\S]*)<\/style>/)
    expect(m).not.toBeNull()
    return (m as RegExpMatchArray)[1].replace(/\/\*[\s\S]*?\*\//g, '')
  }

  /** Every `selector { ... }` block, keeping the selector text. */
  function rules(css: string): Array<{ selector: string; body: string }> {
    const out: Array<{ selector: string; body: string }> = []
    const re = /([^{}]+)\{([^{}]*)\}/g
    let m: RegExpExecArray | null
    while ((m = re.exec(css))) {
      out.push({ selector: m[1].trim().replace(/\s+/g, ' '), body: m[2] })
    }
    return out
  }

  it('renders exactly two pictures that share the same class', () => {
    const src = readWebFile(FILE)
    // One host picture, one forwarded member picture. If a third element ever
    // uses this class it must also be part of the same overlap contract.
    expect(src.match(/class="live-video"/g)).toHaveLength(2)
  })

  it('stacks both pictures in the same box instead of one under the other', () => {
    const all = rules(styleBlock())

    const video = all.filter((r) => r.selector === '.live-video')
    expect(video).toHaveLength(1)
    expect(video[0].body).toMatch(/(^|;)\s*position:\s*absolute/)
    // `inset: 0` is what pins the picture to the shared box; `width/height:
    // 100%` alone keeps it in flow and pushes the second one out of view.
    expect(video[0].body).toMatch(/(^|;)\s*inset:\s*0\b/)

    // The box itself has to be the stage's size for the two to line up.
    const zoom = all.filter((r) => r.selector === '.live-zoom')
    expect(zoom).toHaveLength(1)
    expect(zoom[0].body).toMatch(/(^|;)\s*position:\s*absolute/)
    expect(zoom[0].body).toMatch(/(^|;)\s*inset:\s*0\b/)

    // And the stage still clips, which is why an in-flow second picture was
    // invisible rather than merely scrolled below.
    const stage = all.filter((r) => r.selector === '.live-stage')
    expect(stage).toHaveLength(1)
    expect(stage[0].body).toMatch(/(^|;)\s*overflow:\s*hidden/)
  })
})
