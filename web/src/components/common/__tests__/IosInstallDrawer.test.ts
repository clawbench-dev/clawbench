import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import IosInstallDrawer from '@/components/common/IosInstallDrawer.vue'

// ── i18n ─────────────────────────────────────────────────────
const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      pwa: {
        iosInstallTitle: '安装说明',
        iosStep1Part1: '点击',
        iosStep1Part2: '按钮',
        iosStep2: '添加到主屏幕',
        iosStep3: '完成安装',
        gotIt: '知道了',
      },
    },
  },
})

// Mock BottomSheet — replaces the real component (which uses Teleport) with a simple mock
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    props: ['open', 'title', 'compact'],
    emits: ['close'],
    template: '<div class="bottom-sheet-stub" v-if="open"><slot /><div class="bs-footer-stub"><slot name="footer" /></div></div>',
  }),
}))

vi.mock('lucide-vue-next', () => ({
  Share: { template: '<span class="share-icon-stub" />' },
}))

function mountSheet(props: Record<string, any> = {}) {
  return mount(IosInstallDrawer, {
    props: {
      open: false,
      ...props,
    },
    global: {
      plugins: [i18n],
    },
  })
}

describe('IosInstallDrawer', () => {
  it('emits close when the got-it button is clicked', async () => {
    const wrapper = mountSheet({ open: true })

    // Find the got-it button and click it
    const btn = wrapper.find('.ios-got-it-btn')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')

    // Should emit close event
    expect(wrapper.emitted('close')).toBeTruthy()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('renders the install steps when open', () => {
    const wrapper = mountSheet({ open: true })

    // Should render the steps container
    expect(wrapper.find('.ios-install-steps').exists()).toBe(true)

    // Should render 3 steps
    const steps = wrapper.findAll('.ios-step')
    expect(steps).toHaveLength(3)
  })

  it('does not render content when not open', () => {
    const wrapper = mountSheet({ open: false })

    // The stub only renders when open=true
    expect(wrapper.find('.ios-install-steps').exists()).toBe(false)
  })
})
