import { beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { mount } from '@vue/test-utils'
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
      'minutes.md',
      false,
    )
  })

  it('shows the empty hint when there is nothing to list', () => {
    const wrapper = mountResources({ groups: [], total: 0 })

    expect(wrapper.find('.res-row').exists()).toBe(false)
    expect(wrapper.text()).toContain(enMessages.meeting.resources.empty)
  })

  it('emits reload from the error state', async () => {
    const wrapper = mountResources({ groups: [], error: 'boom' })

    await wrapper.find('.meeting-link').trigger('click')

    expect(wrapper.emitted('reload')).toHaveLength(1)
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
