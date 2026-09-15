# 工具详情「换行切换」状态不持久 — 问题分析与修复建议

> 状态：**待修复**（未实施）
> 日期：2026-09-15
> 发现于：排查 AskUserQuestion 补充信息被清空（见 `2026-09-15-ask-question-answer-state.md`）时的同源排查
> 关联代码：`web/src/utils/renderToolDetail.ts`

---

## 1. 现象

在聊天里展开一个工具调用详情（Edit / Read / Write / Bash / JSON 等），点击右上角的「换行」按钮切到不换行（或切回换行）后：

- 消息列表重渲染（切走再切回、前后台切换、`loadHistory` 刷新）
- 或把工具详情抽屉关掉再打开

**换行状态回到默认值（换行开）**，用户的选择丢失。

## 2. 根因

与 AskUserQuestion 的补充信息是**同一类问题**：状态只存在于 DOM，没有任何状态承载。

### 2.1 状态写入只在 DOM

`renderToolDetail.ts:1419-1425`（`handleToolContentHeaderClick` 的 `wrap` 分支）：

```ts
} else if (action === 'wrap') {
  wrapper.classList.toggle('word-wrap')
  btn.classList.toggle('is-wrapped')
  const isWrapped = wrapper.classList.contains('word-wrap')
  btn.setAttribute('title', isWrapped ? gt('toolDetailBlock.wrapOn') : gt('toolDetailBlock.wrapOff'))
  btn.setAttribute('aria-label', isWrapped ? gt('toolDetailBlock.wrapOn') : gt('toolDetailBlock.wrapOff'))
}
```

`word-wrap` / `is-wrapped` 两个 class 就是全部状态，没有回写到任何 `ref` / store / 全局设置。

### 2.2 默认值硬编码为「换行开」

`renderToolDetail.ts:73`（`toolContentHeaderHtml`）：

```ts
html += `<button class="tool-content-wrap-btn is-wrapped" data-action="wrap" ...>`
```

`is-wrapped` 是写死在 HTML 字符串里的。内容容器同理带 `word-wrap`（见 `renderToolDetail.ts` 各渲染器的 `tool-content-wrap word-wrap`）。

所以每次重新渲染都是「换行开」，用户切到「不换行」的选择必然丢失。

### 2.3 与 AskUserQuestion 的关键区别

| | AskUserQuestion 补充信息 | 工具详情换行切换 |
|---|---|---|
| 丢的是什么 | **用户数据**（用户填的答案） | **视图偏好**（显示方式） |
| 严重程度 | 真 bug，数据丢失 | 小瑕疵，需重新点一次 |
| 正确修法 | 新建状态层持久化答案 | **接上已有的全局设置** |
| 改动性质 | 新增机制 | 复用现有机制 |

**结论：两者不该用同一种修法，也不该混在一个 PR 里。** 换行是「偏好」，仓库里已经有承载偏好的机制，只是没被这个渲染器使用。

## 3. 已有的全局设置（未被使用）

仓库**已经存在**换行偏好的全局设置，文件预览等场景在用：

| 项 | 值 |
|---|---|
| 设置 key | `wordWrap` |
| localStorage key | `clawbench-word-wrap` |
| 默认值 | `true`（`useSettingsConfig.ts:318`） |
| 定义 | `useSettingsConfig.ts:20`（迁移表）、`:98`（设置项） |
| 设置面板 | `settingsFieldMap.ts:236`（`settings.items.wordWrap`） |
| 消费方 | `FileViewer.vue:577,585`（`localConfig.wordWrap`） |

**但 `renderToolDetail.ts` 完全没有引用它** —— 全文 grep `wordWrap` / `clawbench-word-wrap` 零命中。这就是问题所在：偏好有地方存，渲染器却写死默认值。

## 4. 修复建议

### 4.1 主方案：让渲染器读全局设置

1. **渲染时用设置值决定初始 class**：`toolContentHeaderHtml` 不再硬编码 `is-wrapped`，而是根据当前 `wordWrap` 设置输出 `is-wrapped` 或 `is-wrapping-off`；内容容器的 `word-wrap` 同理。
2. **切换时写回设置**：`wrap` 分支除了改 DOM，再调用 `setLocalConfig('wordWrap', isWrapped)`（或 `useSettingsConfig` 的等价写入），让选择跨渲染、跨工具、跨会话保留。
3. **注意作用域**：全局设置是「一处改、处处改」。如果产品上希望「每个工具卡片各自记住」，那就不能复用全局设置，需另建按 card key 的状态（可复用 AskUserQuestion 那套 `askQuestionState` 的模式）。

**建议先确认产品预期**：换行偏好应该是全局的，还是每卡片独立的？从 `FileViewer` 的现有行为看，**全局**更符合仓库既有语义。

### 4.2 涉及文件

| 文件 | 改动 |
|---|---|
| `web/src/utils/renderToolDetail.ts` | `toolContentHeaderHtml` 读设置；`wrap` 分支写设置 |
| `web/src/composables/useSettingsConfig.ts` | 可能需导出读取函数（若尚无同步读取入口） |
| `web/src/utils/__tests__/renderToolDetail.test.ts` | 补回归测试 |

### 4.3 测试要点

- 设置 `wordWrap=false` 时，渲染出的 header **不含** `is-wrapped`、内容容器**不含** `word-wrap`
- 点击换行按钮后，设置被写为切换后的值
- 重新渲染后仍保持用户选择（这是本问题的核心回归点）

## 5. 附带发现：`is-copied` 不是 bug

同文件的复制按钮（`renderToolDetail.ts:1401-1417`）也用 class 表达状态，但它是 **1.5 秒后自动恢复的瞬时反馈**（`setTimeout(..., 1500)`），不是需要持久化的用户选择。**不要一并「修复」。**

## 6. 相关

- 同源排查记录：`docs/plans/2026-09-15-ask-question-answer-state.md`（AskUserQuestion 答案状态，已实施）
- `layoutRefreshKey` 死代码：`ChatPanelContent.vue:262,649,675` 提供并递增，但全仓库**无任何 `inject('layoutRefreshKey')`**。与本问题无关，但同属该区域的清理债。
