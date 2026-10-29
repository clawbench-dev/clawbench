import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ShareAddressList from '../ShareAddressList.vue'
import type { ShareLink } from '@/composables/useShareLinks'

/**
 * The address list: ONE primary link by default, expandable to every address.
 *
 * The two behaviours that matter are "the default view is not a wall of
 * addresses" and "a virtual adapter is shown but labelled" — hiding it would
 * leave a user unable to explain why a colleague cannot open that address.
 */

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      shareAddress: {
        copy: '复制链接',
        showAll: '查看全部 {count} 个地址',
        hideAll: '收起地址列表',
        kindVirtual: '虚拟网卡',
        kindPublic: '公网',
        kindCurrent: '当前地址',
      },
    },
  },
})

function link(ip: string, kind: string, iface = ''): ShareLink {
  return {
    url: `http://${ip}:20000/share/tok`,
    address: { ip, interface: iface, kind },
  }
}

function mountList(links: ShareLink[]) {
  return mount(ShareAddressList, {
    props: { links },
    global: {
      plugins: [i18n],
      stubs: { CopyButton: { template: '<button class="copy-btn" />' } },
    },
  })
}

describe('ShareAddressList', () => {
  it('shows only the primary link by default', () => {
    const w = mountList([
      link('192.168.100.104', 'lan', 'wlp1s0'),
      link('192.168.100.142', 'lan', 'enx'),
      link('172.17.0.1', 'virtual', 'docker0'),
    ])

    const input = w.find('[data-share-primary]')
    expect((input.element as HTMLInputElement).value).toBe('http://192.168.100.104:20000/share/tok')
    // The extra addresses must be collapsed: a wall of IPs is noise in the
    // common case.
    expect(w.findAll('.share-addr-row')).toHaveLength(0)
  })

  it('does not render the toggle when there is only one address', () => {
    const w = mountList([link('192.168.1.5', 'lan', 'eth0')])

    // A toggle that expands to the same single row would be a dead control.
    expect(w.find('[data-share-toggle]').exists()).toBe(false)
  })

  it('expands to every address with labels', async () => {
    const w = mountList([
      link('192.168.100.104', 'lan', 'wlp1s0'),
      link('172.17.0.1', 'virtual', 'docker0'),
    ])

    await w.find('[data-share-toggle]').trigger('click')

    const rows = w.findAll('.share-addr-row')
    expect(rows).toHaveLength(2)
    // The virtual adapter is LISTED (so the user can see why it may not work)
    // and LABELED (so they know to prefer the other one).
    expect(rows[1].text()).toContain('172.17.0.1')
    expect(rows[1].text()).toContain('虚拟网卡')
  })

  it('labels the browser-origin entry as current', async () => {
    const w = mountList([
      link('localhost', 'current'),
      link('192.168.1.5', 'lan', 'eth0'),
    ])

    await w.find('[data-share-toggle]').trigger('click')

    expect(w.findAll('.share-addr-row')[0].text()).toContain('当前地址')
  })

  it('does not label a plain LAN address', async () => {
    const w = mountList([
      link('192.168.100.104', 'lan', 'wlp1s0'),
      link('172.17.0.1', 'virtual', 'docker0'),
    ])

    await w.find('[data-share-toggle]').trigger('click')

    // "局域网" on every row would be noise; only the entries that carry a
    // warning (virtual) or extra information (public) get a chip.
    expect(w.findAll('.share-addr-row')[0].find('.share-addr-kind').exists()).toBe(false)
  })

  it('toggles back to collapsed', async () => {
    const w = mountList([
      link('192.168.1.5', 'lan', 'eth0'),
      link('172.17.0.1', 'virtual', 'docker0'),
    ])
    const toggle = w.find('[data-share-toggle]')

    await toggle.trigger('click')
    expect(w.findAll('.share-addr-row')).toHaveLength(2)

    await toggle.trigger('click')
    expect(w.findAll('.share-addr-row')).toHaveLength(0)
  })

  it('renders the trailing slot beside the primary input', () => {
    const w = mount(ShareAddressList, {
      props: { links: [link('192.168.1.5', 'lan', 'eth0')] },
      slots: { trailing: '<button class="regen">R</button>' },
      global: {
        plugins: [i18n],
        stubs: { CopyButton: { template: '<button class="copy-btn" />' } },
      },
    })

    // The regenerate button must stay in the primary row, not move into the
    // expandable list.
    expect(w.find('.share-addr-primary .regen').exists()).toBe(true)
  })

  it('handles an empty list without crashing', () => {
    const w = mountList([])

    expect((w.find('[data-share-primary]').element as HTMLInputElement).value).toBe('')
    expect(w.find('[data-share-toggle]').exists()).toBe(false)
  })
})
