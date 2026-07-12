import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PlanPanel from '@/components/chat/PlanPanel.vue'
import type { PlanEntry } from '@/composables/usePlanProgress'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        plan: {
          title: 'Execution Plan',
          completedCount: '{completed}/{total} done',
        },
      },
    },
  },
})

const entries: PlanEntry[] = [
  { content: 'Step one', status: 'completed', priority: 'high' },
  { content: 'Step two', status: 'in_progress', priority: 'medium' },
  { content: 'Step three', status: 'pending', priority: 'low' },
]

function mountPanel(props: Record<string, unknown>) {
  return mount(PlanPanel, {
    props: { entries, collapsed: false, hasUpdate: false, ...props },
    global: { plugins: [i18n] },
  })
}

describe('PlanPanel', () => {
  it('emits toggle-collapse when clicking anywhere on the expanded header', async () => {
    const wrapper = mountPanel({ collapsed: false })
    await wrapper.get('.plan-expanded__header').trigger('click')
    expect(wrapper.emitted('toggle-collapse')).toHaveLength(1)
  })

  it('emits toggle-collapse when clicking the header title (not just the chevron)', async () => {
    const wrapper = mountPanel({ collapsed: false })
    await wrapper.get('.plan-expanded__title').trigger('click')
    expect(wrapper.emitted('toggle-collapse')).toHaveLength(1)
  })

  it('emits toggle-collapse when clicking the collapsed chip', async () => {
    const wrapper = mountPanel({ collapsed: true })
    await wrapper.get('.plan-chip').trigger('click')
    expect(wrapper.emitted('toggle-collapse')).toHaveLength(1)
  })
})
