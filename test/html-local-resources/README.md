# 本地资源引用演示

用于验证 **HTML 文件预览**能否加载本地资源。每个资源都带实时判据 —— 加载成功与否由浏览器
实际解码结果决定，而不是「标签存在」。

## 用法

在 ClawBench 文件管理器里打开 `test/html-local-resources/index.html`，切到渲染视图。
表格中 7 项应全部显示 **✓ 已加载**。

## 引用的资源

| 资源 | 引用方式 | 相对谁解析 |
|---|---|---|
| `style.css` | `<link rel=stylesheet>` | 本文档所在目录 |
| `app.js` | `<script type="module">` | 本文档所在目录 |
| `../images/img_cute_cat.svg` | `<img>` / `<object>` | 本文档所在目录的上级 |
| `../images/img_beauty_001.jpg` | `<img>` | 同上 |
| `../images/img_cute_cat.svg` | CSS 里的 `url()` | **CSS 文件自身**所在目录 |
| 内联 `<svg>` | 写在 HTML 里 | 不依赖请求（对照组） |

## 为什么这些判据有区分度

修复前，HTML 预览用 `srcdoc` 内联文档。`about:srcdoc` 没有自己的 URL、继承父页面的 base，
于是所有相对引用都打到**应用根目录**：

```
GET /style.css              → 404   （应为 /api/fs/raw/test/html-local-resources/style.css）
GET /app.js                 → 404
GET /images/img_cute_cat.svg → 404
```

修复后文档以真实 URL 载入，相对引用以文档所在目录为基准解析。两种行为的对照实测：

| | 修复前 | 修复后 |
|---|---|---|
| `style.css` | 404（且 MIME 为 octet-stream 时会被静默丢弃） | 200 `text/css` |
| `app.js`（ES module） | 404，且 octet-stream 会被 MIME 校验拒绝 | 200 `text/javascript` |
| 图片 | 404 | 200，`naturalWidth > 0` |
| `index.html` 本身 | — | 200（不因文件名触发 301） |

## 已知限制

**项目外的 HTML 文件**（绝对路径）不适用本演示的效果：其 URL 形如
`/api/fs/raw/?target=/abs/index.html`，浏览器取 base 时会丢掉查询串，相对引用会解析成
`<项目根>/pic.png` —— 同名文件存在时会**静默加载错误的文件**。因此这类文件回退到
`srcdoc`（即上述修复前的行为）。详见 `docs/spec/features/file-management.md`。

## 安全提示

HTML 预览的 iframe 使用 `sandbox="allow-scripts allow-same-origin"`。`allow-same-origin`
是加载本地子资源所必需的（否则 iframe 是不透明 origin，子资源请求算跨站，
`SameSite=Lax` 的会话 Cookie 不下发）。**代价是被预览的 HTML 以用户会话运行** ——
可调用任意鉴权接口、读写父页面 DOM。预览内容不可信（AI 生成或下载而来），
这是主动取舍，`web/src/components/__tests__/fileViewerSandbox.test.ts` 记录了该决策。
