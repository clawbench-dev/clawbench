import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DropOverlay from '@/components/common/DropOverlay.vue'

function mountOverlay(props: { visible: boolean; label: string }) {
  return mount(DropOverlay, {
    props,
    global: {
      stubs: {
        // The overlay animates in/out; transitions complicate presence checks.
        transition: false,
      },
    },
  })
}

describe('DropOverlay', () => {
  it('renders the label when visible', () => {
    const wrapper = mountOverlay({ visible: true, label: '松开上传到当前目录' })

    const overlay = wrapper.find('.drop-overlay')
    expect(overlay.exists()).toBe(true)
    expect(overlay.text()).toContain('松开上传到当前目录')
  })

  it('renders nothing when not visible', () => {
    const wrapper = mountOverlay({ visible: false, label: '松开上传到当前目录' })

    expect(wrapper.find('.drop-overlay').exists()).toBe(false)
  })

  it('uses the shared class name the file manager and terminal both rely on', () => {
    // Both hosts keep this class in their selectors/tests; renaming it here
    // would silently break them.
    const wrapper = mountOverlay({ visible: true, label: 'x' })
    expect(wrapper.classes()).toContain('drop-overlay')
  })
})
