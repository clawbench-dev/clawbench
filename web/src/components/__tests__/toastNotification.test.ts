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
    pause: vi.fn(),
    resume: vi.fn(),
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
    mountToast(createMockToast({ visible: true }))
    expect(document.querySelector('.toast')).toBeTruthy()
  })

  it('hides when not visible', () => {
    mountToast(createMockToast({ visible: false }))
    expect(document.querySelector('.toast')).toBeFalsy()
  })

  it('applies type-specific class for info', () => {
    mountToast(createMockToast({ type: 'info' }))
    expect(document.querySelector('.toast')!.classList.contains('toast-info')).toBe(true)
  })

  it('applies type-specific class for success', () => {
    mountToast(createMockToast({ type: 'success' }))
    expect(document.querySelector('.toast')!.classList.contains('toast-success')).toBe(true)
  })

  it('applies type-specific class for error', () => {
    mountToast(createMockToast({ type: 'error' }))
    expect(document.querySelector('.toast')!.classList.contains('toast-error')).toBe(true)
  })

  it('displays the message text', () => {
    mountToast(createMockToast({ message: 'Hello world' }))
    expect(document.querySelector('.toast-text')!.textContent).toBe('Hello world')
  })

  // Every toast carries an icon — the type default when the caller names none.
  it('always renders an icon, defaulting by type when none is named', () => {
    mountToast(createMockToast({ icon: '', type: 'error' }))
    expect(document.querySelector('.toast-icon')).toBeTruthy()
    expect(document.querySelector('.toast-icon svg')).toBeTruthy()
  })

  it('renders the icon for a named semantic key', () => {
    mountToast(createMockToast({ icon: 'copy' }))
    expect(document.querySelector('.toast-icon svg')).toBeTruthy()
  })

  it('calls onClick and then dismiss when clicked with onClick', () => {
    const onClick = vi.fn()
    const toast = createMockToast({ onClick })
    mountToast(toast)

    const el = document.querySelector('.toast')! as HTMLElement
    el.click()

    expect(onClick).toHaveBeenCalledTimes(1)
    expect(toast.dismiss).toHaveBeenCalledTimes(1)
    expect(onClick.mock.invocationCallOrder[0]).toBeLessThan(toast.dismiss.mock.invocationCallOrder[0])
  })

  it('calls only dismiss when clicked without onClick', () => {
    const toast = createMockToast({ onClick: null })
    mountToast(toast)

    const el = document.querySelector('.toast')! as HTMLElement
    el.click()

    expect(toast.dismiss).toHaveBeenCalledTimes(1)
  })

  it('has base toast class alongside type class', () => {
    mountToast(createMockToast({ type: 'error' }))
    const classes = document.querySelector('.toast')!.classList
    expect(classes.contains('toast')).toBe(true)
    expect(classes.contains('toast-error')).toBe(true)
  })

  it('pauses the countdown on hover and resumes on leave', () => {
    const toast = createMockToast()
    mountToast(toast)
    const el = document.querySelector('.toast')! as HTMLElement

    el.dispatchEvent(new MouseEvent('mouseenter'))
    expect(toast.pause).toHaveBeenCalledTimes(1)

    el.dispatchEvent(new MouseEvent('mouseleave'))
    expect(toast.resume).toHaveBeenCalledTimes(1)
  })
})
