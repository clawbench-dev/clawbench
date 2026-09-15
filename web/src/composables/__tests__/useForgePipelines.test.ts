import { describe, expect, it, vi, beforeEach } from 'vitest'
import { nextTick } from 'vue'

// The composable resolves through the shared forge API client; mock it so the
// tests assert request shaping and state handling rather than HTTP.
const mockFetchForgePipelines = vi.fn()
const mockFetchForgePipeline = vi.fn()
const mockMarkForgeRead = vi.fn()

vi.mock('@/utils/forgeApi', async () => {
  const actual = await vi.importActual<typeof import('@/utils/forgeApi')>('@/utils/forgeApi')
  return {
    ...actual,
    fetchForgePipelines: (...a: unknown[]) => mockFetchForgePipelines(...a),
    fetchForgePipeline: (...a: unknown[]) => mockFetchForgePipeline(...a),
    markForgeRead: (...a: unknown[]) => mockMarkForgeRead(...a),
  }
})

// Marking read re-derives the dock badge; the badge itself is covered by
// useForgeUnread.test.ts, so stub it here to keep this test about the list.
vi.mock('@/composables/useForgeUnread', () => ({
  useForgeUnread: () => ({
    forgeUnreadCount: { value: 0 },
    refresh: vi.fn(),
    onForgeEvent: vi.fn(),
    // The list delegates the repo-wide write to the shared composable; forward
    // it so this test still asserts the request shape.
    markAllRead: () => mockMarkForgeRead(),
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { useForgePipelines, useForgePipelineDetail } from '@/composables/useForge'
import { ForgeApiError } from '@/utils/forgeApi'

function run(id: number, status = 'failure') {
  return {
    platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets',
    id, name: 'CI', number: id, status, ref: 'main', sha: 'abc1234',
    event: 'push', actor: 'octocat', url: `https://ci/run/${id}`,
    createdAt: '2026-09-14T10:00:00Z', updatedAt: '2026-09-14T10:05:00Z',
    slug: 'acme/widgets',
  }
}

const binding = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }

describe('useForgePipelines', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMarkForgeRead.mockResolvedValue({ count: 0 })
  })

  it('defaults to the all filter', async () => {
    // The list is a history. Defaulting to "failure" hid every successful run,
    // so the tab looked empty whenever nothing was broken.
    mockFetchForgePipelines.mockResolvedValue({ pipelines: [run(1)], hasMore: false, nextPage: 2, binding })
    const p = useForgePipelines(() => '/proj')

    expect(p.filter.value).toBe('all')
    await p.load()

    // "all" means no status parameter at all.
    expect(mockFetchForgePipelines).toHaveBeenCalledWith(
      expect.objectContaining({ page: 1 }),
    )
    const arg = mockFetchForgePipelines.mock.calls[0][0] as Record<string, unknown>
    expect(arg.status).toBeUndefined()
    expect(p.pipelines.value).toHaveLength(1)
  })

  it('sends no status param for the "all" filter', async () => {
    mockFetchForgePipelines.mockResolvedValue({ pipelines: [], hasMore: false, nextPage: 2, binding })
    const p = useForgePipelines(() => '/proj')

    // "all" is the default now, so switch away and back to force a reload —
    // re-selecting the active filter is deliberately a no-op.
    p.setFilter('failure')
    await nextTick()
    mockFetchForgePipelines.mockClear()

    p.setFilter('all')
    await nextTick()
    await vi.waitFor(() => expect(mockFetchForgePipelines).toHaveBeenCalled())

    const params = mockFetchForgePipelines.mock.calls[0][0]
    expect(params.status).toBeUndefined()
  })

  it('does not reload when re-selecting the active filter', async () => {
    mockFetchForgePipelines.mockResolvedValue({ pipelines: [], hasMore: false, nextPage: 2, binding })
    const p = useForgePipelines(() => '/proj')
    await p.load()
    mockFetchForgePipelines.mockClear()

    p.setFilter('all')
    await nextTick()

    expect(mockFetchForgePipelines).not.toHaveBeenCalled()
  })

  it('clears the list without a request when no project is selected', async () => {
    const p = useForgePipelines(() => '')
    p.pipelines.value = [run(1)] as never

    await p.load()

    expect(mockFetchForgePipelines).not.toHaveBeenCalled()
    expect(p.pipelines.value).toEqual([])
  })

  it('surfaces a classified error with its code', async () => {
    mockFetchForgePipelines.mockRejectedValue(new ForgeApiError('no CI here', 'ForgeNoPipelines', 400))
    const p = useForgePipelines(() => '/proj')

    await p.load()

    expect(p.error.value).toEqual({ message: 'no CI here', code: 'ForgeNoPipelines' })
    expect(p.pipelines.value).toEqual([])
    expect(p.loading.value).toBe(false)
  })

  it('appends the next page on loadMore', async () => {
    mockFetchForgePipelines
      .mockResolvedValueOnce({ pipelines: [run(2)], hasMore: true, nextPage: 2, binding })
      .mockResolvedValueOnce({ pipelines: [run(1)], hasMore: false, nextPage: 3, binding })
    const p = useForgePipelines(() => '/proj')

    await p.load()
    await p.loadMore()

    expect(p.pipelines.value.map(r => r.id)).toEqual([2, 1])
    expect(p.hasMore.value).toBe(false)
  })

  it('does not load more when there is nothing more', async () => {
    mockFetchForgePipelines.mockResolvedValue({ pipelines: [run(1)], hasMore: false, nextPage: 2, binding })
    const p = useForgePipelines(() => '/proj')
    await p.load()

    await p.loadMore()

    expect(mockFetchForgePipelines).toHaveBeenCalledTimes(1)
  })

  it('ignores a stale response so a fast filter switch cannot be overwritten', async () => {
    // Two overlapping loads: the slower FIRST one must not clobber the second.
    let resolveFirst: (v: unknown) => void = () => {}
    mockFetchForgePipelines
      .mockImplementationOnce(() => new Promise(r => { resolveFirst = r }))
      .mockResolvedValueOnce({ pipelines: [run(9, 'success')], hasMore: false, nextPage: 2, binding })

    const p = useForgePipelines(() => '/proj')
    const first = p.load()
    const second = p.load()
    await second
    resolveFirst({ pipelines: [run(1)], hasMore: false, nextPage: 2, binding })
    await first

    expect(p.pipelines.value.map(r => r.id)).toEqual([9])
  })

  it('does not let an in-flight loadMore pollute a different filter', async () => {
    // The append is fetched for the OLD filter. If the user switches filters
    // while it is in flight, those rows must not be appended to the new list —
    // they belong to a result set the user has already left.
    let resolveAppend: (v: unknown) => void = () => {}
    mockFetchForgePipelines
      // load(): the failure filter, with more available.
      .mockResolvedValueOnce({ pipelines: [run(1, 'failure')], hasMore: true, nextPage: 2, binding })
      // loadMore(): held open, then resolved AFTER the filter switch.
      .mockImplementationOnce(() => new Promise(r => { resolveAppend = r }))
      // setFilter('running') -> load()
      .mockResolvedValueOnce({ pipelines: [run(7, 'running')], hasMore: false, nextPage: 2, binding })

    const p = useForgePipelines(() => '/proj')
    await p.load()

    const appending = p.loadMore()
    p.setFilter('running')
    await vi.waitFor(() => expect(p.pipelines.value.map(r => r.id)).toEqual([7]))

    // The stale append lands now; it must be dropped.
    resolveAppend({ pipelines: [run(2, 'failure')], hasMore: true, nextPage: 3, binding })
    await appending

    expect(p.pipelines.value.map(r => r.id)).toEqual([7])
    expect(p.hasMore.value).toBe(false)
    expect(p.loadingMore.value).toBe(false)
  })

  it('clears the append spinner when a reload supersedes it', async () => {
    // The superseded append bails on its seq check without clearing the flag,
    // so the reload has to clear it or loadMore would refuse to run forever.
    let resolveAppend: (v: unknown) => void = () => {}
    mockFetchForgePipelines
      .mockResolvedValueOnce({ pipelines: [run(1, 'failure')], hasMore: true, nextPage: 2, binding })
      .mockImplementationOnce(() => new Promise(r => { resolveAppend = r }))
      .mockResolvedValueOnce({ pipelines: [run(7, 'running')], hasMore: true, nextPage: 2, binding })

    const p = useForgePipelines(() => '/proj')
    await p.load()
    const appending = p.loadMore()

    p.setFilter('running')
    await vi.waitFor(() => expect(p.loading.value).toBe(false))

    expect(p.loadingMore.value).toBe(false, 'the spinner must not stick')

    resolveAppend({ pipelines: [run(2, 'failure')], hasMore: true, nextPage: 3, binding })
    await appending
    // The flag stays clear, so a later loadMore can still run.
    expect(p.loadingMore.value).toBe(false)
  })
})

describe('useForgePipelineDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMarkForgeRead.mockResolvedValue({ count: 0 })
  })

  it('loads the run and its jobs', async () => {
    mockFetchForgePipeline.mockResolvedValue({
      pipeline: run(42),
      jobs: [
        { id: 1, name: 'build', status: 'success', url: 'u', durationSeconds: 30 },
        { id: 2, name: 'test', status: 'failure', url: 'u', stage: 'test', failureReason: 'script_failure' },
      ],
      binding,
    })
    const d = useForgePipelineDetail()

    await d.open(42)

    expect(mockFetchForgePipeline).toHaveBeenCalledWith(42, expect.anything())
    expect(d.run.value?.id).toBe(42)
    expect(d.jobs.value).toHaveLength(2)
    expect(d.jobs.value[1].failureReason).toBe('script_failure')
  })

  it('treats missing jobs as an empty list rather than an error', async () => {
    // The backend returns jobs best-effort, so an absent key is normal.
    mockFetchForgePipeline.mockResolvedValue({ pipeline: run(42), binding })
    const d = useForgePipelineDetail()

    await d.open(42)

    expect(d.jobs.value).toEqual([])
    expect(d.error.value).toBeNull()
  })

  it('reports a load failure', async () => {
    mockFetchForgePipeline.mockRejectedValue(new ForgeApiError('gone', 'ForgeNotFound', 404))
    const d = useForgePipelineDetail()

    await d.open(999)

    expect(d.error.value).toEqual({ message: 'gone', code: 'ForgeNotFound' })
    expect(d.run.value).toBeNull()
    expect(d.loading.value).toBe(false)
  })

  it('clears state on close', async () => {
    mockFetchForgePipeline.mockResolvedValue({ pipeline: run(42), jobs: [{ id: 1, name: 'x', status: 'success', url: 'u' }], binding })
    const d = useForgePipelineDetail()
    await d.open(42)

    d.close()

    expect(d.run.value).toBeNull()
    expect(d.jobs.value).toEqual([])
  })
})

describe('useForgePipelines read state', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMarkForgeRead.mockResolvedValue({ count: 0 })
  })

  it('marks one run read by its pipeline key, leaving the others alone', async () => {
    mockFetchForgePipelines.mockResolvedValue({
      pipelines: [
        { ...run(100, 'failure'), unread: true },
        { ...run(101, 'failure'), unread: true },
      ],
      hasMore: false, nextPage: 2, binding,
    })

    const p = useForgePipelines(() => '/proj')
    await p.load()

    await p.markItemRead(p.pipelines.value[0])

    // The key must carry the run id: every run has number 0, so an item-number
    // key would clear all of them at once.
    expect(mockMarkForgeRead).toHaveBeenCalledWith('pipeline/run:100')
    expect(p.pipelines.value[0].unread).toBe(false)
    expect(p.pipelines.value[1].unread).toBe(true)
  })

  it('does not call the API for a run that is already read', async () => {
    mockFetchForgePipelines.mockResolvedValue({
      pipelines: [{ ...run(100, 'success'), unread: false }],
      hasMore: false, nextPage: 2, binding,
    })
    const p = useForgePipelines(() => '/proj')
    await p.load()

    await p.markItemRead(p.pipelines.value[0])

    expect(mockMarkForgeRead).not.toHaveBeenCalled()
  })

  it('restores the unread flag when the mark fails', async () => {
    mockFetchForgePipelines.mockResolvedValue({
      pipelines: [{ ...run(100, 'failure'), unread: true }],
      hasMore: false, nextPage: 2, binding,
    })
    mockMarkForgeRead.mockRejectedValueOnce(new Error('offline'))

    const p = useForgePipelines(() => '/proj')
    await p.load()
    await p.markItemRead(p.pipelines.value[0])

    // The UI must not claim the item was seen when the write failed.
    expect(p.pipelines.value[0].unread).toBe(true)
  })

  it('marks all read with no itemKey and clears every row', async () => {
    mockFetchForgePipelines.mockResolvedValue({
      pipelines: [
        { ...run(100, 'failure'), unread: true },
        { ...run(101, 'failure'), unread: true },
      ],
      hasMore: false, nextPage: 2, binding,
    })
    mockMarkForgeRead.mockResolvedValue({ count: 0 })

    const p = useForgePipelines(() => '/proj')
    await p.load()
    await p.markAllRead()

    // No itemKey: the whole repository.
    expect(mockMarkForgeRead).toHaveBeenCalledWith()
    expect(p.pipelines.value.every(r => !r.unread)).toBe(true)
  })

  it('restores the rows when the repo-wide clear fails', async () => {
    // Without the rollback the list claims every run was seen while the server
    // still holds them unread.
    mockFetchForgePipelines.mockResolvedValue({
      pipelines: [
        { ...run(100, 'failure'), unread: true },
        { ...run(101, 'failure'), unread: false },
      ],
      hasMore: false, nextPage: 2, binding,
    })
    mockMarkForgeRead.mockRejectedValueOnce(new Error('offline'))

    const p = useForgePipelines(() => '/proj')
    await p.load()
    await p.markAllRead()

    expect(p.pipelines.value[0].unread).toBe(true, 'a failed clear must roll back')
    expect(p.pipelines.value[1].unread).toBe(false)
  })
})
