import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import ExternalBadge from '@/components/file/ExternalBadge.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        nav: {
          externalTip: 'Outside the project directory',
        },
      },
    },
  },
})

function mountBadge(props: Record<string, any> = {}) {
  return mount(ExternalBadge, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('ExternalBadge', () => {
  it('renders no text at all', () => {
    // Regression guard: the badge sits inline in dense rows, and every word it
    // carried — even the single short one it used to show — competed with the
    // row's own filename. The meaning lives in the tooltip now.
    for (const kind of ['dir', 'file']) {
      expect(mountBadge({ kind }).find('.external-badge').text().trim()).toBe('')
    }
  })

  it('spells the full meaning out in the tooltip', () => {
    expect(mountBadge({ kind: 'dir' }).find('.external-badge').attributes('title'))
      .toBe('Outside the project directory')
  })

  it('exposes the tooltip text as the accessible name', () => {
    // Icon-only means the tooltip is the ONLY thing a screen reader can read;
    // a bare decorative span would announce nothing at all.
    const badge = mountBadge({ kind: 'file' }).find('.external-badge')
    expect(badge.attributes('aria-label')).toBe('Outside the project directory')
  })

  it('renders an icon', () => {
    // It renders as an inline <svg> — a module-level `lucide-vue-next` stub
    // would NOT apply here, since the component imports FolderOpen/FileText as
    // named bindings.
    expect(mountBadge({ kind: 'dir' }).find('.external-badge svg').exists()).toBe(true)
  })

  it('picks a different icon for a directory than for a file', () => {
    // With no text left, the icon is the ONLY thing telling the two kinds
    // apart. If both branches ever collapsed to the same component the badge
    // would still render — and silently stop distinguishing anything.
    const dirIcon = mountBadge({ kind: 'dir' }).find('.external-badge svg').classes()
    const fileIcon = mountBadge({ kind: 'file' }).find('.external-badge svg').classes()
    expect(dirIcon).toContain('lucide-folder-open')
    expect(fileIcon).toContain('lucide-file-text')
    expect(dirIcon).not.toEqual(fileIcon)
  })

  it('defaults to the file icon when no kind is given', () => {
    // Callers drop the badge next to a filename in the common case, so a
    // forgotten `kind` must read as a file rather than as a directory.
    expect(mountBadge().find('.external-badge svg').classes()).toContain('lucide-file-text')
  })

  it('never shrinks, so the neighbouring name yields instead', () => {
    // jsdom does not load SFC <style>, so assert against the source. The badge
    // is a fixed-size cue; if flex could compress it the row would clip the
    // very thing that says "this is outside the project".
    const src = readFileSync(
      resolve(__dirname, '../../../components/file/ExternalBadge.vue'),
      'utf8',
    )
    const rule = src.match(/\.external-badge\s*\{([^}]*)\}/)
    expect(rule, '.external-badge rule must exist').not.toBeNull()
    expect(rule![1]).toMatch(/flex-shrink:\s*0/)
  })
})
