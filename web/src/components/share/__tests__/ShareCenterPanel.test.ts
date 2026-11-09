import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'

/**
 * Behaviour of the share centre: which rows appear, what the row says about its
 * state, and which request each action sends.
 *
 * The three lists are stubbed at the `fetch` boundary rather than by mocking the
 * composable — the aggregation (including the "a finished, unpublished meeting
 * is not a share" rule) is the thing under test, and mocking the composable
 * would mock that rule away.
 */

const h = vi.hoisted(() => ({
  toastShow: vi.fn(),
  copyText: vi.fn(),
  openExternalUrl: vi.fn(),
  confirm: vi.fn().mockResolvedValue(true),
  compose: vi.fn().mockResolvedValue(true),
  openSystemMail: vi.fn(),
  shareLink: vi.fn().mockResolvedValue('shared'),
  capability: vi.fn().mockReturnValue('sheet'),
  buildShareLinks: vi.fn(async (path: string) => [
    { url: `http://10.0.0.5:20000${path}`, address: { ip: '10.0.0.5', interface: 'eth0', kind: 'lan' } },
  ]),
}))

vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: h.toastShow }) }))
vi.mock('@/composables/useDialog', () => ({ useDialog: () => ({ confirm: h.confirm }) }))
vi.mock('@/utils/clipboard', () => ({ copyText: (t: string, ok?: () => void) => { h.copyText(t); ok?.() } }))
vi.mock('@/utils/externalLink', () => ({ openExternalUrl: (u: string) => h.openExternalUrl(u) }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))
vi.mock('@/composables/useShareLinks', () => ({ buildShareLinks: h.buildShareLinks }))
vi.mock('@/composables/useMailCompose', () => ({ useMailCompose: () => ({ compose: h.compose }) }))
vi.mock('@/utils/shareActions', () => ({
  shareCapability: h.capability,
  shareLink: h.shareLink,
  builtInMailAvailable: () => true,
  openSystemMail: h.openSystemMail,
}))
vi.mock('@/composables/useSettingsConfig', () => ({ serverConfig: { value: { mail: { enabled: true } } } }))

import ShareCenterPanel from '@/components/share/ShareCenterPanel.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  missingWarn: false,
  fallbackWarn: false,
  messages: {
    en: {
      common: { retry: 'Retry', loading: 'Loading' },
      shareCenter: {
        title: 'Sharing',
        refresh: 'Refresh',
        empty: 'Nothing shared yet',
        emptyFiltered: 'No shares match',
        filterAll: 'All',
        filterFile: 'Files',
        filterSession: 'Conversations',
        filterMeeting: 'Meetings',
        kindFile: 'File',
        kindDir: 'Folder',
        kindSession: 'Conversation',
        kindMeeting: 'Meeting',
        count: '{count}',
        loadFailed: 'Could not load',
        partialFailed: 'Partial: {kinds}',
        fileMissing: 'File deleted',
        sessionArchived: 'Archived',
        meetingRunning: 'In progress',
        meetingPrivate: 'Private',
        meetingPrivateHint: 'Private hint',
        meetingSharedHint: 'Shared hint',
        open: 'Open',
        copy: 'Copy link',
        copied: 'Link copied',
        shareTo: 'Share to another app',
        copyInstead: 'Copy instead',
        mail: 'Send by email',
        mailSubject: '{name}',
        mailBody: 'Link: {url}',
        revoke: 'Stop sharing',
        revoked: 'Stopped',
        confirmRevoke: 'Stop "{name}"?',
        confirmRevokeMeeting: 'Unpublish "{name}"?',
        revokedMeeting: 'Unpublished',
        clearAll: 'Clear',
        clearAllTip: 'Clear all',
        confirmClearAll: 'Clear all {count}?',
        cleared: 'Cleared',
        sharedAt: 'Shared {time}',
        meetingSwitchOn: 'Public',
        meetingSwitchOff: 'Private',
      },
    },
  },
})

/** Route fetch by URL, with per-test overrides. */
type Routes = Record<string, () => { status?: number; body: unknown }>

function stubFetch(routes: Routes) {
  const calls: Array<{ url: string; init?: RequestInit }> = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    const key = Object.keys(routes).find((k) => url.startsWith(k))
    if (!key) return { ok: false, status: 404, statusText: 'not found', json: async () => ({}) }
    const r = routes[key]()
    const status = r.status ?? 200
    return { ok: status < 400, status, statusText: '', json: async () => r.body }
  }))
  return calls
}

const FILES = {
  shares: [
    { token: 'f1', name: 'report.md', path: 'docs/report.md', createdAt: '2026-11-01T00:00:00Z', exists: true, isDir: false },
    { token: 'f2', name: 'old.txt', path: 'old.txt', createdAt: '2026-10-01T00:00:00Z', exists: false, isDir: false },
  ],
}
const SESSIONS = {
  shares: [
    { token: 's1', sessionId: 'sess-1', title: '发版讨论', messageCount: 12, createdAt: '2026-11-02T00:00:00Z', archived: false },
    { token: 's2', sessionId: 'sess-2', title: '旧对话', messageCount: 3, createdAt: '2026-09-01T00:00:00Z', archived: true },
  ],
}
const MEETINGS = {
  sessions: [
    // Published finished meeting: a share.
    { token: 'm1', title: '周会', startedAt: Date.parse('2026-11-03T02:00:00Z'), endedAt: Date.parse('2026-11-03T03:00:00Z'), shareEnabled: true },
    // Finished but NOT published: its link 404s, so it must not be listed.
    { token: 'm2', title: '未公开', startedAt: Date.parse('2026-11-04T02:00:00Z'), endedAt: Date.parse('2026-11-04T03:00:00Z'), shareEnabled: false },
    // Still running: the link works right now (people join through it).
    { token: 'm3', title: '进行中', startedAt: Date.parse('2026-11-05T02:00:00Z'), endedAt: 0, shareEnabled: false },
  ],
}

function mountPanel() {
  return mount(ShareCenterPanel, { global: { plugins: [i18n] } })
}

/**
 * Open one specific row.
 *
 * Selected by NAME, never by position: rows are ordered newest-first, so the
 * index of any given share depends on the fixture timestamps — an index-based
 * pick silently tests a different row when a timestamp changes.
 */
async function openRow(name: string) {
  const wrapper = mountPanel()
  await flushPromises()
  const row = wrapper.findAll('.share-row').find((r) => r.find('.share-row-name').text() === name)
  expect(row, `row "${name}" must be listed`).toBeTruthy()
  await row!.find('.share-row-main').trigger('click')
  await flushPromises()
  return wrapper
}


beforeEach(() => {
  vi.clearAllMocks()
  h.confirm.mockResolvedValue(true)
  h.compose.mockResolvedValue(true)
  h.shareLink.mockResolvedValue('shared')
  h.capability.mockReturnValue('sheet')
})

describe('ShareCenterPanel — aggregation', () => {
  it('lists files, conversations and published/running meetings, newest first', async () => {
    stubFetch({
      '/api/share/list': () => ({ body: FILES }),
      '/api/share/session/list': () => ({ body: SESSIONS }),
      '/api/live-share/notes/sessions': () => ({ body: MEETINGS }),
    })

    const wrapper = mountPanel()
    await flushPromises()

    const rows = wrapper.findAll('.share-row')
    // 2 files + 2 sessions + 2 meetings (m2 is skipped).
    expect(rows).toHaveLength(6)

    const names = wrapper.findAll('.share-row-name').map((n) => n.text())
    expect(names).toContain('report.md')
    expect(names).toContain('发版讨论')
    expect(names).toContain('周会')
    expect(names).toContain('进行中')
    // A finished, unpublished meeting is not a share: its token resolves to 404.
    expect(names).not.toContain('未公开')
  })

  it('marks a deleted file and an archived conversation without hiding them', async () => {
    stubFetch({
      '/api/share/list': () => ({ body: FILES }),
      '/api/share/session/list': () => ({ body: SESSIONS }),
      '/api/live-share/notes/sessions': () => ({ body: { sessions: [] } }),
    })

    const wrapper = mountPanel()
    await flushPromises()

    const badges = wrapper.findAll('.share-badge').map((b) => b.text())
    expect(badges).toContain('File deleted')
    expect(badges).toContain('Archived')
    // Still rows, because the links still work and must stay revocable.
    expect(wrapper.findAll('.share-row')).toHaveLength(4)
  })

  it('labels a running meeting and a private one differently', async () => {
    stubFetch({
      '/api/share/list': () => ({ body: { shares: [] } }),
      '/api/share/session/list': () => ({ body: { shares: [] } }),
      '/api/live-share/notes/sessions': () => ({ body: { sessions: [MEETINGS.sessions[2]] } }),
    })

    const wrapper = mountPanel()
    await flushPromises()

    const badges = wrapper.findAll('.share-badge').map((b) => b.text())
    expect(badges).toContain('In progress')
  })

  it('keeps the rows that did load when one list fails', async () => {
    stubFetch({
      '/api/share/list': () => ({ body: FILES }),
      '/api/share/session/list': () => ({ status: 500, body: {} }),
      '/api/live-share/notes/sessions': () => ({ body: { sessions: [] } }),
    })

    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.share-row')).toHaveLength(2)
    // No error banner: the list works, so a banner would read as "all broken".
    expect(wrapper.find('.share-hint--error').exists()).toBe(false)
  })

  it('reports a total failure instead of an empty state', async () => {
    stubFetch({})

    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.share-hint--error').exists()).toBe(true)
    expect(wrapper.text()).toContain('Could not load')
  })
})

describe('ShareCenterPanel — filters', () => {
  it('narrows to one kind and counts each bucket', async () => {
    stubFetch({
      '/api/share/list': () => ({ body: FILES }),
      '/api/share/session/list': () => ({ body: SESSIONS }),
      '/api/live-share/notes/sessions': () => ({ body: MEETINGS }),
    })

    const wrapper = mountPanel()
    await flushPromises()

    const counts = wrapper.findAll('.share-filter-count').map((c) => c.text())
    // all=6, files=2, sessions=2, meetings=2
    expect(counts).toEqual(['6', '2', '2', '2'])

    await wrapper.find('[data-filter="session"]').trigger('click')
    await nextTick()
    expect(wrapper.findAll('.share-row')).toHaveLength(2)
    expect(wrapper.findAll('.share-row-name').map((n) => n.text())).toEqual(['发版讨论', '旧对话'])
  })
})

describe('ShareCenterPanel — actions', () => {

  function stubAll() {
    return stubFetch({
      '/api/share/list': () => ({ body: FILES }),
      '/api/share/session/list': () => ({ body: SESSIONS }),
      '/api/live-share/notes/sessions': () => ({ body: { sessions: [] } }),
    })
  }

  it('copies the primary address, not the raw path', async () => {
    stubAll()
    const wrapper = await openRow('report.md')

    await wrapper.find('[data-action="share-to"]').exists()
    const copyBtn = wrapper.findAll('.share-actions .fbtn').find((b) => b.text().includes('Copy link'))
    await copyBtn!.trigger('click')

    expect(h.copyText).toHaveBeenCalledWith('http://10.0.0.5:20000/share/f1')
  })

  it('hands the link to the share sheet when the host has one', async () => {
    stubAll()
    const wrapper = await openRow('report.md')

    await wrapper.find('[data-action="share-to"]').trigger('click')
    await flushPromises()

    expect(h.shareLink).toHaveBeenCalledWith('http://10.0.0.5:20000/share/f1', 'report.md')
    expect(h.toastShow).toHaveBeenCalledWith('Share to another app', expect.anything())
  })

  it('says "copied" instead of "shared" when the host has no share sheet', async () => {
    h.capability.mockReturnValue('copy')
    h.shareLink.mockResolvedValue('copied')
    stubAll()
    const wrapper = await openRow('report.md')

    await wrapper.find('[data-action="share-to"]').trigger('click')
    await flushPromises()

    expect(h.toastShow).toHaveBeenCalledWith('Link copied', expect.anything())
  })

  it('opens the composer with the link prefilled', async () => {
    stubAll()
    const wrapper = await openRow('report.md')

    await wrapper.find('[data-action="mail"]').trigger('click')
    await flushPromises()

    expect(h.compose).toHaveBeenCalledWith(
      'report.md',
      'Link: http://10.0.0.5:20000/share/f1',
    )
    expect(h.openSystemMail).not.toHaveBeenCalled()
  })

  it('falls back to the system mail app when no composer could be opened', async () => {
    h.compose.mockResolvedValue(false)
    stubAll()
    const wrapper = await openRow('report.md')

    await wrapper.find('[data-action="mail"]').trigger('click')
    await flushPromises()

    expect(h.openSystemMail).toHaveBeenCalledWith(
      'report.md',
      'Link: http://10.0.0.5:20000/share/f1',
    )
  })

  it('revokes a file share by token', async () => {
    const calls = stubAll()
    const wrapper = await openRow('report.md')

    await wrapper.find('[data-action="revoke"]').trigger('click')
    await flushPromises()

    const del = calls.find((c) => c.init?.method === 'DELETE' && c.url.startsWith('/api/share/list'))
    expect(del, 'the file share list endpoint must be called with DELETE').toBeTruthy()
    expect(JSON.parse(String(del!.init!.body))).toEqual({ token: 'f1' })
    expect(wrapper.findAll('.share-row')).toHaveLength(3)
  })
})

describe('ShareCenterPanel — meeting rows', () => {
  function stubMeetings() {
    return stubFetch({
      '/api/share/list': () => ({ body: { shares: [] } }),
      '/api/share/session/list': () => ({ body: { shares: [] } }),
      '/api/live-share/notes/sessions': () => ({ body: { sessions: MEETINGS.sessions.slice(0, 1) } }),
    })
  }

  it('unpublishes instead of deleting: the record is the link', async () => {
    const calls = stubMeetings()
    const wrapper = await openRow('周会')

    await wrapper.find('[data-action="revoke"]').trigger('click')
    await flushPromises()

    const patch = calls.find((c) => c.init?.method === 'PATCH')
    expect(patch, 'a meeting is unpublished with PATCH, never deleted').toBeTruthy()
    expect(patch!.url).toContain('/api/live-share/notes/sessions/share')
    expect(JSON.parse(String(patch!.init!.body))).toEqual({ enabled: false })
    // No DELETE at all: deleting the row would take the meeting record with it.
    expect(calls.some((c) => c.init?.method === 'DELETE')).toBe(false)
  })

  it('drops the row once a finished meeting is unpublished', async () => {
    stubMeetings()
    const wrapper = await openRow('周会')

    await wrapper.find('[data-action="revoke"]').trigger('click')
    await flushPromises()

    // The link no longer resolves, so it stops being a share.
    expect(wrapper.findAll('.share-row')).toHaveLength(0)
  })
})

describe('ShareCenterPanel — clear all', () => {
  it('clears files and conversations, and unpublishes meetings one by one', async () => {
    const calls = stubFetch({
      '/api/share/list': () => ({ body: FILES }),
      '/api/share/session/list': () => ({ body: SESSIONS }),
      '/api/live-share/notes/sessions': () => ({ body: MEETINGS }),
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="clear-all"]').trigger('click')
    await flushPromises()

    const deletes = calls.filter((c) => c.init?.method === 'DELETE')
    expect(deletes).toHaveLength(2)
    for (const d of deletes) {
      expect(JSON.parse(String(d.init!.body))).toEqual({ all: true })
    }

    // m1 is published, m3 is running (nothing to unpublish), so exactly one PATCH.
    const patches = calls.filter((c) => c.init?.method === 'PATCH')
    expect(patches).toHaveLength(1)
    expect(patches[0].url).toContain('token=m1')

    // The running meeting keeps its row: its link is how people are joining.
    expect(wrapper.findAll('.share-row')).toHaveLength(1)
    expect(wrapper.find('.share-row-name').text()).toBe('进行中')
  })
})
