# 邮件客户端

左栏 Dock 的「邮件」页签，是一个内置的收发信客户端：配一次 IMAP + SMTP 账号，就能收信、看信、回复、转发、写信。

数据是**全局**的，不带 `project_id`——邮箱属于人，不属于仓库，所以同一个收件箱在每个项目下都能看到。

## 界面形态

左栏默认只占内容区 40%（最窄 320px），放不下「文件夹 / 列表 / 正文」三栏，因此采用**逐层下钻**，和任务、会议面板一致：

```
[ 账号 ▾ ]  [收件箱 12] [已发送] [草稿] [垃圾箱]        ⟳ ✎ ⚙
[ 🔍 搜索邮件… ]
─────────────────────────────────────────────
 ○ 张三        项目周报                  今天 14:20  📎
 ● 李四        关于接口的几点疑问          昨天
─────────────────────────────────────────────
        ↑ 点一行 → 整块换成阅读页（带返回）
```

- **列表页**：账号切换 + 文件夹 chips + 搜索框 + 邮件列表（滚动到底自动加载更早的）。
- **阅读页**：返回 + 操作条（回复 / 全部回复 / 转发 / 星标 / 已读 / 删除），正文，附件。
- **写邮件**：`ModalDialog` 宽档弹窗，不挤在窄栏里。
- **没配账号**：整块是空态，按钮跳转到「设置 → 邮箱账号」。

**账号配置只有一个入口**：设置面板的一级菜单「邮箱账号」（`components/settings/MailSetting.vue`）。邮件面板不再内嵌账号表单——两处配置会让同一份账号在两种布局下各写一遍，校验和预设处理迟早漂移。面板右上角的齿轮与空态按钮都只是跳到设置。

## 技术选型

| 用途 | 库 |
|---|---|
| 收信 | `github.com/emersion/go-imap/v2`（aerc 邮件客户端用的同一套） |
| 解 MIME | `github.com/jhillyerd/enmime`（字符集 / 附件 / 内嵌图 / quoted-printable） |
| 发信 | `github.com/wneessen/go-mail` |
| 协议层测试 | `go-imap/v2/imapserver/imapmemserver`（内存版 IMAP 服务器）+ `emersion/go-smtp`（假 SMTP 服务器） |

全部纯 Go、无 CGO，与 `modernc.org/sqlite` 的取舍一致，交叉编译不受影响。

## 分层

| 位置 | 职责 |
|---|---|
| `internal/mail/` | 纯协议层：连接、LIST/FETCH/STORE/MOVE/APPEND、MIME 解析、发信、错误归类。不认识数据库和 HTTP |
| `internal/service/mail_*.go` | 持久化与编排：账号、同步、按需拉正文、发信、后台 worker |
| `internal/handler/mail.go` | `/api/mail/*` 端点与错误 → i18n 键的映射 |
| `web/src/components/mail/` | 面板、列表、阅读、写信、账号表单 |
| `web/src/composables/useMail*.ts` | 账号 / 列表 / 未读角标状态 |

`mail.Client` 是一个接口，`mail.Dialer` 可替换——service 层的测试用内存假客户端跑，协议层用真实的内存 IMAP 服务器跑。

## 同步策略

1. 后台 worker **每 3 分钟**扫一遍所有 `enabled` 账号（`mail_worker.go`），手动「收信」按钮同步执行同一套逻辑。
2. 每个文件夹先 `STATUS` 拿 `UIDVALIDITY / UIDNEXT / MESSAGES / UNSEEN`；`UIDVALIDITY` 变了就**清空该文件夹缓存**重来（IMAP 的硬规则：旧 UID 不再指向同一批邮件）。
3. 每次同步拉**最新 100 封**的头（`ENVELOPE + FLAGS + BODYSTRUCTURE + INTERNALDATE`），已存在的 UID 不覆盖（正文和本地标志更新）。
4. 再刷新缓存里最近 200 封的标志位，让手机上读过的邮件在这里也变成已读。
5. 列表滑到底时，先走本地 keyset 分页；本地空了但服务端还有（`cachedCount < total`），才 `POST /api/mail/folders/{id}/load-older` 按 UID 往前拉一页（一次 IMAP 往返）。
6. 同步时顺带**预取最新 10 封的正文**（限 300KB 以内），列表的摘要行和第一次点开的即时感都来自这里。

正文和附件**不进同步流程**：打开哪封拉哪封，附件在那一刻解析落盘到 `{DataDir}/mail/attachments/<邮件行id>/`。

## 发送

`POST /api/mail/send` → `go-mail` 发信 → 成功后尝试 `APPEND` 一份到「已发送」。APPEND 失败**不影响**发送结果（已经发出去了），只记日志。

回复/转发由 `GET /api/mail/messages/{id}/reply-context?mode=` 生成默认值：收件人、`Re:`/`Fwd:` 主题、`In-Reply-To`/`References` 线程头、引用正文。

草稿只存本地（`mail_drafts`）：同步到服务器 Drafts 需要另一套 APPEND/EXPUNGE 状态机，而这个功能唯一的承诺是「别丢我打的字」。

## HTML 正文的安全模型

邮件正文是不可信内容，三层独立防线：

1. **DOMPurify** 清洗：去脚本、事件属性，并禁掉 `<form>/<input>/<button>/<iframe>` 等能伪装成应用控件或自行加载内容的标签。
2. **`<iframe sandbox>`**：`allow-same-origin allow-popups allow-popups-to-escape-sandbox`，**没有 `allow-scripts`**——即使清洗漏了，脚本也跑不起来。`allow-same-origin` 只为了让父页面能改写链接，不解除脚本限制。
3. **文档内 CSP**：`default-src 'none'; style-src 'unsafe-inline'; img-src 'self' data:`。**远程图片默认不放行**（追踪像素），用户点「显示图片」才把 `https: http:` 加进 `img-src`。

`cid:` 内嵌图在服务端就重写成 `/api/mail/messages/{id}/attachments/{aid}`，浏览器永远看不到解析不了的 cid URL。

## 交给对话

邮件不是孤岛：阅读页能把整封邮件交给 AI，附件也能进聊天。两者都走已有的 [引用与附件体系](quotes-and-attachments.md)，邮件本身不新增任何通道。

| 动作 | 产物 | 说明 |
|---|---|---|
| 「引用对话」 | 一张 `kind=quote` 卡片 | `text` = 纯文本正文（HTML 邮件回落摘要，超长裁剪）；标签 = `主题 · 发件人`；`language`/`sourceKind` = `mail`，定位字段 `mailId` |
| 附件行「添加到对话」 | 普通文件附件 | 字节重新上传进项目 `uploads/`，再走常规附件管道 |

为什么附件要重传：聊天附件必须位于项目目录内（发送时会 `os.Stat` + 越界校验），而邮件附件在 `{DataDir}/mail/attachments/` 下。重传让大小限制、进度条、去重全部复用现有逻辑，代价是同一份字节多走一次网络。

为什么正文要内联：定位字段 `mailId` 是给**客户端跳转**用的，AI 没有读取邮箱的工具；只给标签（「周报 · 张三」）等于什么也没给。所以引用时把正文一起带上。

「引用对话」打开的是共享引用浮条（composer 模式），因此可以先写批注、也可以换目标会话；卡片上的「跳转来源」发 `clawbench-open-mail` 事件，App 切到邮件页签、面板打开阅读页。

## 凭据

密码/授权码**明文**存在 `mail_accounts.password`，和 `providers.api_key` 是同一个取舍：仓库没有密钥管理设施，数据库文件靠 0600 保护。接口默认只回 `hasPassword`，只有显式 `GET /api/mail/accounts/{id}?reveal=1` 才返回明文（编辑表单用它回填）。

## 错误呈现

协议错误被归类成 `auth / connect / tls / timeout / server`（`internal/mail/errors.go`），handler 映射成 i18n 键（`MailAuthFailed` 等），前端显示「邮箱登录失败，请检查账号和授权码」而不是一串 IMAP 协议文本。同一账号并发同步返回 409（`MailSyncBusy`）。

## 与任务系统联动

任务可以选「邮件触发」：指定一个账号（和可选文件夹），配一组门控条件（发件人 / 主题 / 正文包含，必须带附件），新邮件满足全部条件时自动跑一次 AI 回合。

- 只对**任务保存之后新到**的邮件生效；账号首次同步的历史邮件不触发。
- 同一封邮件对同一个任务只触发一次（`mail_task_triggers` 去重表），与重复同步、重启无关。
- 单次收信每个任务最多触发 5 次，避免一次收到上百封时启动上百个 AI 回合。
- 触发时把邮件上下文前置到 prompt（账号、文件夹、发件人、收件人、主题、时间、正文摘要），也支持 `{{mailSubject}}` 等占位符。

实现见 `internal/service/mail_task_trigger.go`，配置界面在任务表单的「触发方式 → 邮件」。

## 明确不做

- **OAuth2 网页授权**：Gmail / Outlook 要用「应用专用密码」。
- **Exchange / EWS / Microsoft Graph**。
- 邮件规则、过滤器、签名、通讯录、日历邀请解析。
- 服务端全文搜索：`q` 只在本地缓存的主题 / 发件人 / 摘要上做 LIKE。
- IMAP IDLE 实时推送：用 3 分钟轮询代替，省一条常驻连接。
- 与 AI 的联动（把邮件引用到聊天、让智能体代读代写）——后续单独做。

## 验证

```bash
go test ./internal/mail/...                                  # 协议层 + MIME 解析
go test ./internal/service/ -run 'Mail|Sync|ListMail|SendMail'
go test ./internal/handler/ -run 'TestServeMail|TestOpenAPISpec'
npx vitest run web/src/components/mail web/src/composables/__tests__/useMailMessages.test.ts web/src/utils/__tests__/mailHtml.test.ts
```

真实收发需要自己的邮箱实测（QQ / 163 用授权码，不是登录密码）。
