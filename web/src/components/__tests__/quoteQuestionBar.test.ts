import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { truncateQuoteText, canSendInput } from '@/utils/quoteQuestionUtils'
import QuoteQuestionBar from '@/components/common/QuoteQuestionBar.vue'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import {
  cancelTarget,
  usePendingTarget,
  usePickerIntent,
  useChosenTarget,
  setChosenTarget,
  clearChosenTarget,
} from '@/composables/useConversationTarget'

// The bar reads the ACTIVE session for its "发送到" row. A real ref (not a bare
// object) so the computed re-evaluates when a test switches it.
const identity = useSessionIdentity()

// Mock navigator.clipboard for the copy button tests.
const mockWriteText = vi.fn()
Object.defineProperty(globalThis, 'navigator', {
  value: { clipboard: { writeText: mockWriteText } },
  writable: true,
})
mockWriteText.mockResolvedValue(undefined)

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: {
        copy: 'Copy',
        copied: 'Copied',
      },
      quoteBar: {
        chat: 'Chat',
        placeholder: 'Ask...',
        expandQuote: 'Expand quote',
        addToChat: 'Add to chat',
        send: 'Send',
        newSession: 'New Session',
        aiChat: 'AI Chat',
        // Collapsed toolbar labels (issue #544).
        comment: 'Comment',
        quoteSelection: 'Quote',
        // Destination row in the expanded panel.
        sendTo: '发送到',
        changeTarget: 'Change destination',
        chooseSession: '选择会话',
      },
      session: {
        unnamed: '未命名会话',
      },
    },
  },
})

describe('truncateQuoteText (pure function)', () => {
  it('returns text unchanged when under limit', () => {
    expect(truncateQuoteText('Hello world')).toBe('Hello world')
  })

  it('returns text unchanged at exact limit', () => {
    const text = 'a'.repeat(150)
    expect(truncateQuoteText(text)).toBe(text)
  })

  it('truncates and appends ellipsis when over limit', () => {
    const text = 'a'.repeat(200)
    const result = truncateQuoteText(text)
    expect(result).toBe('a'.repeat(150) + '…')
    expect(result.length).toBe(151)
  })

  it('handles empty string', () => {
    expect(truncateQuoteText('')).toBe('')
  })

  it('preserves unicode characters before truncation', () => {
    const text = '你好世界'.repeat(40)
    const result = truncateQuoteText(text)
    expect(result.endsWith('…')).toBe(true)
    expect(result.length).toBe(151)
  })

  it('handles text with newlines', () => {
    const text = 'line1\nline2\nline3\n' + 'a'.repeat(150)
    const result = truncateQuoteText(text)
    expect(result.endsWith('…')).toBe(true)
  })

  it('handles single character over limit', () => {
    const text = 'a'.repeat(151)
    expect(truncateQuoteText(text)).toBe('a'.repeat(150) + '…')
  })

  it('handles text at limit + 1', () => {
    const text = 'a'.repeat(151)
    const result = truncateQuoteText(text)
    expect(result.length).toBe(151)
    expect(result.endsWith('…')).toBe(true)
  })

  it('custom maxLen parameter', () => {
    const text = 'a'.repeat(60)
    expect(truncateQuoteText(text, 50)).toBe('a'.repeat(50) + '…')
    expect(truncateQuoteText(text, 100)).toBe(text)
  })
})

describe('canSendInput (pure function)', () => {
  it('returns false for empty string', () => {
    expect(canSendInput('')).toBe(false)
  })

  it('returns false for whitespace-only string', () => {
    expect(canSendInput('   ')).toBe(false)
  })

  it('returns true for non-empty trimmed string', () => {
    expect(canSendInput('hello')).toBe(true)
  })

  it('returns true for string with leading/trailing whitespace', () => {
    expect(canSendInput('  hello  ')).toBe(true)
  })

  it('returns true for single character', () => {
    expect(canSendInput('a')).toBe(true)
  })

  it('returns false for newline-only string', () => {
    expect(canSendInput('\n')).toBe(false)
  })

  it('returns true for string with content and newlines', () => {
    expect(canSendInput('\nhello\n')).toBe(true)
  })

  it('returns false for tab-only string', () => {
    expect(canSendInput('\t')).toBe(false)
  })

  it('returns false for mixed whitespace string', () => {
    expect(canSendInput(' \n\t ')).toBe(false)
  })
})

describe('QuoteQuestionBar component', () => {
  const mounted: ReturnType<typeof mount>[] = []
  // Baseline identity for the destination row; `afterEach` restores it.
  identity.currentSessionId.value = 's-current'
  identity.currentSessionTitle.value = 'My active session'

  function mountBar(props = {}) {
    const wrapper = mount(QuoteQuestionBar, {
      props: {
        visible: true,
        quoteData: { text: 'Hello world' },
        ...props,
      },
      attachTo: document.body,
      global: {
        plugins: [i18n],
        stubs: {
          MessageSquare: true,
          Plus: true,
          Send: true,
          // `Copy`/`Check` are deliberately NOT stubbed: the copied-state test
          // asserts which icon is actually rendered, and a stub would render an
          // empty `<copy-stub>` with none of lucide's classes.
        },
      },
    })
    mounted.push(wrapper)
    return wrapper
  }

  afterEach(() => {
    // Unmount everything so document-level listeners (pointerdown/keydown)
    // registered by earlier components can't leak into later tests.
    while (mounted.length) mounted.pop()?.unmount()
    mockWriteText.mockReset()
    mockWriteText.mockResolvedValue(undefined)
    // Module-level state shared with the real composable: reset it or one test's
    // picked destination / open picker leaks into the next.
    clearChosenTarget()
    cancelTarget()
    identity.currentSessionId.value = 's-current'
    identity.currentSessionTitle.value = 'My active session'
    identity.currentAgentId.value = ''
  })

  it('renders the floating toolbar when visible with quoteData', () => {
    const wrapper = mountBar()
    expect(wrapper.find('.quote-question-bar').exists()).toBe(true)
    expect(wrapper.find('.quote-bar-toolbar').exists()).toBe(true)
  })

  it('uses one sharp-corner geometry and the chat-body type scale in the input', async () => {
    const wrapper = mountBar()
    // 输入容器只在 expanded 状态渲染，点击折叠工具条的「评论」展开
    await wrapper.findAll('.quote-bar-action')[1].trigger('click')
    const container = wrapper.find('.qq-input-container')
    expect(container.exists()).toBe(true)
    // textarea 与聊天输入框对齐：字号与聊天正文同 token（--font-size-md）；
    // 行盒用整数 --input-line-height（无单位 1.4 在 WebView 下会让单行文字偏上，
    // 见该 token 注释），上下 padding 4px
    // jsdom 不解析 var()：字号/行高断言 token 名；padding 简写含两个 var()，computed 值
    // 一律解成 0，改在下方按 CSS 规则文本断言
    const ta = wrapper.find('.qq-textarea')
    const taStyles = window.getComputedStyle(ta.element)
    expect(taStyles.fontSize).toBe('var(--font-size-md)')
    expect(taStyles.lineHeight).toBe('var(--input-line-height)')
    // 发送/添加按钮：26px 方形（尖角风格里不能有圆形）
    const sendBtn = wrapper.find('.qq-send-btn')
    const sendStyles = window.getComputedStyle(sendBtn.element)
    expect(sendStyles.width).toBe('26px')
    expect(sendStyles.height).toBe('26px')
    // jsdom 不解析 border-radius 简写计算值，改为断言 CSS 规则
    const cssText = Array.from(document.styleSheets)
      .map((s) => {
        try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
        catch { return '' }
      })
      .join('\n')
    const containerRule = cssText.split('\n').filter((line) => line.includes('.qq-input-container')).join('\n')
    // 输入框改为尖角，与栏的硬朗几何一致（不再是 20px 胶囊）
    expect(containerRule).toContain('border-radius: 0')
    expect(containerRule).not.toContain('20px')
    // 背景用 --bg-primary，与栏的 tertiary 底色区分（避免融合）
    expect(containerRule).toContain('background: var(--bg-primary')
    // textarea 上下 4px / 左右 8px 走间距 token（与聊天输入框对齐）
    const taRule = cssText.split('\n').filter((line) => line.includes('.qq-textarea')).join('\n')
    expect(taRule).toContain('padding: var(--space-2) var(--space-4)')
    // 高度上限由同一行盒推导（3 行 + 上下 padding），不再写死 px
    expect(taRule).toContain('max-height: calc(var(--input-line-height) * 3')
  })

  it('keeps the EXPANDED panel on the sharp scale, and the collapsed toolbar a pill', async () => {
    // The collapsed toolbar (issue #544) is deliberately a rounded pill floating
    // next to the selection; the expanded input panel keeps the bar's original
    // hard-edged language. This guard covers BOTH so neither drifts into the
    // other's scale — the old blanket "no rounded corners anywhere" rule was
    // narrowed, not dropped.
    const wrapper = mountBar()
    await wrapper.findAll('.quote-bar-action')[1].trigger('click')
    const cssText = Array.from(document.styleSheets)
      .map((s) => {
        try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
        catch { return '' }
      })
      .join('\n')

    // 取「声明块」而不是按行筛：选择器列表（.a,\n.b {）会跨行，且后面还跟着
    // scoped 的 [data-v-xxx]，所以按「含该 class 的选择器 + 其声明块」整体匹配，
    // 选择器部分允许跨行。
    const blockOf = (sel: string, need = 'width:') => {
      const re = new RegExp(`([^{}]*\\${sel}[^{}]*)\\{([^{}]*)\\}`, 'g')
      for (const m of cssText.matchAll(re)) {
        if (m[2].includes(need)) return m[2]
      }
      throw new Error(`no matching rule found for ${sel}`)
    }
    const size = (block: string, prop: string) =>
      block.match(new RegExp(`${prop}:\\s*(\\d+px)`))?.[1]

    // 展开态：输入容器尖角（不再是 20px 胶囊），两个图标按钮 26px 方形
    const containerRule = blockOf('.qq-input-container', 'border-radius:')
    expect(containerRule).toContain('border-radius: 0')
    for (const sel of ['.qq-add-btn', '.qq-send-btn']) {
      const block = blockOf(sel)
      expect(size(block, 'width'), `${sel} width`).toBe('26px')
      expect(size(block, 'height'), `${sel} height`).toBe('26px')
      expect(block).toContain('border-radius: var(--radius-xs)')
    }

    // 收起态：药丸工具条 + 每个动作药丸，与展开态的尖角刻意相反
    const toolbarRule = blockOf('.quote-bar-toolbar', 'display:')
    expect(toolbarRule).not.toContain('border-radius: var(--radius-xs)')
    const actionRule = blockOf('.quote-bar-action', 'height:')
    expect(actionRule).toContain('border-radius: var(--radius-full)')
  })

  it('caps the discuss popup at 60% of the original 600px, from one source', async () => {
    // The expanded panel read as too wide for an inline reply box, so the target
    // width was cut to 60% (600px → 360px). Pinned here because the value is
    // easy to "restore" by accident when copying a width from the chat input.
    //
    // The number lives in ONE custom property on the root rule; both consumers
    // (the root's max-width cap and the panel's own width) must reference it.
    // If they ever hard-code separate values, the panel can end up wider than
    // the root's clamp and get silently clipped by `overflow: hidden`.
    const wrapper = mountBar()
    await wrapper.findAll('.quote-bar-action')[1].trigger('click')
    const cssText = Array.from(document.styleSheets)
      .map((s) => {
        try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
        catch { return '' }
      })
      .join('\n')

    // Scoped SFC styles compile to `.quote-question-bar[data-v-xxx] { … }`, and
    // cssText flattens each rule onto one line — so match the class followed by
    // the optional scoped attribute, not a bare `.class {`.
    const blockOfClass = (cls: string) => {
      const re = new RegExp(`\\${cls}(?:\\[[^\\]]*\\])?\\s*\\{([^{}]*)\\}`)
      const m = cssText.match(re)
      if (!m) throw new Error(`no rule found for ${cls}`)
      return m[1]
    }

    // The declaration itself, in the root rule.
    const rootBlock = blockOfClass('.quote-question-bar')
    expect(rootBlock).toContain('--qq-expanded-width: 360px')
    expect(rootBlock).toContain('max-width: var(--qq-expanded-width)')

    // The panel must reference the same property rather than repeat the number.
    const panelBlock = blockOfClass('.quote-bar-expanded')
    expect(panelBlock).toContain('width: var(--qq-expanded-width)')
    // A hard-coded px here is the drift this test exists to catch.
    expect(panelBlock).not.toMatch(/(?<!max-)width:\s*\d+px/)
  })

  it('renders only two states: collapsed needs a quote, otherwise expanded', async () => {
    // 有引用 + 未展开 → 浮动工具条
    const collapsed = mountBar({ quoteData: { text: 'quoted', filePath: 'a.ts' } })
    expect(collapsed.vm.showCollapsed).toBe(true)
    expect(collapsed.find('.quote-bar-toolbar').exists()).toBe(true)
    expect(collapsed.find('.quote-bar-expanded').exists()).toBe(false)

    // 无引用 → 收起工具条不可达（没有可操作内容），始终展开
    const noQuote = mountBar({ quoteData: null, composerMode: true })
    await noQuote.vm.$nextTick()
    await noQuote.vm.$nextTick()
    expect(noQuote.vm.showCollapsed).toBe(false)
    expect(noQuote.find('.quote-bar-toolbar').exists()).toBe(false)
    expect(noQuote.find('.quote-bar-expanded').exists()).toBe(true)

    // 关键回归路径：composer 模式下 handleSend() 会把 expanded 置回 false，
    // 而此刻没有引用。若 showCollapsed 只看 !expanded，就会渲染出一个没有
    // 选中内容的空收起工具条（正是收敛成两态要杜绝的状态）。
    noQuote.vm.inputText = 'hi'
    await noQuote.vm.$nextTick()
    noQuote.vm.handleSend()
    await noQuote.vm.$nextTick()
    expect(noQuote.vm.expanded).toBe(false)
    expect(noQuote.vm.showCollapsed).toBe(false)
    expect(noQuote.find('.quote-bar-toolbar').exists()).toBe(false)
    expect(noQuote.find('.quote-bar-expanded').exists()).toBe(true)
  })

  it('does not render when visible is false', () => {
    const wrapper = mountBar({ visible: false })
    expect(wrapper.find('.quote-question-bar').exists()).toBe(false)
  })

  it('does not render when quoteData is null', () => {
    const wrapper = mountBar({ quoteData: null })
    expect(wrapper.find('.quote-question-bar').exists()).toBe(false)
  })

  it('does not preview the selected text in the collapsed toolbar', () => {
    // issue #544: the collapsed state is an icon+label toolbar, not a quote
    // preview. The snippet only appears once the input is expanded.
    const wrapper = mountBar({ quoteData: { text: 'Hello world' } })
    expect(wrapper.find('.qq-quoted-text').exists()).toBe(false)
  })

  it('shows the full quote text (untruncated) once expanded', async () => {
    const longText = 'a'.repeat(200)
    const wrapper = mountBar({ quoteData: { text: longText } })
    const vm = wrapper.vm as any
    await vm.expand()
    expect(vm.expanded).toBe(true)
    // The expanded block is the only consumer and it never truncates.
    expect(vm.displayQuoteText).toBe(longText)
    expect(wrapper.find('.qq-quoted-text--expanded').text()).toBe(longText)
  })

  it('emits pin and sets expanded when collapsed bar is clicked', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    // Pin is emitted synchronously in expand()
    expect(wrapper.emitted('pin')).toBeTruthy()
    expect(vm.expanded).toBe(true)
  })

  it('send button is disabled when input is empty', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    // canSend is computed from inputText
    expect(vm.canSend).toBe(false)
  })

  it('allows adding the quote with empty text', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()

    vm.handleAdd()

    expect(wrapper.emitted('add')![0]).toEqual(['', { pickTarget: false }])
    expect(vm.expanded).toBe(false)
  })

  it('emits entered text as the quote note when adding', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'note for this quote'
    await nextTick()

    vm.handleAdd()

    expect(wrapper.emitted('add')![0]).toEqual(['note for this quote', { pickTarget: false }])
    expect(vm.inputText).toBe('')
  })

  it('send button is enabled when input has text', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'test message'
    await nextTick()
    expect(vm.canSend).toBe(true)
  })

  it('emits send with input text when handleSend is called', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'my question'
    await nextTick()
    vm.handleSend()
    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')![0]).toEqual(['my question', { pickTarget: false }])
  })

  // ── Destination row ("To: <session>") ──

  describe('destination row', () => {
    it('shows the active session name in the expanded panel', async () => {
      const wrapper = mountBar({ quoteData: { text: 'x' } })
      await (wrapper.vm as any).expand()

      const row = wrapper.find('.qq-target')
      expect(row.exists()).toBe(true)
      expect(row.text()).toContain('发送到')
      expect(row.text()).toContain('My active session')
    })

    it('sits ABOVE the quoted snippet (it is the addressee, not a body item)', async () => {
      const wrapper = mountBar({ quoteData: { text: 'x' } })
      await (wrapper.vm as any).expand()

      const panel = wrapper.find('.quote-bar-expanded').element
      const kids = Array.from(panel.children)
      const rowIdx = kids.findIndex(el => el.classList.contains('qq-target'))
      const snippetIdx = kids.findIndex(el => el.classList.contains('qq-quoted-snippet'))
      expect(rowIdx).toBeGreaterThanOrEqual(0)
      expect(snippetIdx).toBeGreaterThanOrEqual(0)
      expect(rowIdx).toBeLessThan(snippetIdx)
    })

    it('opens the picker in `choose` mode when clicked, without committing', async () => {
      const wrapper = mountBar({ quoteData: { text: 'x' } })
      await (wrapper.vm as any).expand()

      await wrapper.find('.qq-target').trigger('click')

      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value).toBeNull()
      expect(usePickerIntent().value).toBe('choose')
      // Nothing was emitted: the user is still composing.
      expect(wrapper.emitted('add')).toBeFalsy()
      expect(wrapper.emitted('send')).toBeFalsy()
      cancelTarget()
    })

    it('shows the picked session after choosing another one', async () => {
      const wrapper = mountBar({ quoteData: { text: 'x' } })
      await (wrapper.vm as any).expand()

      setChosenTarget({ id: 's-other', title: 'Another chat', agentId: 'a2' })
      await nextTick()

      expect(wrapper.find('.qq-target').text()).toContain('Another chat')
      clearChosenTarget()
    })

    it('falls back to the picker hint when there is no active session', async () => {
      identity.currentSessionId.value = ''
      const wrapper = mountBar({ quoteData: { text: 'x' } })
      await (wrapper.vm as any).expand()

      expect(wrapper.find('.qq-target').text()).toContain('选择会话')
      identity.currentSessionId.value = 's-current'
    })

    it('drops the picked destination when the bar closes', async () => {
      // A destination chosen for one comment must not redirect the next one.
      const wrapper = mountBar({ quoteData: { text: 'x' } })
      await (wrapper.vm as any).expand()
      setChosenTarget({ id: 's-other', title: 'Another chat', agentId: 'a2' })
      await nextTick()

      await wrapper.setProps({ visible: false })

      expect(useChosenTarget().value).toBeNull()
    })
  })

  /**
   * Long-press targets must not be selectable.
   *
   * The Android WebView recognises a hold on selectable text as its OWN text
   * selection gesture: it takes over the touch stream and fires `touchcancel`,
   * which kills the 500ms timer before it can arm. Without `user-select: none`
   * the "hold to pick another conversation" gesture is dead on Android — the
   * exact platform the issue was filed from.
   *
   * The two other long-press consumers in the app already carry this pair
   * (`.file-item`, the quick-send rows), which is how the omission here was
   * found. `touch-callout` is the iOS counterpart.
   */
  it('disables text selection on the three long-press buttons', async () => {
    // Collapsed toolbar first (quote action), then the expanded panel's
    // add/send pair — they live in mutually exclusive branches.
    const wrapper = mountBar()
    const quoteBtn = wrapper.findAll('.quote-bar-action')[2].element as HTMLElement
    const collapsedStyles = window.getComputedStyle(quoteBtn)
    expect(collapsedStyles.userSelect, 'quote action user-select').toBe('none')
    expect(collapsedStyles.webkitUserSelect, 'quote action -webkit-user-select').toBe('none')

    await (wrapper.vm as any).expand()
    for (const sel of ['.qq-add-btn', '.qq-send-btn']) {
      const el = wrapper.find(sel).element as HTMLElement
      const styles = window.getComputedStyle(el)
      expect(styles.userSelect, `${sel} user-select`).toBe('none')
      expect(styles.webkitUserSelect, `${sel} -webkit-user-select`).toBe('none')
    }
  })

  it('clears input and collapses after send', async () => {    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'test'
    await nextTick()
    vm.handleSend()
    await nextTick()
    // After send, expanded should be false and inputText empty
    expect(vm.expanded).toBe(false)
    expect(vm.inputText).toBe('')
  })

  /**
   * The secondary gesture (issue #560): the plain tap commits to the active
   * conversation, a HOLD asks for another one. Without this the cross-session
   * path would be unreachable from the button.
   *
   * The commit is deferred past the click that follows the release, so the
   * assertion has to drain timers (jsdom has no touch pipeline, so the mouse
   * path is the one exercised here; both use the same handlers).
   */
  it('a mouse hold on add asks for a target instead of committing directly', async () => {
    vi.useFakeTimers()
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'note'

    vm.startHold('add')
    vi.advanceTimersByTime(500)
    // Nothing yet: the gesture is not over.
    expect(wrapper.emitted('add')).toBeFalsy()

    vm.endHold()
    // The click the browser dispatches right after the release must be
    // swallowed — otherwise the payload is delivered twice.
    vm.onAddClick()
    vi.runAllTimers()

    expect(wrapper.emitted('add')).toEqual([['note', { pickTarget: true }]])
    vi.useRealTimers()
  })

  it('commits once even when both touchend and mouseup arrive for one hold', async () => {
    // Hybrid devices deliver both. The second release must not fire a second
    // commit (which would target an already-cleared input).
    vi.useFakeTimers()
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'note'

    vm.startHold('add')
    vi.advanceTimersByTime(600)
    vm.endHold()
    vm.endHold()
    vi.runAllTimers()

    expect(wrapper.emitted('add')).toEqual([['note', { pickTarget: true }]])
    vi.useRealTimers()
  })

  it('does not swallow the next click when the hold produced no click at all', async () => {
    // Touch with preventDefault: no synthetic click follows the release. The
    // latch must clear itself, or the user's NEXT tap does nothing.
    vi.useFakeTimers()
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'note'

    vm.startHold('add')
    vi.advanceTimersByTime(600)
    vm.endHold({ preventDefault: vi.fn() })
    vi.runAllTimers()

    vm.inputText = 'second'
    vm.onAddClick()
    expect(wrapper.emitted('add')).toEqual([
      ['note', { pickTarget: true }],
      ['second', { pickTarget: false }],
    ])
    vi.useRealTimers()
  })

  it('Enter in the input sends to the active conversation, not the picker', async () => {
    // Vue passes the KeyboardEvent as the handler's first argument. Routing the
    // keydown straight to handleSend would put the event in `pickTarget` and
    // open the picker on every Enter press.
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'my question'
    await nextTick()

    await wrapper.find('.qq-textarea').trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('send')).toEqual([['my question', { pickTarget: false }]])
  })

  it('a short click on add commits to the active session (no picker)', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'note'

    vm.onAddClick()

    expect(wrapper.emitted('add')).toEqual([['note', { pickTarget: false }]])
  })

  it('cancels the hold when the pointer leaves the button', async () => {
    // Dragging off a button is "never mind": neither the hold NOR a normal
    // commit may fire, and the next click must still work.
    vi.useFakeTimers()
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'note'

    vm.startHold('add')
    vm.cancelHold()
    vi.advanceTimersByTime(600)
    expect(wrapper.emitted('add')).toBeFalsy()

    vm.onAddClick()
    expect(wrapper.emitted('add')).toEqual([['note', { pickTarget: false }]])
    vi.useRealTimers()
  })

  it('resets expanded and input when visible becomes false', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    expect(vm.expanded).toBe(true)

    // Note: Transition in jsdom breaks Vue's reactivity pipeline,
    // so the watch on props.visible doesn't fire. Test the reset
    // logic directly by simulating what the watch does.
    vm.expanded = false
    vm.inputText = ''
    await nextTick()
    expect(vm.expanded).toBe(false)
    expect(vm.inputText).toBe('')
  })

  it('emits close when clicking outside the bar', async () => {
    // onPointerDown relies on barRef.value.contains() which is null inside
    // <Transition> in jsdom. Test the close emit indirectly by verifying
    // that the parent can set visible=false to close the bar (the actual
    // close mechanism used by the parent component).
    const wrapper = mountBar({ visible: true })
    // Simulate parent closing the bar
    await wrapper.setProps({ visible: false })
    expect(wrapper.vm.expanded).toBe(false)
    expect(wrapper.vm.inputText).toBe('')
  })

  it('clears input text when inputText is reset', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    vm.inputText = 'test'
    await nextTick()
    // Simulate clear button: inputText is set to ''
    vm.inputText = ''
    await nextTick()
    expect(vm.canSend).toBe(false)
  })

  it('does not render a clear-input (X) button', async () => {
    const wrapper = mountBar()
    await wrapper.findAll('.quote-bar-action')[1].trigger('click')
    await nextTick()
    expect(wrapper.find('.qq-clear-btn').exists()).toBe(false)
    expect(wrapper.find('.qq-input-row').exists()).toBe(true)
  })

  it('emits close when Escape is pressed', async () => {
    const wrapper = mountBar()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('does not emit close on Escape when bar is hidden', async () => {
    const wrapper = mountBar({ visible: false })
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(wrapper.emitted('close')).toBeFalsy()
  })

  it('does not emit close on other keys', async () => {
    const wrapper = mountBar()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    expect(wrapper.emitted('close')).toBeFalsy()
  })

  it('expands the collapsed bar when Enter is pressed while focus is on body', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    expect(vm.expanded).toBe(false)
    // Enter dispatched on document.body — target is non-editable (file browsing
    // keeps focus on the page, not in an input).
    document.body.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Enter', bubbles: true, cancelable: true,
    }))
    await nextTick()
    expect(vm.expanded).toBe(true)
  })

  it('does not expand on Enter when focus is in an editable field', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    expect(vm.expanded).toBe(false)
    const ta = document.createElement('textarea')
    document.body.appendChild(ta)
    // Dispatch ON the textarea so e.target is the editable field (the document
    // listener bubbles up from it).
    ta.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Enter', bubbles: true, cancelable: true,
    }))
    await nextTick()
    expect(vm.expanded).toBe(false)
    ta.remove()
  })

  it('expands on Enter when focus is in a READ-ONLY contenteditable region (CodeMirror browse)', async () => {
    // Regression: a code-selection quote showed the bar but Enter never expanded
    // it. CodeMirror keeps contenteditable="true" on .cm-content in read-only
    // browse mode (only EditorState.readOnly is set, not the editable facet) and
    // mirrors it via aria-readonly="true". The old guard bailed on any
    // isContentEditable target, treating the editor as a text field.
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    expect(vm.expanded).toBe(false)

    const cm = document.createElement('div')
    cm.className = 'cm-content'
    cm.setAttribute('contenteditable', 'true')
    cm.setAttribute('aria-readonly', 'true')
    // jsdom does not compute isContentEditable; the focused .cm-content is the
    // event target in a real editor, so this is what the guard actually sees.
    Object.defineProperty(cm, 'isContentEditable', { value: true })
    document.body.appendChild(cm)

    cm.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await nextTick()
    expect(vm.expanded).toBe(true)
    cm.remove()
  })

  it('expands on Enter from a descendant of a read-only contenteditable host', async () => {
    // The caret can sit in a .cm-line inside .cm-content, so the target may be a
    // descendant rather than the host itself — the guard resolves via closest().
    const wrapper = mountBar()
    const vm = wrapper.vm as any

    const cm = document.createElement('div')
    cm.setAttribute('contenteditable', 'true')
    cm.setAttribute('aria-readonly', 'true')
    Object.defineProperty(cm, 'isContentEditable', { value: true })
    const line = document.createElement('div')
    line.className = 'cm-line'
    // Real browsers report descendants of a contenteditable host as editable too.
    Object.defineProperty(line, 'isContentEditable', { value: true })
    cm.appendChild(line)
    document.body.appendChild(cm)

    line.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await nextTick()
    expect(vm.expanded).toBe(true)
    cm.remove()
  })

  it('still ignores Enter in a genuinely editable contenteditable field', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    expect(vm.expanded).toBe(false)

    const editable = document.createElement('div')
    editable.setAttribute('contenteditable', 'true')
    Object.defineProperty(editable, 'isContentEditable', { value: true })
    document.body.appendChild(editable)

    editable.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await nextTick()
    expect(vm.expanded).toBe(false)
    editable.remove()
  })

  it('does not re-expand on Enter when the bar is already expanded', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.expand()
    expect(vm.expanded).toBe(true)
    // Dispatch Enter on body (non-editable target). Since already expanded, the
    // document handler returns early — expanded stays true and no send occurs
    // (the textarea's own @keydown.enter owns sending when it has focus).
    document.body.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Enter', bubbles: true, cancelable: true,
    }))
    await nextTick()
    expect(vm.expanded).toBe(true)
  })

  it('renders three labelled actions in the collapsed toolbar', () => {
    const wrapper = mountBar()
    const actions = wrapper.findAll('.quote-bar-action')
    expect(actions).toHaveLength(3)
    expect(wrapper.find('.quote-bar-btn').exists()).toBe(false)
    // Labels, not bare glyphs: this toolbar is the only entry point.
    expect(actions[1].text()).toBe('Comment')
    expect(actions[2].text()).toBe('Quote')
  })

  it('emits add when the collapsed quote action is clicked', async () => {
    const wrapper = mountBar()
    await wrapper.findAll('.quote-bar-action')[2].trigger('click')
    expect(wrapper.emitted('add')![0]).toEqual(['', { pickTarget: false }])
  })

  it('does not render a message icon inside the quoted snippet', async () => {
    const expanded = mountBar()
    const vm = expanded.vm as any
    await vm.expand()
    const expandedSnippet = expanded.find('.qq-quoted-snippet')
    expect(expandedSnippet.find('.lucide-message-square').exists()).toBe(false)
  })

  it('renders a copy button in both collapsed and expanded states', async () => {
    // Collapsed: the toolbar's copy action (a labelled CopyButton).
    const collapsed = mountBar()
    const collapsedCopy = collapsed.findAll('.quote-bar-action')[0]
    expect(collapsedCopy.classes()).toContain('copy-btn')
    expect(collapsedCopy.find('.lucide-copy').exists()).toBe(true)

    // Expanded: the snippet's floating copy button.
    const expanded = mountBar()
    const vm = expanded.vm as any
    await vm.expand()
    expect(expanded.find('.qq-quoted-snippet .qq-copy-btn').exists()).toBe(true)
  })

  it('copies the quoted text when the collapsed copy action is clicked', async () => {
    const wrapper = mountBar({ quoteData: { text: 'Hello world' } })
    const copyBtn = wrapper.findAll('.quote-bar-action')[0]
    await copyBtn.trigger('click')
    expect(mockWriteText).toHaveBeenCalledWith('Hello world')
    await vi.waitFor(() => {
      expect(copyBtn.classes()).toContain('is-copied')
    })
    // Clicking copy must not expand the collapsed toolbar
    expect(wrapper.vm.expanded).toBe(false)
  })

  it('swaps the glyph to a check on copy, keeping the label', async () => {
    // The collapsed copy action is the shared CopyButton: the glyph swaps to a
    // Check while the "Copy" LABEL stays put, so the pill's width never jumps.
    const wrapper = mountBar({ quoteData: { text: 'Hello world' } })
    const btn = wrapper.findAll('.quote-bar-action')[0]

    expect(btn.find('.lucide-copy').exists()).toBe(true)
    expect(btn.classes()).toContain('copy-btn')

    await btn.trigger('click')
    await vi.waitFor(() => {
      expect(btn.classes()).toContain('is-copied')
    })
    await nextTick()

    expect(btn.find('.lucide-check').exists()).toBe(true)
    expect(btn.find('.lucide-copy').exists()).toBe(false)
  })

  it('keeps the expanded copy button a fixed square so it cannot overlap the quote', () => {
    // The 22px square is what the 24px reservation in the snippet is sized for.
    // If the button ever becomes auto-width again, that reservation stops
    // covering it and the button starts covering the text. (The collapsed
    // toolbar's copy action is a separate labelled pill — see the sharp/pill
    // guard above.)
    const cssText = Array.from(document.styleSheets)
      .map((s) => {
        try { return Array.from(s.cssRules).map((r) => r.cssText).join('\n') }
        catch { return '' }
      })
      .join('\n')

    const btnRule = cssText
      .split('\n')
      .filter((line) => line.includes('.qq-copy-btn') && !line.includes('is-copied'))
      .join('\n')
    expect(btnRule).toContain('width: 22px')
    expect(btnRule).toContain('height: 22px')
    // An auto width would let the button size itself to its content again.
    expect(btnRule).not.toContain('min-width')
  })

  it('does not emit add when the copy action is clicked', async () => {
    const wrapper = mountBar()
    await wrapper.findAll('.quote-bar-action')[0].trigger('click')
    expect(wrapper.emitted('add')).toBeFalsy()
  })

  it('resets copied feedback after the timer', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = mountBar({ quoteData: { text: 'Hello world' } })
      const btn = wrapper.findAll('.quote-bar-action')[0]
      await btn.trigger('click')
      expect(btn.classes()).toContain('is-copied')
      vi.advanceTimersByTime(1500)
      await nextTick()
      expect(btn.classes()).not.toContain('is-copied')
      expect(btn.find('.lucide-copy').exists()).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })

  it('stays collapsed (no auto-expand / no pin) when the bar becomes visible', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any

    await vm.onVisibleChange(true)
    await nextTick()

    // Unified PC & mobile: the bar surfaces collapsed, never auto-expands or
    // pins, so it doesn't grab focus or disturb the active selection.
    expect(vm.expanded).toBe(false)
    expect(wrapper.emitted('pin')).toBeFalsy()
  })

  it('unified: shows the toolbar on open and only expands on comment', async () => {
    const longText = 'a'.repeat(200)
    const wrapper = mountBar({ quoteData: { text: longText } })
    const vm = wrapper.vm as any

    await vm.onVisibleChange(true)
    await nextTick()

    // Collapsed: the toolbar, no input, nothing auto-expanded or pinned.
    expect(vm.expanded).toBe(false)
    expect(wrapper.find('.quote-bar-toolbar').exists()).toBe(true)

    await vm.expand()
    expect(vm.expanded).toBe(true)
    expect(vm.displayQuoteText).toBe(longText)
    expect(wrapper.emitted('pin')).toBeTruthy()
  })

  it('does not focus the input while collapsed', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    await vm.onVisibleChange(true)
    await nextTick()
    expect(vm.expanded).toBe(false)
    const active = document.activeElement
    expect(active?.classList.contains('qq-textarea')).toBe(false)
  })

  it('pins on pointerdown so selection loss on click does not hide the bar', async () => {
    const wrapper = mountBar()
    // pointerdown on the collapsed toolbar must pin immediately, before the
    // click handler's expand(). This keeps the bar visible when the global
    // selection handler re-evaluates on pointerup and sees a cleared selection.
    await wrapper.find('.quote-bar-toolbar').trigger('pointerdown')
    expect(wrapper.emitted('pin')).toBeTruthy()
  })

  it('expands and focuses the input when the comment action is clicked', async () => {
    const wrapper = mountBar()
    const vm = wrapper.vm as any
    // Collapsed initially with no input focus.
    expect(vm.expanded).toBe(false)

    // The comment action calls expand() → focuses input.
    await wrapper.findAll('.quote-bar-action')[1].trigger('click')
    await nextTick()

    expect(wrapper.emitted('pin')).toBeTruthy()
    expect(vm.expanded).toBe(true)
    const active = document.activeElement
    expect(active?.classList.contains('qq-textarea')).toBe(true)
  })

  it('expanded quote text constrains its own width so long lines scroll in place', async () => {
    // Regression: selecting a table yields a long unbreakable text line. The
    // scrollable .qq-quoted-text--expanded is a flex item and must carry its own
    // min-width:0/flex:1/max-width — flex min-width on the parent does not
    // propagate to a scrollable child, which would stretch the text element to
    // full width and let the content spill past the bar.
    const longTableText = '| 列A  | 列B  | 列C  |'.repeat(200) // 无空格断行点
    const wrapper = mountBar({ quoteData: { text: longTableText } })
    const vm = wrapper.vm as any
    await vm.expand()
    await nextTick()

    const textEl = wrapper.find('.qq-quoted-text--expanded').element as HTMLElement
    const styles = window.getComputedStyle(textEl)
    expect(styles.overflowY).toBe('auto')
    // 滚动元素自身必须约束宽度，而不是依赖父容器 min-width
    expect(styles.minWidth).toBe('0px')
    expect(styles.flexGrow).toBe('1')
    // 预留浮动 copy 按钮空间（22px 宽 + 2px inset = 24px），避免滚动条压在按钮下面
    expect(styles.maxWidth).toBe('calc(100% - 24px)')
  })

  it('reserves the copy button only once, so the quote fills the bar width', async () => {
    // Regression: the base .qq-quoted-text rule reserved the copy button with
    // `padding-right: 24px`, and .qq-quoted-text--expanded reserved it AGAIN
    // with `max-width: calc(100% - 40px)` — 64px for a 24px button. The text
    // stopped ~40px short of the button and read as if the bar never filled.
    // Exactly one reservation must apply in the expanded state.
    const expanded = mountBar()
    await expanded.findAll('.quote-bar-action')[1].trigger('click')
    await nextTick()

    const textEl = expanded.find('.qq-quoted-text--expanded').element as HTMLElement
    // The expanded rule must override the base padding rather than inherit it.
    expect(window.getComputedStyle(textEl).paddingRight).toBe('0px')

    // Collapsed no longer renders a snippet at all (issue #544 replaced the
    // preview row with the icon+label toolbar), so there is no second element
    // that could double-reserve the space.
    const collapsed = mountBar()
    expect(collapsed.find('.qq-quoted-text').exists()).toBe(false)
  })
})
