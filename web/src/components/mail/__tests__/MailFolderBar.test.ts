import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import en from '@/i18n/locales/en'
import type { MailAccount, MailFolder } from '@/utils/mailTypes'
import MailFolderBar from '../MailFolderBar.vue'

/**
 * Two pickers with two different weights: the account is an avatar (switched
 * rarely), the folder carries the name and its unread badge (switched all the
 * time). What must hold: the unread count is visible without opening the menu,
 * and picking either still reaches the parent.
 */

const accounts = [
  { id: 1, name: '工作', email: 'me@example.com' },
  { id: 2, name: '个人', email: 'other@example.com' },
] as MailAccount[]

const folders: MailFolder[] = [
  { id: 1, accountId: 1, path: 'INBOX', name: 'INBOX', delimiter: '/', specialUse: 'inbox', unread: 3, total: 10 },
  { id: 2, accountId: 1, path: 'Sent', name: 'Sent', delimiter: '/', specialUse: 'sent', unread: 0, total: 5 },
]

function mountBar(overrides: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
  return mount(MailFolderBar, {
    props: { accounts, folders, accountId: 1, folderId: 1, query: '', ...overrides },
    // Attached to the document so the search field can really take focus.
    attachTo: document.body,
    // PopupMenu teleports to <body>; render it in place so the options are
    // queryable from the wrapper (same trick as MenuSelect's own test).
    global: { plugins: [i18n], stubs: { Teleport: { template: '<div><slot /></div>' } } },
  })
}

describe('MailFolderBar', () => {
  it('shows the current folder with its unread count as a badge', () => {
    const wrapper = mountBar()
    const trigger = wrapper.find('.mail-bar__folder .menu-select')
    expect(trigger.text()).toContain('Inbox')
    expect(trigger.find('.mail-loc__count').text()).toBe('3')
    wrapper.unmount()

    // Nothing unread: no badge at all, rather than an empty pill that still
    // takes up room next to the folder name.
    const quiet = mountBar({ folderId: 2 })
    expect(quiet.find('.mail-bar__folder .mail-loc__count').exists()).toBe(false)
    quiet.unmount()
  })

  it('collapses the account to an avatar and keeps the address in the menu', async () => {
    const wrapper = mountBar()
    const trigger = wrapper.find('.mail-bar__account .menu-select')
    // First character of the account name, not the 20-character address.
    expect(trigger.find('.mail-acct__avatar').text()).toBe('工')
    expect(trigger.text()).not.toContain('me@example.com')

    await trigger.trigger('click')
    const rows = wrapper.findAll('.mail-bar__account .mail-menu__row')
    expect(rows.map((el) => el.find('.mail-menu__name').text())).toEqual([
      'me@example.com',
      'other@example.com',
    ])
    // The user-given account name is the secondary line, the address the title.
    expect(rows[0].find('.mail-menu__meta').text()).toBe('工作')
    expect(rows[0].classes()).toContain('is-current')

    await rows[1].trigger('click')
    expect(wrapper.emitted('update:accountId')).toEqual([[2]])
    wrapper.unmount()
  })

  it('lists every folder with its count and switches on pick', async () => {
    const wrapper = mountBar()
    await wrapper.find('.mail-bar__folder .menu-select').trigger('click')

    const rows = wrapper.findAll('.mail-bar__folder .mail-menu__row')
    expect(rows.map((el) => el.find('.mail-menu__name').text())).toEqual(['Inbox', 'Sent'])
    // The count is its own element at the right edge, not part of the label.
    expect(rows[0].find('.mail-menu__count').text()).toBe('3')
    expect(rows[1].find('.mail-menu__count').exists()).toBe(false)

    await rows[1].trigger('click')
    expect(wrapper.emitted('select-folder')).toEqual([[2]])
    wrapper.unmount()
  })

  it('marks all read only when the current folder has unread mail', async () => {
    const wrapper = mountBar()
    const button = wrapper.find('[data-action="mark-all-read"]')
    expect(button.attributes('disabled')).toBeUndefined()

    await button.trigger('click')
    expect(wrapper.emitted('mark-all-read')).toHaveLength(1)
    wrapper.unmount()

    // The Sent folder has no unread rows, so there is nothing to mark.
    const readOnly = mountBar({ folderId: 2 })
    expect(readOnly.find('[data-action="mark-all-read"]').attributes('disabled')).toBeDefined()
    readOnly.unmount()

    // In flight: a second click must not start another request.
    const busy = mountBar({ marking: true })
    expect(busy.find('[data-action="mark-all-read"]').attributes('disabled')).toBeDefined()
    busy.unmount()
  })

  // The refresh icon must go through the shared RefreshButton: its spin is
  // driven by `loading` (WAAPI in the browser, the utility class in jsdom) and
  // it blocks a second click while a refresh is already running. A hand-rolled
  // <button><RefreshCw/></button> silently loses both.
  it('uses the unified RefreshButton and spins while refreshing', async () => {
    const wrapper = mountBar()
    const btn = wrapper.find('.mail-icon-btn.refresh-spin')
    expect(btn.classes()).toContain('refresh-spin')
    expect(btn.classes()).not.toContain('refresh-spin--active')

    await btn.trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)
    wrapper.unmount()

    const busy = mountBar({ refreshing: true })
    const busyBtn = busy.find('.mail-icon-btn.refresh-spin')
    expect(busyBtn.classes()).toContain('refresh-spin--active')
    expect(busyBtn.attributes('disabled')).toBeDefined()

    // Disabled means no click reaches the parent while the request is in flight.
    await busyBtn.trigger('click')
    expect(busy.emitted('refresh')).toBeUndefined()
    busy.unmount()
  })
})

/**
 * Search is not a resident row: it is a button that swaps the whole toolbar for
 * the field. What must hold: the toolbar is REPLACED (so the list below does not
 * move), the field takes focus, and closing clears the query — a hidden filter
 * would leave the list filtered with nothing on screen saying why.
 */
describe('MailFolderBar — search', () => {
  it('replaces the toolbar with the field and gives it focus', async () => {
    const wrapper = mountBar()
    expect(wrapper.find('.mail-bar__search-row').exists()).toBe(false)

    await wrapper.find('[data-action="search"]').trigger('click')
    await flushPromises()

    // The pickers and the action buttons are gone, not merely covered.
    expect(wrapper.find('.mail-bar__top').exists()).toBe(false)
    expect(wrapper.find('.mail-bar__search-row').exists()).toBe(true)
    const input = wrapper.find('.mail-bar__search-row input')
    expect(document.activeElement).toBe(input.element)
    wrapper.unmount()
  })

  it('debounces typing and clears the query when the field is closed', async () => {
    vi.useFakeTimers()
    const wrapper = mountBar()
    await wrapper.find('[data-action="search"]').trigger('click')
    const input = wrapper.find('.mail-bar__search-row input')

    await input.setValue('inv')
    expect(wrapper.emitted('update:query')).toBeUndefined()
    vi.advanceTimersByTime(250)
    expect(wrapper.emitted('update:query')).toEqual([['inv']])

    await wrapper.find('.mail-bar__search-cancel').trigger('click')
    // Closing restores the toolbar and drops the filter.
    expect(wrapper.find('.mail-bar__top').exists()).toBe(true)
    expect(wrapper.emitted('update:query')!.at(-1)).toEqual([''])
    vi.useRealTimers()
    wrapper.unmount()
  })

  it('opens already expanded when the list is filtered', async () => {
    // A query without a visible field would be an unexplained filtered list.
    const wrapper = mountBar({ query: 'invoice' })
    expect(wrapper.find('.mail-bar__search-row').exists()).toBe(true)
    expect(wrapper.find('.mail-bar__top').exists()).toBe(false)
    expect((wrapper.find('.mail-bar__search-row input').element as HTMLInputElement).value).toBe('invoice')
    wrapper.unmount()
  })
})

describe('MailFolderBar — action labels', () => {
  it('shows its action labels when the row can hold them', async () => {
    // jsdom has no layout: the row measures 0 wide and assumes room. The
    // overflow maths lives in ActionBar.test.ts.
    const wrapper = mountBar()
    await nextTick()
    // The row is not the ActionBar any more: the account/folder pickers sit
    // beside it, and only the icon buttons live inside.
    const row = wrapper.find('.mail-bar__actions')
    expect(row.classes()).toContain('ab--labels')
    expect(row.findAll('.action-label').length).toBe(5)
    expect(wrapper.find('.mail-bar__top .menu-select').exists()).toBe(true)
    wrapper.unmount()
  })
})
