import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  panel: {
    props: ['foo', 'active'],
    emits: ['ping'],
    template: '<button class="test-panel" @click="$emit(\'ping\')">{{ foo }}|{{ active }}</button>',
  },
  onPing: vi.fn(),
}))

vi.mock('@/composables/dockPluginRuntime', () => ({
  getDockPluginRuntime: () => ({
    panel: {
      component: mocks.panel,
      props: (host: { projectPath: string }) => ({ foo: host.projectPath }),
      on: () => ({ ping: mocks.onPing }),
    },
  }),
}))

import DockPanelHost from '@/components/dock/DockPanelHost.vue'

const host = {
  projectPath: '/proj',
  terminalRequestedCwd: null,
  platformUnsupported: false,
  openFile: vi.fn(),
  openTaskFile: vi.fn(),
  quoteMeeting: vi.fn(),
  openMailSettings: vi.fn(),
  requestProject: vi.fn(),
  openMeetingRelaySettings: vi.fn(),
  clearTerminalCwd: vi.fn(),
}

describe('DockPanelHost', () => {
  beforeEach(() => mocks.onPing.mockClear())

  it('renders the panel with active + host-derived props', () => {
    const wrapper = mount(DockPanelHost, {
      props: { tabId: 'forge', active: true, host },
    })
    expect(wrapper.find('.test-panel').text()).toBe('/proj|true')
  })

  it('binds host-derived event listeners', async () => {
    const wrapper = mount(DockPanelHost, {
      props: { tabId: 'forge', active: true, host },
    })
    await wrapper.find('.test-panel').trigger('click')
    expect(mocks.onPing).toHaveBeenCalledTimes(1)
  })
})
