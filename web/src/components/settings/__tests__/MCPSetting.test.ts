import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

const mockGetServerValue = vi.fn()
const mockSetServerValue = vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })
const mockLoadConfig = vi.fn().mockResolvedValue(undefined)

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValue: mockGetServerValue,
    setServerValue: mockSetServerValue,
    loadConfig: mockLoadConfig,
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// The test button goes through the shared connectivity composable. Mocking it
// keeps these tests about the WIRING (which server gets probed, what the result
// renders as), not about HTTP.
const mockRunTests = vi.fn()
const mockTestResults = { value: [] as Array<{ success: boolean; message: string }> }
vi.mock('@/composables/useConnectivityTest', () => ({
  useConnectivityTest: () => ({
    testing: { value: false },
    testResults: mockTestResults,
    runTests: mockRunTests,
    clearResults: vi.fn(),
  }),
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
          mcpTest: '测试',
          mcpTesting: '测试中…',
          mcpTestHint: '真实连接一次',
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
      stubs: { AlertCircle: true, CheckCircle2: true, Loader2: true, PlugZap: true, Trash2: true },
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

  // The list is read from the shared serverConfig snapshot. Without a refetch
  // here, a server added while Settings was already open (another device, the
  // AI editing config.yaml, a CLI) stayed invisible until a full page reload —
  // reported as "I open MCP services and it is not listed; refreshing fixes it".
  it('refetches the config on mount so an externally added server appears', async () => {
    mountSetting([])
    await flushPromises()
    expect(mockLoadConfig).toHaveBeenCalledTimes(1)
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
    // The row now has two buttons (test, remove); target the remove one by its
    // label rather than by position.
    const row = wrapper.findAll('.mcp-row')[0]
    const removeBtn = row.findAll('button').find((b) => b.text().includes('删除'))
    expect(removeBtn, 'the remove button must be present').toBeTruthy()
    await removeBtn!.trigger('click')

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
    // height is the thing being protected. Expressed in design px now, so it
    // scales with the rest of the UI; the invariant is the 30, not the unit.
    expect(
      /height\s*:\s*calc\(\s*30\s*\*\s*var\(--dp\)\s*\)/.test(body),
      '.mcp-input must keep a 30 design-px height',
    ).toBe(true)
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

// ── Test button ──────────────────────────────────────────────────────────

describe('MCPSetting test button', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSetServerValue.mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })
    mockTestResults.value = []
  })

  function testBtn(wrapper: ReturnType<typeof mountSetting>) {
    return wrapper.findAll('.mcp-row')[0]
      .findAll('button')
      .find((b) => b.text().includes('测试') || b.text().includes('Test'))
  }

  it('probes the named server and sends the WHOLE list', async () => {
    // The backend resolves the definition from the list, so sending only the
    // target would leave it with nothing to resolve. Sending the whole list is
    // also what makes "test an unsaved edit" work.
    mockRunTests.mockResolvedValue(undefined)
    mockTestResults.value = [{ success: true, message: 'connected — 2 tool(s) available' }]

    const wrapper = mountSetting([
      { name: 'alpha', transport: 'stdio', command: 'node' },
      { name: 'beta', transport: 'http', url: 'https://x' },
    ])
    await testBtn(wrapper)!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(mockRunTests).toHaveBeenCalledTimes(1)
    const arg = mockRunTests.mock.calls[0][0]
    expect(arg[0].category).toBe('mcp')
    expect(arg[0].values['mcp.name']).toBe('alpha')
    expect(arg[0].values['mcp.servers']).toHaveLength(2)
    // A launcher such as `npx -y` / `uvx` may download its package on the first
    // run, so the default 15s is not enough — and it must stay above the
    // server's own 45s MCP budget so the server's specific error wins.
    expect(arg[0].timeoutMs).toBe(60_000)
  })

  it('renders a success message with the tool count', async () => {
    mockRunTests.mockResolvedValue(undefined)
    mockTestResults.value = [{ success: true, message: 'connected — 2 tool(s) available' }]

    const wrapper = mountSetting([{ name: 'alpha', transport: 'stdio', command: 'node' }])
    await testBtn(wrapper)!.trigger('click')
    await wrapper.vm.$nextTick()

    const result = wrapper.find('.mcp-test-result')
    expect(result.exists()).toBe(true)
    expect(result.classes()).toContain('mcp-test-result--ok')
    expect(result.text()).toContain('2 tool(s)')
  })

  it('renders a failure message and marks it as failed', async () => {
    // A green result for a broken server is the failure mode that matters: the
    // user would save a configuration that never works.
    mockRunTests.mockResolvedValue(undefined)
    mockTestResults.value = [{ success: false, message: 'command "nope" not found in PATH' }]

    const wrapper = mountSetting([{ name: 'alpha', transport: 'stdio', command: 'nope' }])
    await testBtn(wrapper)!.trigger('click')
    await wrapper.vm.$nextTick()

    const result = wrapper.find('.mcp-test-result')
    expect(result.classes()).toContain('mcp-test-result--fail')
    expect(result.text()).toContain('not found in PATH')
  })

  it('shows a message even when the composable returns no result', async () => {
    // An empty result array must not render a blank box that looks like success.
    mockRunTests.mockResolvedValue(undefined)
    mockTestResults.value = []

    const wrapper = mountSetting([{ name: 'alpha', transport: 'stdio', command: 'node' }])
    await testBtn(wrapper)!.trigger('click')
    await wrapper.vm.$nextTick()

    const result = wrapper.find('.mcp-test-result')
    expect(result.exists()).toBe(true)
    expect(result.classes()).toContain('mcp-test-result--fail')
  })

  it('surfaces a thrown error instead of leaving the row blank', async () => {
    mockRunTests.mockRejectedValue(new Error('network down'))

    const wrapper = mountSetting([{ name: 'alpha', transport: 'stdio', command: 'node' }])
    await testBtn(wrapper)!.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.mcp-test-result').text()).toContain('network down')
  })

  it('drops the stored result when the server is removed', async () => {
    // The list is re-read from the server after a save, so the row disappearing
    // is the server's doing; what this test pins is that the RESULT is dropped
    // too. Otherwise re-adding a server with the same name shows the old
    // verdict before it has been probed — a stale green.
    mockRunTests.mockResolvedValue(undefined)
    mockTestResults.value = [{ success: true, message: 'ok' }]

    const wrapper = mountSetting([
      { name: 'alpha', transport: 'stdio', command: 'node' },
      { name: 'beta', transport: 'stdio', command: 'node' },
    ])
    await testBtn(wrapper)!.trigger('click')
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('.mcp-test-result')).toHaveLength(1)

    const removeBtn = wrapper.findAll('.mcp-row')[0].findAll('button').find((b) => b.text().includes('删除'))
    await removeBtn!.trigger('click')
    await wrapper.vm.$nextTick()

    // The save was issued with the remaining server only.
    expect(mockSetServerValue).toHaveBeenCalledWith('mcp.servers', [
      { name: 'beta', transport: 'stdio', command: 'node' },
    ])
  })
})
