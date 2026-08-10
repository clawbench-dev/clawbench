# ClawBench 桌面版下载入口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增后端 `/api/desktop/latest` 端点（返回各平台桌面版下载 URL），并在 PC 浏览器欢迎界面与配置页加"下载桌面版"按钮。

**Architecture:** 后端复用 `internal/service/upgrade.go` 的 npm registry 查询逻辑（`getRegistryBase`/npmmirror 改写），为 5 个桌面平台包 `@xulongzhe/clawbench-desktop-<os>-<arch>` 查询最新版，返回 `{version, downloads:{win32-x64,…}}`。前端用 `usePlatformDetect.isPC` + UA/arch 检测显示按钮，点击走 `downloadByUrl`。

**Tech Stack:** Go / Vue 3 / TypeScript。

参考 spec：`docs/superpowers/specs/2026-08-10-clawbench-electron-design.md` §8。前置：Plan 1（前端桥）已完成，Plan 3（Electron desktop）已完成（发布到 npm 的桌面包名已定）。

---

## 关键设计

1. **后端**：在 `internal/service` 新增 `FetchDesktopLatest()`（镜像 `fetchUpgradeInfo`，查询 5 个桌面包），`internal/handler` 新增 `ServeDesktopLatest()`（公开端点，无鉴权，返回 JSON）。包名映射与 npmmirror 改写必须与 Plan 3 的 `desktop/src/shared/registry.ts` 一致。
2. **前端 OS→arch**：按钮按当前平台显示；arch 用 `navigator.userAgentData`/`navigator.platform` 检测，darwin 优先 `arm64`，检测不到回退 `x64`。
3. **下载**：Electron 内走 `ClawBenchNative.downloadUrl`；Web 走 `<a>`（`downloadByUrl` 已支持两者）。

---

## 任务分解

### Task 1: 后端 `/api/desktop/latest`

**Files:**
- Create: `internal/service/desktop_upgrade.go`
- Test: `internal/service/desktop_upgrade_test.go`
- Create: `internal/handler/desktop.go`
- Test: `internal/handler/desktop_test.go`
- Modify: `internal/handler/handler.go`

- [ ] **Step 1: `internal/service/desktop_upgrade.go`（镜像 upgrade.go）**

```go
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// desktopPlatformPkg maps GOOS/GOARCH to the desktop npm platform package,
// mirroring desktop/src/shared/registry.ts.
var desktopPlatformPkg = map[string]string{
	"linux/amd64":   "@xulongzhe/clawbench-desktop-linux-x64",
	"linux/arm64":   "@xulongzhe/clawbench-desktop-linux-arm64",
	"darwin/amd64":  "@xulongzhe/clawbench-desktop-darwin-x64",
	"darwin/arm64":  "@xulongzhe/clawbench-desktop-darwin-arm64",
	"windows/amd64": "@xulongzhe/clawbench-desktop-win32-x64",
}

// desktopDownloadKey is the response key for each platform (matches preload arch keys).
var desktopDownloadKey = map[string]string{
	"linux/amd64":   "linux-x64",
	"linux/arm64":   "linux-arm64",
	"darwin/amd64":  "darwin-x64",
	"darwin/arm64":  "darwin-arm64",
	"windows/amd64": "win32-x64",
}

// DesktopLatestResult is the response of GET /api/desktop/latest.
type DesktopLatestResult struct {
	Version   string            `json:"version"`
	Downloads map[string]string `json:"downloads"`
}

// fetchDesktopLatestFrom queries the npm registry base for each desktop platform
// package and returns the latest version plus per-platform tarball URLs.
// base is injectable for tests (httptest server).
func fetchDesktopLatestFrom(base string) (*DesktopLatestResult, error) {
	res := &DesktopLatestResult{Downloads: make(map[string]string)}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for osArch, pkg := range desktopPlatformPkg {
		url := fmt.Sprintf("%s/%s/latest", base, pkg)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := upgradeHTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		var npmResp npmRegistryResponse
		if err := json.NewDecoder(resp.Body).Decode(&npmResp); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		tarball := npmResp.Dist.Tarball
		if tarball == "" {
			continue
		}
		if res.Version == "" || npmResp.Version > res.Version {
			res.Version = npmResp.Version
		}
		res.Downloads[desktopDownloadKey[osArch]] = rewriteTarballURL(tarball, base)
	}
	return res, nil
}

// FetchDesktopLatest queries the npm registry for the current region.
func FetchDesktopLatest() (*DesktopLatestResult, error) {
	return fetchDesktopLatestFrom(getRegistryBase())
}

// rewriteTarballURL points the tarball at the same registry base used for the query.
func rewriteTarballURL(tarball, base string) string {
	const npmjs = "https://registry.npmjs.org"
	if base != npmjs && len(tarball) > len(npmjs) && tarball[:len(npmjs)] == npmjs {
		return base + tarball[len(npmjs):]
	}
	return tarball
}
```

> 说明：`getRegistryBase()`、`npmRegistryResponse`、`upgradeHTTPClient` 已在 `internal/service/upgrade.go` 定义，直接复用。核心逻辑 `fetchDesktopLatestFrom(base)` 可注入 registry base 供测试。

- [ ] **Step 2: `internal/service/desktop_upgrade_test.go`（mock HTTP）**

```go
package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchDesktopLatestFrom(t *testing.T) {
	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pkg := r.URL.Path
		version := "0.1.0"
		tarball := ""
		switch pkg {
		case "/@xulongzhe/clawbench-desktop-win32-x64/latest":
			version = "0.1.0"
			tarball = "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-win32-x64/-/win-0.1.0.tgz"
		case "/@xulongzhe/clawbench-desktop-darwin-arm64/latest":
			version = "0.1.0"
			tarball = "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-darwin-arm64/-/mac-0.1.0.tgz"
		case "/@xulongzhe/clawbench-desktop-linux-x64/latest":
			version = "0.2.0"
			tarball = "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-linux-x64/-/linux-0.2.0.tgz"
		default:
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"` + version + `","dist":{"tarball":"` + tarball + `","integrity":""}}`))
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()
	res, err := fetchDesktopLatestFrom(ts.URL)
	require.NoError(t, err)
	require.NotNil(t, res)
	// Version is the max across platforms.
	assert.Equal(t, "0.2.0", res.Version)
	// All three requested platforms present; tarballs rewritten from npmjs base to ts.URL.
	require.Contains(t, res.Downloads, "win32-x64")
	require.Contains(t, res.Downloads, "darwin-arm64")
	require.Contains(t, res.Downloads, "linux-x64")
	assert.Contains(t, res.Downloads["win32-x64"], ts.URL)
	assert.Contains(t, res.Downloads["darwin-arm64"], ts.URL)
	assert.Contains(t, res.Downloads["linux-x64"], ts.URL)
}
```

> 说明：`fetchDesktopLatestFrom(ts.URL)` 直接打 mock server，`rewriteTarballURL` 会把 npmjs 前缀改写成 ts.URL，故断言 `Contains(ts.URL)` 真实验证改写逻辑。

- [ ] **Step 3: `internal/handler/desktop.go`**

```go
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"clawbench/internal/service"
)

// fetchDesktopLatest is injectable for tests.
var fetchDesktopLatest = service.FetchDesktopLatest

// ServeDesktopLatest returns the latest desktop app version and per-platform
// download URLs. Public endpoint — no auth required.
func ServeDesktopLatest(w http.ResponseWriter, r *http.Request) {
	res, err := fetchDesktopLatest()
	if err != nil {
		slog.Error("desktop latest: fetch failed", "error", err)
		http.Error(w, "failed to fetch latest desktop version", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(res)
}
```

- [ ] **Step 4: 注册路由**

在 `internal/handler/handler.go` 的 `/api/apk`（第 318 行）旁加：

```go
	register("/api/desktop/latest", ServeDesktopLatest)
```

- [ ] **Step 5: `internal/handler/desktop_test.go`（注入 stub）**

```go
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeDesktopLatest(t *testing.T) {
	orig := fetchDesktopLatest
	defer func() { fetchDesktopLatest = orig }()

	fetchDesktopLatest = func() (*service.DesktopLatestResult, error) {
		return &service.DesktopLatestResult{Version: "0.2.0", Downloads: map[string]string{"win32-x64": "https://npm/t.tgz"}}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/desktop/latest", nil)
	rec := httptest.NewRecorder()
	ServeDesktopLatest(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body service.DesktopLatestResult
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "0.2.0", body.Version)
	assert.Equal(t, "https://npm/t.tgz", body.Downloads["win32-x64"])
}
```

- [ ] **Step 6: 运行 Go 测试**

```bash
go test ./internal/service/ -run TestFetchDesktopLatestFrom -v
go test ./internal/handler/ -run TestServeDesktopLatest -v
go build ./...
```
Expected: 全部 PASS，构建成功。

- [ ] **Step 7: 提交**

```bash
git add internal/service/desktop_upgrade.go internal/service/desktop_upgrade_test.go internal/handler/desktop.go internal/handler/desktop_test.go internal/handler/handler.go
git commit -m "feat(server): add /api/desktop/latest endpoint for desktop downloads"
```

---

### Task 2: 前端下载按钮（欢迎页 + 配置页）

**Files:**
- Create: `web/src/composables/useDesktopDownload.ts`
- Test: `web/src/composables/__tests__/useDesktopDownload.test.ts`
- Modify: `web/src/components/WelcomeOverlay.vue`
- Modify: `web/src/components/settings/SettingsCategory.vue`
- Modify: `web/src/i18n/`（下载相关文案，如存在则补；否则用已有文案）

- [ ] **Step 1: `web/src/composables/useDesktopDownload.ts`**

```ts
import { ref } from 'vue'
import { useAppMode } from './useAppMode'
import { isAndroidUA, isIOSUA } from './usePlatformDetect'
import { apiGet } from '@/utils/api'
import { downloadByUrl } from '@/utils/download'

const TAG = 'DesktopDownload'

interface DesktopLatest {
  version: string
  downloads: Record<string, string>
}

/** Detect the current desktop OS+arch platform key, mirroring spec §8.1. */
export function detectPlatformKey(): string {
  const ua = navigator.userAgent
  const archHint = (navigator as unknown as { userAgentData?: { platform?: string; architecture?: string } }).userAgentData
  if (/Windows/i.test(ua)) return 'win32-x64'
  if (/Macintosh|Mac OS X|Mac/i.test(ua)) {
    // Apple Silicon when ARM64, else x64
    const isArm = archHint?.architecture === 'arm' || /arm64|aarch64/i.test(ua)
    return isArm ? 'darwin-arm64' : 'darwin-x64'
  }
  if (/Linux/i.test(ua)) {
    const isArm = archHint?.architecture === 'arm' || /arm64|aarch64/i.test(ua)
    return isArm ? 'linux-arm64' : 'linux-x64'
  }
  return ''
}

export function useDesktopDownload() {
  const { isAppMode } = useAppMode()
  const latest = ref<DesktopLatest | null>(null)
  const loading = ref(false)

  // PC browser, not native app, not mobile browser
  const isDesktop = !isAppMode.value && !isAndroidUA && !isIOSUA

  async function loadLatest(): Promise<void> {
    if (!isDesktop) return
    loading.value = true
    try {
      const data = await apiGet<DesktopLatest>('/api/desktop/latest')
      latest.value = data
    } catch {
      latest.value = null
    } finally {
      loading.value = false
    }
  }

  /** URL for the current platform, or empty if unavailable. */
  function currentDownloadUrl(): string {
    const key = detectPlatformKey()
    if (!key || !latest.value) return ''
    return latest.value.downloads[key] || ''
  }

  function downloadDesktop(): void {
    const url = currentDownloadUrl()
    if (!url) return
    downloadByUrl(url, `clawbench-desktop-${latest.value?.version || 'latest'}.tgz`)
  }

  return { latest, loading, isDesktop, loadLatest, currentDownloadUrl, downloadDesktop }
}
```

- [ ] **Step 2: `web/src/composables/__tests__/useDesktopDownload.test.ts`**

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { detectPlatformKey, useDesktopDownload } from '../useDesktopDownload'

const originalUA = navigator.userAgent
const originalUserAgentData = (navigator as unknown as { userAgentData?: unknown }).userAgentData

function setUA(ua: string, arch?: string) {
  Object.defineProperty(navigator, 'userAgent', { value: ua, configurable: true })
  if (arch) {
    Object.defineProperty(navigator, 'userAgentData', { value: { architecture: arch, platform: 'x' }, configurable: true })
  } else {
    Object.defineProperty(navigator, 'userAgentData', { value: undefined, configurable: true })
  }
}

beforeEach(() => { setUA(originalUA) })
afterEach(() => {
  Object.defineProperty(navigator, 'userAgent', { value: originalUA, configurable: true })
  Object.defineProperty(navigator, 'userAgentData', { value: originalUserAgentData, configurable: true })
  vi.restoreAllMocks()
})

describe('detectPlatformKey', () => {
  it('detects windows', () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    expect(detectPlatformKey()).toBe('win32-x64')
  })
  it('detects intel mac', () => {
    setUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')
    expect(detectPlatformKey()).toBe('darwin-x64')
  })
  it('detects apple silicon mac via userAgentData', () => {
    setUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 'arm')
    expect(detectPlatformKey()).toBe('darwin-arm64')
  })
  it('detects linux x64', () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    expect(detectPlatformKey()).toBe('linux-x64')
  })
  it('returns empty for unknown', () => {
    setUA('Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X)')
    expect(detectPlatformKey()).toBe('')
  })
})
```

> 说明：`useDesktopDownload` 的 `loadLatest` 依赖 apiGet，可在组件层验证；composable 单测聚焦纯函数 `detectPlatformKey`（无副作用、可测）。若需要测 `downloadDesktop`/`currentDownloadUrl`，可注入 `latest`，但保持本测试聚焦纯函数 + 平台判定。

- [ ] **Step 3: `WelcomeOverlay.vue` 加下载按钮**

在 `showInstallSection` 计算属性的 PWA/APK 行附近，加一个"桌面版下载"行。在 `<script>` 中引入：

```ts
import { useDesktopDownload } from '@/composables/useDesktopDownload'
const desktopDownload = useDesktopDownload()
onMounted(() => { desktopDownload.loadLatest() })
```

模板内（仿现有 `welcome-install-row`）：

```html
<div v-if="desktopDownload.isDesktop && desktopDownload.currentDownloadUrl()" class="welcome-install-row" role="button" tabindex="0" @click="desktopDownload.downloadDesktop()" @keydown.enter="desktopDownload.downloadDesktop()">
  <span class="install-icon">🖥️</span>
  <span>{{ t('pwa.downloadDesktopApp') }}</span>
</div>
```

并把该行计入 `showInstallSection` 判断（把 `desktopDownload.isDesktop && !!desktopDownload.currentDownloadUrl()` 并入 `showInstallSection` computed）。

- [ ] **Step 4: `SettingsCategory.vue` 加下载按钮**

在设置页合适分组（如"应用信息/关于"）加一个条目，点击触发 `desktopDownload.downloadDesktop()`。引入同 composable，`onMounted` 调 `loadLatest()`。用现有设置条目样式（`settings-field` / `settings-item` 类，参照仓库既有条目），文案 `t('settings.downloadDesktopApp')`。

- [ ] **Step 5: 文案**

在 `web/src/i18n` 的中文/英文语言文件加：
- `pwa.downloadDesktopApp`：`下载桌面版` / `Download desktop app`
- `settings.downloadDesktopApp`：`下载桌面版` / `Download desktop app`

（找到现有 `pwa.*` 与 `settings.*` 所在文件位置后添加；若 key 组织不同，遵循既有结构。）

- [ ] **Step 6: 运行前端测试 + typecheck**

```bash
cd web && npx vitest run src/composables/__tests__/useDesktopDownload.test.ts
npm run typecheck
```
Expected: 测试 PASS，typecheck 无错。

- [ ] **Step 7: 提交**

```bash
git add web/src/composables/useDesktopDownload.ts web/src/composables/__tests__/useDesktopDownload.test.ts web/src/components/WelcomeOverlay.vue web/src/components/settings/SettingsCategory.vue web/src/i18n/
git commit -m "feat(web): desktop download buttons on welcome + settings"
```

---

## 自检对照（spec §8 → task）

| Spec §8 要求 | Task |
|---|---|
| 后端 `/api/desktop/latest`，复用 upgrade.go 逻辑，os-arch 键 | Task 1 |
| 公开端点，npmmirror 改写 | Task 1 |
| PC 浏览器欢迎页按钮 + OS 检测 | Task 2 |
| 配置页下载按钮 | Task 2 |
| 点击走 downloadByUrl | Task 2 |

## 后续

Plan 4 完成后，Electron 桌面客户端全部 4 个计划（前端桥抽象 / Android 改名 / Electron 客户端 / 下载入口）交付完毕。剩余为发布流程（macOS 签名、npm 发布、CI 打包、APK 重打）——记录在 spec §7.3，属 CI/发布范畴，非代码实现。
