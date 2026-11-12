/**
 * 把一段渲染好的消息 HTML 拆成「普通 HTML」与「表格块」两类片段。
 *
 * 为什么需要拆：聊天里的正文是 marked 生成的 HTML 字符串，表格和文字混在
 * 一起。表格要交给 DataTable 组件（和 CSV 预览同一个控件），就必须把表格
 * 从字符串里拿出来，剩下的部分继续用 v-html 渲染。
 *
 * 只拆**顶层**的 `.table-block-wrapper`：嵌套在引用 / 列表里的表结构会被拆坏，
 * 留在 HTML 里按普通表显示（先不管）。
 *
 * 流式输出期间不拆（每来一小段就重新解析一遍太贵），定稿后再拆。
 */
import { extractTableData } from './tableData'

export interface HtmlSegment {
  kind: 'html'
  html: string
}

export interface TableSegment {
  kind: 'table'
  headers: string[]
  rows: string[][]
}

export type MessageSegment = HtmlSegment | TableSegment

const TABLE_WRAPPER_CLASS = 'table-block-wrapper'

export function splitTableBlocks(html: string, enabled: boolean): MessageSegment[] {
  if (!enabled || !html.includes(TABLE_WRAPPER_CLASS)) {
    return [{ kind: 'html', html }]
  }

  const doc = new DOMParser().parseFromString(html, 'text/html')
  const segments: MessageSegment[] = []
  let buffer: Node[] = []

  const flush = (): void => {
    if (buffer.length === 0) return
    segments.push({ kind: 'html', html: buffer.map(serializeNode).join('') })
    buffer = []
  }

  for (const node of Array.from(doc.body.childNodes)) {
    const wrapper = node.nodeType === Node.ELEMENT_NODE
      && (node as Element).classList.contains(TABLE_WRAPPER_CLASS)
      ? (node as Element)
      : null
    const table = wrapper?.querySelector('table')

    if (wrapper && table) {
      flush()
      segments.push({ kind: 'table', ...extractTableData(table as HTMLTableElement) })
      continue
    }

    buffer.push(node)
  }

  flush()
  return segments.length > 0 ? segments : [{ kind: 'html', html }]
}

function serializeNode(node: Node): string {
  if (node.nodeType === Node.ELEMENT_NODE) return (node as Element).outerHTML
  // 文本节点里的 `<` 在解析时已经被解码成字符，直接拼回去会变成标签。
  if (node.nodeType === Node.TEXT_NODE) return escapeText(node.textContent ?? '')
  if (node.nodeType === Node.COMMENT_NODE) return `<!--${(node as Comment).data}-->`
  return ''
}

function escapeText(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
