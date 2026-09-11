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
const serverConfig = ref<Record<string, unknown>>({ appearance: { active_file: '', panel_opacity: 0.85 } })
// The component treats localConfig as the real module-level reactive singleton
// (it writes localConfig.wallpaperBlur directly for live preview), so the mock
// must be reactive — not a ref — for those direct writes to be observable.
const localConfig = reactive<Record<string, string | number | boolean | null>>({
  theme: 'auto',
  locale: 'zh',
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

/** Server config with the local gallery active. */
function localConfigWith(items: { file: string; name: string }[], selected: string) {
  return {
    appearance: {
      active_file: selected,
      wallpaper_file: '',
      panel_opacity: 0.85,
      wallpaper_mode: 'local',
      wallpaper_enabled: true,
      local: {
        selected,
        items: items.map((it, i) => ({ ...it, uploaded_at: i + 1, size: 100 })),
      },
      bing: { enabled: false, file: '', last_success_date: '', copyright: '', title: '', mkt: 'zh-CN', last_error: '', last_attempt_at: 0 },
    },
  }
}

/** Server config with the Bing daily image active. */
function bingConfigWith(overrides: Record<string, unknown> = {}) {
  return {
    appearance: {
      active_file: 'bing-20260910.jpg',
      wallpaper_file: '',
      panel_opacity: 0.85,
      wallpaper_mode: 'bing',
      wallpaper_enabled: true,
      local: { selected: '', items: [] },
      bing: {
        enabled: true,
        file: 'bing-20260910.jpg',
        last_success_date: '20260910',
        copyright: '© Photographer',
        title: 'A Title',
        mkt: 'zh-CN',
        last_error: '',
        last_attempt_at: 1,
        ...overrides,
      },
    },
  }
}

describe('WallpaperSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.unstubAllGlobals()
    stubMatchMedia()
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) })))
    serverConfig.value = { appearance: { active_file: '', wallpaper_file: '', panel_opacity: 0.85 } }
    localConfig.wallpaperBlur = 0
    localConfig.wallpaperEdgeFade = false
    localConfig.theme = 'auto'
    localConfig.locale = 'zh'
    document.documentElement.classList.remove('wallpaper-active')
  })

  describe('global switch', () => {
    it('renders the enable switch reflecting the server state', async () => {
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const enableSwitch = wrapper.findAll('input[type="checkbox"]')[0]
      expect((enableSwitch.element as HTMLInputElement).checked).toBe(true)
    })

    it('disables the wallpaper via POST /api/theme/wallpaper', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const enableSwitch = wrapper.findAll('input[type="checkbox"]')[0]
      await enableSwitch.setValue(false)

      const call = fetchMock.mock.calls.find(c => c[0] === '/api/theme/wallpaper')
      expect(call).toBeTruthy()
      expect(JSON.parse((call![1] as RequestInit).body as string)).toEqual({ enabled: false })
    })
  })

  describe('source mode', () => {
    it('highlights the active mode and switches on click', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const localBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Local gallery')!
      const bingBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Bing daily')!
      expect(localBtn.classes()).toContain('wallpaper-mode__btn--active')
      expect(bingBtn.classes()).not.toContain('wallpaper-mode__btn--active')

      await bingBtn.trigger('click')
      const call = fetchMock.mock.calls.find(c => c[0] === '/api/theme/wallpaper')
      expect(JSON.parse((call![1] as RequestInit).body as string)).toEqual({ mode: 'bing' })
    })

    it('shows the Bing block only in Bing mode', async () => {
      serverConfig.value = bingConfigWith()
      const wrapper = mountSetting()
      await nextTick()
      const labels = wrapper.findAll('.settings-item__label').map(l => l.text())
      expect(labels).toContain('Current image')
      expect(labels).not.toContain('Local gallery')
    })

    it('shows the gallery only in local mode', async () => {
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const labels = wrapper.findAll('.settings-item__label').map(l => l.text())
      expect(labels).toContain('Local gallery')
      expect(labels).not.toContain('Current image')
    })

    it('polls for the Bing preview after switching to Bing', async () => {
      // The server starts a fetch when the source switches to Bing; the panel
      // must poll so the preview appears without the user hitting 获取.
      const fetchMock = vi.fn(async (url: string) => {
        if (url === '/api/theme/bing/status') {
          return {
            ok: true,
            status: 200,
            json: async () => ({ enabled: true, file: 'bing-20260910.jpg', last_success_date: '20260910', copyright: '', title: '', mkt: 'zh-CN', last_error: '', last_attempt_at: 1 }),
          }
        }
        return { ok: true, status: 200, json: async () => ({}) }
      })
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const bingBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Bing daily')!
      await bingBtn.trigger('click')
      // Let the polling loop run (it awaits a 1s sleep between attempts).
      await vi.waitFor(() => {
        expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/bing/status')).toBe(true)
      }, { timeout: 5000 })
    }, 10000)
    it('does not block the panel while the Bing image downloads', async () => {
      // Regression: switching to Bing used to await the whole download poll
      // while `busy` stayed set, leaving every control disabled for up to the
      // poll budget. The switch must release the UI immediately.
      let statusCalls = 0
      const fetchMock = vi.fn(async (url: string) => {
        if (url === '/api/theme/bing/status') {
          statusCalls += 1
          return {
            ok: true,
            status: 200,
            // Never settles within the poll budget.
            json: async () => ({ enabled: true, file: '', last_success_date: '', copyright: '', title: '', mkt: 'zh-CN', last_error: '', last_attempt_at: 1 }),
          }
        }
        return { ok: true, status: 200, json: async () => ({}) }
      })
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')

      const wrapper = mountSetting()
      await nextTick()
      const bingBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Bing daily')!
      await bingBtn.trigger('click')

      // The panel must become interactive without waiting for the download poll.
      // The status endpoint never settles here, so an awaited poll would keep
      // the buttons disabled for the full 15-iteration budget.
      const localBtn = wrapper.findAll('.wallpaper-mode__btn').find(b => b.text() === 'Local gallery')!
      await vi.waitFor(() => {
        expect(localBtn.attributes('disabled')).toBeUndefined()
      }, { timeout: 2000 })
      expect(statusCalls).toBeLessThan(5)
    }, 10000)

    it('does not poll when an image is already cached for today', async () => {
      // The server skips the fetch when today's image is cached, so polling
      // would spin until the budget expired for a result that never comes.
      const fetchMock = vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ enabled: true, file: 'bing-today.jpg', last_success_date: todayStamp(), copyright: '', title: '', mkt: 'zh-CN', last_error: '', last_attempt_at: 1 }),
      }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')

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
      serverConfig.value = bingConfigWith()
      const wrapper = mountSetting()
      await nextTick()
      expect(wrapper.find('.wallpaper-credit').text()).toContain('© Photographer')
      expect(wrapper.text()).toContain('A Title')
    })

    it('surfaces a sync error instead of hiding the wallpaper', async () => {
      serverConfig.value = bingConfigWith({ last_error: 'network down' })
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
            json: async () => ({ enabled: true, file: 'bing-20260911.jpg', last_success_date: todayStamp(), copyright: 'c', title: 't', mkt: 'zh-CN', last_error: '', last_attempt_at: 1 }),
          }
        }
        return { ok: true, status: 200, json: async () => ({}) }
      })
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = bingConfigWith()

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
              json: async () => ({ enabled: true, file: '', last_success_date: '', copyright: '', title: '', mkt: 'zh-CN', last_error: '', last_attempt_at: 1 }),
            }
          }
          return { ok: true, status: 200, json: async () => ({}) }
        })
        vi.stubGlobal('fetch', fetchMock)
        serverConfig.value = bingConfigWith({ file: '', last_success_date: '' })

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
      serverConfig.value = localConfigWith(
        [{ file: 'local-1-a.png', name: 'a.png' }, { file: 'local-2-b.png', name: 'b.png' }],
        'local-2-b.png',
      )
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
      serverConfig.value = localConfigWith([], '')
      const wrapper = mountSetting()
      await nextTick()
      expect(wrapper.find('.wallpaper-gallery-empty').exists()).toBe(true)
      expect(wrapper.findAll('.wallpaper-gallery__item')).toHaveLength(0)
    })

    it('selects a tile via POST /api/theme/local/select', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith(
        [{ file: 'local-1-a.png', name: 'a.png' }, { file: 'local-2-b.png', name: 'b.png' }],
        'local-1-a.png',
      )
      const wrapper = mountSetting()
      await nextTick()

      await wrapper.findAll('.wallpaper-gallery__thumb')[1].trigger('click')
      const call = fetchMock.mock.calls.find(c => c[0] === '/api/theme/local/select')
      expect(call).toBeTruthy()
      expect(JSON.parse((call![1] as RequestInit).body as string)).toEqual({ name: 'local-2-b.png' })
    })

    it('does not re-select an already-selected tile', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()

      await wrapper.findAll('.wallpaper-gallery__thumb')[0].trigger('click')
      expect(fetchMock.mock.calls.some(c => c[0] === '/api/theme/local/select')).toBe(false)
    })

    it('deletes a tile via DELETE /api/theme/local/item', async () => {
      const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()

      await wrapper.find('.wallpaper-gallery__delete').trigger('click')
      const call = fetchMock.mock.calls.find(c => (c[0] as string).startsWith('/api/theme/local/item'))
      expect(call).toBeTruthy()
      expect(call![0]).toBe('/api/theme/local/item?name=local-1-a.png')
      expect((call![1] as RequestInit).method).toBe('DELETE')
    })

    it('uploads multiple files in one request under the files field', async () => {
      const fetchMock = vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ items: [{ file: 'local-1-a.png' }, { file: 'local-2-b.png' }], errors: [] }),
      }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith([], '')

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

    it('reports a partial upload failure without discarding the successes', async () => {
      const fetchMock = vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ items: [{ file: 'local-1-a.png' }], errors: [{ name: 'bad.png', error: 'nope' }] }),
      }))
      vi.stubGlobal('fetch', fetchMock)
      serverConfig.value = localConfigWith([], '')

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
      serverConfig.value = localConfigWith([], '')

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
      serverConfig.value = localConfigWith(items, 'local-0-a.png')
      const wrapper = mountSetting()
      await nextTick()

      const uploadBtn = wrapper.findAll('button').find(b => b.text() === 'Upload images')!
      expect(uploadBtn.attributes('disabled')).toBeDefined()
    })
  })

  describe('display options', () => {
    it('enables the sliders only when a wallpaper is actually displayed', async () => {
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const ranges = wrapper.findAll('input[type="range"]')
      expect(ranges[0].attributes('disabled')).toBeUndefined()
      expect(ranges[1].attributes('disabled')).toBeUndefined()
    })

    it('disables the sliders when the wallpaper is globally disabled', async () => {
      const cfg = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
      cfg.appearance.wallpaper_enabled = false
      cfg.appearance.active_file = ''
      serverConfig.value = cfg
      const wrapper = mountSetting()
      await nextTick()
      const ranges = wrapper.findAll('input[type="range"]')
      expect(ranges[0].attributes('disabled')).toBeDefined()
      expect(ranges[1].attributes('disabled')).toBeDefined()
    })

    it('persists panel opacity with a debounced PATCH on slider input', async () => {
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const slider = wrapper.findAll('input[type="range"]')[0]
      await slider.setValue('0.75')
      await slider.trigger('input')
      await new Promise(r => setTimeout(r, 400))
      expect(mockPatchConfig).toHaveBeenCalledWith({ appearance: { panel_opacity: 0.75 } })
    })

    it('exposes the relaxed 0.5 lower bound', async () => {
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
      const wrapper = mountSetting()
      await nextTick()
      const slider = wrapper.findAll('input[type="range"]')[0]
      expect(slider.attributes('min')).toBe('0.5')
      expect(slider.attributes('max')).toBe('1')
    })

    it('does not change the wallpaper image URL while dragging the opacity slider', async () => {
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
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
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
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
      serverConfig.value = localConfigWith([{ file: 'local-1-a.png', name: 'a.png' }], 'local-1-a.png')
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

  describe('locale following', () => {
    it('syncs the Bing market when the panel opens in Bing mode', async () => {
      serverConfig.value = bingConfigWith({ mkt: 'en-US' })
      localConfig.locale = 'zh'
      const wrapper = mountSetting()
      await nextTick()
      await nextTick()
      // zh locale with an en-US market persisted → correct it to zh-CN.
      expect(mockPatchConfig).toHaveBeenCalledWith({ appearance: { bing: { mkt: 'zh-CN' } } })
    })

    it('does not patch when the market already matches the locale', async () => {
      serverConfig.value = bingConfigWith({ mkt: 'zh-CN' })
      localConfig.locale = 'zh'
      const wrapper = mountSetting()
      await nextTick()
      await nextTick()
      expect(mockPatchConfig).not.toHaveBeenCalled()
    })
  })
})

/** Today as yyyymmdd, matching the server's LastSuccessDate format. */
function todayStamp(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}`
}
