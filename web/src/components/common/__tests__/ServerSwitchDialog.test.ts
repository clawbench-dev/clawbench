import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ServerSwitchDialog from '@/components/common/ServerSwitchDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { loading: 'Loading...', cancel: 'Cancel' },
      login: { logout: 'Logout', passwordPlaceholder: 'Password', switchServer: 'Switch server' },
      serverSwitch: {
        title: 'Switch server',
        lead: 'Pick a server',
        empty: 'No saved servers',
        current: 'Current',
        connect: 'Connect',
        passwordFor: 'Password for {name}',
        loadFailed: 'Failed to load the server list',
        switchFailed: 'Failed to switch server',
        saveFailed: 'Failed to save the server password',
      },
    },
  },
})

const currentUrl = window.location.origin
const otherUrl = 'https://other.example:20000'

function mountDialog() {
  return mount(ServerSwitchDialog, {
    props: { open: true },
    global: {
      plugins: [i18n],
      stubs: { ChevronRight: true, LogOut: true, Server: true },
    },
  })
}

function installNative(overrides: Record<string, unknown> = {}) {
  const native = {
    getServerList: () => JSON.stringify([
      { url: currentUrl, password: 'current-pass' },
      { url: otherUrl, password: 'other-pass', name: 'Other' },
    ]),
    connectToServer: vi.fn(),
    saveServerNamed: vi.fn(),
    showServerDialog: vi.fn(),
    ...overrides,
  }
  ;(window as unknown as { ClawBenchNative: unknown }).ClawBenchNative = native
  return native
}

describe('ServerSwitchDialog', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    delete (window as unknown as { ClawBenchNative?: unknown }).ClawBenchNative
    document.body.querySelectorAll('.modal-overlay').forEach(el => el.remove())
  })

  it('renders the saved servers and marks the current one', async () => {
    installNative()
    const wrapper = mountDialog()
    await flushPromises()

    const rows = document.body.querySelectorAll('.server-switch__row')
    expect(rows).toHaveLength(2)
    expect(rows[0].classList.contains('is-current')).toBe(true)
    expect(rows[1].classList.contains('is-current')).toBe(false)

    wrapper.unmount()
  })

  it('switches immediately when the server has a saved password', async () => {
    const native = installNative()
    const wrapper = mountDialog()
    await flushPromises()

    ;(document.body.querySelectorAll('.server-switch__row')[1] as HTMLButtonElement).click()
    await flushPromises()

    expect(native.connectToServer).toHaveBeenCalledWith(otherUrl, 'other-pass')
    wrapper.unmount()
  })

  it('does nothing when clicking the current server', async () => {
    const native = installNative()
    const wrapper = mountDialog()
    await flushPromises()

    ;(document.body.querySelectorAll('.server-switch__row')[0] as HTMLButtonElement).click()
    await flushPromises()

    expect(native.connectToServer).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('asks for a password only when the entry has none, then saves and connects', async () => {
    const native = installNative({
      getServerList: () => JSON.stringify([
        { url: currentUrl, password: 'current-pass' },
        { url: otherUrl, password: '', name: 'Other' },
      ]),
    })
    const wrapper = mountDialog()
    await flushPromises()

    ;(document.body.querySelectorAll('.server-switch__row')[1] as HTMLButtonElement).click()
    await flushPromises()

    const input = document.body.querySelector('.server-switch__password-input') as HTMLInputElement
    expect(input).toBeTruthy()
    expect(native.connectToServer).not.toHaveBeenCalled()

    input.value = 'typed-pass'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    ;(document.body.querySelector('.server-switch__password button') as HTMLButtonElement).click()
    await flushPromises()

    expect(native.saveServerNamed).toHaveBeenCalledWith(otherUrl, 'typed-pass', 'Other')
    expect(native.connectToServer).toHaveBeenCalledWith(otherUrl, 'typed-pass')
    wrapper.unmount()
  })

  it('emits logout when the logout button is clicked', async () => {
    installNative()
    const wrapper = mountDialog()
    await flushPromises()

    ;(document.body.querySelector('.server-switch__logout') as HTMLButtonElement).click()
    await flushPromises()

    expect(wrapper.emitted('logout')).toBeTruthy()
    wrapper.unmount()
  })
})
