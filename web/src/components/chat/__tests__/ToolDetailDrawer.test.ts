import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'

// The drawer renders the AskUserQuestion body through v-html, so it shares the
// same answer-state plumbing as the chat list: a restore hook that re-applies
// the stored answer after every update, and an input route that persists the
// supplementary note. Both must be wired here too — the drawer is a second,
// independent mount point for the same card.
vi.mock('@/utils/renderToolDetail.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/utils/renderToolDetail.ts')>()
  return {
    ...actual,
    restoreAskStatesInContainer: vi.fn(),
    handleAskSupplementaryInput: vi.fn().mockReturnValue(false),
  }
})

vi.mock('@/utils/icons', () => ({
  getToolIcon: () => ({ icon: { template: '<span />' }, category: 'ask' }),
}))
vi.mock('@/stores/app.ts', () => ({ store: { state: {} } }))

import ToolDetailDrawer from '@/components/chat/ToolDetailDrawer.vue'
import { restoreAskStatesInContainer, handleAskSupplementaryInput } from '@/utils/renderToolDetail.ts'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { common: { copy: 'Copy' }, toolDetailBlock: { wrapOn: 'Wrap on', wrapOff: 'Wrap off' } } },
})

const LucideStub = { template: '<span />' }

const wrappers: ReturnType<typeof mount>[] = []

function mountDrawer(props: Record<string, unknown> = {}) {
  const wrapper = mount(ToolDetailDrawer, {
    props: {
      show: true,
      toolName: 'AskUserQuestion',
      toolInputHtml: '<div class="ask-question-view" data-ask-key="sess-1|tool:ask-1"><input class="ask-supplementary-input" /></div>',
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs: {
        BottomSheet: { template: '<div class="sheet-stub"><slot name="header" /><slot /></div>' },
        LoadingIndicator: { template: '<span />' },
        TableRowModal: { template: '<span />' },
        CheckCircle2: LucideStub,
        XCircle: LucideStub,
      },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

afterEach(() => {
  for (const w of wrappers.splice(0)) {
    if (w.exists()) w.unmount()
  }
})

describe('ToolDetailDrawer — ask-card answer state', () => {
  beforeEach(() => {
    vi.mocked(restoreAskStatesInContainer).mockClear()
    vi.mocked(handleAskSupplementaryInput).mockClear()
    vi.mocked(handleAskSupplementaryInput).mockReturnValue(false)
  })

  it('re-applies stored answers after the drawer body updates', async () => {
    const wrapper = mountDrawer()
    await nextTick()

    // A re-render of the v-html body — what reopening the drawer, a live block
    // update, or a reload does. The rendered string genuinely differs (that is
    // exactly what makes Vue replace the subtree and drop the user's answer),
    // while the card's identity stays the same.
    await wrapper.setProps({
      toolInputHtml: '<div class="ask-question-view" data-ask-key="sess-1|tool:ask-1"><input class="ask-supplementary-input" /><div class="ask-question-item"></div></div>',
    })
    await nextTick()

    expect(restoreAskStatesInContainer).toHaveBeenCalled()
  })

  it('scopes the restore to the drawer body so it finds the card it renders', async () => {
    const wrapper = mountDrawer()
    await nextTick()
    await wrapper.setProps({ toolSummary: 'changed' })
    await nextTick()

    const spy = vi.mocked(restoreAskStatesInContainer)
    expect(spy).toHaveBeenCalled()
    const arg = spy.mock.calls[spy.mock.calls.length - 1][0] as HTMLElement
    // .tool-detail-body is the element holding the v-html card. A different
    // element would leave the card outside the searched subtree.
    expect(arg?.classList?.contains('tool-detail-body')).toBe(true)
  })

  it('routes supplementary input through the ask handler', async () => {
    const wrapper = mountDrawer()
    await nextTick()

    const input = wrapper.element.querySelector('.ask-supplementary-input') as HTMLInputElement
    expect(input).toBeTruthy()
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()

    expect(handleAskSupplementaryInput).toHaveBeenCalled()
  })

  it('falls back to the generic submit-state refresh when the event is not an ask input', async () => {
    const wrapper = mountDrawer()
    await nextTick()

    // A non-ask input inside the body must not be treated as the note field.
    const other = document.createElement('input')
    other.className = 'some-other-input'
    wrapper.element.querySelector('.tool-detail-body')!.appendChild(other)
    other.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()

    expect(handleAskSupplementaryInput).toHaveBeenCalled()
  })
})

// The drawer is mounted fresh each time it is opened, and onUpdated does not
// fire on initial mount — so the restore must also run on mount, exactly as in
// the chat list (see the list-remount note in ContentBlocks.test.ts).
describe('ToolDetailDrawer — restore on mount', () => {
  beforeEach(() => {
    vi.mocked(restoreAskStatesInContainer).mockClear()
  })

  it('applies stored answers to a freshly mounted drawer', async () => {
    const wrapper = mountDrawer()
    await nextTick()

    expect(restoreAskStatesInContainer).toHaveBeenCalled()
    const arg = vi.mocked(restoreAskStatesInContainer).mock.calls[0][0] as HTMLElement
    expect(arg?.classList?.contains('tool-detail-body')).toBe(true)
  })
})

// A closed drawer renders no body at all. The restore hook must tolerate that
// rather than throw — onUpdated still fires on the component, and a non-null
// assumption would break every update while the drawer is shut.
describe('ToolDetailDrawer — restore with no rendered body', () => {
  beforeEach(() => {
    vi.mocked(restoreAskStatesInContainer).mockClear()
  })

  it('does not throw and does not restore when the body is absent', async () => {
    // show=false → BottomSheet renders nothing, so bodyRef stays null.
    const wrapper = mount(ToolDetailDrawer, {
      props: { show: false, toolName: 'AskUserQuestion', toolInputHtml: '' },
      global: {
        plugins: [i18n],
        stubs: {
          BottomSheet: { template: '<div v-if="open"><slot /></div>', props: ['open'] },
          LoadingIndicator: { template: '<span />' },
          TableRowModal: { template: '<span />' },
          CheckCircle2: LucideStub,
          XCircle: LucideStub,
        },
      },
    })
    wrappers.push(wrapper)
    await nextTick()

    expect(wrapper.find('.tool-detail-body').exists()).toBe(false)
    // A forced update while closed must be harmless.
    await wrapper.setProps({ toolSummary: 'changed' })
    await nextTick()
    expect(restoreAskStatesInContainer).not.toHaveBeenCalled()
  })
})
