import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, reactive, nextTick } from 'vue'
import WallpaperSetting from '@/components/settings/WallpaperSetting.vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'

// appLog relays to native/server; keep it inert in unit tests.
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Module-level state shared by the mocked useSettingsConfig and the tests.
const serverConfig = ref<Record<string, unknown>>({ appearance: { wallpaper_file: '', panel_opacity: 0.85 } })
// The component treats localConfig as the real module-level reactive singleton
// (it writes localConfig.wallpaperBlur directly for live preview), so the mock
// must be reactive — not a ref — for those direct writes to be observable.
const localConfig = reactive<Record<string, string | number | boolean | null>>({
  theme: 'auto',
  wallpaperBlur: 0,
  wallpaperEdgeFade: false,
})
const mockLoadConfig = vi.fn(async () => {})
const mockPatchConfig = vi.fn(async () => ({ needsRestart: false, changedColdFields: [] }))
const mockSetLocalConfig = vi.fn()

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig,
    localConfig,
    loadConfig: mockLoadConfig,
    patchConfig: mockPatchConfig,
    // Inline implementation (factory runs lazily at import, after top-level
    // consts are initialized) mirrors the real setLocalConfig persistence.
    setLocalConfig: (key: string, value: string | number | boolean | null) => {
      localConfig[key] = value
      mockSetLocalConfig(key, value)
    },
  }),
  setLocalConfig: vi.fn(),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      settings: {
        items: {
          wallpaper: 'Background image',
          wallpaperDesc: 'Desc',
          wallpaperLoading: 'Loading',
          wallpaperPreview: 'Preview',
          wallpaperUpload: 'Upload',
          wallpaperReplace: 'Replace',
          wallpaperRemove: 'Remove',
          wallpaperSetOk: 'Set',
          wallpaperRemoved: 'Removed',
          wallpaperUploadFailed: 'Upload failed',
          wallpaperRemoveFailed: 'Remove failed',
          wallpaperSaveFailed: 'Save failed',
          wallpaperPanelOpacity: 'Panel opacity',
          wallpaperPanelOpacityDesc: 'Desc',
          wallpaperBlur: 'Gaussian blur',
          wallpaperBlurDesc: 'Desc',
          wallpaperEdgeFade: 'Edge fade',
          wallpaperEdgeFadeDesc: 'Desc',
          resetToDefault: 'Reset',
        },
      },
    },
  },
})

function mountSetting() {
  return mount(WallpaperSetting, {
    props: { description: 'desc' },
    global: { plugins: [i18n] },
  })
}

function fetchOk() {
  return {
    ok: true,
    status: 200,
    json: vi.fn(async () => ({ file: 'background.png' })),
  }
}

// jsdom lacks matchMedia; the wallpaper scrim resolution calls
// resolveThemeId('auto') → window.matchMedia. Provide a light-scheme stub.
function stubMatchMedia() {
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({
    matches: false,
    media: '',
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    onchange: null,
    dispatchEvent: vi.fn(),
  }))
}

describe('WallpaperSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.unstubAllGlobals()
    stubMatchMedia()
    vi.stubGlobal('fetch', vi.fn())
    serverConfig.value = { appearance: { wallpaper_file: '', panel_opacity: 0.85 } }
    localConfig.wallpaperBlur = 0
    localConfig.wallpaperEdgeFade = false
    localConfig.theme = 'auto'
    document.documentElement.classList.remove('wallpaper-active')
  })

  it('shows the upload button and a disabled opacity slider when no wallpaper is set', async () => {
    const wrapper = mountSetting()
    await nextTick()
    const buttons = wrapper.findAll('button').map(b => b.text())
    expect(buttons).toContain('Upload')
    const slider = wrapper.find('input[type="range"]')
    expect(slider.attributes('disabled')).toBeDefined()
  })

  it('shows the thumbnail, replace and remove buttons when a wallpaper is set', async () => {
    serverConfig.value = { appearance: { wallpaper_file: 'background.png', panel_opacity: 0.85 } }
    const wrapper = mountSetting()
    await nextTick()
    expect(wrapper.find('img.wallpaper-thumb').exists()).toBe(true)
    const buttons = wrapper.findAll('button').map(b => b.text())
    expect(buttons).toContain('Remove')
    expect(buttons).toContain('Replace')
    const slider = wrapper.find('input[type="range"]')
    expect(slider.attributes('disabled')).toBeUndefined()
  })

  it('uploads the selected file via multipart POST', async () => {
    serverConfig.value = { appearance: { wallpaper_file: 'background.png', panel_opacity: 0.85 } }
    const fetchMock = vi.fn(async () => fetchOk())
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountSetting()
    await nextTick()

    const input = wrapper.find('input[type="file"]')
    const file = new File(['x'], 'photo.png', { type: 'image/png' })
    // jsdom lacks DataTransfer/FileList assignment; fake the FileList shape.
    const fileList = { 0: file, length: 1, item: (i: number) => (i === 0 ? file : null) }
    Object.defineProperty(input.element, 'files', {
      configurable: true,
      value: fileList,
    })
    await input.trigger('change')

    await nextTick()
    // The upload triggers an async flow with a loadConfig round-trip.
    await mockLoadConfig()
    await nextTick()

    const calls = fetchMock.mock.calls
    const uploadCall = calls.find(c => c[0] === '/api/theme-background')
    expect(uploadCall).toBeTruthy()
    const [, init] = uploadCall as [string, RequestInit]
    expect((init as RequestInit).method).toBe('POST')
    expect((init as RequestInit).body).toBeInstanceOf(FormData)
  })

  it('removes the wallpaper via DELETE when Remove is clicked', async () => {
    serverConfig.value = { appearance: { wallpaper_file: 'background.png', panel_opacity: 0.85 } }
    const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: vi.fn(async () => ({})) }))
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountSetting()
    await nextTick()
    const removeBtn = wrapper.findAll('button').find(b => b.text() === 'Remove')
    expect(removeBtn).toBeTruthy()
    await removeBtn!.trigger('click')

    await mockLoadConfig()
    await nextTick()
    expect(fetchMock).toHaveBeenCalledWith('/api/theme-background', { method: 'DELETE' })
  })

  it('persists panel opacity with a debounced PATCH on slider input', async () => {
    serverConfig.value = { appearance: { wallpaper_file: 'background.png', panel_opacity: 0.85 } }
    const wrapper = mountSetting()
    await nextTick()
    const slider = wrapper.find('input[type="range"]')
    await slider.setValue('0.75')
    await slider.trigger('input')
    // Debounce fires after 350ms.
    await new Promise(r => setTimeout(r, 400))
    expect(mockPatchConfig).toHaveBeenCalledWith({ appearance: { panel_opacity: 0.75 } })
  })

  it('accepts panel opacity below the old 0.7 floor (relaxed 0.5 lower bound)', async () => {
    // Regression: the translucent-panel tuning range widened from 0.7–1.0 to
    // 0.5–1.0. The slider must expose the relaxed floor and a 0.5x value must
    // reach the server PATCH (settings.go validatePatchValues now allows it).
    serverConfig.value = { appearance: { wallpaper_file: 'background.png', panel_opacity: 0.85 } }
    const wrapper = mountSetting()
    await nextTick()
    const slider = wrapper.find('input[type="range"]')
    expect(slider.attributes('min')).toBe('0.5')
    expect(slider.attributes('max')).toBe('1')
    await slider.setValue('0.55')
    await slider.trigger('input')
    await new Promise(r => setTimeout(r, 400))
    expect(mockPatchConfig).toHaveBeenCalledWith({ appearance: { panel_opacity: 0.55 } })
    expect(document.documentElement.style.getPropertyValue('--panel-alpha')).toBe('55%')
  })


  it('does not change the wallpaper image URL while dragging the opacity slider', async () => {
    serverConfig.value = { appearance: { wallpaper_file: 'background.png', panel_opacity: 0.85 } }
    const wrapper = mountSetting()
    await nextTick()

    // Trigger an initial apply with the wallpaper active.
    const { applyWallpaper } = await import('@/utils/themeBackground')
    applyWallpaper('background.png', 0.85, false)
    const html = document.documentElement
    const urlBefore = html.style.getPropertyValue('--wallpaper-url')
    expect(html.classList.contains('wallpaper-active')).toBe(true)

    // Simulate slider ticks: each calls applyWallpaper with a new alpha.
    const slider = wrapper.find('input[type="range"]')
    await slider.setValue('0.8')
    await slider.trigger('input')
    await slider.setValue('0.78')
    await slider.trigger('input')
    const urlAfter = html.style.getPropertyValue('--wallpaper-url')
    expect(urlAfter).toBe(urlBefore)
    expect(html.style.getPropertyValue('--panel-alpha')).toBe('78%')
  })

  describe('gaussian blur + edge fade', () => {
    it('renders the blur slider and edge-fade switch when a wallpaper is set', async () => {
      serverConfig.value = { appearance: { wallpaper_file: 'background.png', panel_opacity: 0.85 } }
      const wrapper = mountSetting()
      await nextTick()
      const ranges = wrapper.findAll('input[type="range"]')
      expect(ranges).toHaveLength(2)
      // Second range = blur slider; both enabled when set.
      expect(ranges[1].attributes('disabled')).toBeUndefined()
      const labels = wrapper.findAll('.settings-item__label').map(l => l.text())
      expect(labels).toContain('Gaussian blur')
      expect(labels).toContain('Edge fade')
    })

    it('disables blur slider + edge switch when no wallpaper is set', async () => {
      serverConfig.value = { appearance: { wallpaper_file: '', panel_opacity: 0.85 } }
      const wrapper = mountSetting()
      await nextTick()
      const ranges = wrapper.findAll('input[type="range"]')
      expect(ranges[1].attributes('disabled')).toBeDefined()
      const edgeSwitch = wrapper.find('input[type="checkbox"]')
      expect(edgeSwitch.attributes('disabled')).toBeDefined()
    })

    it('updates the local config live while dragging blur and persists debounced', async () => {
      serverConfig.value = { appearance: { wallpaper_file: 'background.png', panel_opacity: 0.85 } }
      const wrapper = mountSetting()
      await nextTick()
      const blurSlider = wrapper.findAll('input[type="range"]')[1]
      await blurSlider.setValue('30')
      await blurSlider.trigger('input')
      // Live preview: localConfig updated immediately, no network.
      expect(localConfig.wallpaperBlur).toBe(30)
      expect(mockPatchConfig).not.toHaveBeenCalled()
      // Debounce persists via setLocalConfig after 250ms.
      await new Promise(r => setTimeout(r, 300))
      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperBlur', 30)
    })

    it('toggles edge fade through setLocalConfig', async () => {
      serverConfig.value = { appearance: { wallpaper_file: 'background.png', panel_opacity: 0.85 } }
      const wrapper = mountSetting()
      await nextTick()
      const edgeSwitch = wrapper.find('input[type="checkbox"]')
      expect(edgeSwitch.attributes('disabled')).toBeUndefined()
      await edgeSwitch.setValue(true)
      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperEdgeFade', true)
    })
  })
})
