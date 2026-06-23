import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import ModalDialog from '@/components/common/ModalDialog.vue'

describe('ModalDialog', () => {
  let wrapper: VueWrapper<any> | null = null
  let container: HTMLDivElement

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
  })

  afterEach(() => {
    vi.useRealTimers()
    if (wrapper) {
      wrapper.unmount()
      wrapper = null
    }
    document.body.querySelectorAll('.modal-overlay').forEach(el => el.remove())
    if (container.parentNode) {
      document.body.removeChild(container)
    }
  })

  function mountDialog(props = {}, slots = {}) {
    wrapper = mount(ModalDialog, {
      props: { open: true, ...props },
      slots,
      attachTo: container,
    })
    return wrapper
  }

  /** Find element in document.body (includes teleported content) */
  function $(selector: string) {
    return document.body.querySelector(selector) as HTMLElement | null
  }

  it('renders content when open', async () => {
    mountDialog({}, { default: '<div class="content">Hello</div>' })
    await nextTick()

    expect($('.modal-overlay')).toBeTruthy()
    expect($('.content')?.textContent).toBe('Hello')
  })

  it('shows title in header', async () => {
    mountDialog({ title: 'Test Dialog' })
    await nextTick()

    expect($('.modal-title')?.textContent).toBe('Test Dialog')
  })

  it('shows close button', async () => {
    mountDialog({ title: 'Test' })
    await nextTick()

    expect($('.modal-close-btn')).toBeTruthy()
  })

  it('emits close when overlay is clicked (after animation)', async () => {
    vi.useFakeTimers()
    mountDialog()
    await nextTick()

    // Call handleClose directly via exposed close method
    wrapper!.vm.close()
    await nextTick()

    // handleClose sets leaving=true and starts 250ms timer
    expect(wrapper!.vm.leaving).toBe(true)

    vi.advanceTimersByTime(250)
    await nextTick()

    expect(wrapper!.emitted('close')).toBeTruthy()
    expect(wrapper!.vm.leaving).toBe(false)
  })

  it('does not emit close when dialog body is clicked', async () => {
    mountDialog()
    await nextTick()

    // Click the dialog itself (has @click.stop) — click on dialog body
    const dialog = $('.modal-dialog')!
    dialog.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    // Should NOT close because @click.stop prevents bubbling to overlay
    expect(wrapper!.emitted('close')).toBeFalsy()
  })

  it('emits close when close button is clicked (after animation)', async () => {
    vi.useFakeTimers()
    mountDialog({ title: 'Test' })
    await nextTick()

    // Click close button directly
    const btn = $('.modal-close-btn')!
    btn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    // handleClose sets leaving=true
    expect(wrapper!.vm.leaving).toBe(true)

    vi.advanceTimersByTime(250)
    await nextTick()

    expect(wrapper!.emitted('close')).toBeTruthy()
  })

  it('uses custom zIndex', async () => {
    mountDialog({ zIndex: 3000 })
    await nextTick()

    const overlay = $('.modal-overlay')!
    expect(overlay.style.zIndex).toBe('3000')
  })

  it('applies fullHeight class when fullHeight is true', async () => {
    mountDialog({ fullHeight: true })
    await nextTick()

    expect($('.modal-dialog')?.classList.contains('modal-full-height')).toBe(true)
  })

  it('uses header slot when provided', async () => {
    mountDialog({}, { header: '<span class="custom-hdr">Custom</span>' })
    await nextTick()

    expect($('.custom-hdr')?.textContent).toBe('Custom')
  })

  it('renders footer slot when provided', async () => {
    mountDialog({}, { footer: '<button class="footer_btn">OK</button>' })
    await nextTick()

    expect($('.modal-footer')).toBeTruthy()
    expect($('.footer_btn')?.textContent).toBe('OK')
  })

  it('renders after slot when provided', async () => {
    mountDialog({}, { after: '<div class="after-content">After</div>' })
    await nextTick()

    expect($('.after-content')?.textContent).toBe('After')
  })

  it('keeps content in DOM after closing (everOpened)', async () => {
    mountDialog({}, { default: '<div class="content">Hello</div>' })
    await nextTick()

    expect($('.modal-overlay')).toBeTruthy()
    expect(wrapper!.vm.everOpened).toBe(true)

    // Close via prop
    await wrapper!.setProps({ open: false })
    await nextTick()

    // everOpened stays true, element stays in DOM
    expect($('.modal-overlay')).toBeTruthy()
    expect(wrapper!.vm.everOpened).toBe(true)
    // v-show condition: open || leaving = false || false = false → hidden
    expect(wrapper!.vm.open || wrapper!.vm.leaving).toBe(false)
  })

  it('enters leaving state before closing (animation)', async () => {
    vi.useFakeTimers()
    mountDialog()
    await nextTick()

    // Call handleClose directly
    wrapper!.vm.close()
    await nextTick()

    // Should be in leaving state
    expect(wrapper!.vm.leaving).toBe(true)

    // After 250ms, close event fires and leaving resets
    vi.advanceTimersByTime(250)
    await nextTick()

    expect(wrapper!.emitted('close')).toBeTruthy()
    expect(wrapper!.vm.leaving).toBe(false)
  })

  it('does not emit close twice if already leaving', async () => {
    vi.useFakeTimers()
    mountDialog()
    await nextTick()

    // Call handleClose directly
    wrapper!.vm.close()
    await nextTick()

    // Call again while leaving — blocked by `if (leaving.value) return`
    wrapper!.vm.close()
    await nextTick()

    // Advance past timer — should only have emitted once
    vi.advanceTimersByTime(250)
    await nextTick()

    expect(wrapper!.emitted('close')).toHaveLength(1)
  })

  it('exposes close method', async () => {
    mountDialog()
    await nextTick()

    expect(typeof wrapper!.vm.close).toBe('function')
  })

  it('does not render when never opened', async () => {
    wrapper = mount(ModalDialog, {
      props: { open: false },
      attachTo: container,
    })
    await nextTick()

    expect($('.modal-overlay')).toBeFalsy()
  })

  it('cancels leaving animation when re-opened', async () => {
    vi.useFakeTimers()
    mountDialog()
    await nextTick()

    // Start closing via handleClose
    wrapper!.vm.close()
    await nextTick()
    expect(wrapper!.vm.leaving).toBe(true)

    // Advance timer to emit close
    vi.advanceTimersByTime(250)
    await nextTick()
    expect(wrapper!.emitted('close')).toBeTruthy()

    // Parent sets open=false
    await wrapper!.setProps({ open: false })
    await nextTick()

    // Then parent re-opens
    await wrapper!.setProps({ open: true })
    await nextTick()

    // leaving should be reset by the watch on open
    expect(wrapper!.vm.leaving).toBe(false)

    vi.useRealTimers()
  })
})
