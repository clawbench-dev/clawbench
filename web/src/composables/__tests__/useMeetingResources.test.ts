import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { nextTick, ref } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '/project', homeDir: '/home/user' } },
}))

vi.mock('@/utils/appLog.ts', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const { buildMarkdownPreviewDom } = vi.hoisted(() => ({
  buildMarkdownPreviewDom: vi.fn(),
}))

vi.mock('@/composables/useMarkdownRenderPipeline.ts', () => ({
  buildMarkdownPreviewDom,
}))

import { useMeetingResources } from '../useMeetingResources'
import type { MeetingRecord } from '../useMeetingHistory'

const DIR = '.clawbench/notes/2026-11-01-1430-review-abc123'

function record(over: Partial<MeetingRecord> = {}): MeetingRecord {
  return {
    token: 'tok-1',
    title: 'Release review',
    startedAt: 1_700_000_000_000,
    endedAt: 1_700_002_880_000,
    dirPath: `/project/${DIR}`,
    hasMinutes: true,
    hasTranscript: true,
    hasRevised: true,
    hasMeetingAudio: true,
    audioFiles: 3,
    chatCount: 0,
    status: 'ready',
    updatedAt: Date.now(),
    ...over,
  }
}

interface DirItem { name: string; type: 'dir' | 'file' | 'image'; size?: number; modified?: string }

function jsonResponse(body: unknown) {
  return { ok: true, status: 200, json: async () => body } as unknown as Response
}

function installFetch(opts: {
  root: DirItem[]
  audio?: DirItem[]
  shots?: DirItem[]
  exists?: Record<string, string>
  /** Held open before responding, so a test can assert mid-flight. */
  gate?: Promise<void>
}) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (opts.gate) await opts.gate
    if (url.startsWith('/api/dir?')) {
      const path = decodeURIComponent(url.slice('/api/dir?path='.length))
      if (path === DIR) return jsonResponse({ items: opts.root })
      if (path === `${DIR}/audio`) return jsonResponse({ items: opts.audio ?? [] })
      if (path === `${DIR}/shots`) return jsonResponse({ items: opts.shots ?? [] })
      return jsonResponse({ items: [] })
    }
    if (url === '/api/file/batch-exists') {
      return jsonResponse({ results: opts.exists ?? {} })
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const ROOT: DirItem[] = [
  { name: 'transcript.vtt', type: 'file', size: 200 },
  { name: 'minutes.md', type: 'file', size: 100 },
  { name: 'transcript-revised.md', type: 'file', size: 300 },
  { name: 'audio', type: 'dir' },
]

const AUDIO: DirItem[] = [
  { name: 'system.webm', type: 'file', size: 10 },
  { name: 'meeting.webm', type: 'file', size: 30 },
  { name: 'mic.webm', type: 'file', size: 20 },
]

function setup(rec: MeetingRecord | null, minutes: string) {
  const recordRef = ref<MeetingRecord | null>(rec)
  const minutesText = ref(minutes)
  const minutesPath = ref(rec?.dirPath ? `${rec.dirPath}/minutes.md` : '')
  const api = useMeetingResources({
    record: () => recordRef.value,
    minutesText: () => minutesText.value,
    minutesPath: () => minutesPath.value,
  })
  return { api, recordRef, minutesText, minutesPath }
}

beforeEach(() => {
  buildMarkdownPreviewDom.mockReset()
  buildMarkdownPreviewDom.mockReturnValue({ html: '', detectedPaths: [] })
})

describe('useMeetingResources', () => {
  it('lists the meeting folder and audio channel files in reading order', async () => {
    installFetch({ root: ROOT, audio: AUDIO })
    const { api } = setup(record(), '')
    await flushPromises()

    const folder = api.groups.value.find((group) => group.key === 'folder')
    const audio = api.groups.value.find((group) => group.key === 'audio')
    expect(folder?.items.map((item) => item.name)).toEqual([
      'minutes.md',
      'transcript.vtt',
      'transcript-revised.md',
    ])
    expect(audio?.items.map((item) => item.name)).toEqual([
      'meeting.webm',
      'mic.webm',
      'system.webm',
    ])
    expect(api.total.value).toBe(6)
  })

  it('lists key frames as their own group, not as a folder row', async () => {
    installFetch({
      root: [...ROOT, { name: 'shots', type: 'dir' }],
      audio: AUDIO,
      shots: [
        { name: '1700000000000-file-3f2a.webp', type: 'image', size: 1200 },
        { name: '1700000005000-manual-9c1b.webp', type: 'image', size: 900 },
      ],
    })
    const { api } = setup(record(), '')
    await flushPromises()

    const shots = api.groups.value.find((group) => group.key === 'shots')
    expect(shots?.items.map((item) => item.displayPath)).toEqual([
      'shots/1700000000000-file-3f2a.webp',
      'shots/1700000005000-manual-9c1b.webp',
    ])
    expect(shots?.items.every((item) => item.kind === 'image')).toBe(true)
    // The directory itself is not repeated as an opaque folder entry.
    const folder = api.groups.value.find((group) => group.key === 'folder')
    expect(folder?.items.some((item) => item.name === 'shots')).toBe(false)
    expect(api.total.value).toBe(8)
  })

  it('adds the paths the minutes reference, de-duplicated and outside the meeting folder', async () => {
    installFetch({
      root: ROOT,
      audio: AUDIO,
      exists: { 'web/src/a.ts': 'file', 'docs/spec/client/design-guide.md': 'file' },
    })
    buildMarkdownPreviewDom.mockReturnValue({
      html: '',
      detectedPaths: [
        // The minutes link to its sibling transcript — that file already has a
        // row in the folder group and must not be listed twice.
        `${DIR}/transcript.vtt`,
        'web/src/a.ts',
        'web/src/a.ts',
        'docs/spec/client/design-guide.md',
        'missing/nope.ts',
      ],
    })

    const { api } = setup(record(), 'See [transcript](transcript.vtt) and web/src/a.ts')
    await flushPromises()

    const refs = api.groups.value.find((group) => group.key === 'referenced')
    expect(refs?.items.map((item) => item.displayPath)).toEqual([
      'docs/spec/client/design-guide.md',
      'web/src/a.ts',
    ])
    // buildMarkdownPreviewDom is the same pipeline the minutes render with;
    // generated content must be sanitized.
    expect(buildMarkdownPreviewDom).toHaveBeenCalledWith(
      expect.objectContaining({ path: `/project/${DIR}/minutes.md` }),
      expect.objectContaining({ sanitize: true }),
    )
  })

  it('drops referenced paths that do not exist as files', async () => {
    installFetch({ root: ROOT, audio: [], exists: { 'web/src/a.ts': 'none', 'web/src/b.ts': 'dir' } })
    buildMarkdownPreviewDom.mockReturnValue({
      html: '',
      detectedPaths: ['web/src/a.ts', 'web/src/b.ts'],
    })

    const { api } = setup(record(), 'web/src/a.ts web/src/b.ts')
    await flushPromises()

    expect(api.groups.value.some((group) => group.key === 'referenced')).toBe(false)
  })

  it('still lists referenced files when the meeting has no folder', async () => {
    installFetch({ root: [], exists: { 'web/src/a.ts': 'file' } })
    buildMarkdownPreviewDom.mockReturnValue({ html: '', detectedPaths: ['web/src/a.ts'] })

    const { api } = setup(record({ dirPath: undefined }), 'web/src/a.ts')
    await flushPromises()

    expect(api.groups.value.map((group) => group.key)).toEqual(['referenced'])
    expect(api.total.value).toBe(1)
  })

  it('reloads when the minutes text arrives after the record', async () => {
    installFetch({ root: ROOT, audio: AUDIO })
    const { api, minutesText } = setup(record(), '')
    await flushPromises()
    const before = api.total.value

    buildMarkdownPreviewDom.mockReturnValue({ html: '', detectedPaths: ['web/src/a.ts'] })
    // The second load needs the existence result too.
    installFetch({ root: ROOT, audio: AUDIO, exists: { 'web/src/a.ts': 'file' } })
    minutesText.value = 'web/src/a.ts'
    await flushPromises()

    expect(before).toBe(6)
    expect(api.total.value).toBe(7)
  })

  it('refreshes silently when the detail poll hands back an equal record', async () => {
    // The detail view polls a just-ended meeting every 2.5s and assigns a FRESH
    // record object each tick (`record.value = await history.get(...)`), so the
    // watcher fires again even though every value it reads is unchanged.
    // Showing the spinner for that refresh is the reported "the resource tab
    // keeps flickering by itself": the list is swapped for a centered 加载中.
    installFetch({ root: ROOT, audio: AUDIO })
    const { api, recordRef } = setup(record(), '')
    await flushPromises()
    expect(api.loading.value).toBe(false)
    expect(api.groups.value.map((group) => group.key)).toEqual(['folder', 'audio'])

    let release: (() => void) | null = null
    const inFlight = new Promise<void>((resolve) => { release = resolve })
    installFetch({ root: ROOT, audio: AUDIO, gate: inFlight })

    recordRef.value = record() // same token/dirPath, new object — one poll tick
    await nextTick()

    // Asserted WHILE the refresh is in flight: `loading` only lives for a few
    // microtasks, so checking after it settles passes whichever way it is
    // implemented.
    expect(api.loading.value).toBe(false)
    expect(api.groups.value.map((group) => group.key)).toEqual(['folder', 'audio'])

    release?.()
    await flushPromises()
    expect(api.loading.value).toBe(false)
    expect(api.groups.value).toHaveLength(2)
  })

  it('still shows the spinner when a different meeting is loaded', async () => {
    // The silent path must be keyed on the meeting, not on "we already have
    // groups": switching records has to blank-and-load like a first paint.
    installFetch({ root: ROOT, audio: AUDIO })
    const { api, recordRef } = setup(record(), '')
    await flushPromises()

    let release: (() => void) | null = null
    const inFlight = new Promise<void>((resolve) => { release = resolve })
    installFetch({ root: [], gate: inFlight })

    recordRef.value = record({ token: 'tok-2', dirPath: '/project/.clawbench/notes/other' })
    await nextTick()
    expect(api.loading.value).toBe(true)

    release?.()
    await flushPromises()
    expect(api.loading.value).toBe(false)
  })

  it('keeps the list on screen when a background refresh fails', async () => {
    installFetch({ root: ROOT, audio: AUDIO })
    // Non-empty minutes: `detectReferenced` short-circuits on blank text, so a
    // blank body would never reach the pipeline this test makes throw.
    const { api, recordRef } = setup(record(), 'see web/src/a.ts')
    await flushPromises()
    expect(api.groups.value).toHaveLength(2)

    buildMarkdownPreviewDom.mockImplementation(() => { throw new Error('bad minutes') })
    recordRef.value = record()
    await flushPromises()

    expect(api.error.value).toBe('')
    expect(api.groups.value).toHaveLength(2)
  })
})
