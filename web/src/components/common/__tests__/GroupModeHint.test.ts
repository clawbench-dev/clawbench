import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupModeHint from '@/components/common/GroupModeHint.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// PopupMenu uses <Teleport> + <Transition>, which break reactivity in
// test-utils Vue 3.5. Stub it so the popover content renders inline when shown.
vi.mock('@/components/common/PopupMenu.vue', () => ({
  default: {
    name: 'PopupMenu',
    props: ['show', 'targetElement', 'maxWidth', 'maxHeight', 'menuItemsCount', 'anchor'],
    template: '<div v-if="show" class="popup-menu-stub"><slot /></div>',
  },
}))

function mountHint(props = {}) {
  return mount(GroupModeHint, { props: { mode: 'host', ...props } })
}

// jsdom does not implement matchMedia, so `hoverCapable` resolves to false and
// the component takes the tap-to-toggle path — which is what these tests cover.
describe('GroupModeHint', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders the host chip with the crown glyph', () => {
    const wrapper = mountHint({ mode: 'host' })
    const chip = wrapper.find('.group-mode-hint-chip')
    expect(chip.exists()).toBe(true)
    // i18n mock returns the key verbatim.
    expect(chip.text()).toContain('group.hostMode')
    expect(chip.find('.lucide-crown').exists()).toBe(true)
    expect(chip.find('.lucide-at-sign').exists()).toBe(false)
  })

  it('renders the free chip with the @ glyph', () => {
    const wrapper = mountHint({ mode: 'free' })
    const chip = wrapper.find('.group-mode-hint-chip')
    expect(chip.text()).toContain('group.freeMode')
    expect(chip.find('.lucide-at-sign').exists()).toBe(true)
    expect(chip.find('.lucide-crown').exists()).toBe(false)
  })

  it('starts with the popover closed', () => {
    const wrapper = mountHint({ mode: 'host' })
    expect(wrapper.find('.popup-menu-stub').exists()).toBe(false)
  })

  it('opens the popover on tap (non-hover path) with title + desc + tips', async () => {
    const wrapper = mountHint({ mode: 'host' })
    await wrapper.find('.group-mode-hint-chip').trigger('click')

    const card = wrapper.find('.group-mode-hint-card')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('chat.messageList.groupModeHostTitle')
    expect(card.text()).toContain('chat.messageList.groupModeHostDesc')
    expect(card.text()).toContain('chat.messageList.groupModeHostTip1')
    expect(card.text()).toContain('chat.messageList.groupModeHostTip2')
    expect(card.text()).toContain('chat.messageList.groupModeHostTip3')
    // Three bulleted tips.
    expect(wrapper.findAll('.group-mode-hint-tips li').length).toBe(3)
  })

  it('shows the free-mode copy when mode is free', async () => {
    const wrapper = mountHint({ mode: 'free' })
    await wrapper.find('.group-mode-hint-chip').trigger('click')
    const card = wrapper.find('.group-mode-hint-card')
    expect(card.text()).toContain('chat.messageList.groupModeFreeTitle')
    expect(card.text()).toContain('chat.messageList.groupModeFreeDesc')
  })

  it('toggles the popover closed on a second tap', async () => {
    const wrapper = mountHint({ mode: 'host' })
    const chip = wrapper.find('.group-mode-hint-chip')
    await chip.trigger('click')
    expect(wrapper.find('.popup-menu-stub').exists()).toBe(true)
    await chip.trigger('click')
    expect(wrapper.find('.popup-menu-stub').exists()).toBe(false)
  })

  it('does not bubble the chip click (stop) so a parent close target stays open', async () => {
    // The chip lives in a BottomSheet header/footer where a click may close the
    // sheet; the trigger must not propagate.
    const onParentClick = vi.fn()
    const wrapper = mount(
      { components: { GroupModeHint }, template: '<div @click="onParentClick"><GroupModeHint mode="host" /></div>' },
      { global: { mocks: { onParentClick } } },
    )
    await wrapper.find('.group-mode-hint-chip').trigger('click')
    expect(onParentClick).not.toHaveBeenCalled()
  })
})
