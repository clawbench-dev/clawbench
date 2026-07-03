# Settings Page Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a unified settings page that consolidates all configuration from localStorage, the gear menu, and backend config.yaml into a two-level navigation drawer.

**Architecture:** Right-side full-screen drawer triggered by gear button. Two-level navigation: category list → detail page. Backend provides GET/PATCH /api/config and POST /api/config/restart APIs. Frontend reads from both API and localStorage, writes to respective sources.

**Tech Stack:** Go (net/http handlers), Vue 3 + TypeScript, Vitest for frontend tests, Go testing + testify for backend tests.

**Design Document:** `docs/plans/2026-09-11-settings-page-design.md`

**Worktree:** `/home/xulongzhe/projects/clawbench/.worktrees/settings-page/`

---

## Task 1: Backend — GET /api/config (Read Config)

**Files:**
- Create: `internal/handler/settings.go`
- Create: `internal/handler/settings_test.go`
- Modify: `internal/handler/handler.go` (add route)

**Step 1: Write the failing test**

```go
// internal/handler/settings_test.go
package handler_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
    "clawbench/internal/handler"
    "clawbench/internal/middleware"
    "clawbench/internal/model"
)

func TestServeConfig_Get(t *testing.T) {
    // Setup: set ConfigInstance with known values
    model.ConfigInstance = model.Config{
        Upload: struct {
            MaxSizeMB int `yaml:"max_size_mb" json:"max_size_mb"`
            MaxFiles  int `yaml:"max_files" json:"max_files"`
        }{MaxSizeMB: 50, MaxFiles: 10},
        Chat: struct {
            InitialMessages      int `yaml:"initial_messages" json:"initial_messages"`
            PageSize             int `yaml:"page_size" json:"page_size"`
            CollapsedHeight      int `yaml:"collapsed_height" json:"collapsed_height"`
            SystemPromptInterval int `yaml:"system_prompt_interval" json:"system_prompt_interval"`
        }{InitialMessages: 15, PageSize: 25, CollapsedHeight: 200, SystemPromptInterval: 5},
        Session: struct {
            MaxCount int `yaml:"max_count" json:"max_count"`
        }{MaxCount: 5},
    }

    mux := http.NewServeMux()
    handler.RegisterRoutes(mux)

    req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
    req.AddCookie(&http.Cookie{Name: "session", Value: model.SessionToken})
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)

    var resp map[string]any
    json.Unmarshal(w.Body.Bytes(), &resp)

    // Verify sensitive fields are NOT present
    _, hasPassword := resp["password"]
    assert.False(t, hasPassword, "password should not be in response")

    _, hasTLS := resp["tls"]
    assert.False(t, hasTLS, "tls should not be in response")

    // Verify allowed fields ARE present
    _, hasChat := resp["chat"]
    assert.True(t, hasChat, "chat should be in response")

    _, hasUpload := resp["upload"]
    assert.True(t, hasUpload, "upload should be in response")
}

func TestServeConfig_Get_Unauthorized(t *testing.T) {
    mux := http.NewServeMux()
    handler.RegisterRoutes(mux)

    req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    assert.Equal(t, http.StatusUnauthorized, w.Code)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && go test ./internal/handler/ -run TestServeConfig -v`
Expected: FAIL — `ServeConfig` undefined, route `/api/config` not registered

**Step 3: Write minimal implementation**

Create `internal/handler/settings.go`:

```go
package handler

import (
    "clawbench/internal/model"
    "net/http"
)

// configResponse is the sanitized config returned to clients.
// It only contains fields safe for frontend display.
type configResponse struct {
    Chat     configChat     `json:"chat"`
    Session  configSession  `json:"session"`
    Upload   configUpload   `json:"upload"`
    Terminal configTerminal `json:"terminal"`
    TTS      configTTS      `json:"tts"`
    RAG      configRAG      `json:"rag"`
    Proxy    configProxy    `json:"proxy"`
    SSH      configSSH      `json:"ssh"`
    Push     configPush     `json:"push"`
}

type configChat struct {
    InitialMessages      int `json:"initial_messages"`
    PageSize             int `json:"page_size"`
    CollapsedHeight      int `json:"collapsed_height"`
    SystemPromptInterval int `json:"system_prompt_interval"`
}

type configSession struct {
    MaxCount int `json:"max_count"`
}

type configUpload struct {
    MaxSizeMB int `json:"max_size_mb"`
    MaxFiles  int `json:"max_files"`
}

type configTerminal struct {
    Enabled     bool   `json:"enabled"`
    IdleTimeout string `json:"idle_timeout"`
    MaxSessions int    `json:"max_sessions"`
    BufferLines int    `json:"buffer_lines"`
}

type configTTS struct {
    Engine           string  `json:"engine"`
    TTSModel         string  `json:"tts_model"`
    Format           string  `json:"format"`
    SummarizeBackend string  `json:"summarize_backend"`
    SummarizeModel   string  `json:"summarize_model"`
    Speed            float64 `json:"speed"`
    Voice            string  `json:"voice"`
    MaxCacheFiles    int     `json:"max_cache_files"`
}

type configRAG struct {
    Enabled      bool   `json:"enabled"`
    OllamaBaseURL string `json:"ollama_base_url"`
    OllamaModel  string `json:"ollama_model"`
    ChunkSize    int    `json:"chunk_size"`
    SearchLimit  int    `json:"search_limit"`
    RetentionDays int   `json:"retention_days"`
}

type configProxy struct {
    Enabled      bool   `json:"enabled"`
    AllowedPorts string `json:"allowed_ports"`
}

type configSSH struct {
    Enabled bool `json:"enabled"`
    Port    int  `json:"port"`
}

// ServeConfig handles GET /api/config — returns sanitized config.
func ServeConfig(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
        return
    }

    cfg := model.ConfigInstance
    resp := configResponse{
        Chat: configChat{
            InitialMessages:      cfg.Chat.InitialMessages,
            PageSize:             cfg.Chat.PageSize,
            CollapsedHeight:      cfg.Chat.CollapsedHeight,
            SystemPromptInterval: cfg.Chat.SystemPromptInterval,
        },
        Session: configSession{
            MaxCount: cfg.Session.MaxCount,
        },
        Upload: configUpload{
            MaxSizeMB: cfg.Upload.MaxSizeMB,
            MaxFiles:  cfg.Upload.MaxFiles,
        },
        Terminal: configTerminal{
            Enabled:     cfg.Terminal.Enabled,
            IdleTimeout: cfg.Terminal.IdleTimeout,
            MaxSessions: cfg.Terminal.MaxSessions,
            BufferLines: cfg.Terminal.BufferLines,
        },
        TTS: configTTS{
            Engine:           cfg.TTS.Engine,
            TTSModel:         cfg.TTS.TTSModel,
            Format:           cfg.TTS.Format,
            SummarizeBackend: cfg.TTS.SummarizeBackend,
            SummarizeModel:   cfg.TTS.SummarizeModel,
            Speed:            cfg.TTS.Speed,
            Voice:            cfg.TTS.Voice,
            MaxCacheFiles:    cfg.TTS.MaxCacheFiles,
        },
        RAG: configRAG{
            Enabled:       cfg.RAG.Enabled,
            OllamaBaseURL: cfg.RAG.OllamaBaseURL,
            OllamaModel:   cfg.RAG.OllamaModel,
            ChunkSize:     cfg.RAG.ChunkSize,
            SearchLimit:   cfg.RAG.SearchLimit,
            RetentionDays: cfg.RAG.RetentionDays,
        },
        Proxy: configProxy{
            Enabled:      cfg.Proxy.Enabled,
            AllowedPorts: cfg.Proxy.AllowedPorts,
        },
        SSH: configSSH{
            Enabled: cfg.SSH.Enabled,
            Port:    cfg.SSH.Port,
        },
    }

    writeJSON(w, http.StatusOK, resp)
}
```

Add route in `internal/handler/handler.go` inside `RegisterRoutes`, after the `/api/watch-dir` line:

```go
register("/api/config", middleware.Auth(ServeConfig))
```

**Step 4: Run test to verify it passes**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && go test ./internal/handler/ -run TestServeConfig -v`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add internal/handler/settings.go internal/handler/settings_test.go internal/handler/handler.go
git commit -m "feat: add GET /api/config endpoint for reading sanitized config"
```

---

## Task 2: Backend — PATCH /api/config (Update Config) with Whitelist + Atomic Write

**Files:**
- Modify: `internal/handler/settings.go`
- Modify: `internal/handler/settings_test.go`
- Modify: `internal/model/config.go` (add mutex + helper)

**Step 1: Write the failing test**

```go
// Add to internal/handler/settings_test.go

func TestServeConfig_Patch_Success(t *testing.T) {
    model.ConfigInstance = model.Config{
        Upload: struct {
            MaxSizeMB int `yaml:"max_size_mb" json:"max_size_mb"`
            MaxFiles  int `yaml:"max_files" json:"max_files"`
        }{MaxSizeMB: 100, MaxFiles: 20},
        Chat: struct {
            InitialMessages      int `yaml:"initial_messages" json:"initial_messages"`
            PageSize             int `yaml:"page_size" json:"page_size"`
            CollapsedHeight      int `yaml:"collapsed_height" json:"collapsed_height"`
            SystemPromptInterval int `yaml:"system_prompt_interval" json:"system_prompt_interval"`
        }{InitialMessages: 20, PageSize: 20, CollapsedHeight: 150, SystemPromptInterval: 10},
    }

    mux := http.NewServeMux()
    handler.RegisterRoutes(mux)

    body := `{"chat":{"collapsed_height":200},"upload":{"max_size_mb":50}}`
    req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.AddCookie(&http.Cookie{Name: "session", Value: model.SessionToken})
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)

    var resp map[string]any
    json.Unmarshal(w.Body.Bytes(), &resp)
    assert.True(t, resp["needs_restart"].(bool))
    changed, _ := resp["changed_cold_fields"].([]any)
    assert.True(t, len(changed) >= 2)
}

func TestServeConfig_Patch_ForbiddenField(t *testing.T) {
    mux := http.NewServeMux()
    handler.RegisterRoutes(mux)

    body := `{"password":"hacked"}`
    req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.AddCookie(&http.Cookie{Name: "session", Value: model.SessionToken})
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_InvalidValue(t *testing.T) {
    mux := http.NewServeMux()
    handler.RegisterRoutes(mux)

    body := `{"tts":{"engine":"invalid_engine"}}`
    req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.AddCookie(&http.Cookie{Name: "session", Value: model.SessionToken})
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    assert.Equal(t, http.StatusBadRequest, w.Code)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && go test ./internal/handler/ -run TestServeConfig_Patch -v`
Expected: FAIL — PATCH not implemented

**Step 3: Write minimal implementation**

Add to `internal/handler/settings.go`:

- Add `ConfigMutex sync.RWMutex` (or add to `model` package)
- Add PATCH handler with field whitelist validation
- Add value validation for enums (tts.engine, tts.summarize_backend, tts.format)
- Add atomic yaml write (tmp + rename)
- Add backup (config.yaml.bak)

Add to `internal/model/config.go`:

```go
var ConfigMutex sync.RWMutex

// PatchableConfigPaths defines the whitelist of config paths that PATCH /api/config accepts.
var PatchableConfigPaths = map[string]bool{
    "chat.initial_messages":       true,
    "chat.page_size":              true,
    "chat.collapsed_height":       true,
    "chat.system_prompt_interval": true,
    "session.max_count":           true,
    "upload.max_size_mb":          true,
    "upload.max_files":            true,
    "terminal.enabled":            true,
    "terminal.idle_timeout":       true,
    "terminal.max_sessions":       true,
    "terminal.buffer_lines":       true,
    "tts.engine":                  true,
    "tts.tts_model":               true,
    "tts.format":                  true,
    "tts.summarize_backend":       true,
    "tts.summarize_model":        true,
    "tts.speed":                   true,
    "tts.voice":                   true,
    "tts.max_cache_files":         true,
    "rag.enabled":                 true,
    "rag.ollama_base_url":         true,
    "rag.ollama_model":            true,
    "rag.chunk_size":              true,
    "rag.search_limit":            true,
    "rag.retention_days":          true,
    "proxy.enabled":               true,
    "proxy.allowed_ports":         true,
    "ssh.enabled":                 true,
    "ssh.port":                    true,
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && go test ./internal/handler/ -run TestServeConfig_Patch -v`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add internal/handler/settings.go internal/handler/settings_test.go internal/model/config.go
git commit -m "feat: add PATCH /api/config with field whitelist and atomic write"
```

---

## Task 3: Backend — POST /api/config/restart (Restart Service)

**Files:**
- Modify: `internal/handler/settings.go`
- Modify: `internal/handler/settings_test.go`
- Modify: `cmd/server/main.go` (expose shutdown trigger)

**Step 1: Write the failing test**

```go
// Add to internal/handler/settings_test.go

func TestServeConfig_Restart(t *testing.T) {
    mux := http.NewServeMux()
    handler.RegisterRoutes(mux)

    req := httptest.NewRequest(http.MethodPost, "/api/config/restart", nil)
    req.AddCookie(&http.Cookie{Name: "session", Value: model.SessionToken})
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
    var resp map[string]any
    json.Unmarshal(w.Body.Bytes(), &resp)
    assert.Equal(t, "restarting", resp["status"])
}

func TestServeConfig_Restart_Unauthorized(t *testing.T) {
    mux := http.NewServeMux()
    handler.RegisterRoutes(mux)

    req := httptest.NewRequest(http.MethodPost, "/api/config/restart", nil)
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    assert.Equal(t, http.StatusUnauthorized, w.Code)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && go test ./internal/handler/ -run TestServeConfig_Restart -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add to `internal/handler/settings.go`:

- `ServeConfigRestart` handler
- Sentinel process launch (Unix: `kill -0` loop with retry; Windows: `timeout` delay)
- Process group isolation (`Setpgid` / `CREATE_NEW_PROCESS_GROUP`)
- Supervisor detection (`isRunningUnderSupervisor`)
- Backup config.yaml before write
- Write restart sentinel file

Modify `cmd/server/main.go`:

- Expose `ShutdownSignal` channel or function that handler can trigger
- The handler starts sentinel → waits 200ms → sends to shutdown channel

**Step 4: Run test to verify it passes**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && go test ./internal/handler/ -run TestServeConfig_Restart -v`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add internal/handler/settings.go internal/handler/settings_test.go cmd/server/main.go
git commit -m "feat: add POST /api/config/restart with sentinel process"
```

---

## Task 4: Frontend — SettingsItem Component (TDD)

**Files:**
- Create: `web/src/components/settings/SettingsItem.vue`
- Create: `web/src/components/settings/__tests__/SettingsItem.test.ts`

**Step 1: Write the failing test**

```typescript
// web/src/components/settings/__tests__/SettingsItem.test.ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsItem from '../SettingsItem.vue'

describe('SettingsItem', () => {
  it('renders switch type', () => {
    const wrapper = mount(SettingsItem, {
      props: { label: 'Auto Speech', type: 'switch', modelValue: true }
    })
    expect(wrapper.text()).toContain('Auto Speech')
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(true)
  })

  it('renders select type with current value', () => {
    const wrapper = mount(SettingsItem, {
      props: {
        label: 'Engine',
        type: 'select',
        modelValue: 'edge',
        options: [
          { label: 'Edge', value: 'edge' },
          { label: 'MiniMax', value: 'minimax' },
        ]
      }
    })
    expect(wrapper.text()).toContain('Engine')
    expect(wrapper.text()).toContain('Edge')
  })

  it('renders number type', () => {
    const wrapper = mount(SettingsItem, {
      props: { label: 'Max Files', type: 'number', modelValue: 20 }
    })
    expect(wrapper.text()).toContain('Max Files')
    expect(wrapper.text()).toContain('20')
  })

  it('renders needsRestart badge', () => {
    const wrapper = mount(SettingsItem, {
      props: { label: 'Engine', type: 'text', modelValue: 'edge', needsRestart: true }
    })
    expect(wrapper.text()).toContain('需重启')
  })

  it('does not render needsRestart badge when false', () => {
    const wrapper = mount(SettingsItem, {
      props: { label: 'Engine', type: 'text', modelValue: 'edge', needsRestart: false }
    })
    expect(wrapper.text()).not.toContain('需重启')
  })

  it('emits update:modelValue on switch toggle', async () => {
    const wrapper = mount(SettingsItem, {
      props: { label: 'Auto', type: 'switch', modelValue: false }
    })
    await wrapper.find('input[type="checkbox"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')).toBeTruthy()
    expect(wrapper.emitted('update:modelValue')![0]).toEqual([true])
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/components/settings/__tests__/SettingsItem.test.ts`
Expected: FAIL — component doesn't exist

**Step 3: Write minimal implementation**

Create `web/src/components/settings/SettingsItem.vue` — a generic settings row component supporting switch, select, number, text, slider, action types. Shows `需重启` badge when `needsRestart` prop is true.

**Step 4: Run test to verify it passes**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/components/settings/__tests__/SettingsItem.test.ts`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add web/src/components/settings/SettingsItem.vue web/src/components/settings/__tests__/SettingsItem.test.ts
git commit -m "feat: add SettingsItem component with switch/select/number/text/slider/action types"
```

---

## Task 5: Frontend — SettingsCategory Component (TDD)

**Files:**
- Create: `web/src/components/settings/SettingsCategory.vue`
- Create: `web/src/components/settings/__tests__/SettingsCategory.test.ts`

**Step 1: Write the failing test**

```typescript
// web/src/components/settings/__tests__/SettingsCategory.test.ts
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsCategory from '../SettingsCategory.vue'

// Mock apiGet
vi.mock('@/utils/api', () => ({
  apiGet: vi.fn().mockResolvedValue({
    chat: { initial_messages: 20, page_size: 20, collapsed_height: 150, system_prompt_interval: 10 },
    session: { max_count: 10 },
    upload: { max_size_mb: 100, max_files: 20 },
    terminal: { enabled: true, idle_timeout: '10m', max_sessions: 10, buffer_lines: 2000 },
    tts: { engine: 'edge', tts_model: '', format: '', summarize_backend: 'simple', summarize_model: '', speed: 1, voice: '', max_cache_files: 100 },
    rag: { enabled: false, ollama_base_url: 'http://localhost:11434', ollama_model: 'bge-m3', chunk_size: 512, search_limit: 5, retention_days: 90 },
    proxy: { enabled: true, allowed_ports: '1024-65535' },
    ssh: { enabled: true, port: 0 },
  }),
  apiPatch: vi.fn().mockResolvedValue({ needs_restart: false, changed_cold_fields: [] }),
}))

describe('SettingsCategory', () => {
  it('renders chat category items', () => {
    const wrapper = mount(SettingsCategory, {
      props: { categoryId: 'chat' },
      global: { stubs: { SettingsItem: true } }
    })
    expect(wrapper.text()).toContain('自动朗读')
  })

  it('renders appearance category items', () => {
    const wrapper = mount(SettingsCategory, {
      props: { categoryId: 'appearance' },
      global: { stubs: { SettingsItem: true } }
    })
    expect(wrapper.text()).toContain('语言')
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/components/settings/__tests__/SettingsCategory.test.ts`
Expected: FAIL

**Step 3: Write minimal implementation**

Create `web/src/components/settings/SettingsCategory.vue`:
- Props: `categoryId: string`
- Uses a composable `useSettingsConfig` to fetch and manage config state
- Renders the correct set of `SettingsItem` components based on `categoryId`
- Categories defined as a static map: `{ appearance: [...items], chat: [...items], ... }`

**Step 4: Run test to verify it passes**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/components/settings/__tests__/SettingsCategory.test.ts`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add web/src/components/settings/SettingsCategory.vue web/src/components/settings/__tests__/SettingsCategory.test.ts
git commit -m "feat: add SettingsCategory component with per-category config items"
```

---

## Task 6: Frontend — SettingsIndex Component (TDD)

**Files:**
- Create: `web/src/components/settings/SettingsIndex.vue`
- Create: `web/src/components/settings/__tests__/SettingsIndex.test.ts`

**Step 1: Write the failing test**

```typescript
// web/src/components/settings/__tests__/SettingsIndex.test.ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsIndex from '../SettingsIndex.vue'

describe('SettingsIndex', () => {
  it('renders all category rows', () => {
    const wrapper = mount(SettingsIndex)
    const categories = ['外观', '聊天', 'Agent 偏好', '文件管理', '文件查看器', '终端', 'TTS 语音', 'RAG 记忆', '端口转发', 'SSH 隧道', '推送', 'Android', '服务器', '关于']
    for (const cat of categories) {
      expect(wrapper.text()).toContain(cat)
    }
  })

  it('emits navigate with category id on click', async () => {
    const wrapper = mount(SettingsIndex)
    const items = wrapper.findAll('.settings-category-item')
    await items[0].trigger('click')  // 外观
    expect(wrapper.emitted('navigate')).toBeTruthy()
    expect(wrapper.emitted('navigate')![0]).toEqual(['appearance'])
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/components/settings/__tests__/SettingsIndex.test.ts`
Expected: FAIL

**Step 3: Write minimal implementation**

Create `web/src/components/settings/SettingsIndex.vue`:
- Renders category list with icon, name, and arrow
- Each row clickable, emits `navigate` with categoryId

**Step 4: Run test to verify it passes**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/components/settings/__tests__/SettingsIndex.test.ts`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add web/src/components/settings/SettingsIndex.vue web/src/components/settings/__tests__/SettingsIndex.test.ts
git commit -m "feat: add SettingsIndex component with category list"
```

---

## Task 7: Frontend — SettingsDrawer + SettingsRestartDialog (TDD)

**Files:**
- Create: `web/src/components/settings/SettingsDrawer.vue`
- Create: `web/src/components/settings/SettingsRestartDialog.vue`
- Create: `web/src/components/settings/__tests__/SettingsDrawer.test.ts`
- Create: `web/src/components/settings/__tests__/SettingsRestartDialog.test.ts`

**Step 1: Write the failing tests**

SettingsDrawer test:

```typescript
// web/src/components/settings/__tests__/SettingsDrawer.test.ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsDrawer from '../SettingsDrawer.vue'

describe('SettingsDrawer', () => {
  it('renders SettingsIndex when nav stack is empty', () => {
    const wrapper = mount(SettingsDrawer, {
      props: { show: true },
      global: { stubs: { SettingsIndex: { template: '<div class="mock-index">Index</div>' }, SettingsCategory: true } }
    })
    expect(wrapper.find('.mock-index').exists()).toBe(true)
  })

  it('renders SettingsCategory when nav stack has item', async () => {
    const wrapper = mount(SettingsDrawer, {
      props: { show: true },
      global: { stubs: { SettingsIndex: { template: '<div class="mock-index" @click="$emit(\'navigate\', \'chat\')">Index</div>' }, SettingsCategory: { template: '<div class="mock-category">Category</div>', props: ['categoryId'] } } }
    })
    // Simulate navigation
    await wrapper.find('.mock-index').trigger('click')
    expect(wrapper.find('.mock-category').exists()).toBe(true)
  })

  it('emits close when back button clicked on index', async () => {
    const wrapper = mount(SettingsDrawer, {
      props: { show: true },
      global: { stubs: { SettingsIndex: true, SettingsCategory: true } }
    })
    await wrapper.find('.settings-back-btn').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})
```

SettingsRestartDialog test:

```typescript
// web/src/components/settings/__tests__/SettingsRestartDialog.test.ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsRestartDialog from '../SettingsRestartDialog.vue'

describe('SettingsRestartDialog', () => {
  it('renders changed fields list', () => {
    const wrapper = mount(SettingsRestartDialog, {
      props: { changedFields: ['TTS 引擎', '折叠高度'] }
    })
    expect(wrapper.text()).toContain('TTS 引擎')
    expect(wrapper.text()).toContain('折叠高度')
  })

  it('emits restart on restart button click', async () => {
    const wrapper = mount(SettingsRestartDialog, {
      props: { changedFields: ['TTS 引擎'] }
    })
    await wrapper.find('.restart-btn').trigger('click')
    expect(wrapper.emitted('restart')).toBeTruthy()
  })

  it('emits later on later button click', async () => {
    const wrapper = mount(SettingsRestartDialog, {
      props: { changedFields: ['TTS 引擎'] }
    })
    await wrapper.find('.later-btn').trigger('click')
    expect(wrapper.emitted('later')).toBeTruthy()
  })
})
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/components/settings/__tests__/`
Expected: FAIL

**Step 3: Write minimal implementation**

- `SettingsDrawer.vue`: Full-screen right-side drawer with navigation stack management
- `SettingsRestartDialog.vue`: Modal dialog with changed fields list, "稍后" and "立即重启" buttons

**Step 4: Run tests to verify they pass**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/components/settings/__tests__/`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add web/src/components/settings/SettingsDrawer.vue web/src/components/settings/SettingsRestartDialog.vue web/src/components/settings/__tests__/SettingsDrawer.test.ts web/src/components/settings/__tests__/SettingsRestartDialog.test.ts
git commit -m "feat: add SettingsDrawer and SettingsRestartDialog components"
```

---

## Task 8: Frontend — useSettingsConfig Composable (TDD)

**Files:**
- Create: `web/src/composables/useSettingsConfig.ts`
- Create: `web/src/composables/__tests__/useSettingsConfig.test.ts`

**Step 1: Write the failing test**

```typescript
// web/src/composables/__tests__/useSettingsConfig.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useSettingsConfig } from '../useSettingsConfig'

const mockApiGet = vi.fn()
const mockApiPatch = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
  apiPatch: (...args: any[]) => mockApiPatch(...args),
}))

describe('useSettingsConfig', () => {
  beforeEach(() => {
    mockApiGet.mockResolvedValue({
      chat: { initial_messages: 20, page_size: 20, collapsed_height: 150, system_prompt_interval: 10 },
      session: { max_count: 10 },
      upload: { max_size_mb: 100, max_files: 20 },
      terminal: { enabled: true, idle_timeout: '10m', max_sessions: 10, buffer_lines: 2000 },
      tts: { engine: 'edge', tts_model: '', format: '', summarize_backend: 'simple', summarize_model: '', speed: 1, voice: '', max_cache_files: 100 },
      rag: { enabled: false, ollama_base_url: 'http://localhost:11434', ollama_model: 'bge-m3', chunk_size: 512, search_limit: 5, retention_days: 90 },
      proxy: { enabled: true, allowed_ports: '1024-65535' },
      ssh: { enabled: true, port: 0 },
    })
    mockApiPatch.mockResolvedValue({ needs_restart: false, changed_cold_fields: [] })
  })

  it('loads config from API', async () => {
    const { serverConfig, loadConfig } = useSettingsConfig()
    await loadConfig()
    expect(mockApiGet).toHaveBeenCalledWith('/api/config')
    expect(serverConfig.value.chat.collapsed_height).toBe(150)
  })

  it('patches backend config and returns restart info', async () => {
    mockApiPatch.mockResolvedValue({ needs_restart: true, changed_cold_fields: ['tts.engine'] })
    const { patchConfig, loadConfig } = useSettingsConfig()
    await loadConfig()
    const result = await patchConfig({ chat: { collapsed_height: 200 } })
    expect(mockApiPatch).toHaveBeenCalledWith('/api/config', { chat: { collapsed_height: 200 } })
    expect(result.needsRestart).toBe(true)
  })

  it('reads localStorage values', async () => {
    localStorage.setItem('clawbench-auto-speech', 'true')
    const { localConfig } = useSettingsConfig()
    expect(localConfig.value.autoSpeech).toBe(true)
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/composables/__tests__/useSettingsConfig.test.ts`
Expected: FAIL

**Step 3: Write minimal implementation**

Create `web/src/composables/useSettingsConfig.ts`:
- `loadConfig()`: fetch GET /api/config → store in `serverConfig` ref
- `patchConfig(changes)`: PATCH /api/config → return `{ needsRestart, changedColdFields }`
- `localConfig`: reactive object reading from localStorage
- `setLocalConfig(key, value)`: write to localStorage + update reactive
- `restartServer()`: POST /api/config/restart

**Step 4: Run test to verify it passes**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run web/src/composables/__tests__/useSettingsConfig.test.ts`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add web/src/composables/useSettingsConfig.ts web/src/composables/__tests__/useSettingsConfig.test.ts
git commit -m "feat: add useSettingsConfig composable for config read/write"
```

---

## Task 9: Frontend — Integrate SettingsDrawer into App

**Files:**
- Modify: `web/src/components/common/AppHeader.vue` (gear button → emit)
- Modify: `web/src/App.vue` (add SettingsDrawer, wire events)

**Step 1: Write the failing test**

No new test file — integration is tested via existing component structure.

**Step 2: Modify AppHeader.vue**

- Remove `PopupMenu` from gear button
- Change `toggleSettingsMenu` to emit `openSettings` instead
- Remove all settings-related refs and handlers (`settingsMenuOpen`, `settingsItemCount`, `handleThemeSwitch`, `handleLocaleSwitch`, `toggleDebugLog`, etc.)
- Keep connection status button as-is

**Step 3: Modify App.vue**

- Import and add `SettingsDrawer` in template (alongside other overlays)
- Add `settingsDrawerOpen` ref
- Listen for `@open-settings` from AppHeader → set `settingsDrawerOpen = true`
- Pass `theme`, `applyTheme` to SettingsDrawer for direct control
- Handle `@close` from SettingsDrawer → set `settingsDrawerOpen = false`

**Step 4: Run all frontend tests**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run`
Expected: PASS (all existing tests still pass)

**Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add web/src/components/common/AppHeader.vue web/src/App.vue
git commit -m "feat: integrate SettingsDrawer into App, replace gear PopupMenu"
```

---

## Task 10: Frontend — Styling + Polish

**Files:**
- Create: `web/src/components/settings/settings.scss`
- Modify: `web/src/components/settings/SettingsDrawer.vue`
- Modify: `web/src/components/settings/SettingsIndex.vue`
- Modify: `web/src/components/settings/SettingsCategory.vue`
- Modify: `web/src/components/settings/SettingsItem.vue`

**Step 1: No failing test (visual styling)**

**Step 2: Write styles**

- iOS-style grouped list with rounded card corners and gray background
- Drawer slide-in animation from right
- Category push/pop slide animation
- Touch-friendly row height (48px+)
- `需重启` badge styling (small, muted)
- Dark mode support
- Mobile (100vw) vs desktop (max-width 420px) responsive widths

**Step 3: Visual verification**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && ./dev-server.sh`
Manual check: gear button → drawer opens → click categories → see items → toggle switches → save with restart prompt

**Step 4: Commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add web/src/components/settings/
git commit -m "feat: add settings page styling with iOS-style grouped lists"
```

---

## Task 11: End-to-End Verification

**Step 1: Run all Go tests**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && go test ./...`
Expected: All pass

**Step 2: Run all frontend tests**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && npx vitest run`
Expected: All pass (2048+ tests)

**Step 3: Build and manual test**

Run: `cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page && ./build.sh && ./server.sh`

Verify:
1. Gear button opens settings drawer
2. All categories visible in index
3. Each category shows correct items
4. localStorage items toggle immediately
5. Backend items show "需重启" badge
6. Changing backend item → save → restart dialog appears
7. "立即重启" → service restarts → reconnects
8. Theme/locale changes still work
9. Android debug log toggle still works
10. Existing functionality unaffected (chat, file manager, terminal, etc.)

**Step 4: Final commit**

```bash
cd /home/xulongzhe/projects/clawbench/.worktrees/settings-page
git add -A
git commit -m "feat: complete settings page implementation"
```
