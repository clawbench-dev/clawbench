import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

const mockGetServerValue = vi.fn()
const mockSetServerValue = vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValue: mockGetServerValue,
    setServerValue: mockSetServerValue,
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import MCPSetting from '@/components/settings/MCPSetting.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          mcpDesc: '全局 MCP 服务',
          mcpEmpty: '还没有配置',
          mcpName: '名称',
          mcpCommand: '命令',
          mcpArgs: '参数',
          mcpEnv: '环境变量',
          mcpUrl: '地址',
          mcpHeaders: '请求头',
          mcpAdd: '添加服务',
          mcpRemove: '删除',
        },
      },
    },
  },
})

function mountSetting(servers: unknown[]) {
  mockGetServerValue.mockReturnValue(servers)
  return mount(MCPSetting, {
    global: {
      plugins: [i18n],
      stubs: { AlertCircle: true, Trash2: true },
    },
  })
}

describe('MCPSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSetServerValue.mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })
  })

  it('renders the empty hint when nothing is configured', () => {
    const wrapper = mountSetting([])
    expect(wrapper.text()).toContain('还没有配置')
    expect(wrapper.findAll('.mcp-row')).toHaveLength(0)
  })

  it('lists each configured server with its transport', () => {
    const wrapper = mountSetting([
      { name: 'websearch', transport: 'stdio', command: 'node', args: ['a.js'] },
      { name: 'remote', transport: 'http', url: 'https://x/mcp' },
    ])
    const rows = wrapper.findAll('.mcp-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('websearch')
    expect(rows[0].text()).toContain('stdio')
    expect(rows[0].text()).toContain('node a.js')
    expect(rows[1].text()).toContain('https://x/mcp')
  })

  it('adds a stdio server with parsed args and env', async () => {
    const wrapper = mountSetting([])
    await wrapper.find('input[placeholder="名称"]').setValue('srv')
    await wrapper.find('input[placeholder="命令"]').setValue('node')
    await wrapper.find('input[placeholder="参数"]').setValue('a.js  --flag')
    await wrapper.find('textarea').setValue('KEY=v1\nOTHER=v2\n')
    await wrapper.findAll('button').filter((b) => b.text().includes('添加服务'))[0].trigger('click')

    expect(mockSetServerValue).toHaveBeenCalledWith('mcp.servers', [
      {
        name: 'srv',
        transport: 'stdio',
        command: 'node',
        args: ['a.js', '--flag'],
        env: { KEY: 'v1', OTHER: 'v2' },
      },
    ])
  })

  it('adds an http server with parsed headers and no command fields', async () => {
    const wrapper = mountSetting([])
    await wrapper.find('input[placeholder="名称"]').setValue('remote')
    await wrapper.find('select').setValue('http')
    await wrapper.find('input[placeholder="地址"]').setValue('https://x/mcp')
    await wrapper.find('textarea').setValue('Authorization=Bearer t')
    await wrapper.findAll('button').filter((b) => b.text().includes('添加服务'))[0].trigger('click')

    expect(mockSetServerValue).toHaveBeenCalledWith('mcp.servers', [
      {
        name: 'remote',
        transport: 'http',
        url: 'https://x/mcp',
        headers: { Authorization: 'Bearer t' },
      },
    ])
  })

  it('omits an empty args list rather than sending []', async () => {
    const wrapper = mountSetting([])
    await wrapper.find('input[placeholder="名称"]').setValue('srv')
    await wrapper.find('input[placeholder="命令"]').setValue('node')
    await wrapper.findAll('button').filter((b) => b.text().includes('添加服务'))[0].trigger('click')

    const arg = mockSetServerValue.mock.calls[0][1] as unknown[]
    expect(Object.prototype.hasOwnProperty.call(arg[0], 'args')).toBe(false)
  })

  it('sends the whole list on removal (the endpoint replaces the array)', async () => {
    const wrapper = mountSetting([
      { name: 'a', transport: 'stdio', command: 'node' },
      { name: 'b', transport: 'stdio', command: 'node' },
    ])
    await wrapper.findAll('.mcp-row')[0].find('button').trigger('click')

    expect(mockSetServerValue).toHaveBeenCalledWith('mcp.servers', [
      { name: 'b', transport: 'stdio', command: 'node' },
    ])
  })

  it('keeps the draft when saving fails, and shows the server error', async () => {
    // The server validates hard (unknown transport, missing command, a command
    // that is not on PATH) and the message names the offending entry. Losing the
    // draft on failure would force the user to retype it.
    mockSetServerValue.mockRejectedValueOnce(new Error('mcp.servers[0].command "nope" not found in PATH'))
    const wrapper = mountSetting([])
    await wrapper.find('input[placeholder="名称"]').setValue('srv')
    await wrapper.find('input[placeholder="命令"]').setValue('nope')
    await wrapper.findAll('button').filter((b) => b.text().includes('添加服务'))[0].trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('not found in PATH')
    expect((wrapper.find('input[placeholder="名称"]').element as HTMLInputElement).value).toBe('srv')
  })

  it('does not submit without a name', async () => {
    const wrapper = mountSetting([])
    await wrapper.find('input[placeholder="命令"]').setValue('node')
    const addBtn = wrapper.findAll('button').filter((b) => b.text().includes('添加服务'))[0]
    expect(addBtn.attributes('disabled')).toBeDefined()
  })
})
