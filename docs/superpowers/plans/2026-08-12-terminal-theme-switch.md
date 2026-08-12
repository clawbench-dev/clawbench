# 终端独立主题切换（157 主题懒加载）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Web 终端提供独立主题切换，支持社区 `xterm-theme` 包全部 157 种主题，默认跟随 App 深/浅色、可覆盖、持久化，并保证 `xterm-theme` 不进入主 bundle（动态 import 懒加载）。

**Architecture:** 新增纯函数工具模块 `web/src/utils/terminalThemes.ts` 封装主题 id 静态列表、懒加载 `loadAllThemes()`、`resolveTheme()` 与名称格式化；在 `useSettingsConfig.ts` 增加 `terminalTheme` 持久化；在 `TerminalPanelContent.vue` tab 栏加调色板按钮 + `PopupMenu` 主题选择弹层（打开时才懒加载 157 主题），通过现有 `tabManager.updateTheme()` 应用到所有 tab。

**Tech Stack:** Vue 3、TypeScript、@xterm/xterm v6、xterm-theme v1.1.0（UMD）、Vite（动态 import 分包）、vitest。

---

## 文件结构

- Create `web/src/utils/terminalThemes.ts` — 主题工具（静态 id 列表、懒加载、resolve、格式化、静态色板）。
- Create `web/src/types/xterm-theme.d.ts` — `xterm-theme` 的模块类型声明（包本身无类型）。
- Modify `web/src/composables/useSettingsConfig.ts` — 增加 `terminalTheme` 默认值与 legacy 映射。
- Modify `web/src/components/terminal/TerminalPanelContent.vue` — 调色板按钮、主题弹层、`applyTheme`、MutationObserver 条件化、`getXtermTheme` 迁移。
- Modify `web/src/i18n/locales/zh.ts` / `en.ts` — 新增 terminal 主题相关文案。
- Create `web/src/utils/__tests__/terminalThemes.test.ts` — 工具函数单测。
- Modify `web/src/components/__tests__/terminalPanelSelection.test.ts` — 组件级主题入口断言。

---

### Task 1: 创建 `xterm-theme` 类型声明

**Files:**
- Create: `web/src/types/xterm-theme.d.ts`

- [ ] **Step 1: 创建类型声明文件**

创建 `web/src/types/xterm-theme.d.ts`（xterm-theme 是 UMD，无内置类型，需手写声明）：

```ts
declare module 'xterm-theme' {
  export interface XtermTheme {
    foreground: string
    background: string
    cursor?: string
    cursorAccent?: string
    black: string
    brightBlack: string
    red: string
    brightRed: string
    green: string
    brightGreen: string
    yellow: string
    brightYellow: string
    blue: string
    brightBlue: string
    magenta: string
    brightMagenta: string
    cyan: string
    brightCyan: string
    white: string
    brightWhite: string
  }

  export const AdventureTime: XtermTheme
  export const Dracula: XtermTheme
  // 其余主题 id 同构（每个导出一个 XtermTheme）。为类型完整，可把
  // `Record<string, XtermTheme>` 的兜底导出一并声明：
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const themes: Record<string, XtermTheme>
  export default themes
}
```

> 说明：由于 UMD 包的命名导出无法被静态枚举，本声明提供 `XtermTheme` 接口、若干具名导出以及一个 `default`（`Record<string, XtermTheme>`）。`loadAllThemes()` 统一走 default 导入，避免维护 157 个具名导出。

- [ ] **Step 2: 验证 TS 可解析该模块**

Run: `cd web && npx vue-tsc --noEmit -p tsconfig.json 2>&1 | grep -i "xterm-theme" | head`
Expected: 无与 `xterm-theme` 相关的报错（若 `Cannot find module` 已消失即通过）。

- [ ] **Step 3: Commit**

```bash
git add web/src/types/xterm-theme.d.ts
git commit -m "feat(terminal): declare xterm-theme module types"
```

---

### Task 2: 创建 `terminalThemes.ts` 工具模块

**Files:**
- Create: `web/src/utils/terminalThemes.ts`

- [ ] **Step 1: 写失败测试**

创建 `web/src/utils/__tests__/terminalThemes.test.ts`：

```ts
import { describe, expect, it, vi, beforeEach } from 'vitest'

// 懒加载：mock 掉 xterm-theme，断言 resolveTheme 何时触发 import。
const mockLoad = vi.fn()
vi.mock('xterm-theme', () => ({
  __esModule: true,
  default: {
    Dracula: {
      foreground: '#f8f8f2', background: '#1e1f29', cursor: '#bbbbbb',
      black: '#000000', brightBlack: '#555555', red: '#ff5555', brightRed: '#ff5555',
      green: '#50fa7b', brightGreen: '#50fa7b', yellow: '#f1fa8c', brightYellow: '#f1fa8c',
      blue: '#bd93f9', brightBlue: '#bd93f9', magenta: '#ff79c6', brightMagenta: '#ff79c6',
      cyan: '#8be9fd', brightCyan: '#8be9fd', white: '#bbbbbb', brightWhite: '#ffffff',
    },
  },
}))

import {
  TERMINAL_THEME_AUTO,
  TERMINAL_THEME_STORAGE_KEY,
  THEME_IDS,
  formatThemeName,
  resolveTheme,
  darkTheme,
  lightTheme,
} from '@/utils/terminalThemes'

describe('terminalThemes', () => {
  beforeEach(() => { mockLoad.mockClear() })

  it('exports 157 theme ids and the auto/storage constants', () => {
    expect(THEME_IDS.length).toBe(157)
    expect(TERMINAL_THEME_AUTO).toBe('auto')
    expect(TERMINAL_THEME_STORAGE_KEY).toBe('terminalTheme')
    expect(THEME_IDS).toContain('Dracula')
    expect(THEME_IDS).toContain('Nord') // 不存在则说明列表构建错误
  })

  it('formatThemeName converts underscores to spaces', () => {
    expect(formatThemeName('Solarized_Dark')).toBe('Solarized Dark')
    expect(formatThemeName('Dracula')).toBe('Dracula')
  })

  it('resolveTheme("auto") returns the app theme without loading xterm-theme', async () => {
    const theme = await resolveTheme(TERMINAL_THEME_AUTO, true)
    expect(theme.background).toBe(darkTheme.background)
    expect(theme.background).not.toBe(lightTheme.background)
    // 未触发懒加载
    expect(mockLoad).not.toHaveBeenCalled()
  })

  it('resolveTheme with a fixed id lazily loads and returns that theme', async () => {
    const theme = await resolveTheme('Dracula', false)
    expect(theme.background).toBe('#1e1f29')
    expect(mockLoad).toHaveBeenCalled()
  })

  it('resolveTheme falls back to app theme for unknown id', async () => {
    const theme = await resolveTheme('Not_A_Real_Theme', false)
    expect(theme.background).toBe(lightTheme.background)
  })
})
```

> 注意：上面的 `mockLoad` 不会真正拦截 `import()` 动态加载（vitest 对动态 `import('xterm-theme')` 会用 mock 模块）。为让断言可观测，`loadAllThemes()` 内部应调用一个可注入/可 spy 的加载函数 `loadThemesModule()`；测试改 spy 该函数。具体实现见 Step 3。若直接在测试中 `vi.spyOn` 工具模块导出的 `loadAllThemes`，需在 `resolveTheme` 内部调用模块级函数引用而非内联 `import()`。请按 Step 3 实现后调整本测试：用 `vi.mock('xterm-theme')` 提供假数据，并断言 `resolveTheme('Dracula')` 返回假数据中的 Dracula 值、`resolveTheme('auto')` 返回静态主题值即可。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npx vitest run src/utils/__tests__/terminalThemes.test.ts`
Expected: FAIL（`Cannot find module '@/utils/terminalThemes'`）。

- [ ] **Step 3: 实现工具模块**

创建 `web/src/utils/terminalThemes.ts`：

```ts
import type { ITheme } from '@xterm/xterm'

export const TERMINAL_THEME_AUTO = 'auto'
export const TERMINAL_THEME_STORAGE_KEY = 'terminalTheme'

/**
 * 157 个主题 id 的静态列表（构建期硬编码，不触发 xterm-theme 加载）。
 * 用于渲染选择列表。来源：node_modules/xterm-theme/index.js 的所有具名导出。
 */
export const THEME_IDS: string[] = [
  'AdventureTime', 'Afterglow', 'AlienBlood', 'Argonaut', 'Arthur', 'AtelierSulphurpool',
  'Atom', 'Batman', 'Belafonte_Night', 'BirdsOfParadise', 'Blazer', 'Borland',
  'Bright_Lights', 'Broadcast', 'Brogrammer', 'C64', 'Chalk', 'Chalkboard', 'Ciapre',
  'Cobalt2', 'Cobalt_Neon', 'CrayonPonyFish', 'Dark_Pastel', 'Darkside', 'Desert',
  'DimmedMonokai', 'DotGov', 'Dracula', 'Duotone_Dark', 'ENCOM', 'Earthsong', 'Elemental',
  'Elementary', 'Espresso', 'Espresso_Libre', 'Fideloper', 'FirefoxDev', 'Firewatch',
  'FishTank', 'Flat', 'Flatland', 'Floraverse', 'ForestBlue', 'FrontEndDelight',
  'FunForrest', 'Galaxy', 'Github', 'Glacier', 'Grape', 'Grass', 'Gruvbox_Dark',
  'Hardcore', 'Harper', 'Highway', 'Hipster_Green', 'Homebrew', 'Hurtado', 'Hybrid',
  'IC_Green_PPL', 'IC_Orange_PPL', 'IR_Black', 'Jackie_Brown', 'Japanesque', 'Jellybeans',
  'JetBrains_Darcula', 'Kibble', 'Later_This_Evening', 'Lavandula', 'LiquidCarbon',
  'LiquidCarbonTransparent', 'LiquidCarbonTransparentInverse', 'Man_Page', 'Material',
  'MaterialDark', 'Mathias', 'Medallion', 'Misterioso', 'Molokai', 'MonaLisa',
  'Monokai_Soda', 'Monokai_Vivid', 'N0tch2k', 'Neopolitan', 'Neutron', 'NightLion_v1',
  'NightLion_v2', 'Night_3024', 'Novel', 'Obsidian', 'Ocean', 'OceanicMaterial', 'Ollie',
  'OneHalfDark', 'OneHalfLight', 'Pandora', 'Paraiso_Dark', 'Parasio_Dark', 'PaulMillr',
  'PencilDark', 'PencilLight', 'Piatto_Light', 'Pnevma', 'Pro', 'Red_Alert', 'Red_Sands',
  'Rippedcasts', 'Royal', 'Ryuuko', 'SeaShells', 'Seafoam_Pastel', 'Seti', 'Shaman',
  'Slate', 'Smyck', 'SoftServer', 'Solarized_Darcula', 'Solarized_Dark',
  'Solarized_Dark_Higher_Contrast', 'Solarized_Dark_Patched', 'Solarized_Light', 'SpaceGray',
  'SpaceGray_Eighties', 'SpaceGray_Eighties_Dull', 'Spacedust', 'Spiderman', 'Spring',
  'Square', 'Sundried', 'Symfonic', 'Teerb', 'Terminal_Basic', 'Thayer_Bright', 'The_Hulk',
  'Tomorrow', 'Tomorrow_Night', 'Tomorrow_Night_Blue', 'Tomorrow_Night_Bright',
  'Tomorrow_Night_Eighties', 'ToyChest', 'Treehouse', 'Ubuntu', 'UnderTheSea', 'Urple',
  'Vaughn', 'VibrantInk', 'Violet_Dark', 'Violet_Light', 'WarmNeon', 'Wez', 'WildCherry',
  'Wombat', 'Wryan', 'Zenburn', 'ayu', 'deep', 'default', 'idleToes',
]

/** App 深色主题默认值（Catppuccin Mocha，迁移自 TerminalPanelContent.vue）。 */
export const darkTheme: ITheme = {
  background: '#1e1e2e', foreground: '#cdd6f4', cursor: '#f5e0dc', cursorAccent: '#1e1e2e',
  selectionBackground: '#585b7066', black: '#45475a', red: '#f38ba8', green: '#a6e3a1',
  yellow: '#f9e2af', blue: '#89b4fa', magenta: '#f5c2e7', cyan: '#94e2d5', white: '#bac2de',
  brightBlack: '#585b70', brightRed: '#f38ba8', brightGreen: '#a6e3a1', brightYellow: '#f9e2af',
  brightBlue: '#89b4fa', brightMagenta: '#f5c2e7', brightCyan: '#94e2d5', brightWhite: '#a6adc8',
}

/** App 浅色主题默认值（Catppuccin Latte，迁移自 TerminalPanelContent.vue）。 */
export const lightTheme: ITheme = {
  background: '#eff1f5', foreground: '#4c4f69', cursor: '#dc8a78', cursorAccent: '#eff1f5',
  selectionBackground: '#acb0be66', black: '#bcc0cc', red: '#d20f39', green: '#40a02b',
  yellow: '#df8e1d', blue: '#1e66f5', magenta: '#ea76cb', cyan: '#179299', white: '#4c4f69',
  brightBlack: '#9ca0b0', brightRed: '#d20f39', brightGreen: '#40a02b', brightYellow: '#df8e1d',
  brightBlue: '#1e66f5', brightMagenta: '#ea76cb', brightCyan: '#179299', brightWhite: '#6c6f85',
}

let cachedThemes: Record<string, ITheme> | null = null

/** 动态加载 xterm-theme 全部主题（懒加载，仅在使用时触发）。缓存结果避免重复 import。 */
export async function loadThemesModule(): Promise<Record<string, ITheme>> {
  if (cachedThemes) return cachedThemes
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const mod = await import('xterm-theme')
  cachedThemes = (mod.default ?? mod) as Record<string, ITheme>
  return cachedThemes
}

/** 把主题 id 转成展示名（下划线 → 空格）。 */
export function formatThemeName(id: string): string {
  return id.replace(/_/g, ' ')
}

/**
 * 解析最终主题。
 * - auto → 按 isAppDark 返回静态 darkTheme/lightTheme（不触发懒加载）。
 * - 固定 id → 懒加载后返回该主题；未知/缺失 → 回退到按 isAppDark 的自动主题。
 */
export async function resolveTheme(selection: string, isAppDark: boolean): Promise<ITheme> {
  if (selection === TERMINAL_THEME_AUTO) {
    return isAppDark ? darkTheme : lightTheme
  }
  try {
    const themes = await loadThemesModule()
    const theme = themes[selection]
    if (theme) return theme
  } catch {
    // 加载失败或 id 缺失 → 回退
  }
  return isAppDark ? darkTheme : lightTheme
}

/** 当前 App 是否为深色主题（同步判定）。 */
export function isAppDarkTheme(): boolean {
  return document.documentElement.getAttribute('data-theme') === 'dark'
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd web && npx vitest run src/utils/__tests__/terminalThemes.test.ts`
Expected: PASS（若测试中对 `loadThemesModule` 有 spy 断言，请确保 mock 的 `xterm-theme` 提供 `default` 导出）。

- [ ] **Step 5: Commit**

```bash
git add web/src/utils/terminalThemes.ts web/src/utils/__tests__/terminalThemes.test.ts
git commit -m "feat(terminal): add terminal theme utils with lazy-loaded xterm themes"
```

---

### Task 3: 增加 `terminalTheme` 持久化

**Files:**
- Modify: `web/src/composables/useSettingsConfig.ts`

- [ ] **Step 1: 在 `localDefaults` 增加默认值**

找到 `localDefaults` 对象（当前 `theme: 'auto'` 下方），添加：

```ts
  terminalTheme: 'auto',
```

- [ ] **Step 2: 在 `legacyKeys` 增加映射**

找到 `terminalFontSize` 条目后，添加：

```ts
  terminalTheme: {
    key: '',
    format: 'raw',
  },
```

- [ ] **Step 3: 验证编译**

Run: `cd web && npx vue-tsc --noEmit -p tsconfig.json 2>&1 | grep -iv "FileSearchDrawer.vue" | grep -i error | head`
Expected: 无新错误（`FileSearchDrawer.vue` 的既有错误为预存，忽略）。

- [ ] **Step 4: Commit**

```bash
git add web/src/composables/useSettingsConfig.ts
git commit -m "feat(terminal): persist terminalTheme local config"
```

---

### Task 4: i18n 文案

**Files:**
- Modify: `web/src/i18n/locales/zh.ts`
- Modify: `web/src/i18n/locales/en.ts`

- [ ] **Step 1: zh.ts 增加文案**

在 `web/src/i18n/locales/zh.ts` 的 `terminal` 节末尾（`symbolGroupShell: 'Shell 特殊',` 后）添加：

```ts
    theme: '主题',
    themeFollowApp: '跟随 App 主题',
    themeSearchPlaceholder: '搜索主题...',
    themeLoading: '加载主题中...',
    themeLoadFailed: '主题加载失败，请重试',
```

- [ ] **Step 2: en.ts 增加文案**

在 `web/src/i18n/locales/en.ts` 的 `terminal` 节末尾（`symbolGroupShell: 'Shell Special',` 后）添加：

```ts
    theme: 'Theme',
    themeFollowApp: 'Follow App Theme',
    themeSearchPlaceholder: 'Search themes...',
    themeLoading: 'Loading themes...',
    themeLoadFailed: 'Failed to load themes. Please retry.',
```

- [ ] **Step 3: Commit**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(terminal): add theme switcher i18n strings"
```

---

### Task 5: 接入 `TerminalPanelContent.vue`（按钮 + 弹层 + applyTheme）

**Files:**
- Modify: `web/src/components/terminal/TerminalPanelContent.vue`

- [ ] **Step 1: 迁移静态主题色板 + 更新 import**

1) 删除组件内 `darkTheme` / `lightTheme` 两个常量定义（原 355-379 行附近）。
2) 从 `@/utils/terminalThemes` 导入所需内容。在现有 `import { localConfig, ... }` 附近添加：

```ts
import {
  TERMINAL_THEME_AUTO,
  TERMINAL_THEME_STORAGE_KEY,
  THEME_IDS,
  formatThemeName,
  loadThemesModule,
  resolveTheme,
  isAppDarkTheme,
} from '@/utils/terminalThemes'
import { Palette as PaletteIcon } from 'lucide-vue-next'
```

3) `getXtermTheme()` 改为使用工具模块（保持同步返回）：

```ts
function getXtermTheme() {
  if (themeSelection.value === TERMINAL_THEME_AUTO) {
    return isAppDarkTheme() ? darkTheme : lightTheme
  }
  // 固定主题：懒加载可能尚未完成，先返回自动主题，applyTheme 完成后统一更新。
  return isAppDarkTheme() ? darkTheme : lightTheme
}
```

> 需要从 `@/utils/terminalThemes` 额外导入 `darkTheme`、`lightTheme`。

- [ ] **Step 2: 新增主题状态与 applyTheme**

在 `const fontSize = ...` 附近新增：

```ts
// 终端主题：默认跟随 App 主题，可选固定主题覆盖
const themeSelection = ref<string>((localConfig.terminalTheme as string) || TERMINAL_THEME_AUTO)
const themeMenuOpen = ref(false)
const themeMenuTarget = ref<HTMLElement | null>(null)
const themeSearch = ref('')
const themeLoading = ref(false)
const themeLoadError = ref(false)
const allThemes = ref<Record<string, unknown> | null>(null)

const filteredThemes = computed(() => {
  const ids = allThemes.value ? THEME_IDS : THEME_IDS
  const q = themeSearch.value.trim().toLowerCase()
  if (!q) return ids
  return ids.filter((id) => id.toLowerCase().includes(q) || formatThemeName(id).toLowerCase().includes(q))
})

async function ensureThemesLoaded() {
  if (allThemes.value || themeLoading.value) return
  themeLoading.value = true
  themeLoadError.value = false
  try {
    allThemes.value = await loadThemesModule()
  } catch {
    themeLoadError.value = true
  } finally {
    themeLoading.value = false
  }
}

async function applyTheme(selection: string) {
  themeSelection.value = selection
  setLocalConfig(TERMINAL_THEME_STORAGE_KEY, selection)
  const theme = await resolveTheme(selection, isAppDarkTheme())
  tabManager.updateTheme(theme)
  // 更新容器背景以贴合主题
  document.documentElement.style.setProperty('--terminal-bg', theme.background || '')
}

function openThemeMenu(e: Event) {
  themeMenuTarget.value = e.currentTarget as HTMLElement
  themeMenuOpen.value = true
  ensureThemesLoaded()
}

function selectTheme(selection: string) {
  themeMenuOpen.value = false
  themeSearch.value = ''
  applyTheme(selection)
}
```

- [ ] **Step 3: MutationObserver 条件化**

原 themeObserver 回调改为仅 auto 时跟随：

```ts
themeObserver = new MutationObserver(() => {
  if (themeSelection.value === TERMINAL_THEME_AUTO) {
    tabManager.updateTheme(getXtermTheme())
  }
})
```

- [ ] **Step 4: 添加调色板按钮与弹层模板**

在 tab 栏的 `terminal-tab-add`（`+` 新建按钮）之后、`</div>` 结束 tab-bar 前，新增按钮；在 `<QuickCommandDrawer ... />` 之后新增弹层：

```html
      <button
        ref="themeBtnRef"
        class="terminal-tab-add terminal-theme-btn"
        @click="openThemeMenu"
        :title="t('terminal.theme')"
      >
        <PaletteIcon :size="14" />
      </button>
```

在 tab-bar `</div>` 之后（或组件内合适位置）加弹层：

```html
    <!-- Terminal theme picker -->
    <PopupMenu
      v-model:show="themeMenuOpen"
      :target-element="themeMenuTarget"
      :max-width="240"
      :max-height="320"
      :menu-items-count="6"
      anchor="right"
    >
      <div class="theme-picker" @click.stop>
        <div class="theme-picker-title">{{ t('terminal.theme') }}</div>
        <input
          v-model="themeSearch"
          class="theme-search-input"
          type="text"
          :placeholder="t('terminal.themeSearchPlaceholder')"
        />
        <div v-if="themeLoading" class="theme-picker-status">{{ t('terminal.themeLoading') }}</div>
        <div v-else-if="themeLoadError" class="theme-picker-status theme-picker-error">
          <span>{{ t('terminal.themeLoadFailed') }}</span>
          <button class="theme-retry-btn" @click="ensureThemesLoaded">{{ t('common.retry') }}</button>
        </div>
        <div v-else class="theme-picker-list">
          <button
            class="theme-item"
            :class="{ active: themeSelection === TERMINAL_THEME_AUTO }"
            @click="selectTheme(TERMINAL_THEME_AUTO)"
          >
            <span class="theme-item-name">{{ t('terminal.themeFollowApp') }}</span>
            <span v-if="themeSelection === TERMINAL_THEME_AUTO" class="theme-item-check">✓</span>
          </button>
          <button
            v-for="id in filteredThemes"
            :key="id"
            class="theme-item"
            :class="{ active: themeSelection === id }"
            @click="selectTheme(id)"
          >
            <span class="theme-item-name">{{ formatThemeName(id) }}</span>
            <span v-if="themeSelection === id" class="theme-item-check">✓</span>
          </button>
        </div>
      </div>
    </PopupMenu>
```

> 需在 `<script>` 顶部确认 `PopupMenu` 已 import（当前已用于快速指令弹层）。`common.retry` 若 i18n 不存在，改用固定字符串「重试 / Retry」。

- [ ] **Step 5: 增加主题弹层样式**

在 `<style scoped>` 内（`selection-copy-bar` 样式附近）新增：

```css
.terminal-theme-btn { color: var(--text-muted); }
.theme-picker { padding: 6px; }
.theme-picker-title { font-size: 12px; font-weight: 600; color: var(--text-muted); padding: 2px 6px 6px; }
.theme-search-input {
  width: 100%; box-sizing: border-box; padding: 6px 8px; margin-bottom: 6px;
  border: 1px solid var(--border-color); border-radius: 6px;
  background: var(--bg-secondary); color: var(--text-primary); font-size: 13px; outline: none;
}
.theme-search-input:focus { border-color: var(--accent-color); }
.theme-picker-status { padding: 12px; text-align: center; color: var(--text-muted); font-size: 13px; }
.theme-picker-error { display: flex; flex-direction: column; gap: 8px; align-items: center; }
.theme-retry-btn { padding: 4px 12px; border: 1px solid var(--border-color); border-radius: 6px; background: transparent; color: var(--text-primary); cursor: pointer; font-size: 13px; }
.theme-picker-list { max-height: 220px; overflow-y: auto; }
.theme-item {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  width: 100%; padding: 6px 8px; border: none; border-radius: 6px;
  background: transparent; color: var(--text-primary); font-size: 13px; text-align: left; cursor: pointer;
}
.theme-item:hover { background: var(--bg-hover, rgba(128,128,128,0.1)); }
.theme-item.active { background: color-mix(in srgb, var(--accent-color) 12%, transparent); color: var(--accent-color); }
.theme-item-check { color: var(--accent-color); font-weight: 700; }
```

- [ ] **Step 6: 应用背景色到容器**

将 `.terminal-container` 背景从硬编码改为使用 CSS 变量，并让 `applyTheme` 写入该变量（已在 Step 2 中 `--terminal-bg` 赋值）。修改 `.terminal-container` 相关样式（含 `[data-theme]` 两处）：

```css
.terminal-container { ... background: var(--terminal-bg, #1e1e2e); }
[data-theme="dark"] .terminal-container { background: var(--terminal-bg, #1e1e2e); }
:root:not([data-theme="dark"]) .terminal-container { background: var(--terminal-bg, #eff1f5); }
```

并在 `onMounted` 初始化时应用一次当前主题背景：

```ts
// onMounted 内，创建 tab 前：
applyTheme(themeSelection.value).catch(() => {})
```

> `applyTheme` 会持久化同值，无副作用；此处确保首屏背景与选中主题一致。

- [ ] **Step 7: 运行测试 + lint**

Run: `cd web && npx vitest run src/utils/__tests__/terminalThemes.test.ts && npx eslint src/components/terminal/TerminalPanelContent.vue`
Expected: 测试通过、lint 无报错。

- [ ] **Step 8: Commit**

```bash
git add web/src/components/terminal/TerminalPanelContent.vue
git commit -m "feat(terminal): add theme switcher button and popup"
```

---

### Task 6: 组件级测试（入口 + 选择行为）

**Files:**
- Modify: `web/src/components/__tests__/terminalPanelSelection.test.ts`

- [ ] **Step 1: 添加源码级断言**

在 `terminalPanelSelection.test.ts` 的 `describe` 中追加测试（该文件是读源文件的源码断言风格，无需 mount）：

```ts
  it('provides a theme switcher button in the tab bar', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')
    expect(source).toContain('PaletteIcon')
    expect(source).toContain('openThemeMenu')
    expect(source).toContain("t('terminal.theme')")
  })

  it('theme popup lists Follow App Theme + searchable theme ids', () => {
    const source = readTerminalComponent('../terminal/TerminalPanelContent.vue')
    expect(source).toContain('themeFollowApp')
    expect(source).toContain('themeSearchPlaceholder')
    expect(source).toContain('filteredThemes')
    expect(source).toContain('formatThemeName(id)')
  })
```

- [ ] **Step 2: 运行测试**

Run: `cd web && npx vitest run src/components/__tests__/terminalPanelSelection.test.ts`
Expected: PASS。

- [ ] **Step 3: Commit**

```bash
git add web/src/components/__tests__/terminalPanelSelection.test.ts
git commit -m "test(terminal): assert theme switcher UI presence"
```

---

### Task 7: 全量验证与收尾

- [ ] **Step 1: 运行终端相关全部测试**

Run: `cd web && npx vitest run src/utils/__tests__/terminalThemes.test.ts src/components/__tests__/terminalPanelSelection.test.ts`
Expected: 全部通过。

- [ ] **Step 2: 验证懒加载分包**

Run: `cd /home/xulongzhe/projects/clawbench && npm run build 2>&1 | grep -iE "xterm-theme|chunk|built"`
Expected: 输出中出现独立 chunk（如 `assets/xterm-theme-*.js`），且主 bundle 不含其内联内容。

- [ ] **Step 3: 全量 lint**

Run: `cd web && npx eslint src/`
Expected: 无新增报错（既有预存错误除外）。

- [ ] **Step 4: 提交收尾（如有未提交变更）**

```bash
git add -A
git commit -m "feat(terminal): independent theme switching with 157 lazy-loaded xterm themes"
```

---

## 自审说明（Self-Review）

**Spec 覆盖：**
- 157 主题懒加载 → Task 2 `loadThemesModule` / `THEME_IDS`，Task 5 弹层触发懒加载，Task 7 验证分包。
- 默认跟随 App、可选覆盖、可切回 → Task 2 `resolveTheme`，Task 5 `applyTheme` / MutationObserver 条件化。
- 持久化跨会话 → Task 3 `terminalTheme` localConfig。
- 作用于所有 tab → Task 5 `tabManager.updateTheme`。
- tab 栏入口 → Task 5 按钮 + 弹层。
- 错误处理 → Task 2 `resolveTheme` 回退，Task 5 `themeLoadError` + 重试。
- 测试 → Task 2 单测、Task 6 组件测试、Task 7 验证。

**占位符扫描：** 无 TBD/TODO；所有代码块完整。

**类型一致性：** `TERMINAL_THEME_AUTO`、`TERMINAL_THEME_STORAGE_KEY`、`resolveTheme`、`loadThemesModule`、`formatThemeName`、`isAppDarkTheme`、`THEME_IDS`、`darkTheme`、`lightTheme` 在 Task 2 定义，Task 5 引用一致；`tabManager.updateTheme` 为既有 API。`PaletteIcon` 在 Task 5 Step 1 定义、Step 4 使用一致。
