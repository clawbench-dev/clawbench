/**
 * 「默认自动换行」的读取（只依赖 localStorage）。
 *
 * 单独拆出来是因为聊天里的表格组件要读它，而 `blockWrapPref` 还带着改文案 /
 * 重刷 DOM 的逻辑，会间接引入 app 的 i18n 单例——测试里对 vue-i18n 做局部
 * mock 时（forge 详情等）会直接炸在模块加载阶段。
 */

/**
 * Same key `setLocalConfig('tableWrap', …)` writes (LOCAL_PREFIX + key). The
 * key keeps its original name so preferences saved before code blocks shared
 * this setting keep working.
 */
export const BLOCK_WRAP_STORAGE_KEY = 'clawbench-settings-tableWrap'

/** `null` until the first read, so localStorage is touched once per session. */
let cached: boolean | null = null

/** The default wrap mode for a block that has not been toggled by hand. */
export function getDefaultBlockWrap(): boolean {
  if (cached === null) {
    let value = true
    try {
      const raw = localStorage.getItem(BLOCK_WRAP_STORAGE_KEY)
      if (raw !== null) value = raw !== 'false'
    } catch {
      // Private mode / disabled storage: fall back to the shipped default.
    }
    cached = value
  }
  return cached
}

/** 写入口只改内存里的值（落盘由 blockWrapPref 负责）。 */
export function setBlockWrapCache(wrap: boolean): void {
  cached = wrap
}

/** @internal Reset the memoised value — for tests only. */
export function _resetBlockWrapCache(): void {
  cached = null
}
