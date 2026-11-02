import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * 创建会议 and 会中「基础配置」 are two screens of ONE form: the same heading,
 * the same hint line, the same checkbox row, and the same picture / camera /
 * microphone controls.
 *
 * They used to be two scoped copies (`.meeting-*` on the create page,
 * `.live-dialog-*` on the stage), and they drifted: the stage drew the screen
 * as a checkbox plus a separate "切换共享源" button while the create page drew
 * the identical decision as a grid of frames, and its hint lines lost the icon
 * gutter. This file pins the shared sheet and the shared controls so the two
 * cannot drift again.
 *
 * jsdom applies no stylesheets and cannot see a class name that exists on only
 * one of two screens, so this reads the sources.
 */

const ROOT = join(__dirname, '..', '..', '..', '..', '..')

const SHARED = 'web/src/assets/meeting-form.css'
const CREATE = 'web/src/components/live/MeetingCreate.vue'
const STAGE = 'web/src/components/live/MeetingStage.vue'

function sourceOf(file: string): string {
  return readFileSync(join(ROOT, file), 'utf8')
}

function styleOf(file: string): string {
  const src = sourceOf(file)
  const start = src.indexOf('<style')
  if (start < 0) throw new Error(`${file} has no <style> block`)
  return src.slice(start)
}

function ruleDecls(css: string, selector: string): Record<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))
  if (!m) throw new Error(`rule ${selector} not found`)
  const out: Record<string, string> = {}
  for (const decl of m[1].split(';')) {
    const i = decl.indexOf(':')
    if (i < 0) continue
    const prop = decl.slice(0, i).trim()
    const value = decl.slice(i + 1).trim().replace(/\s+/g, ' ')
    if (prop) out[prop] = value
  }
  return out
}

const PRIMITIVES = ['.meeting-section', '.meeting-input', '.meeting-hint', '.meeting-check']

describe('one meeting form language', () => {
  it('declares every primitive in the shared stylesheet', () => {
    const css = sourceOf(SHARED)
    for (const selector of PRIMITIVES) {
      expect(css, `${SHARED} must define ${selector}`).toMatch(
        new RegExp(`${selector.replace(/\./g, '\\.')}\\s*\\{`),
      )
    }
  })

  it.each([CREATE, STAGE])('%s loads the shared stylesheet', (file) => {
    expect(sourceOf(file)).toContain("import '@/assets/meeting-form.css'")
  })

  it.each([CREATE, STAGE])('%s keeps no second copy of the primitives', (file) => {
    // The sheet is global; a scoped copy would compile to (0,2,0) and silently
    // out-rank it, which is exactly how the two screens drifted.
    const css = styleOf(file)
    for (const selector of PRIMITIVES) {
      expect(css, `${file} must not redeclare ${selector}`).not.toMatch(
        new RegExp(`${selector.replace(/\./g, '\\.')}\\s*\\{`),
      )
    }
  })

  it('asks the picture question with the same grid on both screens', () => {
    for (const file of [CREATE, STAGE]) {
      const src = sourceOf(file)
      // "Share nothing" is a tile, not a checkbox: the host compares it against
      // the actual screens side by side.
      expect(src, `${file} must render the shared source grid`).toMatch(/<CaptureSourceGrid\b/)
      expect(src, `${file} must offer the 不共享 tile`).toMatch(/<CaptureSourceGrid\b[^>]*\ballow-off\b/)
      expect(src, `${file} must not keep a screen checkbox`).not.toMatch(/data-role="stage-screen"/)
    }
  })

  it('offers camera and microphone as the same card groups', () => {
    for (const file of [CREATE, STAGE]) {
      const src = sourceOf(file)
      expect(src, `${file} needs the camera cards`).toMatch(/<DeviceChoiceGrid\b[^>]*kind="video"/)
      expect(src, `${file} needs the microphone cards`).toMatch(/<DeviceChoiceGrid\b[^>]*kind="audio"/)
    }
  })

  it('groups the same controls under the same heading', () => {
    for (const file of [CREATE, STAGE]) {
      expect(sourceOf(file), `${file} must use the 画面与声音 heading`).toContain(
        "t('meeting.create.av')",
      )
    }
  })

  it('draws the 听记 switch with the shared checkbox row', () => {
    const stage = sourceOf(STAGE)
    expect(stage).toMatch(/class="meeting-check"[\s\S]*?data-role="stage-notes"/)
    // The stage-only row language is gone; two checkbox looks on one screen is
    // the drift this file exists to prevent.
    expect(stage).not.toMatch(/class="live-check"/)
    expect(stage).not.toMatch(/class="live-dialog-section"/)
  })

  it('lets the meeting tabs use the full panel width', () => {
    // User requirement: the dock is a workspace, so the tab body must not cap
    // itself at a reading column and leave the rest empty.
    const body = ruleDecls(styleOf(STAGE), '.live-panel__body.live-dialog')
    expect(body['max-width']).toBeUndefined()
    expect(body.width).toBeUndefined()
    expect(body.margin).toBeUndefined()
  })
})
