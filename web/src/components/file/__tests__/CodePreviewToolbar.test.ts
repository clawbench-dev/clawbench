import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import CodePreviewToolbar from '@/components/file/CodePreviewToolbar.vue'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function mountToolbar() {
  return mount(CodePreviewToolbar, {
    props: {
      metaText: '120 lines',
      showTextTools: true,
      showRenderToggle: true,
      isRenderedView: false,
      isDirView: false,
      isImageTarget: false,
      tooLarge: false,
      isSearchOpen: false,
      isWordWrap: false,
      showLineNumbers: true,
      copied: false,
      isPathCopied: false,
      onRowPointerDown: () => {},
      showTooltip: () => {},
      hideTooltip: () => {},
    },
    global: { plugins: [i18n] },
  })
}

describe('CodePreviewToolbar labels', () => {
  it('shows its labels when the row can hold them', async () => {
    // jsdom has no layout: the row measures 0 wide and assumes room, which is
    // the wide-sheet case. The overflow maths lives in
    // ActionBar.test.ts; this pins the wiring.
    const wrapper = mountToolbar()
    await nextTick()
    // jsdom has no layout, so the row measures 0 wide and assumes room — the
    // wide-sheet case. The width decisions live in ActionBar's own test.
    const row = wrapper.find('.code-preview-meta')
    expect(row.classes()).toContain('ab')
    expect(row.findAll('.action-label, .copy-btn-label').length).toBeGreaterThan(5)
  })

  it('labels every tool, including the copy buttons', async () => {
    // CopyButton owns its own label span (`.copy-btn-label`), the rest use the
    // shared `.action-label`. A button without either would be icon-only while
    // its neighbours are not, which is the drift this guards.
    const wrapper = mountToolbar()
    await nextTick()

    const buttons = wrapper.findAll('.code-preview-actions .code-preview-btn')
    expect(buttons.length).toBeGreaterThan(5)
    for (const b of buttons) {
      const hasLabel =
        b.find('.action-label').exists() || b.find('.copy-btn-label').exists()
      expect(hasLabel, `unlabelled tool: ${b.attributes('title') || b.html().slice(0, 60)}`).toBe(true)
    }
  })
})
