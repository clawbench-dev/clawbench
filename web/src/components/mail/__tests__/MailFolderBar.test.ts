import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import en from '@/i18n/locales/en'
import type { MailAccount, MailFolder } from '@/utils/mailTypes'
import MailFolderBar from '../MailFolderBar.vue'

/**
 * The folder picker replaced the chip strip: one dropdown next to the account
 * picker. Two things matter — the unread counts stay visible (they used to be
 * badges on the chips) and picking a folder still switches the list.
 */

const accounts = [
  { id: 1, name: '工作', email: 'me@example.com' },
] as MailAccount[]

const folders: MailFolder[] = [
  { id: 1, accountId: 1, path: 'INBOX', name: 'INBOX', delimiter: '/', specialUse: 'inbox', unread: 3, total: 10 },
  { id: 2, accountId: 1, path: 'Sent', name: 'Sent', delimiter: '/', specialUse: 'sent', unread: 0, total: 5 },
]

function mountBar(overrides: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
  return mount(MailFolderBar, {
    props: { accounts, folders, accountId: 1, folderId: 1, query: '', ...overrides },
    // PopupMenu teleports to <body>; render it in place so the options are
    // queryable from the wrapper (same trick as MenuSelect's own test).
    global: { plugins: [i18n], stubs: { Teleport: { template: '<div><slot /></div>' } } },
  })
}

describe('MailFolderBar', () => {
  it('shows the current folder with its unread count in the picker', () => {
    const wrapper = mountBar()
    const trigger = wrapper.find('.mail-bar__folder .menu-select')
    expect(trigger.text()).toContain('Inbox')
    expect(trigger.text()).toContain('(3)')
    wrapper.unmount()
  })

  it('lists every folder and switches on pick', async () => {
    const wrapper = mountBar()
    await wrapper.find('.mail-bar__folder .menu-select').trigger('click')

    const items = wrapper.findAll('.menu-select-item')
    const labels = items.map((el) => el.text())
    expect(labels).toContain('Inbox (3)')
    expect(labels).toContain('Sent')

    const sent = items.find((el) => el.text() === 'Sent')!
    await sent.trigger('click')

    expect(wrapper.emitted('select-folder')).toEqual([[2]])
    wrapper.unmount()
  })

  it('marks all read only when the current folder has unread mail', async () => {
    const wrapper = mountBar()
    const button = wrapper.find('.mail-read-all')
    expect(button.attributes('disabled')).toBeUndefined()

    await button.trigger('click')
    expect(wrapper.emitted('mark-all-read')).toHaveLength(1)
    wrapper.unmount()

    // The Sent folder has no unread rows, so there is nothing to mark.
    const readOnly = mountBar({ folderId: 2 })
    expect(readOnly.find('.mail-read-all').attributes('disabled')).toBeDefined()
    readOnly.unmount()

    // In flight: a second click must not start another request.
    const busy = mountBar({ marking: true })
    expect(busy.find('.mail-read-all').attributes('disabled')).toBeDefined()
    busy.unmount()
  })

  // The refresh icon must go through the shared RefreshButton: its spin is
  // driven by `loading` (WAAPI in the browser, the utility class in jsdom) and
  // it blocks a second click while a refresh is already running. A hand-rolled
  // <button><RefreshCw/></button> silently loses both.
  it('uses the unified RefreshButton and spins while refreshing', async () => {
    const wrapper = mountBar()
    const btn = wrapper.findAll('.mail-icon-btn')[0]
    expect(btn.classes()).toContain('refresh-spin')
    expect(btn.classes()).not.toContain('refresh-spin--active')

    await btn.trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)
    wrapper.unmount()

    const busy = mountBar({ refreshing: true })
    const busyBtn = busy.findAll('.mail-icon-btn')[0]
    expect(busyBtn.classes()).toContain('refresh-spin--active')
    expect(busyBtn.attributes('disabled')).toBeDefined()

    // Disabled means no click reaches the parent while the request is in flight.
    await busyBtn.trigger('click')
    expect(busy.emitted('refresh')).toBeUndefined()
    busy.unmount()
  })
})

describe('MailFolderBar — action labels', () => {
  it('shows its action labels when the row can hold them', async () => {
    // jsdom has no layout: the row measures 0 wide and assumes room. The
    // overflow maths lives in ActionBar.test.ts.
    const wrapper = mountBar()
    await nextTick()
    const row = wrapper.find('.mail-bar__top')
    expect(row.classes()).toContain('ab--labels')
    expect(row.findAll('.action-label').length).toBe(3)
    wrapper.unmount()
  })
})
