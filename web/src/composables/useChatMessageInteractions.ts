import { onBeforeUnmount, onMounted, type Ref } from 'vue'
import { closeAllTableBlockMenus, handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import { useDoubleClickCopy, type LinkHandler } from '@/composables/useDoubleClickCopy.ts'
import { useTextSelectionActive } from '@/composables/useTextSelection.ts'
import { isAnchorLink, isExternalLink } from '@/utils/doubleClickUtils.ts'

export interface ChatMessageInteractionsOptions {
  /**
   * 桌面端点击相对链接时的打开逻辑。不传表示这个宿主**没有文件查看器**（会话
   * 分享页就是这样）：此时相对链接必须被 preventDefault 掉，见
   * {@link useChatMessageInteractions} 里 suppressRelativeLinkNavigation 的说明。
   */
  onOpenFile?: (path: string, lineStart?: number, lineEnd?: number, lineRanges?: string) => void | Promise<void>
  /** 打开文件前的收尾钩子（例如先关掉悬浮的代码链接预览）。 */
  beforeOpenFile?: () => void
}

export interface ChatMessageInteractions {
  /** 表格行详情弹窗的状态（模板里直接喂给 TableRowModal）。 */
  tableRowModal: Ref<{ headers: string[]; rows: string[][]; currentIndex: number } | null>
  closeTableRowModal: () => void
  tableRowPrev: () => void
  tableRowNext: () => void
  /**
   * 挂在消息容器的 @click 上：代码块/表格块头部按钮 → 表格行 → 双击。
   * 返回 true 表示事件已被消费（调用方应直接 return）。
   *
   * 只有**没有插入自己步骤**的宿主才能一次调完整个序列（会话分享页）。聊天面板
   * 必须在中间插 localhost / 文件路径 / worktree / commit 处理，所以它用下面三个
   * 细分入口按原顺序调用——两者走的是同一份实现，不会各自漂移。
   */
  handleContainerClick: (event: MouseEvent) => boolean
  /** 仅代码块 + 表格块的头部按钮（`data-action` 委托）。 */
  handleBlockHeaderClick: (event: MouseEvent) => boolean
  /** 表格数据行点击 → 打开行详情弹窗（仅触摸端）。 */
  handleTableRowClick: (event: MouseEvent | PointerEvent) => boolean
  /**
   * 双击处理：先尝试链接（锚点 / 相对路径 → onOpenFile），触摸端再走双击复制。
   * 宿主必须把它放在自己的点击链**最后**，与原实现一致。
   */
  handleDblClick: (event: MouseEvent) => void
  /** 挂在 @mousedown 上（表格横向拖拽守卫）。 */
  onContainerMouseDown: (event: MouseEvent) => void
  /** 挂在 @touchstart.passive 上（同上，触摸端用 touch 事件记录起点）。 */
  onContainerTouchStart: (event: TouchEvent) => void
  /** 用户正在选中文本（宿主据此隐藏浮动按钮）。 */
  textSelecting: Ref<boolean>
}

/**
 * 消息容器上的**纯客户端**交互委托。
 *
 * 抽出来的原因：这些交互只依赖渲染出来的 DOM（复制、换行、表格行展开、双击
 * 复制），任何渲染了 ChatMessageItem 的宿主都该有——而会话分享页曾经只是
 * `v-for` 出消息，于是按钮渲染在 DOM 里却点了没反应。
 *
 * 刻意**不**包含依赖活会话或鉴权的部分（worktree 切换、commit 跳转、localhost
 * 端口转发、文件路径打开、懒加载、流式跟随）：它们留在 ChatMessageList 里。
 */
export function useChatMessageInteractions(options?: ChatMessageInteractionsOptions): ChatMessageInteractions {
  const { tableRowModal, closeTableRowModal, tableRowPrev, tableRowNext, handleTableRowClick, onTableMouseDown, onTableTouchStart } = useTableRowExpand()
  const { handleDblClick: rawHandleDblClick } = useDoubleClickCopy()
  const { active: textSelecting } = useTextSelectionActive()

  const openFile = options?.onOpenFile

  /**
   * 相对链接的默认导航兜底。
   *
   * `handleAnchorClick` 只在**有** onOpenFile 时才 preventDefault 相对链接；没有
   * 就一路 return false，交给浏览器按 href 导航。这对聊天面板是对的（它总能
   * 打开文件），但对分享页是灾难：点击 `docs/guide.md` 会让整个只读分享页跳去
   * 一个不存在的相对地址，把读者从对话里踢出去。分享页没有文件查看器，所以这
   * 类链接的正确行为是「什么都不发生」。
   *
   * 只拦「有 href、非锚点、非外链」的 <a>：锚点由 handleHashLink 处理，外链本就
   * 该由浏览器打开。
   */
  function suppressRelativeLinkNavigation(event: MouseEvent): void {
    if (openFile) return
    const anchor = (event.target as HTMLElement | null)?.closest?.('a[href]')
    if (!anchor) return
    const href = anchor.getAttribute('href')
    if (!href || isAnchorLink(href) || isExternalLink(href)) return
    event.preventDefault()
  }

  /**
   * 把 options.onOpenFile 包成 handleAnchorClick 需要的回调。
   *
   * 原实现的三件事都在这里补齐，缺一件就是行为回归：
   *   1. stopPropagation —— 打开文件后不让这次点击继续冒泡到外层处理器
   *   2. beforeOpenFile —— 关掉悬浮的代码链接预览
   *   3. onOpenFile 本身（异步；这里不 await，与原实现一致：双击不是可等待的手势）
   *
   * 事件只在调用栈里可用，所以包装器必须按次构造，不能预先建好。
   */
  function makeAnchorHandler(event: MouseEvent): LinkHandler | undefined {
    if (!openFile) return undefined
    return (path, lineStart, lineEnd, lineRanges) => {
      event.stopPropagation()
      options?.beforeOpenFile?.()
      void openFile(path, lineStart, lineEnd, lineRanges)
    }
  }

  function handleDblClick(event: MouseEvent): void {
    suppressRelativeLinkNavigation(event)
    rawHandleDblClick(event, makeAnchorHandler(event))
  }

  function handleBlockHeaderClick(event: MouseEvent): boolean {
    if (handleCodeBlockClick(event)) return true
    if (handleTableBlockClick(event)) return true
    return false
  }

  function handleContainerClick(event: MouseEvent): boolean {
    if (handleBlockHeaderClick(event)) return true
    if (handleTableRowClick(event)) return true
    // 双击本身不消费事件：一次单击仍是单击，后续处理器（以及浏览器默认行为）
    // 必须照常执行。
    handleDblClick(event)
    return false
  }

  /**
   * 点击别处时收起表格复制下拉菜单。
   *
   * 菜单是 `position: fixed` 的浮层，只有「点它自己」或「再点一次触发按钮」才会
   * 关。聊天面板在 `ChatMessageList.onDocumentClick` 里做了这件事、主应用在
   * `App.vue` 的 overflow 处理器里也做了——两处都是各自组件内的私有监听，所以
   * 任何新宿主（分享页）都会漏掉：菜单打开后点页面空白处收不回去，浮层一直挂在
   * 屏幕上，只能靠点同一个按钮再关一次。
   *
   * 用捕获阶段（`true`）与原实现一致：菜单项自身的点击要先经过这里，但
   * `handleTableBlockClick` 已经在菜单内的 `data-action` 上 stopPropagation，
   * 而这里的判断是「点的是不是菜单/触发按钮」，两者互不干扰。
   */
  function onDocumentClickForMenus(event: MouseEvent): void {
    const target = event.target as Element | null
    if (target?.closest?.('.table-block-copy-menu') || target?.closest?.('.table-block-copy-btn')) return
    closeAllTableBlockMenus()
  }

  onMounted(() => {
    document.addEventListener('click', onDocumentClickForMenus, true)
  })
  onBeforeUnmount(() => {
    document.removeEventListener('click', onDocumentClickForMenus, true)
  })

  return {
    tableRowModal,
    closeTableRowModal,
    tableRowPrev,
    tableRowNext,
    handleContainerClick,
    handleBlockHeaderClick,
    handleTableRowClick,
    handleDblClick,
    onContainerMouseDown: onTableMouseDown,
    onContainerTouchStart: onTableTouchStart,
    textSelecting,
  }
}
