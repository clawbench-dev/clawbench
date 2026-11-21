import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import McpImportDialog from '@/components/settings/McpImportDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { cancel: '取消', close: '关闭' },
      settings: {
        items: {
          mcpName: '名称',
          mcpImportTitle: '从 JSON 导入 MCP 服务',
          mcpImportHint: '粘贴配置',
          mcpImportInvalidJson: '不是合法的 JSON',
          mcpImportUnsupported: '没认出这是 MCP 配置',
          mcpImportEmpty: '没有可导入的服务',
          mcpImportCount: '识别到 {count} 条',
          mcpImportSkipped: '{count} 条未导入',
          mcpImportExisting: '已存在',
          mcpImportConfirm: '导入 {count} 条',
          mcpImportIssueEmptyName: '名称不能为空',
          mcpImportIssueInvalidName: '名称非法',
          mcpImportIssueDuplicateName: '名称重复',
          mcpImportIssueMissingCommand: '缺少命令',
          mcpImportIssueMissingUrl: '缺少地址',
          mcpImportIssueBadUrl: '地址非法',
          mcpImportIssueBadTransport: '传输方式非法',
        },
      },
    },
  },
})

function mountDialog(existingNames: string[] = [], errorMessage?: string, busy = false) {
  return mount(McpImportDialog, {
    props: { open: true, existingNames, errorMessage, busy },
    global: {
      plugins: [i18n],
      // ModalDialog teleports to <body>; keep the DOM inside the wrapper.
      stubs: { Teleport: true },
    },
  })
}

const CONFIG = JSON.stringify({
  mcpServers: {
    github: { command: 'npx', args: ['-y', 'server-github'] },
    remote: { type: 'http', url: 'https://example.com/mcp' },
  },
})

describe('McpImportDialog', () => {
  it('lists parsed servers and preselects them', async () => {
    const wrapper = mountDialog()
    await wrapper.find('.mcp-import__text').setValue(CONFIG)

    expect(wrapper.findAll('.mcp-import__row')).toHaveLength(2)
    const boxes = wrapper.findAll('.mcp-import__check')
    expect(boxes.every((box) => (box.element as HTMLInputElement).checked)).toBe(true)
    expect(wrapper.find('.fbtn-primary').text()).toContain('导入 2 条')
  })

  it('emits the selected servers with the edited names', async () => {
    const wrapper = mountDialog()
    await wrapper.find('.mcp-import__text').setValue(CONFIG)
    // A migration config often carries a name the server rejects; renaming in
    // place must be enough to import it.
    await wrapper.findAll('.mcp-import__name')[0].setValue('github-mcp')
    await wrapper.find('.fbtn-primary').trigger('click')

    expect(wrapper.emitted('import')?.[0][0]).toEqual([
      { name: 'github-mcp', transport: 'stdio', command: 'npx', args: ['-y', 'server-github'] },
      { name: 'remote', transport: 'http', url: 'https://example.com/mcp' },
    ])
  })

  it('leaves a name that is already configured unselected, and imports it only when ticked', async () => {
    const wrapper = mountDialog(['github'])
    await wrapper.find('.mcp-import__text').setValue(CONFIG)

    const row = wrapper.findAll('.mcp-import__row')[0]
    expect(row.text()).toContain('已存在')
    expect((row.find('.mcp-import__check').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.find('.fbtn-primary').text()).toContain('导入 1 条')

    await row.find('.mcp-import__check').setValue(true)
    expect(wrapper.find('.fbtn-primary').text()).toContain('导入 2 条')
  })

  it('blocks a row whose edited name is invalid or duplicated', async () => {
    const wrapper = mountDialog()
    await wrapper.find('.mcp-import__text').setValue(CONFIG)

    await wrapper.findAll('.mcp-import__name')[1].setValue('github')
    const rows = wrapper.findAll('.mcp-import__row')
    expect(rows[0].text()).toContain('名称重复')
    expect(rows[1].text()).toContain('名称重复')
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeDefined()

    await wrapper.findAll('.mcp-import__name')[1].setValue('my server')
    expect(wrapper.findAll('.mcp-import__row')[1].text()).toContain('名称非法')
  })

  it('unblocks a row once its name is fixed', async () => {
    const wrapper = mountDialog()
    await wrapper.find('.mcp-import__text').setValue('{"my.server": {"command": "node"}}')
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeDefined()

    await wrapper.find('.mcp-import__name').setValue('my-server')
    expect(wrapper.find('.mcp-import__row').text()).not.toContain('名称非法')
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeUndefined()
  })

  it('disables the confirm button while the parent is saving', async () => {
    const wrapper = mountDialog([], undefined, true)
    await wrapper.find('.mcp-import__text').setValue(CONFIG)
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeDefined()
  })

  it('shows a parse error instead of rows for unusable input', async () => {
    const wrapper = mountDialog()
    await wrapper.find('.mcp-import__text').setValue('nope')

    expect(wrapper.find('.mcp-import__error').text()).toContain('不是合法的 JSON')
    expect(wrapper.findAll('.mcp-import__row')).toHaveLength(0)
  })

  it('shows the server rejection passed by the parent', () => {
    const wrapper = mountDialog([], 'mcp.servers[0].command "nope" not found in PATH')
    expect(wrapper.find('.mcp-import__error').text()).toContain('not found in PATH')
  })

  it('reports entries the source had disabled', async () => {
    const wrapper = mountDialog()
    await wrapper.find('.mcp-import__text').setValue(JSON.stringify({
      mcpServers: { a: { command: 'node' }, b: { command: 'node', disabled: true } },
    }))
    expect(wrapper.text()).toContain('1 条未导入')
    expect(wrapper.findAll('.mcp-import__row')).toHaveLength(1)
  })
})
