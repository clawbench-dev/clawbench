import { beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'

const { openFilePath, requestAttachmentTarget, startAttachDrag, cleanupDragGhost } = vi.hoisted(() => ({
  openFilePath: vi.fn(),
  requestAttachmentTarget: vi.fn(),
  startAttachDrag: vi.fn(),
  cleanupDragGhost: vi.fn(),
}))

vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({ openFilePath }),
}))

vi.mock('@/composables/useConversationTarget', () => ({
  requestAttachmentTarget,
}))

vi.mock('@/utils/attachDrag', () => ({
  startAttachDrag,
  cleanupDragGhost,
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

// The rows now take their glyph from the same place the file manager does:
// material icons for types, `/api/fs/thumb` for raster images. Stub the icon
// resolver so the `img` exists in jsdom without a HEAD request per icon.
vi.mock('@/utils/materialIcons', () => ({
  getFileIconUrl: (path: string) => Promise.resolve(`/material-icons/${path}.svg`),
  getFolderIconUrl: (path: string) => Promise.resolve(`/material-icons/folder-${path}.svg`),
}))

import MeetingResources from '../MeetingResources.vue'
import type { MeetingResourceGroup } from '@/composables/useMeetingResources'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })

const GROUPS: MeetingResourceGroup[] = [
  {
    key: 'folder',
    title: 'Meeting folder',
    items: [
      {
        key: 'folder:minutes',
        name: 'minutes.md',
        path: '.clawbench/notes/x/minutes.md',
        displayPath: '.clawbench/notes/x/minutes.md',
        kind: 'minutes',
        badge: 'Minutes',
        typeLabel: 'Markdown',
        size: 12_400,
        modified: '2026-11-01T15:12:00Z',
        isDir: false,
      },
    ],
  },
  {
    key: 'referenced',
    title: 'Referenced in minutes',
    hint: 'Only existing files',
    items: [
      {
        key: 'ref:src/a.ts',
        name: 'a.ts',
        path: 'src/a.ts',
        displayPath: 'src/a.ts',
        kind: 'code',
        typeLabel: 'TypeScript',
        isDir: false,
      },
    ],
  },
]

function mountResources(props: Record<string, unknown> = {}) {
  return mount(MeetingResources, {
    props: {
      groups: GROUPS,
      loading: false,
      error: '',
      total: 2,
      folderPath: '.clawbench/notes/x',
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

beforeEach(() => {
  openFilePath.mockReset()
  requestAttachmentTarget.mockReset()
  startAttachDrag.mockReset()
  cleanupDragGhost.mockReset()
})

describe('MeetingResources', () => {
  it('renders both groups with badges and the referenced path', () => {
    const wrapper = mountResources()

    expect(wrapper.findAll('.res-group')).toHaveLength(2)
    expect(wrapper.text()).toContain('Meeting folder')
    expect(wrapper.text()).toContain('Referenced in minutes')
    expect(wrapper.text()).toContain('Minutes')
    expect(wrapper.text()).toContain('src/a.ts')
    expect(wrapper.findAll('.res-row')).toHaveLength(2)
  })

  it('keeps the meeting link above the files, with the retention note', () => {
    const wrapper = mountResources({
      shareToken: 'tok-9',
      links: [{ url: 'http://10.0.0.5:20000/share/tok-9', address: { ip: '10.0.0.5', interface: '', kind: 'lan' } }],
    })

    const block = wrapper.find('[data-role="meeting-link"]')
    expect(block.exists()).toBe(true)
    expect(block.find('input[data-share-primary]').attributes('value')).toBe(
      'http://10.0.0.5:20000/share/tok-9',
    )
    // The sentence that answers "how long does this keep working" is the whole
    // point of showing the link here rather than in the running stage.
    expect(block.text()).toContain(enMessages.meeting.resources.linkHint)
  })

  it('hides the link block when there is no record token', () => {
    const wrapper = mountResources()
    expect(wrapper.find('[data-role="meeting-link"]').exists()).toBe(false)
  })

  it('opens a row through the live navigation source', async () => {
    const wrapper = mountResources()

    await wrapper.findAll('.res-row')[1].trigger('click')

    expect(openFilePath).toHaveBeenCalledWith('src/a.ts', undefined, undefined, 'live')
  })

  it('attaches a row without also opening it', async () => {
    const wrapper = mountResources()

    await wrapper.find('[data-attach="src/a.ts"]').trigger('click')

    expect(requestAttachmentTarget).toHaveBeenCalledWith(
      { path: 'src/a.ts', isDir: false },
      { deliverTo: 'active' },
    )
    expect(openFilePath).not.toHaveBeenCalled()
  })

  it('starts the shared attach drag with the row identity', async () => {
    const wrapper = mountResources()

    await wrapper.findAll('.res-row')[0].trigger('dragstart')

    expect(startAttachDrag).toHaveBeenCalledWith(
      expect.anything(),
      '.clawbench/notes/x/minutes.md',
      { name: 'minutes.md', isDir: false },
    )
  })

  it('shows the empty card when the meeting left nothing at all', () => {
    const wrapper = mountResources({ groups: [], total: 0, folderPath: '' })

    expect(wrapper.find('.res-row').exists()).toBe(false)
    const card = wrapper.find('.meeting-empty')
    expect(card.exists()).toBe(true)
    // Filled, so it centres in the tab body like the other three tabs' empty
    // states instead of hugging the top edge.
    expect(card.classes()).toContain('is-fill')
    expect(wrapper.find('.meeting-empty__title').text()).toBe(enMessages.meeting.resources.emptyTitle)
    expect(wrapper.text()).toContain(enMessages.meeting.resources.empty)
  })

  it('keeps the small hint when a folder exists but holds nothing', () => {
    // The header above already states the folder and its zero count, so the
    // full card would repeat it; the hint is the right weight there.
    const wrapper = mountResources({ groups: [], total: 0 })

    expect(wrapper.find('.meeting-empty').exists()).toBe(false)
    expect(wrapper.find('.res-hint').text()).toContain(enMessages.meeting.resources.empty)
  })

  it('emits reload from the error state', async () => {
    const wrapper = mountResources({ groups: [], error: 'boom' })

    await wrapper.find('.meeting-link').trigger('click')

    expect(wrapper.emitted('reload')).toHaveLength(1)
  })
})

/**
 * Rows take their glyph from the same place the file manager does: the material
 * icon theme for types, `/api/fs/thumb` for decodable raster images.
 */
describe('MeetingResources row glyphs (file manager parity)', () => {
  const SHOTS: MeetingResourceGroup = {
    key: 'shots',
    title: 'Shots',
    items: [
      {
        key: 'shots:a.png',
        name: 'a.png',
        path: '.clawbench/notes/x/shots/a.png',
        displayPath: '.clawbench/notes/x/shots/a.png',
        kind: 'image',
        typeLabel: 'PNG',
        isDir: false,
      },
      {
        key: 'shots:b.webp',
        name: 'b.webp',
        path: '.clawbench/notes/x/shots/b.webp',
        displayPath: '.clawbench/notes/x/shots/b.webp',
        kind: 'image',
        typeLabel: 'WebP',
        isDir: false,
      },
      {
        key: 'shots:c.svg',
        name: 'c.svg',
        path: '.clawbench/notes/x/shots/c.svg',
        displayPath: '.clawbench/notes/x/shots/c.svg',
        kind: 'image',
        typeLabel: 'SVG',
        isDir: false,
      },
    ],
  }

  function mountShots() {
    return mountResources({ groups: [SHOTS], total: SHOTS.items.length })
  }

  it('shows the backend thumbnail for images the file manager can rasterize', () => {
    const wrapper = mountShots()

    const img = wrapper.find('img.res-thumb')
    expect(img.exists()).toBe(true)
    // Same endpoint and width the file manager list uses.
    expect(img.attributes('src')).toContain('/api/fs/thumb')
    expect(img.attributes('src')).toContain(encodeURIComponent(SHOTS.items[0].path))
    expect(img.attributes('src')).toContain('w=80')
  })

  // Meeting key frames are stored as WebP — the backend registers the WebP
  // decoder for exactly this reason, so the shots must get real thumbnails.
  it('thumbnails WebP shots too', () => {
    const wrapper = mountShots()

    const webpRow = wrapper.findAll('.res-row')[1]
    const img = webpRow.find('img.res-thumb')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain(encodeURIComponent(SHOTS.items[1].path))
  })

  it('keeps the type icon for images no decoder handles (SVG)', async () => {
    const wrapper = mountShots()
    await flushPromises()

    const svgRow = wrapper.findAll('.res-row')[2]
    expect(svgRow.find('img.res-thumb').exists()).toBe(false)
    // The row falls back to the material icon.
    expect(svgRow.find('img.file-type-icon').exists()).toBe(true)
  })

  // `shots` used to fall into the `v-else` and render the link glyph — it
  // holds screenshots, not references.
  it('labels the shots group with a picture glyph, referenced keeps the link glyph', () => {
    const shots = mountShots()
    expect(shots.find('.res-group__label svg.lucide-image').exists()).toBe(true)
    expect(shots.find('.res-group__label svg.lucide-link-2').exists()).toBe(false)

    const refs = mountResources()
    expect(refs.find('.res-group__label svg.lucide-link-2').exists()).toBe(true)
  })

  it('renders no lucide glyph inside the icon tile (the material theme owns it)', async () => {
    const wrapper = mountResources()
    await flushPromises()

    expect(wrapper.findAll('.res-icon svg')).toHaveLength(0)
    expect(wrapper.findAll('.res-icon img.file-type-icon').length).toBeGreaterThan(0)
  })

  it('falls back to the type icon after a thumbnail request fails', async () => {
    const wrapper = mountShots()

    await wrapper.find('img.res-thumb').trigger('error')
    await flushPromises()

    const rows = wrapper.findAll('.res-row')
    expect(rows[0].find('img.res-thumb').exists()).toBe(false)
    expect(rows[0].find('img.file-type-icon').exists()).toBe(true)
    // The other rows are untouched.
    expect(rows[1].find('img.res-thumb').exists()).toBe(true)
  })
})

/**
 * jsdom applies no stylesheets, so a leftover chevron rule cannot be asserted on
 * the DOM. Read the source instead — the row already reads as clickable (hover,
 * cursor, and the two action buttons), so the arrow was pure noise.
 */
describe('MeetingResources row affordances (source)', () => {
  const src = readFileSync(join(__dirname, '..', 'MeetingResources.vue'), 'utf8')

  it('does not render a chevron at the end of the row', () => {
    expect(src).not.toContain('ChevronRight')
    expect(src).not.toContain('res-chevron')
  })
})
