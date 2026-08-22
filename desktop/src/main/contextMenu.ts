/** Label map for native context-menu items that have no Electron role default.
 * cut/copy/paste use Electron roles and localize themselves; only the custom
 * items (copy link / copy image) need explicit per-locale labels.
 *
 * 原生右键菜单中无 Electron role 默认值的自定义项标签。剪切/复制/粘贴走
 * Electron role(自动按系统语言本地化);仅"复制链接/复制图片"需要按语言
 * 显式提供文案。 */
export interface ContextMenuLabels {
  copyLink: string
  copyImage: string
}

/** Resolve context-menu labels from a raw locale string (e.g. `zh-CN`, `en`).
 * Any locale starting with `zh` maps to Chinese; everything else (including
 * empty) falls back to English. The locale is compared case-insensitively and
 * ignores region suffixes.
 *
 * 根据原始 locale 字符串(如 `zh-CN`、`en`)解析右键菜单标签。任何以 `zh`
 * 开头的 locale 映射为中文,其余(含空串)回退英文。比较不区分大小写并忽略
 * 地区后缀。 */
export function contextMenuLabels(locale: string): ContextMenuLabels {
  const isZh = locale.toLowerCase().startsWith('zh')
  return {
    copyLink: isZh ? '复制链接' : 'Copy Link',
    copyImage: isZh ? '复制图片' : 'Copy Image',
  }
}