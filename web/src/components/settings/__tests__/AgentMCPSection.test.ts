import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

// The component reads the global server list and patches the agent through the
// composable, so the mock has to provide BOTH entry points. A partial mock makes
// setup throw and fails every test in the file (observed on SettingsAgentDetail).
const mockGetServerValue = vi.fn()
const mockPatchAgentField = vi.fn().mockResolvedValue(undefined)

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValue: mockGetServerValue,
    patchAgentField: mockPatchAgentField,
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import AgentMCPSection from '@/components/settings/AgentMCPSection.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          mcpEmpty: '还没有配置 MCP 服务。',
          mcpAgentAll: '全部启用',
          mcpAgentNone: '全部禁用',
          mcpAgentSelected: '只选这几个',
          mcpAgentUnsupported: '该智能体不支持 MCP',
          mcpAgentTransportUnsupported: '该智能体不支持 {transport} 型',
          mcpAgentSlowStart: '首次对话可能拿不到工具',
        },
      },
    },
  },
})

function mountSection(agent: Record<string, unknown> | null, servers: unknown[] = []) {
  mockGetServerValue.mockReturnValue(servers)
  return mount(AgentMCPSection, {
    props: { agent },
    global: {
      plugins: [i18n],
      stubs: { AlertCircle: true },
    },
  })
}

describe('AgentMCPSection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPatchAgentField.mockResolvedValue(undefined)
  })

  it('shows the unsupported notice for a backend that ignores MCP', () => {
    // The fact comes from the server (the backend's spec), so the component
    // reads the flag rather than matching on a backend id: a new backend with
    // the same quirk only has to declare it in its Go spec.
    const wrapper = mountSection(
      { id: 'a', backend: 'some-new-agent', mcpUnsupportedBackend: true, mcpServers: { mode: 'all' } },
      [{ name: 'x', transport: 'stdio' }],
    )
    expect(wrapper.text()).toContain('该智能体不支持 MCP')
    expect(wrapper.find('input[type="radio"]').exists()).toBe(false)
  })

  it('does not consult the backend id for the unsupported notice', () => {
    // A backend id that used to be in the hard-coded list must be treated as
    // supported when the server does not flag it (and vice versa below).
    const wrapper = mountSection({ id: 'a', backend: 'dsh', mcpServers: { mode: 'all' } }, [
      { name: 'x', transport: 'stdio' },
    ])
    expect(wrapper.text()).not.toContain('该智能体不支持 MCP')
    expect(wrapper.findAll('input[type="radio"]')).toHaveLength(3)
  })

  it('renders three mode options for a supported backend', () => {
    const wrapper = mountSection({ id: 'a', backend: 'claude', mcpServers: { mode: 'all' } }, [
      { name: 'x', transport: 'stdio' },
    ])
    expect(wrapper.findAll('input[type="radio"]')).toHaveLength(3)
  })

  it('treats an absent mode as "all", not "none"', () => {
    // The zero value means all (see model.MCPServerPolicy.Normalized). Reading
    // it as none would show the wrong mode and hide the servers.
    const wrapper = mountSection({ id: 'a', backend: 'claude' }, [{ name: 'x', transport: 'stdio' }])
    const checked = wrapper.findAll('input[type="radio"]').filter((r) => (r.element as HTMLInputElement).checked)
    expect(checked).toHaveLength(1)
    expect((checked[0].element as HTMLInputElement).value).toBe('all')
  })

  it('shows the checklist only in selected mode', () => {
    const all = mountSection({ id: 'a', backend: 'claude', mcpServers: { mode: 'all' } }, [
      { name: 'x', transport: 'stdio' },
    ])
    expect(all.findAll('input[type="checkbox"]')).toHaveLength(0)
    all.unmount()

    const selected = mountSection({ id: 'a', backend: 'claude', mcpServers: { mode: 'selected', names: ['x'] } }, [
      { name: 'x', transport: 'stdio' },
    ])
    expect(selected.findAll('input[type="checkbox"]')).toHaveLength(1)
  })

  it('disables and grays out an SSE server on a backend that rejects SSE', () => {
    // Such a backend fails session/new outright on an sse server, so a tick that
    // cannot work must not be tickable. Flag-driven, not backend-id-driven.
    const wrapper = mountSection(
      { id: 'a', backend: 'some-new-agent', mcpRejectsSse: true, mcpServers: { mode: 'selected', names: [] } },
      [
        { name: 'ok', transport: 'stdio' },
        { name: 'nope', transport: 'sse' },
      ],
    )
    const boxes = wrapper.findAll('input[type="checkbox"]')
    expect(boxes).toHaveLength(2)
    expect((boxes[0].element as HTMLInputElement).disabled).toBe(false)
    expect((boxes[1].element as HTMLInputElement).disabled).toBe(true)
    expect(wrapper.findAll('.agent-mcp__row--off')).toHaveLength(1)
    expect(wrapper.text()).toContain('该智能体不支持 sse 型')
  })

  it('does not disable SSE on a backend that supports it', () => {
    const wrapper = mountSection(
      { id: 'a', backend: 'claude', mcpServers: { mode: 'selected', names: [] } },
      [{ name: 's', transport: 'sse' }],
    )
    expect((wrapper.find('input[type="checkbox"]').element as HTMLInputElement).disabled).toBe(false)
  })

  it('patches the policy when a mode is chosen', async () => {
    const wrapper = mountSection({ id: 'agent-1', backend: 'claude', mcpServers: { mode: 'all' } }, [
      { name: 'x', transport: 'stdio' },
    ])
    const radios = wrapper.findAll('input[type="radio"]')
    await radios[1].trigger('change')
    expect(mockPatchAgentField).toHaveBeenCalledWith('agent-1', 'mcp_servers', { mode: 'none', names: [] })
  })

  it('keeps the existing selection when switching to selected mode', async () => {
    const wrapper = mountSection(
      { id: 'agent-1', backend: 'claude', mcpServers: { mode: 'all', names: ['x'] } },
      [{ name: 'x', transport: 'stdio' }],
    )
    await wrapper.findAll('input[type="radio"]')[2].trigger('change')
    expect(mockPatchAgentField).toHaveBeenCalledWith('agent-1', 'mcp_servers', {
      mode: 'selected',
      names: ['x'],
    })
  })

  it('adds and removes a name when a checkbox is toggled', async () => {
    const wrapper = mountSection(
      { id: 'agent-1', backend: 'claude', mcpServers: { mode: 'selected', names: ['a'] } },
      [
        { name: 'a', transport: 'stdio' },
        { name: 'b', transport: 'stdio' },
      ],
    )
    const boxes = wrapper.findAll('input[type="checkbox"]')
    await boxes[1].setValue(true)
    expect(mockPatchAgentField).toHaveBeenLastCalledWith('agent-1', 'mcp_servers', {
      mode: 'selected',
      names: ['a', 'b'],
    })

    await boxes[0].setValue(false)
    expect(mockPatchAgentField).toHaveBeenLastCalledWith('agent-1', 'mcp_servers', {
      mode: 'selected',
      names: [],
    })
  })

  it('shows the slow-start hint only when the backend attaches servers async', () => {
    const on = mountSection(
      { id: 'a', backend: 'slow-start-agent', mcpAttachesAsync: true, mcpServers: { mode: 'all' } },
      [{ name: 'x', transport: 'stdio' }],
    )
    expect(on.text()).toContain('首次对话可能拿不到工具')
    on.unmount()

    const off = mountSection(
      { id: 'a', backend: 'slow-start-agent', mcpAttachesAsync: true, mcpServers: { mode: 'none' } },
      [{ name: 'x', transport: 'stdio' }],
    )
    expect(off.text()).not.toContain('首次对话可能拿不到工具')
    off.unmount()

    const other = mountSection({ id: 'a', backend: 'claude', mcpServers: { mode: 'all' } }, [
      { name: 'x', transport: 'stdio' },
    ])
    expect(other.text()).not.toContain('首次对话可能拿不到工具')
  })

  it('shows the empty hint when no global server exists', () => {
    const wrapper = mountSection({ id: 'a', backend: 'claude', mcpServers: { mode: 'all' } }, [])
    expect(wrapper.text()).toContain('还没有配置 MCP 服务')
  })

  it('surfaces a save error instead of failing silently', async () => {
    mockPatchAgentField.mockRejectedValueOnce(new Error('mcp.servers[0].command not found in PATH'))
    const wrapper = mountSection({ id: 'agent-1', backend: 'claude', mcpServers: { mode: 'all' } }, [
      { name: 'x', transport: 'stdio' },
    ])
    await wrapper.findAll('input[type="radio"]')[1].trigger('change')
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('not found in PATH')
  })
})
