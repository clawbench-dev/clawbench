/**
 * 表格 / 代码块表头栏图标的唯一来源。
 *
 * 「全屏查看」这个按钮有两套表头在用，它们渲染方式不同：
 *
 *   - `composables/useCodeBlockHeader.ts`：给 markdown 表格与代码块**注入**表头，
 *     只能拼 HTML 字符串；
 *   - `components/common/DataTable.vue`：组件自己的 `.dt-head`，走 Vue 模板。
 *
 * 图形（lucide `maximize`，四角直角括号）定义在这里，两边都取同一份，
 * 改图标只改这个文件。此前两处各写一份，改了一处漏另一处，用户看到的是
 * 「还是旋转图标」。
 *
 * 为什么是字符串而不是 lucide 组件：注入那条路拿不到组件，只能用字符串。
 * 这里不含任何用户输入，`v-html` 是安全的；图形本身用 `currentColor`，
 * 不依赖 scoped 样式（见设计手册红线 1）。
 */

/** @param size 像素（设计 px，调用方各自决定：注入表头 14、组件表头 12） */
export function maximizeIconSvg(size = 14): string {
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 24 24" ` +
    'fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' +
    '<path d="M8 3H5a2 2 0 0 0-2 2v3"/>' +
    '<path d="M21 8V5a2 2 0 0 0-2-2h-3"/>' +
    '<path d="M3 16v3a2 2 0 0 0 2 2h3"/>' +
    '<path d="M16 21h3a2 2 0 0 0 2-2v-3"/>' +
    '</svg>'
  )
}
