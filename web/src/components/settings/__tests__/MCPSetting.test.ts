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

// The add form mixes a ROW (.mcp-add-grid) and a COLUMN (.mcp-add) flex
// container, and `flex` means different things on each axis. A bare
// `flex: 1` on the shared `.mcp-input` class therefore collapses the height:
// in the column container it becomes `flex-basis: 0%` on the VERTICAL axis,
// which overrides `height: 30px`. Measured in a real browser before the fix:
// computed height 30px, rendered box 17px — the text fields looked shorter
// than the select next to them.
//
// jsdom performs no layout, so no rendered-geometry assertion can catch this
// (every rect is 0). This guard pins the CSS structure instead: the shared
// class must not set `flex`, and the two containers must size their own
// children.
describe('MCPSetting layout guard', () => {
  const css = Object.entries(
    import.meta.glob('@/components/settings/MCPSetting.vue', {
      eager: true,
      query: '?raw',
      import: 'default',
    }) as Record<string, string>,
  )[0][1]

  /** ruleBody returns the declarations of the first rule with this selector. */
  function ruleBody(selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const m = css.match(new RegExp(`(?:^|\\n)${escaped}\\s*\\{([^}]*)\\}`))
    return m ? m[1] : ''
  }

  it('does not put a bare flex on the shared .mcp-input class', () => {
    const body = ruleBody('.mcp-input')
    expect(body, '.mcp-input rule must exist').not.toBe('')
    // `flex` (shorthand) or `flex-basis` on the shared class is the bug.
    expect(/(^|[\s;])flex\s*:/.test(body), '.mcp-input must not set `flex`').toBe(false)
    expect(/(^|[\s;])flex-basis\s*:/.test(body)).toBe(false)
    // height is the thing being protected.
    expect(/height\s*:\s*30px/.test(body), '.mcp-input must keep height: 30px').toBe(true)
  })

  it('sizes row children and column children separately', () => {
    const rowChild = ruleBody('.mcp-add-grid > .mcp-input:not(.mcp-select)')
    expect(rowChild, 'the row child rule is what makes the name field share the row').not.toBe('')
    expect(/(^|[\s;])flex\s*:\s*1/.test(rowChild)).toBe(true)

    const colChild = ruleBody('.mcp-add > .mcp-input')
    expect(colChild, 'the column child rule keeps height by refusing to grow').not.toBe('')
    expect(/(^|[\s;])flex\s*:\s*none/.test(colChild)).toBe(true)
  })

  it('keeps the textarea height automatic', () => {
    // A textarea has no intrinsic control height; stretching it vertically would
    // make `rows` meaningless and force it to fill the column.
    const body = ruleBody('.mcp-textarea')
    expect(/height\s*:\s*auto/.test(body)).toBe(true)
  })
})
