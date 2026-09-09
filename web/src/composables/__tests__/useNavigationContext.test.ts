import { describe, it, expect, beforeEach } from 'vitest'
import {
  useNavigationContext,
  isSameVisit,
  shouldSettleOrigin,
  resolveJumpOriginTab,
  surfaceToTab,
  type NavigationOrigin,
} from '../useNavigationContext'

describe('useNavigationContext', () => {
  const nav = useNavigationContext()

  beforeEach(() => {
    nav.resetForTesting()
  })

  it('1. 初始为空', () => {
    expect(nav.hasOrigin.value).toBe(false)
    expect(nav.origin.value).toBeNull()
    expect(nav.originLabel.value).toBeNull()
    expect(nav.busy.value).toBe(false)
  })

  it('2. start() 成功写入', () => {
    const origin: NavigationOrigin = {
      surface: 'chat',
      tab: 'chat',
      label: '返回对话',
      sessionId: 'session-123',
    }
    const started = nav.start(origin)
    expect(started).toBe(true)
    expect(nav.hasOrigin.value).toBe(true)
    expect(nav.origin.value).toEqual(origin)
    expect(nav.originLabel.value).toBe('返回对话')
  })

  it('3. 重复 start 不覆盖', () => {
    const origin1: NavigationOrigin = {
      surface: 'chat',
      tab: 'chat',
      label: '返回对话',
      sessionId: 'session-1',
    }
    const origin2: NavigationOrigin = {
      surface: 'file',
      tab: 'view',
      label: '返回 README.md',
      filePath: 'README.md',
    }
    expect(nav.start(origin1)).toBe(true)
    expect(nav.start(origin2)).toBe(false)
    expect(nav.origin.value?.label).toBe('返回对话')
    expect(nav.origin.value?.surface).toBe('chat')
  })

  it('4. consume 只消费一次', () => {
    const origin: NavigationOrigin = {
      surface: 'chat',
      tab: 'chat',
      label: '返回对话',
    }
    nav.start(origin)
    const consumed = nav.consume()
    expect(consumed).toEqual(origin)
    expect(nav.hasOrigin.value).toBe(false)
    expect(nav.origin.value).toBeNull()

    // Second consume returns null
    expect(nav.consume()).toBeNull()
  })

  it('5. clear 完整清理', () => {
    const origin: NavigationOrigin = {
      surface: 'task',
      tab: 'tasks',
      label: '返回任务',
    }
    nav.start(origin)
    nav.setBusy(true)
    expect(nav.busy.value).toBe(true)

    nav.clear()
    expect(nav.hasOrigin.value).toBe(false)
    expect(nav.origin.value).toBeNull()
    expect(nav.busy.value).toBe(false)
  })

  it('6. busy 状态可切换', () => {
    expect(nav.busy.value).toBe(false)
    nav.setBusy(true)
    expect(nav.busy.value).toBe(true)
    nav.setBusy(false)
    expect(nav.busy.value).toBe(false)
  })

  it('7. snapshot/restore preserves a project navigation context', () => {
    const origin: NavigationOrigin = {
      surface: 'file',
      tab: 'view',
      label: '返回 README.md',
      filePath: 'README.md',
    }
    nav.start(origin)

    const snapshot = nav.snapshot()
    nav.clear()
    nav.restore(snapshot)

    expect(nav.origin.value).toEqual(origin)
    expect(nav.hasOrigin.value).toBe(true)
  })

  it('9. 非 file 来源不保留文件恢复数据（防止返回时打开无关文件）', () => {
    nav.start({
      surface: 'chat',
      tab: 'chat',
      label: '返回对话',
      // A stale file must not survive on a chat origin.
      filePath: 'unrelated.md',
      lineStart: 3,
      lineEnd: 5,
      viewMode: 'raw',
      scrollTop: 99,
    } as NavigationOrigin)
    expect(nav.origin.value).toEqual({
      surface: 'chat',
      tab: 'chat',
      label: '返回对话',
    })
  })

  it('10. file 来源完整保留文件恢复数据', () => {
    nav.start({
      surface: 'file',
      tab: 'view',
      label: '返回 doc.md',
      filePath: 'doc.md',
      lineStart: 10,
      lineEnd: 20,
      viewMode: 'rendered',
      scrollTop: 350,
    })
    expect(nav.origin.value?.filePath).toBe('doc.md')
    expect(nav.origin.value?.lineStart).toBe(10)
    expect(nav.origin.value?.scrollTop).toBe(350)
  })

  it('11. replace 强制替换已被遗弃的 origin', () => {
    nav.start({ surface: 'chat', tab: 'chat', label: '返回对话' })
    // A second jump from a different surface: start() refuses, replace() wins.
    expect(nav.start({ surface: 'task', tab: 'tasks', label: '返回任务' })).toBe(false)
    nav.replace({ surface: 'task', tab: 'tasks', label: '返回任务' })
    expect(nav.origin.value?.surface).toBe('task')
    expect(nav.hasOrigin.value).toBe(true)
  })

  it('12. isSameVisit 区分同来源与跨来源的两次跳转', () => {
    const chat = { surface: 'chat' as const, tab: 'chat', label: 'a' }
    const chatAgain = { surface: 'chat' as const, tab: 'chat', label: 'b' }
    const task = { surface: 'task' as const, tab: 'tasks', label: 'c' }
    expect(isSameVisit(chat, chatAgain)).toBe(true)
    expect(isSameVisit(chat, task)).toBe(false)
    expect(isSameVisit(chat, null)).toBe(false)
  })

  describe('13. shouldSettleOrigin 目录跳转期间不得被切换 tab 结算掉', () => {
    const fileOrigin: NavigationOrigin = {
      surface: 'file',
      tab: 'view',
      label: '返回 a.md',
      filePath: 'docs/a.md',
    }

    it('无 origin / tab 不匹配时一律不结算', () => {
      expect(shouldSettleOrigin(null, 'view', { pending: false })).toBe(false)
      expect(shouldSettleOrigin(fileOrigin, 'browse', { pending: false })).toBe(false)
    })

    it('普通跳转：用户手动回到来源 tab 即结算', () => {
      const chatOrigin: NavigationOrigin = { surface: 'chat', tab: 'chat', label: '返回对话' }
      expect(shouldSettleOrigin(chatOrigin, 'chat', { pending: false })).toBe(true)
      expect(shouldSettleOrigin(fileOrigin, 'view', { pending: false })).toBe(true)
    })

    it('存在挂起的目录跳转时，切到文件 tab 打开别的文件不结算', () => {
      // 用户从跳转到的目录 B 里打开 b.md —— 这不等于回到了被挂起的 a.md
      expect(shouldSettleOrigin(fileOrigin, 'view', {
        pending: true,
        currentFilePath: 'src/b.md',
      })).toBe(false)
    })

    it('存在挂起的目录跳转时，回到被挂起的文件本身才结算', () => {
      expect(shouldSettleOrigin(fileOrigin, 'view', {
        pending: true,
        currentFilePath: 'docs/a.md',
      })).toBe(true)
    })

    it('挂起但没有 filePath 的 origin 仍按普通规则结算（不被误判为永久挂起）', () => {
      const bare: NavigationOrigin = { surface: 'file', tab: 'view', label: '返回 a.md' }
      expect(shouldSettleOrigin(bare, 'view', { pending: true, currentFilePath: 'src/b.md' })).toBe(true)
    })
  })

  describe('14. resolveJumpOriginTab 宽屏对话跳转的 origin.tab 解析', () => {
    it('宽屏下对话跳转必须记录 chat，而不是左侧栏页签', () => {
      // 宽屏下 activeTab 跟随左侧栏（用户看聊天时它可能是 browse），若照抄，
      // 跳转自身的 switchTab('browse') 会立刻把 origin 结算掉，返回横幅消失。
      expect(resolveJumpOriginTab('chat', 'browse', { isWideScreen: true, chatPaneActive: true })).toBe('chat')
    })

    it('宽屏下左侧栏操作（文件搜索选文件等）仍记录当前左侧页签', () => {
      expect(resolveJumpOriginTab('chat', 'browse', { isWideScreen: true, chatPaneActive: false })).toBe('browse')
      expect(resolveJumpOriginTab('history', 'history', { isWideScreen: true, chatPaneActive: false })).toBe('history')
    })

    it('窄屏对话跳转记录当前 activeTab（即 chat）', () => {
      expect(resolveJumpOriginTab('chat', 'chat', { isWideScreen: false, chatPaneActive: true })).toBe('chat')
    })

    it('非 chat 来源一律记录当前 activeTab', () => {
      expect(resolveJumpOriginTab('file', 'view', { isWideScreen: true, chatPaneActive: true })).toBe('view')
      expect(resolveJumpOriginTab('task', 'tasks', { isWideScreen: false, chatPaneActive: false })).toBe('tasks')
    })
  })

  describe('15. surfaceToTab 将 surface 映射为有效的 tabId', () => {
    it('映射 task/tasks 为 tasks', () => {
      expect(surfaceToTab('task')).toBe('tasks')
      expect(surfaceToTab('tasks')).toBe('tasks')
    })

    it('映射 file/view 为 view', () => {
      expect(surfaceToTab('file')).toBe('view')
      expect(surfaceToTab('view')).toBe('view')
    })

    it('映射 history 为 history', () => {
      expect(surfaceToTab('history')).toBe('history')
    })

    it('映射 browse 为 browse', () => {
      expect(surfaceToTab('browse')).toBe('browse')
    })

    it('保持直接 tab 名 terminal/proxy/stats/settings', () => {
      expect(surfaceToTab('terminal')).toBe('terminal')
      expect(surfaceToTab('proxy')).toBe('proxy')
      expect(surfaceToTab('stats')).toBe('stats')
      expect(surfaceToTab('settings')).toBe('settings')
    })

    it('映射 chat 为 chat', () => {
      expect(surfaceToTab('chat')).toBe('chat')
    })

    it('未知 surface 返回 null 而不是静默回退到 chat', () => {
      expect(surfaceToTab('unknown')).toBeNull()
      expect(surfaceToTab('')).toBeNull()
      expect(surfaceToTab('Chat')).toBeNull()
      // 原型链上的属性不能被当成合法 surface 命中
      expect(surfaceToTab('constructor')).toBeNull()
      expect(surfaceToTab('toString')).toBeNull()
    })
  })
})
