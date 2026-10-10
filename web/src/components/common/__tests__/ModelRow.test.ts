import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import ModelRow from '@/components/common/ModelRow.vue'

// Stub ProviderIcon so the test does not pull the provider-icon SVG data and so
// the model-name passed to it can be asserted directly.
vi.mock('@/components/common/ProviderIcon.vue', () => ({
  default: defineComponent({
    name: 'ProviderIcon',
    props: { modelName: String, name: String, size: Number },
    template: '<span class="provider-icon-stub" :data-model-name="modelName" />',
  }),
}))

function mountRow(props: Record<string, any> = {}, slot = '') {
  return mount(ModelRow, {
    props: { name: 'Claude Sonnet 4.6', ...props },
    slots: slot ? { default: slot } : {},
  })
}

describe('ModelRow', () => {
  it('renders the name and the raw id as two separate lines', () => {
    const wrapper = mountRow({ name: 'Claude Sonnet 4.6', modelId: 'claude-sonnet-4-6' })
    expect(wrapper.find('.model-row__name').text()).toBe('Claude Sonnet 4.6')
    expect(wrapper.find('.model-row__id').text()).toBe('claude-sonnet-4-6')
    // Both live inside the label column, so the layout is stacked (two lines).
    const labels = wrapper.find('.model-row__labels')
    expect(labels.find('.model-row__name').exists()).toBe(true)
    expect(labels.find('.model-row__id').exists()).toBe(true)
  })

  it('hides the id line when it equals the display name', () => {
    const wrapper = mountRow({ name: 'gpt-5.1-codex', modelId: 'gpt-5.1-codex' })
    expect(wrapper.find('.model-row__id').exists()).toBe(false)
  })

  it('hides the id line when no id is given', () => {
    const wrapper = mountRow({ name: 'Some Model' })
    expect(wrapper.find('.model-row__id').exists()).toBe(false)
  })

  it('formats a JSON-shaped ACP wire id for display', () => {
    // The raw value is a JSON-stringified pair; it must read as provider/model,
    // never as a JSON blob.
    const wrapper = mountRow({
      name: 'DeepSeek-V4-Pro',
      modelId: '["deepseek-official","deepseek-v4-pro"]',
    })
    const idText = wrapper.find('.model-row__id').text()
    expect(idText).toBe('deepseek-official/deepseek-v4-pro')
    expect(idText).not.toContain('[')
    expect(idText).not.toContain('"')
  })

  it('marks the active row so the name can be emphasised', () => {
    const active = mountRow({ name: 'M', modelId: 'm', active: true })
    expect(active.classes()).toContain('model-row--active')
    const inactive = mountRow({ name: 'M', modelId: 'm' })
    expect(inactive.classes()).not.toContain('model-row--active')
  })

  it('detects the provider from the name, falling back to the id', () => {
    const byName = mountRow({ name: 'Claude Sonnet', modelId: 'claude-sonnet' })
    expect(byName.find('.provider-icon-stub').attributes('data-model-name')).toBe('Claude Sonnet')

    // Backends that report no display name fall back to the id.
    const byId = mountRow({ name: '', modelId: 'gpt-5.1-codex' })
    expect(byId.find('.provider-icon-stub').attributes('data-model-name')).toBe('gpt-5.1-codex')
  })

  it('uses the id for provider detection when the name carries no provider', () => {
    // Gateway models put the provider in the id, not the display name:
    // name "OpenCode Zen/Big Pickle" has no known provider, but
    // id "opencode/big-pickle" does. Feeding only the name would show a letter.
    const wrapper = mountRow({
      name: 'OpenCode Zen/Big Pickle',
      modelId: 'opencode/big-pickle',
    })
    expect(wrapper.find('.provider-icon-stub').attributes('data-model-name')).toBe(
      'opencode/big-pickle',
    )
  })

  it('still prefers the name when it detects a provider', () => {
    // The name wins whenever it is recognizable — the id fallback must not
    // hijack a row that already has a good provider.
    const wrapper = mountRow({
      name: 'Claude Sonnet 4.6',
      modelId: 'openrouter/anthropic/claude-sonnet-4.6',
    })
    expect(wrapper.find('.provider-icon-stub').attributes('data-model-name')).toBe(
      'Claude Sonnet 4.6',
    )
  })

  it('renders trailing slot content after the labels (badge / star / check)', () => {
    const wrapper = mountRow(
      { name: 'M', modelId: 'm' },
      '<span class="trailing">★</span>',
    )
    const row = wrapper.find('.model-row')
    expect(row.find('.trailing').exists()).toBe(true)
    // The slot content is a sibling of the label column, not nested inside it.
    expect(wrapper.find('.model-row__labels .trailing').exists()).toBe(false)
  })
})
