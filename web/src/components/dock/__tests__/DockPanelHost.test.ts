import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  panel: {
    props: ['foo', 'bar', 'active'],
    emits: ['ping'],
    template: '<button class="test-panel" @click="$emit(\'ping\')">{{ foo }}|{{ active }}|{{ bar }}</button>',
  },
  onPing: vi.fn(),
}))

vi.mock('@/composables/dockPluginRuntime', () => ({
  resolveDockPlugin: () => ({
    panel: {
      component: mocks.panel,
      props: (host: { projectPath: string }, extras: { task: { openTaskFile: unknown } }) => ({
        foo: host.projectPath,
        bar: !!extras.task.openTaskFile,
      }),
      on: () => ({ ping: mocks.onPing }),
    },
  }),
}))

import DockPanelHost from '@/components/dock/DockPanelHost.vue'

// The GENERIC host half: shared by every panel, never widened by one.
const host = {
  projectPath: '/proj',
  isWideScreen: true,
  openFile: vi.fn(),
  openSettingsCategory: vi.fn(),
  openTab: vi.fn(),
}
// The per-plugin half: a panel takes only its own group.
const extras = {
  task: { openTaskFile: vi.fn() },
}

describe('DockPanelHost', () => {
  beforeEach(() => mocks.onPing.mockClear())

  it('renders the panel with active + host-derived props', () => {
    const wrapper = mount(DockPanelHost, {
      props: { tabId: 'forge', active: true, host, extras },
    })
    expect(wrapper.find('.test-panel').text()).toBe('/proj|true|true')
  })

  it('binds host-derived event listeners', async () => {
    const wrapper = mount(DockPanelHost, {
      props: { tabId: 'forge', active: true, host, extras },
    })
    await wrapper.find('.test-panel').trigger('click')
    expect(mocks.onPing).toHaveBeenCalledTimes(1)
  })
})
