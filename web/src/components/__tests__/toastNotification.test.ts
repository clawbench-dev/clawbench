import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import ToastNotification from '@/components/common/ToastNotification.vue'

function createMockToast(overrides = {}) {
  return {
    visible: ref(overrides.visible ?? true),
    type: ref(overrides.type ?? 'info'),
    message: ref(overrides.message ?? 'Test message'),
    icon: ref(overrides.icon ?? ''),
    onClick: ref(overrides.onClick ?? null),
    dismiss: vi.fn(),
  }
}

describe('ToastNotification', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  function mountToast(toast) {
    return mount(ToastNotification, {
      props: { toast },
      attachTo: document.body,
    })
  }

  it('renders when visible', () => {
    const toast = createMockToast({ visible: true })
    mountToast(toast)

    expect(document.querySelector('.toast')).toBeTruthy()
  })

  it('hides when not visible', () => {
    const toast = createMockToast({ visible: false })
    mountToast(toast)

    expect(document.querySelector('.toast')).toBeFalsy()
  })

  it('applies type-specific class for info', () => {
    const toast = createMockToast({ type: 'info' })
    mountToast(toast)

    const el = document.querySelector('.toast')!
    expect(el.classList.contains('toast-info')).toBe(true)
  })

  it('applies type-specific class for success', () => {
    const toast = createMockToast({ type: 'success' })
    mountToast(toast)

    const el = document.querySelector('.toast')!
    expect(el.classList.contains('toast-success')).toBe(true)
  })

  it('applies type-specific class for error', () => {
    const toast = createMockToast({ type: 'error' })
    mountToast(toast)

    const el = document.querySelector('.toast')!
    expect(el.classList.contains('toast-error')).toBe(true)
  })

  it('displays the message text', () => {
    const toast = createMockToast({ message: 'Hello world' })
    mountToast(toast)

    const el = document.querySelector('.toast-text')!
    expect(el.textContent).toBe('Hello world')
  })

  it('shows icon when icon value is truthy', () => {
    const toast = createMockToast({ icon: '✓' })
    mountToast(toast)

    expect(document.querySelector('.toast-icon')).toBeTruthy()
    expect(document.querySelector('.toast-icon')!.textContent).toBe('✓')
  })

  it('hides icon when icon value is falsy', () => {
    const toast = createMockToast({ icon: '' })
    mountToast(toast)

    expect(document.querySelector('.toast-icon')).toBeFalsy()
  })

  it('calls onClick and then dismiss when clicked with onClick', async () => {
    const onClick = vi.fn()
    const toast = createMockToast({ onClick })
    mountToast(toast)

    const el = document.querySelector('.toast')! as HTMLElement
    el.click()

    expect(onClick).toHaveBeenCalledTimes(1)
    expect(toast.dismiss).toHaveBeenCalledTimes(1)
    // onClick should be called before dismiss
    const onClickCallOrder = onClick.mock.invocationCallOrder[0]
    const dismissCallOrder = toast.dismiss.mock.invocationCallOrder[0]
    expect(onClickCallOrder).toBeLessThan(dismissCallOrder)
  })

  it('calls only dismiss when clicked without onClick', async () => {
    const toast = createMockToast({ onClick: null })
    mountToast(toast)

    const el = document.querySelector('.toast')! as HTMLElement
    el.click()

    expect(toast.dismiss).toHaveBeenCalledTimes(1)
  })

  it('has base toast class alongside type class', () => {
    const toast = createMockToast({ type: 'error' })
    mountToast(toast)

    const el = document.querySelector('.toast')!
    const classes = el.classList
    expect(classes.contains('toast')).toBe(true)
    expect(classes.contains('toast-error')).toBe(true)
  })
})
