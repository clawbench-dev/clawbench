import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, reactive, nextTick } from 'vue'
import WallpaperSetting from '@/components/settings/WallpaperSetting.vue'

// appLog relays to native/server; keep it inert in unit tests.
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Module-level state shared by the mocked useSettingsConfig and the tests.
const serverConfig = ref<Record<string, unknown>>({ appearance: { active_file: '' } })
// The component treats localConfig as the real module-level reactive singleton
// (it writes localConfig.wallpaperBlur directly for live preview), so the mock
// must be reactive — not a ref — for those direct writes to be observable.
const localConfig = reactive<Record<string, string | number | boolean | null>>({
  theme: 'auto',
  locale: 'zh',
  panelOpacity: 0.7,
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

const toastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: toastShow }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      settings: {
        items: {
          wallpaperEnable: 'Enable wallpaper',
          wallpaperEnableDesc: 'Desc',
          wallpaperSource: 'Wallpaper source',
          wallpaperSourceDesc: 'Desc',
          wallpaperModeLocal: 'Local gallery',
          wallpaperModeBing: 'Bing daily',
          wallpaperBingFollow: 'Follow the Bing daily image',
          wallpaperBingFollowDesc: 'Desc',
          wallpaperBingSync: 'Sync now',
          wallpaperBingSyncing: 'Syncing…',
          wallpaperBingSynced: 'Synced',
          wallpaperBingPending: 'Pending',
          wallpaperBingStatus: 'Current image',
          wallpaperBingSyncedAt: 'Synced {date}',
          wallpaperBingNoImage: 'No image fetched yet',
          wallpaperBingFailed: 'Sync failed',
          wallpaperGallery: 'Local gallery',
          wallpaperGalleryDesc: 'Desc',
          wallpaperGalleryUpload: 'Upload images',
          wallpaperGalleryEmpty: 'Empty',
          wallpaperGalleryCount: '{count} / {max} images',
          wallpaperGalleryDelete: 'Delete',
          wallpaperGalleryLimit: 'Limit {max}',
          wallpaperUploadPartial: '{ok} ok, {failed} failed',
          wallpaperUploadTooMany: 'Too many {max}',
          wallpaperSetFailed: 'Set failed',
          wallpaperSetOk: 'Set',
          wallpaperUploadFailed: 'Upload failed',
          wallpaperRemoveFailed: 'Remove failed',
          wallpaperSaveFailed: 'Save failed',
          wallpaperPreview: 'Preview',
          wallpaperPanelOpacity: 'Panel opacity',
          wallpaperPanelOpacityDesc: 'Desc',
          wallpaperBlur: 'Gaussian blur',
          wallpaperBlurDesc: 'Desc',
          wallpaperEdgeFade: 'Edge fade',
          wallpaperEdgeFadeDesc: 'Desc',
          wallpaperModeWave: 'Animated',
          wallpaperWaveSpeed: 'Animation speed',
          wallpaperWaveSpeedDesc: 'Desc',
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

/**
 * Server config carrying only the shared wallpaper RESOURCES. The source, the
 * on/off switch and the selection are this device's own local state, so they are
 * driven through localConfig below, not through the server config.
 */
function serverWith(
  items: { file: string; name: string }[] = [],
  bing: Record<string, unknown> = {},
) {
  return {
    appearance: {
      local: { items: items.map((it, i) => ({ ...it, uploaded_at: i + 1, size: 100 })) },
      bing: { file: '', last_success_date: '', copyright: '', title: '', last_error: '', last_attempt_at: 0, ...bing },
    },
  }
}

/** Set this device's wallpaper choice (the localStorage-backed half). */
function chooseDevice(mode: 'local' | 'bing' | 'wave' | 'none', selected = '', enabled = true) {
  localConfig.wallpaperMode = mode
  localConfig.wallpaperLocalSelected = selected
  localConfig.wallpaperEnabled = enabled
}

describe('WallpaperSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.unstubAllGlobals()
    stubMatchMedia()
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) })))
    serverConfig.value = serverWith()
    chooseDevice('wave')
    localConfig.wallpaperBlur = 0
    localConfig.wallpaperEdgeFade = false
    localConfig.theme = 'auto'
    localConfig.locale = 'zh'
    document.documentElement.classList.remove('wallpaper-active')
  })

  describe('global switch', () => {
    it('renders the enable switch reflecting this device\'s choice', async () => {
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const enableSwitch = wrapper.findAll('input[type="checkbox"]')[0]
      expect((enableSwitch.element as HTMLInputElement).checked).toBe(true)
    })

    it('turns the wallpaper off locally, with no server round-trip', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      chooseDevice('local', 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const enableSwitch = wrapper.findAll('input[type="checkbox"]')[0]
      await enableSwitch.setValue(false)

      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperEnabled', false)
      expect(localConfig.wallpaperEnabled).toBe(false)
      // The choice is per-device: it must never reach the server.
      expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/wallpaper')).toBe(false)
      expect(mockPatchConfig).not.toHaveBeenCalled()
    })
  })

  describe('source mode', () => {
    it('highlights the active mode and switches locally on click', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      chooseDevice('local', 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const localBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Local gallery')!
      const bingBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Bing daily')!
      expect(localBtn.classes()).toContain('wallpaper-mode__btn--active')
      expect(bingBtn.classes()).not.toContain('wallpaper-mode__btn--active')

      await bingBtn.trigger('click')
      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperMode', 'bing')
      expect(localConfig.wallpaperMode).toBe('bing')
      // Switching source is a per-device decision, never a server write.
      expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/wallpaper')).toBe(false)
    })

    it('shows the Bing block only in Bing mode', async () => {
      serverConfig.value = serverWith([], { file: 'bing-20260910.jpg', last_success_date: '20260910' })
      chooseDevice('bing')
      const wrapper = mountSetting()
      await nextTick()
      const labels = wrapper.findAll('.settings-item__label').map(l => l.text())
      expect(labels).toContain('Current image')
      expect(labels).not.toContain('Local gallery')
    })

    it('shows the gallery only in local mode', async () => {
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const labels = wrapper.findAll('.settings-item__label').map(l => l.text())
      expect(labels).toContain('Local gallery')
      expect(labels).not.toContain('Current image')
    })

    it('offers the animated wave as a third source', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      chooseDevice('local', 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const waveBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Animated')
      expect(waveBtn).toBeTruthy()

      await waveBtn!.trigger('click')
      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperMode', 'wave')
      expect(localConfig.wallpaperMode).toBe('wave')
    })

    it('marks the wave button active in wave mode', async () => {
      chooseDevice('wave')
      const wrapper = mountSetting()
      await nextTick()
      const waveBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Animated')!
      const localBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Local gallery')!
      expect(waveBtn.classes()).toContain('wallpaper-mode__btn--active')
      expect(localBtn.classes()).not.toContain('wallpaper-mode__btn--active')
    })

    it('shows the wave speed row and neither the gallery nor the Bing block', async () => {
      // The wave must not fall through to the gallery. The gallery branch is a
      // v-else, so this passes because the wave branch is matched FIRST.
      chooseDevice('wave')
      const wrapper = mountSetting()
      await nextTick()
      const labels = wrapper.findAll('.settings-item__label').map(l => l.text())
      expect(labels).toContain('Animation speed')
      expect(labels).not.toContain('Local gallery')
      expect(labels).not.toContain('Current image')
    })

    it('still shows the gallery when the mode is unset', async () => {
      // 'none' resolves from an unset/unknown stored value. The gallery must
      // render there — it is the only way to pick an image. Gating it on
      // `mode === 'local'` would hide it.
      chooseDevice('none')
      const wrapper = mountSetting()
      await nextTick()
      const labels = wrapper.findAll('.settings-item__label').map(l => l.text())
      expect(labels).toContain('Local gallery')
    })

    it('hides the image-only rows in wave mode but keeps panel opacity', async () => {
      // Blur and edge fade have no effect on the wave, so they are removed
      // outright rather than shown disabled — a control that can never apply to
      // the active background is noise. Panel opacity does apply (the wave shows
      // through the translucent panels), so it stays.
      chooseDevice('wave')
      const wrapper = mountSetting()
      await nextTick()
      const labels = wrapper.findAll('.settings-item__label').map(l => l.text())
      expect(labels).toContain('Panel opacity')
      expect(labels).not.toContain('Gaussian blur')
      expect(labels).not.toContain('Edge fade')
    })

    it('keeps the image-only rows enabled for an image wallpaper', async () => {
      // The counterpart to the test above: the split must not disable them for
      // images, where blur/edge-fade genuinely apply.
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const rows = wrapper.findAll('.settings-item')
      const rowFor = (label: string) =>
        rows.find(r => r.find('.settings-item__label').exists() && r.find('.settings-item__label').text() === label)!

      expect(rowFor('Gaussian blur').classes()).not.toContain('settings-item--disabled')
      expect(rowFor('Edge fade').classes()).not.toContain('settings-item--disabled')
    })

    it('writes the wave speed to local config', async () => {
      chooseDevice('wave')
      const wrapper = mountSetting()
      await nextTick()
      const slider = wrapper.findAll('input[type="range"]').find(i => {
        const el = i.element as HTMLInputElement
        return el.min === '10' && el.max === '100'
      })!
      ;(slider.element as HTMLInputElement).value = '80'
      await slider.trigger('input')
      expect(localConfig.wallpaperWaveSpeed).toBe(80)
    })

    it('polls for the Bing preview after switching to Bing', async () => {
      // The server fetches in the background; the panel must poll so the preview
      // appears without the user hitting 获取.
      const fetchMock = vi.fn(async (url: string) => {
        if (url === '/api/theme/bing/status') {
          return {
            ok: true,
            status: 200,
            json: async () => ({ file: 'bing-20260910.jpg', last_success_date: '20260910', copyright: '', title: '', last_error: '', last_attempt_at: 1 }),
          }
        }
        return { ok: true, status: 200, json: async () => ({}) }
      })
      vi.stubGlobal('fetch', fetchMock)
      chooseDevice('local', 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const bingBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Bing daily')!
      await bingBtn.trigger('click')
      // Let the polling loop run (it awaits a 1s sleep between attempts).
      await vi.waitFor(() => {
        expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/bing/status')).toBe(true)
      }, { timeout: 5000 })
    }, 10000)

    it('does not poll when an image is already cached for today', async () => {
      // The server skips the fetch when today's image is cached, so polling
      // would spin until the budget expired for a result that never comes.
      const fetchMock = vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ file: 'bing-today.jpg', last_success_date: todayStamp(), copyright: '', title: '', last_error: '', last_attempt_at: 1 }),
      }))
      vi.stubGlobal('fetch', fetchMock)
      chooseDevice('local', 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const bingBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Bing daily')!
      await bingBtn.trigger('click')
      await nextTick()

      expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/bing/status')).toBe(false)
    }, 10000)
  })

  describe('Bing', () => {
    it('shows the photographer credit and title in the panel', async () => {
      serverConfig.value = serverWith([], {
        file: 'bing-20260910.jpg',
        last_success_date: '20260910',
        copyright: '© Photographer',
        title: 'A Title',
      })
      chooseDevice('bing')
      const wrapper = mountSetting()
      await nextTick()
      expect(wrapper.find('.wallpaper-credit').text()).toContain('© Photographer')
      expect(wrapper.text()).toContain('A Title')
    })

    it('surfaces a sync error instead of hiding the wallpaper', async () => {
      serverConfig.value = serverWith([], { file: 'bing-20260910.jpg', last_error: 'network down' })
      chooseDevice('bing')
      const wrapper = mountSetting()
      await nextTick()
      const err = wrapper.find('.wallpaper-error')
      expect(err.exists()).toBe(true)
      expect(err.text()).toContain('network down')
      // The cached image is still shown.
      expect(wrapper.find('img.wallpaper-thumb').exists()).toBe(true)
    })

    it('triggers an immediate sync and polls the status endpoint', async () => {
      const fetchMock = vi.fn(async (url: string) => {
        if (url === '/api/theme/bing/status') {
          return {
            ok: true,
            status: 200,
            json: async () => ({ file: 'bing-20260911.jpg', last_success_date: todayStamp(), copyright: 'c', title: 't', last_error: '', last_attempt_at: 1 }),
          }
        }
        return { ok: true, status: 200, json: async () => ({}) }
      })
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([], { file: 'bing-20260910.jpg', last_success_date: '20260910' })
      chooseDevice('bing')

      const wrapper = mountSetting()
      await nextTick()
      const syncBtn = wrapper.findAll('button').find(b => b.text() === 'Sync now')!
      expect(syncBtn).toBeTruthy()
      await syncBtn.trigger('click')

      // The sync POST fires immediately; the status poll runs on a 1s timer.
      expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/bing/sync')).toBe(true)
      await new Promise(r => setTimeout(r, 1200))
      expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/bing/status')).toBe(true)
    }, 10000)

    it('reports "still fetching" instead of a false success when the poll times out', async () => {
      vi.useFakeTimers()
      try {
        // The fetch never settles within the budget: no error, no today's image.
        const fetchMock = vi.fn(async (url: string) => {
          if (url === '/api/theme/bing/status') {
            return {
              ok: true,
              status: 200,
              json: async () => ({ file: '', last_success_date: '', copyright: '', title: '', last_error: '', last_attempt_at: 1 }),
            }
          }
          return { ok: true, status: 200, json: async () => ({}) }
        })
        vi.stubGlobal('fetch', fetchMock)
        serverConfig.value = serverWith([], { file: '', last_success_date: '' })
        chooseDevice('bing')

        const wrapper = mountSetting()
        await vi.advanceTimersByTimeAsync(0)
        const syncBtn = wrapper.findAll('button').find(b => b.text() === 'Sync now')!
        await syncBtn.trigger('click')

        // Run past the 15s poll budget (the loop sleeps 1s per attempt).
        await vi.advanceTimersByTimeAsync(20000)

        // It must NOT claim success; the pending message is shown instead.
        const shownKeys = toastShow.mock.calls.map(c => c[0])
        expect(shownKeys).toContain('Pending')
        expect(shownKeys).not.toContain('Synced')
      } finally {
        vi.useRealTimers()
      }
    }, 20000)
  })

  describe('gallery', () => {
    it('renders one tile per item with the selected one marked', async () => {
      serverConfig.value = serverWith([
        { file: 'local-1-a.png', name: 'a.png' },
        { file: 'local-2-b.png', name: 'b.png' },
      ])
      chooseDevice('local', 'local-2-b.png')
      const wrapper = mountSetting()
      await nextTick()
      const tiles = wrapper.findAll('.wallpaper-gallery__item')
      expect(tiles).toHaveLength(2)
      expect(tiles[1].classes()).toContain('wallpaper-gallery__item--active')
      expect(tiles[0].classes()).not.toContain('wallpaper-gallery__item--active')
      // Thumbnails point at the by-name endpoint.
      expect(tiles[0].find('img').attributes('src')).toContain('/api/file/theme-wallpaper?name=local-1-a.png')
    })

    it('shows an empty state when the gallery has no items', async () => {
      serverConfig.value = serverWith([])
      chooseDevice('local')
      const wrapper = mountSetting()
      await nextTick()
      expect(wrapper.find('.wallpaper-gallery-empty').exists()).toBe(true)
      expect(wrapper.findAll('.wallpaper-gallery__item')).toHaveLength(0)
    })

    it('hides a thumbnail whose image fails, keeping the tile usable', async () => {
      // A missing file used to leave a broken-image glyph inside the tile, next
      // to the delete button. The tile itself must stay (it is still deletable),
      // so the image is hidden rather than the tile removed.
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local')
      const wrapper = mountSetting()
      await nextTick()

      const thumb = wrapper.find('.wallpaper-gallery__thumb')
      expect(thumb.classes()).not.toContain('local-media-hidden')

      await thumb.trigger('error')

      expect(wrapper.find('.wallpaper-gallery__thumb').classes()).toContain('local-media-hidden')
      // The tile and its delete control survive.
      expect(wrapper.find('.wallpaper-gallery__item').exists()).toBe(true)
      expect(wrapper.find('.wallpaper-gallery__delete').exists()).toBe(true)
    })

    it('hides a failed Bing preview thumbnail', async () => {
      serverConfig.value = serverWith([], { file: 'bing-20260910.jpg' })
      chooseDevice('bing')
      const wrapper = mountSetting()
      await nextTick()

      const thumb = wrapper.find('.wallpaper-thumb')
      expect(thumb.exists()).toBe(true)
      await thumb.trigger('error')

      expect(wrapper.find('.wallpaper-thumb').classes()).toContain('local-media-hidden')
    })

    it('selects a tile locally, with no server round-trip', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([
        { file: 'local-1-a.png', name: 'a.png' },
        { file: 'local-2-b.png', name: 'b.png' },
      ])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()

      await wrapper.findAll('.wallpaper-gallery__thumb')[1].trigger('click')
      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperLocalSelected', 'local-2-b.png')
      expect(localConfig.wallpaperLocalSelected).toBe('local-2-b.png')
      expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/local/select')).toBe(false)
    })

    it('does not re-select an already-selected tile', async () => {
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()

      await wrapper.findAll('.wallpaper-gallery__thumb')[0].trigger('click')
      expect(mockSetLocalConfig).not.toHaveBeenCalledWith('wallpaperLocalSelected', 'local-1-a.png')
    })

    it('deletes a tile via DELETE /api/theme/local/item', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()

      await wrapper.find('.wallpaper-gallery__delete').trigger('click')
      const call = fetchMock.mock.calls.find(c => (c[0] as string).startsWith('/api/theme/local/item'))
      expect(call).toBeTruthy()
      expect(call![0]).toBe('/api/theme/local/item?name=local-1-a.png')
      expect((call![1] as RequestInit).method).toBe('DELETE')
    })

    it('clears the local selection when this device deletes the image it shows', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()

      await wrapper.find('.wallpaper-gallery__delete').trigger('click')
      // The server no longer reselects a neighbour, so leaving the pointer set
      // would leave this device showing a file that no longer exists.
      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperLocalSelected', '')
    })

    it('keeps the local selection when a different image is deleted', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([
        { file: 'local-1-a.png', name: 'a.png' },
        { file: 'local-2-b.png', name: 'b.png' },
      ])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()

      await wrapper.findAll('.wallpaper-gallery__delete')[1].trigger('click')
      expect(mockSetLocalConfig).not.toHaveBeenCalledWith('wallpaperLocalSelected', '')
    })

    it('uploads multiple files in one request under the files field', async () => {
      const fetchMock = vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ items: [{ file: 'local-1-a.png' }, { file: 'local-2-b.png' }], errors: [] }),
      }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([])
      chooseDevice('local')

      const wrapper = mountSetting()
      await nextTick()
      const input = wrapper.find('input[type="file"]')
      expect(input.attributes('multiple')).toBeDefined()

      const files = [new File(['a'], 'a.png'), new File(['b'], 'b.png')]
      Object.defineProperty(input.element, 'files', {
        configurable: true,
        value: { 0: files[0], 1: files[1], length: 2, item: (i: number) => files[i] ?? null },
      })
      await input.trigger('change')
      await nextTick()

      const call = fetchMock.mock.calls.find(c => c[0] === '/api/theme/local/upload')
      expect(call).toBeTruthy()
      const form = (call![1] as RequestInit).body as FormData
      expect(form.getAll('files')).toHaveLength(2)
    })

    it('adopts the first uploaded image when nothing is selected yet', async () => {
      // The server no longer auto-selects on upload, so if the panel did not do
      // this the image would land in the gallery but the wallpaper would never
      // change.
      const fetchMock = vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ items: [{ file: 'local-1-a.png' }], errors: [] }),
      }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([])
      chooseDevice('local', '')

      const wrapper = mountSetting()
      await nextTick()
      const input = wrapper.find('input[type="file"]')
      const file = new File(['a'], 'a.png')
      Object.defineProperty(input.element, 'files', {
        configurable: true,
        value: { 0: file, length: 1, item: () => file },
      })
      await input.trigger('change')
      await new Promise(r => setTimeout(r, 20))

      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperLocalSelected', 'local-1-a.png')
      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperMode', 'local')
    })

    it('does not steal the wallpaper on a second upload', async () => {
      const fetchMock = vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ items: [{ file: 'local-9-z.png' }], errors: [] }),
      }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const input = wrapper.find('input[type="file"]')
      const file = new File(['a'], 'z.png')
      Object.defineProperty(input.element, 'files', {
        configurable: true,
        value: { 0: file, length: 1, item: () => file },
      })
      await input.trigger('change')
      await new Promise(r => setTimeout(r, 20))

      expect(mockSetLocalConfig).not.toHaveBeenCalledWith('wallpaperLocalSelected', 'local-9-z.png')
    })

    it('reports a partial upload failure without discarding the successes', async () => {
      const fetchMock = vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ items: [{ file: 'local-1-a.png' }], errors: [{ name: 'bad.png', error: 'nope' }] }),
      }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([])
      chooseDevice('local')

      const wrapper = mountSetting()
      await nextTick()
      const input = wrapper.find('input[type="file"]')
      const file = new File(['a'], 'a.png')
      Object.defineProperty(input.element, 'files', {
        configurable: true,
        value: { 0: file, length: 1, item: () => file },
      })
      await input.trigger('change')
      await nextTick()
      // The handler awaits the upload plus a loadConfig round-trip before
      // setting the error, so let the microtask queue drain.
      await new Promise(r => setTimeout(r, 20))
      await nextTick()

      expect(wrapper.find('.wallpaper-error').text()).toContain('1 ok, 1 failed')
    })

    it('refuses more files than the per-request cap before uploading', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = serverWith([])
      chooseDevice('local')

      const wrapper = mountSetting()
      await nextTick()
      const input = wrapper.find('input[type="file"]')
      // 11 files exceeds MaxUploadsPerRequest (10).
      const files = Array.from({ length: 11 }, (_, i) => new File(['x'], `f${i}.png`))
      const fileList: Record<number, File> & { length: number; item: (i: number) => File | null } = {
        length: files.length,
        item: (i: number) => files[i] ?? null,
      }
      files.forEach((f, i) => { fileList[i] = f })
      Object.defineProperty(input.element, 'files', { configurable: true, value: fileList })
      await input.trigger('change')
      await nextTick()

      expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/local/upload')).toBe(false)
      expect(wrapper.find('.wallpaper-error').exists()).toBe(true)
    })

    it('disables the upload button when the gallery is full', async () => {
      const items = Array.from({ length: 50 }, (_, i) => ({ file: `local-${i}-a.png`, name: `f${i}.png` }))
      serverConfig.value = serverWith(items)
      chooseDevice('local', 'local-0-a.png')
      const wrapper = mountSetting()
      await nextTick()

      const uploadBtn = wrapper.findAll('button').find(b => b.text() === 'Upload images')!
      expect(uploadBtn.attributes('disabled')).toBeDefined()
    })
  })

  describe('display options', () => {
    it('enables the sliders when an image wallpaper is displayed', async () => {
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const ranges = wrapper.findAll('input[type="range"]')
      expect(ranges[0].attributes('disabled')).toBeUndefined()
      expect(ranges[1].attributes('disabled')).toBeUndefined()
    })

    it('disables the sliders when this device turned the wallpaper off', async () => {
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png', false)
      const wrapper = mountSetting()
      await nextTick()
      const ranges = wrapper.findAll('input[type="range"]')
      expect(ranges[0].attributes('disabled')).toBeDefined()
      expect(ranges[1].attributes('disabled')).toBeDefined()
    })

    it('keeps panel opacity usable for the wave, which has no image', async () => {
      // Blur/edge-fade are image-only, but panel translucency applies to the
      // wave too — so the opacity slider must not follow the image rows.
      chooseDevice('wave')
      const wrapper = mountSetting()
      await nextTick()
      const opacity = wrapper.findAll('input[type="range"]').find(i => {
        const el = i.element as HTMLInputElement
        return el.min === '0'
      })!
      expect(opacity.attributes('disabled')).toBeUndefined()
    })

    it('persists panel opacity to localStorage, never to the server', async () => {
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const slider = wrapper.findAll('input[type="range"]')[0]
      await slider.setValue('0.75')
      await slider.trigger('input')
      await new Promise(r => setTimeout(r, 400))
      // Panel opacity is a per-device display tweak — it must go through
      // setLocalConfig (localStorage), never a config PATCH.
      expect(mockSetLocalConfig).toHaveBeenCalledWith('panelOpacity', 0.75)
      expect(mockPatchConfig).not.toHaveBeenCalled()
    })

    it('exposes the full 0-100 opacity range', async () => {
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const slider = wrapper.findAll('input[type="range"]')[0]
      expect(slider.attributes('min')).toBe('0')
      expect(slider.attributes('max')).toBe('1')
    })

    it('does not change the wallpaper image URL while dragging the opacity slider', async () => {
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()

      const { applyWallpaper } = await import('@/utils/themeBackground')
      applyWallpaper('local-1-a.png', 0.85, false)
      const html = document.documentElement
      const urlBefore = html.style.getPropertyValue('--wallpaper-url')
      expect(html.classList.contains('wallpaper-active')).toBe(true)

      const slider = wrapper.findAll('input[type="range"]')[0]
      await slider.setValue('0.8')
      await slider.trigger('input')
      await slider.setValue('0.78')
      await slider.trigger('input')
      expect(html.style.getPropertyValue('--wallpaper-url')).toBe(urlBefore)
      expect(html.style.getPropertyValue('--panel-alpha')).toBe('78%')
    })

    it('updates the local config live while dragging blur and persists debounced', async () => {
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const blurSlider = wrapper.findAll('input[type="range"]')[1]
      await blurSlider.setValue('30')
      await blurSlider.trigger('input')
      expect(localConfig.wallpaperBlur).toBe(30)
      expect(mockPatchConfig).not.toHaveBeenCalled()
      await new Promise(r => setTimeout(r, 300))
      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperBlur', 30)
    })

    it('toggles edge fade through setLocalConfig', async () => {
      serverConfig.value = serverWith([{ file: 'local-1-a.png', name: 'a.png' }])
      chooseDevice('local', 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      // The last checkbox is the edge-fade switch.
      const switches = wrapper.findAll('input[type="checkbox"]')
      const edgeSwitch = switches[switches.length - 1]
      expect(edgeSwitch.attributes('disabled')).toBeUndefined()
      await edgeSwitch.setValue(true)
      expect(mockSetLocalConfig).toHaveBeenCalledWith('wallpaperEdgeFade', true)
    })
  })
})

/** Today as yyyymmdd, matching the server's LastSuccessDate format. */
function todayStamp(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}`
}
