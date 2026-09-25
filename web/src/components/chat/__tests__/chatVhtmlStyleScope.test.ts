import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Chat bubble: classes that only ever appear on v-html-injected elements must
 * be styled from the NON-scoped <style> block.
 *
 * Vue only stamps the scope attribute (`data-v-xxxx`) on elements it compiles
 * from the template. Nodes inserted through `v-html` are invisible to that
 * pass, so a scoped rule like `.chat-video-player[data-v-…]` can never match
 * them — the declaration silently does nothing. Verified in the built bundle:
 *
 *   scoped   : .chat-video-player[data-v-bed9e482]{width:100%;max-width:400px}
 *   unscoped : .chat-message .chat-audio-player{width:100%;max-width:280px}
 *
 * The video rule shipped in the scoped block and its player therefore rendered
 * at the video's intrinsic resolution, overflowing the bubble (issue #497).
 * Audio had already been moved out for the same reason; video was missed.
 *
 * jsdom has no CSS engine and the scope attribute is applied at compile time,
 * so this is a source-contract check (the same pattern chatLeakedControlWrap
 * and chatMetaBarAlignment use).
 */
describe('chat bubble: v-html target classes must be styled from the non-scoped block', () => {
  const source = readWebFile('src/components/chat/ChatMessageItem.vue')

  /**
   * Every `<style …>` block in the file, tagged with whether it is scoped.
   * A file may hold several blocks and their order is not part of the contract,
   * so we locate each rule by scanning all of them rather than assuming the
   * global block is last.
   */
  const blocks: Array<{ scoped: boolean; body: string }> = []
  {
    const re = /<style([^>]*)>([\s\S]*?)<\/style>/g
    let m: RegExpExecArray | null
    while ((m = re.exec(source)) !== null) {
      blocks.push({ scoped: /\bscoped\b/.test(m[1]), body: m[2] })
    }
  }

  /** All declaration blocks whose selector mentions `cls` as a class token. */
  function rulesFor(cls: string): Array<{ scoped: boolean; selector: string; decls: string }> {
    const out: Array<{ scoped: boolean; selector: string; decls: string }> = []
    // Selectors may be a comma list; split so `.a, .b { }` reports each part.
    const ruleRe = /([^{}]+)\{([^{}]*)\}/g
    for (const block of blocks) {
      let m: RegExpExecArray | null
      while ((m = ruleRe.exec(block.body)) !== null) {
        const selectors = m[1].split(',').map(s => s.trim())
        for (const selector of selectors) {
          // `\b` after the class keeps `.chat-img` from matching `.chat-image-x`.
          if (new RegExp(`\\.${cls}\\b`).test(selector)) {
            out.push({ scoped: block.scoped, selector, decls: m[2] })
          }
        }
      }
    }
    return out
  }

  it('parses both style blocks and can tell them apart', () => {
    // Guard the guard: if the regexes stop matching (e.g. the blocks are
    // restructured), every assertion below would vacuously pass.
    expect(blocks.length, 'ChatMessageItem must keep at least two <style> blocks').toBeGreaterThanOrEqual(2)
    expect(blocks.some(b => b.scoped), 'one block must be scoped').toBe(true)
    expect(blocks.some(b => !b.scoped), 'one block must be non-scoped').toBe(true)
  })

  // Every class produced by the chat render pipeline that ends up inside the
  // v-html body. Each one must have its rule(s) in the non-scoped block only.
  const VHTML_CLASSES = [
    'chat-video-wrapper',
    'chat-video-player',
    'chat-audio-wrapper',
    'chat-audio-player',
    'chat-img',
  ]

  for (const cls of VHTML_CLASSES) {
    it(`styles .${cls} only from the non-scoped block`, () => {
      const rules = rulesFor(cls)
      expect(rules.length, `.${cls} must have at least one style rule`).toBeGreaterThan(0)
      const scopedRules = rules.filter(r => r.scoped)
      expect(
        scopedRules.map(r => r.selector),
        `.${cls} is injected via v-html, so a scoped rule can never match it`,
      ).toEqual([])
    })
  }

  it('sizes the video player so it cannot overflow the bubble', () => {
    // The regression was an unsized <video>; the fix must keep a width cap.
    const player = rulesFor('chat-video-player').find(r => !r.scoped)
    expect(player, '.chat-video-player must be defined in the non-scoped block').toBeDefined()
    expect(player!.decls, 'must cap the rendered width').toMatch(/max-width:\s*\d/)
    expect(player!.decls, 'must not exceed the bubble').toMatch(/width:\s*100%/)
  })

  it('anchors the video rules to .chat-message so they cannot leak app-wide', () => {
    // Moving out of the scoped block removes the automatic scope, so the
    // selector must carry its own anchor — mirroring the audio block.
    for (const cls of ['chat-video-wrapper', 'chat-video-player']) {
      for (const rule of rulesFor(cls)) {
        expect(
          rule.selector,
          `.${cls} must stay anchored to .chat-message`,
        ).toContain('.chat-message')
      }
    }
  })

  it('drops the dead .chat-image-thumb rule', () => {
    // No template, script or pipeline emits this class — it was a leftover.
    expect(rulesFor('chat-image-thumb')).toEqual([])
  })
})
