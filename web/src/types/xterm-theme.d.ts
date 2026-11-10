/**
 * xterm-theme 是 UMD 包，唯一有用的是默认导出：`{ 主题名: ITheme }` 的映射。
 *
 * 这里只声明默认导出。原先还逐个罗列了 `XtermTheme` 与若干具名导出
 * （AdventureTime / Dracula），但全仓没有任何 import 方——主题一律经
 * `loadThemesModule()` 走默认导入按 id 取，列表也只剩精选的十几套，
 * 不会再按名字引用。
 */
declare module 'xterm-theme' {
  const themes: Record<string, import('@xterm/xterm').ITheme>
  export default themes
}
