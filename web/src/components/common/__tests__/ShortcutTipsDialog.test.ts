import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ShortcutTipsDialog from '../ShortcutTipsDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) =>
    params && params.count !== undefined ? `${key} [${params.count}]` : key }),
}))

type MockTip = { context: string; contextKey: string; keys?: string[]; actionKey: string }
const mockAll = vi.fn<() => MockTip[]>()
vi.mock('@/config/shortcutTips', () => ({
  getAllShortcutTips: () => mockAll(),
  SHORTCUT_CONTEXT_ORDER: ['common', 'chat', 'browse'],
}))

async function mountDialog(open = true) {
  const wrapper = mount(ShortcutTipsDialog, {
    props: { open },
    attachTo: document.body,
  })
  await nextTick()
  return wrapper
}

describe('ShortcutTipsDialog', () => {
  beforeEach(() => {
    mockAll.mockReset()
    document.body.innerHTML = ''
  })

  it('renders grouped tables with keys/context/action columns', async () => {
    mockAll.mockReturnValue([
      { context: 'common', contextKey: 'c.search', keys: ['Ctrl+F'], actionKey: 'a.search' },
      { context: 'chat', contextKey: 'c.send', keys: ['Enter'], actionKey: 'a.send' },
      { context: 'browse', contextKey: 'c.f2', keys: ['F2'], actionKey: 'a.f2' },
    ])
    await mountDialog()
    const groups = document.body.querySelectorAll('.st-group')
    expect(groups.length).toBe(3)
    const common = groups[0]
    expect(common.querySelector('.st-group-title')!.textContent).toContain('appHeader.shortcutTipGroup.common')
    expect(common.querySelectorAll('.st-kbd')[0].textContent).toBe('Ctrl+F')
    expect(common.textContent).toContain('a.search')
    expect(document.body.querySelector('.modal-title')!.textContent).toContain('3')
  })

  it('shows a dash when a tip has no keys', async () => {
    mockAll.mockReturnValue([
      { context: 'chat', contextKey: 'c.reco', actionKey: 'a.reco' },
    ])
    await mountDialog()
    const nokey = document.body.querySelector('.st-nokey')
    expect(nokey).not.toBeNull()
  })

  it('renders empty state when there are no tips', async () => {
    mockAll.mockReturnValue([])
    await mountDialog()
    expect(document.body.querySelector('.st-dialog-empty')).not.toBeNull()
    expect(document.body.querySelectorAll('.st-group').length).toBe(0)
  })

  it('declares a close event', async () => {
    mockAll.mockReturnValue([])
    await mountDialog()
    expect(ShortcutTipsDialog.emits).toBeDefined()
  })
})
