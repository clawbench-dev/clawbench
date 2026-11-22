import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

/**
 * A integrated install (desktop shell downloads the whole package) takes a while.
 * The prompt used to close on click, leaving an app that looked idle while the
 * shell worked. It must stay up with a spinner for the duration and close only
 * once `startUpgrade` resolves — that is the regression this file pins.
 */
const installingIntegrated = ref(false)
const startUpgrade = vi.fn()

vi.mock('@/composables/useUpgrade', () => ({
  useUpgrade: () => ({
    skipVersion: vi.fn(),
    startUpgrade,
    releaseNotesUrl: ref(''),
    isDocker: ref(false),
    installingIntegrated,
  }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      upgrade: {
        newVersion: 'New version',
        promptMessage: 'Version {version} is available (current {currentVersion}).',
        releaseNotes: 'Release notes for {version}',
        upgradeNow: 'Upgrade now',
        skipVersion: 'Skip this version',
        remindLater: 'Remind me later',
        integratedInstalling: 'Installing the new version…',
        dockerHintBody: 'Docker hint',
        dockerHintRestart: 'restart',
      },
    },
  },
})

async function mountOverlay() {
  const { default: UpgradePromptOverlay } = await import('@/components/UpgradePromptOverlay.vue')
  const wrapper = mount(UpgradePromptOverlay, {
    global: {
      plugins: [i18n],
      stubs: { LoadingIndicator: { template: '<div class="loading-stub" />' } },
    },
  })
  wrapper.vm.show('2.0.0', '1.0.0')
  await flushPromises()
  return wrapper
}

describe('UpgradePromptOverlay', () => {
  beforeEach(() => {
    installingIntegrated.value = false
    startUpgrade.mockReset()
  })

  it('closes immediately when the upgrade is not a integrated install', async () => {
    startUpgrade.mockResolvedValue(undefined)
    const wrapper = await mountOverlay()
    expect(wrapper.find('.up-overlay').exists()).toBe(true)

    await wrapper.find('.up-upgrade').trigger('click')
    await flushPromises()

    expect(startUpgrade).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.up-overlay').exists()).toBe(false)
  })

  it('stays up with a spinner while the integrated install runs, then closes', async () => {
    let finish: () => void = () => {}
    startUpgrade.mockImplementation(() => {
      installingIntegrated.value = true
      return new Promise<void>((resolve) => { finish = resolve })
    })
    const wrapper = await mountOverlay()

    await wrapper.find('.up-upgrade').trigger('click')
    await flushPromises()

    // The shell is still downloading: the prompt must not disappear, and the
    // action buttons give way to the spinner so they cannot be clicked twice.
    expect(wrapper.find('.up-overlay').exists()).toBe(true)
    expect(wrapper.find('.up-installing').exists()).toBe(true)
    expect(wrapper.find('.up-footer').exists()).toBe(false)

    installingIntegrated.value = false
    finish()
    await flushPromises()

    expect(wrapper.find('.up-overlay').exists()).toBe(false)
  })
})
