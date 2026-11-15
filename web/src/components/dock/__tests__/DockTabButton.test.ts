import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import DockTabButton from '@/components/dock/DockTabButton.vue'

/**
 * ONE button for every dock surface.
 *
 * These assertions pin the part that used to be copied three times in App.vue:
 * which state class the button wears, when a badge/dot is suppressed (the tab
 * you are reading shows no badge), and the pop animation restarting on a count
 * change. All of it is driven by the plugin's own declaration — the component
 * knows no plugin by name.
 */

const plugins = vi.hoisted(() => ({
  tasks: {
    id: 'tasks',
    titleKey: 'nav.tasks',
    icon: { name: 'TasksIcon', render: () => null },
    appearance: {
      unread: () => true,
      running: () => true,
      justCompleted: () => true,
    },
  },
  live: {
    id: 'live',
    titleKey: 'nav.meeting',
    icon: { name: 'MeetingIcon', render: () => null },
    appearance: { liveDot: () => true },
  },
  mail: {
    id: 'mail',
    titleKey: 'nav.mail',
    icon: { name: 'MailIcon', render: () => null },
  },
  unknown: undefined,
}))

// Real refs (not plain objects): the button's computed must TRACK the count,
// otherwise neither the badge nor its animation would ever update.
vi.mock('@/composables/dockPluginRuntime', async () => {
  const { ref } = await import('vue')
  const counts = { tasks: ref(0), live: ref(0), mail: ref(0) }
  return {
    __counts: counts,
    resolveDockPlugin: (id: string) => (plugins as Record<string, unknown>)[id],
    resolveDockBadge: (id: string) =>
      id in counts ? { count: (counts as Record<string, { value: number }>)[id].value } : null,
    resolveDockIcon: () => undefined,
    resolveDockTitle: () => undefined,
    dockTabIcon: (id: string) =>
      (plugins as Record<string, { icon?: unknown } | undefined>)[id]?.icon,
    dockTabTitleKey: (id: string) =>
      (plugins as Record<string, { titleKey?: string } | undefined>)[id]?.titleKey,
  }
})

import { __counts as counts } from '@/composables/dockPluginRuntime'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: { nav: { tasks: 'Tasks', meeting: 'Meeting', mail: 'Mail' } },
  },
})

function mountButton(props: Record<string, unknown>) {
  return mount(DockTabButton, {
    props: { tab: 'tasks', variant: 'wide', ...props },
    global: {
      plugins: [i18n],
      // The real directive teleports a chip into <body>; the button's own
      // contract is only that it passes the label through.
      directives: { 'dock-tip': { mounted: () => {}, updated: () => {} } },
    },
  })
}

beforeEach(() => {
  counts.tasks.value = 0
  counts.live.value = 0
  counts.mail.value = 0
})

describe('DockTabButton', () => {
  it('renders the wide variant as an icon-only button with an aria label', () => {
    const wrapper = mountButton({})
    const btn = wrapper.find('.dock-btn')
    expect(btn.attributes('data-tab')).toBe('tasks')
    expect(btn.attributes('aria-label')).toBe('Tasks')
    // The vertical dock uses the themed chip, not the OS tooltip.
    expect(btn.attributes('title')).toBeUndefined()
  })

  it('renders the inline variant with a native title instead', () => {
    const wrapper = mountButton({ variant: 'inline' })
    expect(wrapper.find('.dock-btn').attributes('title')).toBe('Tasks')
  })

  it('applies every declared state class, and drops them on the active tab', async () => {
    const wrapper = mountButton({})
    const classes = () => wrapper.find('.dock-btn').classes()
    expect(classes()).toContain('has-unread')
    expect(classes()).toContain('has-running')
    expect(classes()).toContain('just-completed')

    await wrapper.setProps({ active: true })
    expect(classes()).toContain('active')
    expect(classes()).not.toContain('has-unread')
    expect(classes()).not.toContain('has-running')
    expect(classes()).not.toContain('just-completed')
  })

  it('suppresses the badge independently of the active class (collapsed pane)', async () => {
    counts.tasks.value = 3
    const wrapper = mountButton({ active: false, suppressBadge: true })
    expect(wrapper.find('.dock-btn').classes()).not.toContain('active')
    expect(wrapper.find('.dock-badge-count').exists()).toBe(false)
  })

  it('shows the count badge only when there is a count', async () => {
    const wrapper = mountButton({})
    expect(wrapper.find('.dock-badge-count').exists()).toBe(false)

    counts.tasks.value = 7
    await nextTick()
    expect(wrapper.find('.dock-badge-count').text()).toBe('7')
  })

  it('draws the live dot from the plugin declaration, not from a count', () => {
    const wrapper = mountButton({ tab: 'live' })
    expect(wrapper.find('.dock-badge--live').exists()).toBe(true)
    expect(wrapper.find('.dock-badge-count').exists()).toBe(false)
  })

  it('hides the dot while the tab is the one being looked at', async () => {
    const wrapper = mountButton({ tab: 'live' })
    await wrapper.setProps({ active: true })
    expect(wrapper.find('.dock-badge--live').exists()).toBe(false)
  })

  it('restarts the pop animation when the count changes', async () => {
    counts.tasks.value = 1
    const wrapper = mountButton({})
    await nextTick()
    const badge = () => wrapper.find('.dock-badge-count')
    expect(badge().classes()).not.toContain('dock-badge-pop')

    counts.tasks.value = 2
    // false → true spans two renders (the re-arm waits for the next tick), so
    // a single await can race the second render.
    await vi.waitFor(() => expect(badge().classes()).toContain('dock-badge-pop'))

    // The class is removed when the CSS animation finishes, so the next change
    // can replay it.
    await badge().trigger('animationend')
    expect(badge().classes()).not.toContain('dock-badge-pop')
  })

  it('renders the popup variant as a labelled row and keeps the count while active', () => {
    counts.mail.value = 4
    const wrapper = mountButton({ tab: 'mail', variant: 'popup', active: true })
    const row = wrapper.find('.dock-overflow-item')
    expect(row.classes()).toContain('active')
    expect(row.text()).toContain('Mail')
    // A popup row always shows its count: the menu itself is the surface.
    expect(wrapper.find('.dock-overflow-count').text()).toBe('4')
  })

  it('emits click once, from the real button', async () => {
    const wrapper = mountButton({})
    await wrapper.find('.dock-btn').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
  })
})
