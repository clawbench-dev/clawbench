# 表格测试 Demo

## 基础表格

| 姓名 | 年龄 | 城市 |
|------|------|------|
| Alice | 28 | 北京 |
| Bob | 35 | 上海 |
| Carol | 22 | 广州 |

## 对齐方式

| 左对齐 | 居中对齐 | 右对齐 |
|:-------|:--------:|-------:|
| Left | Center | Right |
| Apple | Banana | Cherry |
| 100 | 200 | 300 |

## 宽表格（横向滚动）

| 指标 | Q1 | Q2 | Q3 | Q4 | 同比增长 | 环比增长 | 目标达成率 | 评分 |
|------|-----|-----|-----|-----|---------|---------|-----------|------|
| 收入 | 120万 | 135万 | 148万 | 162万 | +18.5% | +9.5% | 96% | A |
| 用户数 | 5.2万 | 5.8万 | 6.5万 | 7.1万 | +22.3% | +9.2% | 102% | A+ |
| 转化率 | 3.2% | 3.5% | 3.8% | 4.1% | +0.9pp | +0.3pp | 95% | B+ |

## 技术规格表

| 特性 | 描述 | 默认值 | 必选 |
|:-----|:-----|:------:|:----:|
| `port` | 服务监听端口 | `20000` | 否 |
| `dataDir` | 数据存储目录 | `~/.clawbench` | 否 |
| `password` | 访问密码 | `null` | 否 |
| `watchDir` | Markdown 监控目录 | `./docs` | 否 |
| `logLevel` | 日志级别 | `info` | 否 |

## 包含链接和代码的表格

| 命令 | 用途 | 示例 |
|------|------|------|
| `ls` | 列出文件 | `ls -la` |
| `grep` | 搜索文本 | `grep -rn "TODO" .` |
| `git` | 版本控制 | `git log --oneline` |
| [文档](code-block-demo.md) | 代码块示例 | 参见链接 |

## 空单元格

| 项目 | 状态 | 负责人 | 截止日期 |
|------|------|--------|---------|
| 前端重构 | 进行中 | Alice | 2026-09-01 |
| API 优化 | | | 2026-09-15 |
| 性能测试 | 已完成 | Bob | 2026-08-01 |
| 文档更新 | 待开始 | | |

## 嵌套 Markdown 表格

| 类型 | 语法 | 渲染效果 |
|------|------|---------|
| 粗体 | `**bold**` | **bold** |
| 斜体 | `*italic*` | *italic* |
| 删除线 | `~~strike~~` | ~~strike~~ |
| 代码 | `` `code` `` | `code` |

## 长文本表格

| 模块 | 说明 |
|------|------|
| AI Backend | 抽象层统一多种 AI CLI 工具的调用方式，支持 CLI 行解析和 ACP JSON-RPC over stdio 两种传输协议，内置无进度看门狗防止进程挂起 |
| StreamHub | WebSocket 事件通道，实现会话级扇出广播，支持断线重连缓冲回放，保证消息不丢失 |
| RAG Engine | 基于 SQLite + sqlite-vec 的向量存储与 FTS5 全文检索，兼容 OpenAI 嵌入 API，消息聚类使用 Union-Find + Sørensen-Dice 算法 |

## 包含图片的表格

### 图片 + 文字混合

| 图片 | 名称 | 描述 |
|:----:|------|------|
| ![Beauty](../images/img_beauty_001.jpg) | 人物写真 1 | JPEG 格式人物写真 |
| ![Portrait](../images/img_portrait_001.jpg) | 人物肖像 | JPEG 格式人物肖像 |
| ![Beautiful Girl](../images/img_beautiful_girl_001.jpg) | 人物写真 2 | JPEG 格式人物写真 |

### 纯图片表格（图库风格）

| | | |
|:---:|:---:|:---:|
| ![Beauty 1](../images/beauty_001.jpg) | ![Beauty 2](../images/beauty_002.jpg) | ![Chinese Beauty](../images/img_chinese_beauty_001.jpg) |
| ![Portrait](../images/img_portrait_001.jpg) | ![Beautiful Girl](../images/img_beautiful_girl_001.jpg) | ![Full Body](../images/img_beauty_fullbody_001.jpg) |

### 图片 + 规格（产品展示风格）

| 预览 | 名称 | 尺寸 | 格式 |
|:----:|------|------|------|
| ![Portrait](../images/img_portrait_001.jpg) | Portrait 001 | 小 | JPEG |
| ![Beauty](../images/img_beauty_001.jpg) | Beauty 001 | 中 | JPEG |
| ![Full Body](../images/img_beauty_fullbody_001.jpg) | Full Body 001 | 大 | JPEG |

## 单列表格

| 备忘事项 |
|---------|
| 完成单元测试覆盖率达标 |
| 修复终端 TUI 冻结问题 |
| 发布 v2.0 版本 |
