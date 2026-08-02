# Unified Fullscreen Connection Overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the yellow blinking server icon in the App Header with a unified fullscreen overlay that shows server-connection status (disconnect/reconnecting with 1.5s delay, or restarting) and disappears on reconnect.

**Architecture:** A single global component `ConnectionOverlay.vue` teleported to `body` with `position: fixed; inset: 0; z-index: 9999` reads a new `mode` computed from a `useConnectionOverlay` composable. The composable combines `wsStatus` + `hasConnectedOnce` (from `useGlobalEvents`) with a lifted module-level `restartingOverlay` (from `useSettingsNavigation`). Restart mode shows immediately and takes priority; reconnect mode shows only after 1.5s of sustained disconnect AND only after the connection has been established at least once (first-load flash protection). The old `.restart-overlay` markup/CSS in `SettingsPage.vue` is removed.

**Tech Stack:** Vue 3 (`<script setup lang="ts">`), TypeScript, Vue Teleport, lucide-vue-next icons, vue-i18n, Vitest (`./scripts/vitest-run.sh`).

**Reference spec:** `docs/superpowers/specs/2026-08-03-connection-overlay-design.md`

---

## Key facts (read before starting)

- `useGlobalEvents` (`web/src/composables/useGlobalEvents.ts`) is a **module-level singleton**. `wsStatus` computed (line 412) = `'connected' | 'reconnecting' | 'disconnected'`. `connected` is a module-level `ref`. `destroy()` (line 460) resets handlers/state.
- `useSettingsNavigation` (`web/src/composables/useSettingsNavigation.ts`) already uses a **module-level registry** (`guards` Map, lines 13-29). `restartingOverlay` is currently a per-call `ref` (line 45) — this must be lifted to module level so the global overlay shares it with SettingsPage.
- `SettingsPage.vue` renders the old restart overlay via `<Teleport to="body">` (lines 38-46) with `.restart-overlay` CSS (lines 281-319). It will be removed.
- `App.vue` mounts the authenticated app tree inside `<div class="app-container" :key="projectKey">` (line 10). `ConnectionOverlay` must be mounted inside this branch (after `<AppHeader .../>`, line 15-19).
- `.header` is `position: fixed; z-index: 1100` (`web/css/layout.css:16-34`). The overlay at `z-index: 9999` covers it.
- i18n: reconnect text goes under `systemResources` in `web/src/i18n/locales/zh.ts` (line ~103) and `en.ts` (line ~104). `settings.restartingPleaseWait` already exists (zh:1503, en:1502).
- Test command: `./scripts/vitest-run.sh <path>` (from repo root). Frontend tests are `*.test.ts` in `__tests__/` or next to source.
- AGENTS.md rules: frontend code must use `appLog` (not needed here), and **features must include unit tests**.

---

### Task 1: Add `hasConnectedOnce` to useGlobalEvents

**Files:**
- Modify: `web/src/composables/useGlobalEvents.ts` (add module ref near line 43, set in `onopen` line 177, reset in `destroy` line 460, return at line 471)
- Test: `web/src/composables/__tests__/useGlobalEvents.test.ts`

- [ ] **Step 1: Write the failing test**

Add a new `describe` block in `web/src/composables/__tests__/useGlobalEvents.test.ts` (append near the `wsStatus computed` describe at line 381):

```ts
describe('hasConnectedOnce', () => {
    it('is false before any connection', () => {
        expect(events.hasConnectedOnce.value).toBe(false)
    })

    it('becomes true after first successful connection', () => {
        connectAndGetWs()
        expect(events.hasConnectedOnce.value).toBe(true)
    })

    it('resets to false on destroy', () => {
        connectAndGetWs()
        expect(events.hasConnectedOnce.value).toBe(true)
        events.destroy()
        expect(events.hasConnectedOnce.value).toBe(false)
    })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useGlobalEvents.test.ts`
Expected: FAIL — `hasConnectedOnce` is `undefined`.

- [ ] **Step 3: Implement**

In `web/src/composables/useGlobalEvents.ts`:

Add a module-level ref next to `const connected = ref(false)` (line 43):

```ts
const connected = ref(false)
// True once the WS connection has been established at least once.
// Used to suppress the reconnect overlay during initial page load.
const hasConnectedOnce = ref(false)
```

In `ws.onopen` (line 177), set it true:

```ts
    ws.onopen = () => {
        connected.value = true
        hasConnectedOnce.value = true
        missedPongs = 0
```

In `destroy()` (line 460), reset it (keeps tests isolated):

```ts
    function destroy() {
        document.removeEventListener('visibilitychange', handleVisibilityChange)
        disconnect()
        // ISS-192: Clear handlers and state on destroy to prevent stale closures
        // from firing after SPA hot project switch.
        handlers.length = 0
        processedEventIds.clear()
        missedPongs = 0
        hasConnectedOnce.value = false
        initialized = false
    }
```

In the return object (line 471), add `hasConnectedOnce`:

```ts
    return {
        connected,
        hasConnectedOnce,
        wsStatus,
        connect,
        disconnect,
        onEvent,
        sendWsMessage: send,
        init,
        destroy,
    }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useGlobalEvents.test.ts`
Expected: PASS (all tests in file, including the 3 new ones).

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useGlobalEvents.ts web/src/composables/__tests__/useGlobalEvents.test.ts
git commit -m "feat(web): track hasConnectedOnce in global WS events"
```

---

### Task 2: Lift `restartingOverlay` to module level

**Files:**
- Modify: `web/src/composables/useSettingsNavigation.ts` (line 45)
- Test: `web/src/composables/__tests__/useSettingsNavigation.test.ts`

- [ ] **Step 1: Write the failing test**

The existing tests already exercise `restartingOverlay` (lines 104-122, 188-239). Since it becomes module-level, add a reset in `beforeEach` and add a test proving the ref is shared across calls. Replace the `beforeEach` (lines 38-41) with:

```ts
import { restartingOverlay } from '@/composables/useSettingsNavigation'

describe('useSettingsNavigation', () => {
    beforeEach(() => {
        vi.clearAllMocks()
        vi.useFakeTimers()
        restartingOverlay.value = false
    })
```

Append this test inside the describe (e.g. after `returned values`):

```ts
    describe('module-level restartingOverlay', () => {
        it('shares the same ref across multiple useSettingsNavigation() calls', () => {
            const nav1 = useSettingsNavigation()
            const nav2 = useSettingsNavigation()
            expect(nav1.restartingOverlay).toBe(nav2.restartingOverlay)
        })
    })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useSettingsNavigation.test.ts`
Expected: FAIL — `restartingOverlay` is a per-call ref, so `nav1.restartingOverlay !== nav2.restartingOverlay`, and `import { restartingOverlay }` fails (not exported).

- [ ] **Step 3: Implement**

In `web/src/composables/useSettingsNavigation.ts`:

Add a module-level ref next to the `guards` registry (after `unregisterGuard`, ~line 29):

```ts
// Module-level shared overlay state — the global ConnectionOverlay reads the same
// ref that SettingsPage writes to, so both stay in sync.
export const restartingOverlay = ref(false)
```

Remove the local declaration inside the function (line 45):

```ts
  const restarting = ref(false)
  const restartingOverlay = ref(false)   // ← DELETE this line
```

The function body already sets/returns `restartingOverlay`; it now refers to the module-level ref. The import at line 1 (`import { ref, onUnmounted } from 'vue'`) already covers `ref`.

- [ ] **Step 4: Run test to verify it passes**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useSettingsNavigation.test.ts`
Expected: PASS (all tests, including the new shared-ref test).

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useSettingsNavigation.ts web/src/composables/__tests__/useSettingsNavigation.test.ts
git commit -m "feat(web): lift restartingOverlay to module-level shared ref"
```

---

### Task 3: Add i18n keys for the reconnect overlay text

**Files:**
- Modify: `web/src/i18n/locales/zh.ts` (systemResources block, line ~114)
- Modify: `web/src/i18n/locales/en.ts` (systemResources block, line ~114)

- [ ] **Step 1: Add keys**

In `web/src/i18n/locales/zh.ts`, inside the `systemResources` object (after `reconnecting`, line 114):

```ts
    disconnected: '连接断开',
    reconnecting: '正在重连...',
    overlayReconnecting: '连接断开，正在重连…',
```

In `web/src/i18n/locales/en.ts`, inside `systemResources` (after `reconnecting`, line 114):

```ts
    disconnected: 'Connection Lost',
    reconnecting: 'Reconnecting...',
    overlayReconnecting: 'Connection lost, reconnecting…',
```

- [ ] **Step 2: Verify no test regression**

Run: `./scripts/vitest-run.sh web/src/i18n` (or the full suite if the i18n dir has no tests — then run a quick smoke: `./scripts/vitest-run.sh web/src/components/common/__tests__/SystemResourcesPanel.test.ts`)
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(web): add overlayReconnecting i18n keys"
```

---

### Task 4: Create `useConnectionOverlay` composable

**Files:**
- Create: `web/src/composables/useConnectionOverlay.ts`
- Test: `web/src/composables/__tests__/useConnectionOverlay.test.ts`

- [ ] **Step 1: Write the failing test**

Create `web/src/composables/__tests__/useConnectionOverlay.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Mutable shared refs created once (vi.mock is hoisted)
const { wsStatusRef, hasConnectedOnceRef, restartingOverlayRef } = vi.hoisted(() => {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const { ref } = require('vue')
    return {
        wsStatusRef: ref('connected'),
        hasConnectedOnceRef: ref(false),
        restartingOverlayRef: ref(false),
    }
})

vi.mock('@/composables/useGlobalEvents', () => ({
    useGlobalEvents: () => ({
        wsStatus: wsStatusRef,
        hasConnectedOnce: hasConnectedOnceRef,
    }),
}))

vi.mock('@/composables/useSettingsNavigation', () => ({
    useSettingsNavigation: () => ({
        restartingOverlay: restartingOverlayRef,
    }),
}))

// Mock vue's onUnmounted to be a no-op outside a component instance
// (watch/computed/nextTick/ref stay real via ...actual)
vi.mock('vue', async () => {
    const actual = await vi.importActual('vue')
    return { ...actual, onUnmounted: vi.fn() }
})

import { nextTick } from 'vue'
import { useConnectionOverlay, RECONNECT_OVERLAY_DELAY_MS } from '@/composables/useConnectionOverlay'

describe('useConnectionOverlay', () => {
    beforeEach(() => {
        wsStatusRef.value = 'connected'
        hasConnectedOnceRef.value = false
        restartingOverlayRef.value = false
        vi.useFakeTimers()
    })

    afterEach(() => {
        vi.useRealTimers()
    })

    function make() {
        return useConnectionOverlay()
    }

    // Vue's default watch flush is async — always await nextTick() after mutating a ref
    // so the watcher has run (and scheduled/cleared the timer) before advancing time.

    it('mode is null when connected', () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        expect(overlay.mode.value).toBeNull()
    })

    it('shows reconnect mode after 1.5s of disconnect once connected before', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        wsStatusRef.value = 'reconnecting'
        await nextTick()
        expect(overlay.mode.value).toBeNull()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBe('reconnect')
    })

    it('does NOT show on cold start (never connected before)', async () => {
        const overlay = make()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBeNull()
    })

    it('does NOT show when reconnected within the 1.5s window', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS - 100)
        wsStatusRef.value = 'connected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS)
        expect(overlay.mode.value).toBeNull()
    })

    it('clears reconnect mode when back to connected', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBe('reconnect')
        wsStatusRef.value = 'connected'
        await nextTick()
        expect(overlay.mode.value).toBeNull()
    })

    it('shows restart mode immediately (no delay) and takes priority over reconnect', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        wsStatusRef.value = 'reconnecting'
        restartingOverlayRef.value = true
        // mode reads restartingOverlay directly — no flush needed
        expect(overlay.mode.value).toBe('restart')
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBe('restart')
    })

    it('hides restart mode when restartingOverlay goes false', async () => {
        const overlay = make()
        restartingOverlayRef.value = true
        expect(overlay.mode.value).toBe('restart')
        restartingOverlayRef.value = false
        expect(overlay.mode.value).toBeNull()
    })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useConnectionOverlay.test.ts`
Expected: FAIL — module `useConnectionOverlay` not found.

- [ ] **Step 3: Implement**

Create `web/src/composables/useConnectionOverlay.ts`:

```ts
import { ref, computed, watch, onUnmounted } from 'vue'
import { useGlobalEvents } from './useGlobalEvents'
import { useSettingsNavigation } from './useSettingsNavigation'

// Delay before showing the reconnect mask, so transient blips that recover
// within this window never flash a fullscreen overlay.
export const RECONNECT_OVERLAY_DELAY_MS = 1500

export type ConnectionOverlayMode = 'restart' | 'reconnect' | null

/**
 * Drives the unified fullscreen status overlay.
 *
 * - 'restart'  → shown immediately while the server is restarting (user-initiated).
 * - 'reconnect' → shown only after the WS stays disconnected for
 *   RECONNECT_OVERLAY_DELAY_MS AND the connection was established at least once
 *   (prevents a mask flash on first page load).
 * - null       → overlay hidden.
 *
 * Restart takes priority over reconnect.
 */
export function useConnectionOverlay() {
    const { wsStatus, hasConnectedOnce } = useGlobalEvents()
    const { restartingOverlay } = useSettingsNavigation()

    const showReconnect = ref(false)
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    function clearTimer() {
        if (reconnectTimer) {
            clearTimeout(reconnectTimer)
            reconnectTimer = null
        }
    }

    watch(
        wsStatus,
        (status) => {
            if (status === 'connected') {
                clearTimer()
                showReconnect.value = false
                return
            }
            // disconnected or reconnecting
            if (!hasConnectedOnce.value || showReconnect.value || reconnectTimer) return
            reconnectTimer = setTimeout(() => {
                showReconnect.value = true
                reconnectTimer = null
            }, RECONNECT_OVERLAY_DELAY_MS)
        },
        { immediate: true },
    )

    onUnmounted(clearTimer)

    const mode = computed<ConnectionOverlayMode>(() => {
        if (restartingOverlay.value) return 'restart'
        if (showReconnect.value) return 'reconnect'
        return null
    })

    return { mode }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useConnectionOverlay.test.ts`
Expected: PASS (all 8 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useConnectionOverlay.ts web/src/composables/__tests__/useConnectionOverlay.test.ts
git commit -m "feat(web): add useConnectionOverlay composable with 1.5s reconnect delay"
```

---

### Task 5: Create `ConnectionOverlay.vue` component

**Files:**
- Create: `web/src/components/common/ConnectionOverlay.vue`
- Test: `web/src/components/common/__tests__/ConnectionOverlay.test.ts`

- [ ] **Step 1: Write the failing test**

Create `web/src/components/common/__tests__/ConnectionOverlay.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import ConnectionOverlay from '@/components/common/ConnectionOverlay.vue'

const { wsStatusRef, hasConnectedOnceRef, restartingOverlayRef } = vi.hoisted(() => {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const { ref } = require('vue')
    return {
        wsStatusRef: ref('connected'),
        hasConnectedOnceRef: ref(false),
        restartingOverlayRef: ref(false),
    }
})

vi.mock('@/composables/useGlobalEvents', () => ({
    useGlobalEvents: () => ({
        wsStatus: wsStatusRef,
        hasConnectedOnce: hasConnectedOnceRef,
    }),
}))

vi.mock('@/composables/useSettingsNavigation', () => ({
    useSettingsNavigation: () => ({
        restartingOverlay: restartingOverlayRef,
    }),
}))

const i18n = createI18n({
    legacy: false,
    locale: 'zh',
    messages: {
        zh: {
            systemResources: { overlayReconnecting: '连接断开，正在重连…' },
            settings: { restartingPleaseWait: '正在重启，请稍候…' },
        },
    },
})

const LucideStub = { template: '<span class="lucide-stub" />' }

function $(selector: string) {
    return document.body.querySelector(selector) as HTMLElement | null
}

function mountOverlay() {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const wrapper = mount(ConnectionOverlay, {
        attachTo: container,
        global: {
            plugins: [i18n],
            stubs: { 'lucide-vue-next': LucideStub },
        },
    })
    return { wrapper, container }
}

describe('ConnectionOverlay', () => {
    let activeContainer: HTMLDivElement | null = null

    beforeEach(() => {
        wsStatusRef.value = 'connected'
        hasConnectedOnceRef.value = false
        restartingOverlayRef.value = false
        vi.useFakeTimers()
    })

    afterEach(() => {
        vi.useRealTimers()
        document.body.querySelectorAll('.connection-overlay').forEach(el => el.remove())
        if (activeContainer?.parentNode) {
            document.body.removeChild(activeContainer)
            activeContainer = null
        }
    })

    async function mountAndWait() {
        const mounted = mountOverlay()
        activeContainer = mounted.container
        await nextTick()
        return mounted.wrapper
    }

    it('renders nothing while connected', async () => {
        hasConnectedOnceRef.value = true
        await mountAndWait()
        expect($('.connection-overlay')).toBeNull()
    })

    it('renders reconnect mask with server icon and text after delay', async () => {
        hasConnectedOnceRef.value = true
        await mountAndWait()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(1600)
        await nextTick()
        const overlay = $('.connection-overlay')
        expect(overlay).not.toBeNull()
        expect($('.connection-overlay__icon')).not.toBeNull()
        expect($('.connection-overlay__spinner')).not.toBeNull()
        expect($('.connection-overlay__text')?.textContent).toContain('连接断开，正在重连…')
    })

    it('does not render on cold start (never connected before)', async () => {
        await mountAndWait()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(1600)
        await nextTick()
        expect($('.connection-overlay')).toBeNull()
    })

    it('renders restart mask immediately with restart text, taking priority', async () => {
        hasConnectedOnceRef.value = true
        await mountAndWait()
        restartingOverlayRef.value = true
        wsStatusRef.value = 'reconnecting'
        await nextTick()
        const overlay = $('.connection-overlay')
        expect(overlay).not.toBeNull()
        expect($('.connection-overlay__text')?.textContent).toContain('正在重启，请稍候…')
    })

    it('hides the mask once connection is restored', async () => {
        hasConnectedOnceRef.value = true
        await mountAndWait()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(1600)
        await nextTick()
        expect($('.connection-overlay')).not.toBeNull()
        wsStatusRef.value = 'connected'
        await nextTick()
        expect($('.connection-overlay')).toBeNull()
    })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./scripts/vitest-run.sh web/src/components/common/__tests__/ConnectionOverlay.test.ts`
Expected: FAIL — component `ConnectionOverlay.vue` not found.

- [ ] **Step 3: Implement**

Create `web/src/components/common/ConnectionOverlay.vue`:

```vue
<template>
  <Teleport to="body">
    <Transition name="overlay-fade">
      <div v-if="mode" class="connection-overlay">
        <div class="connection-overlay__content">
          <Server :size="40" class="connection-overlay__icon" />
          <div class="connection-overlay__spinner"></div>
          <div class="connection-overlay__text">{{ overlayText }}</div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Server } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useConnectionOverlay } from '@/composables/useConnectionOverlay'

const { t } = useI18n()
const { mode } = useConnectionOverlay()

const overlayText = computed(() =>
  mode.value === 'restart'
    ? t('settings.restartingPleaseWait')
    : t('systemResources.overlayReconnecting'),
)
</script>

<style scoped>
.connection-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
}

.connection-overlay__content {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 20px;
  padding: 40px 48px;
  border-radius: 16px;
  background: var(--bg-primary);
  box-shadow: var(--shadow-md);
}

.connection-overlay__icon {
  color: var(--accent-color);
}

.connection-overlay__spinner {
  width: 36px;
  height: 36px;
  border: 3px solid var(--border-color);
  border-top-color: var(--accent-color);
  border-radius: 50%;
  animation: overlay-spin 0.8s linear infinite;
}

.connection-overlay__text {
  font-size: 15px;
  font-weight: 500;
  color: var(--text-primary);
  white-space: nowrap;
}

@keyframes overlay-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Fade transition (teleported to body) */
.overlay-fade-enter-active,
.overlay-fade-leave-active {
  transition: opacity 0.2s ease;
}
.overlay-fade-enter-from,
.overlay-fade-leave-to {
  opacity: 0;
}
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./scripts/vitest-run.sh web/src/components/common/__tests__/ConnectionOverlay.test.ts`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/common/ConnectionOverlay.vue web/src/components/common/__tests__/ConnectionOverlay.test.ts
git commit -m "feat(web): add fullscreen ConnectionOverlay component"
```

---

### Task 6: Mount in App.vue and remove old restart overlay

**Files:**
- Modify: `web/src/App.vue` (template ~line 15, imports ~line 339)
- Modify: `web/src/components/settings/SettingsPage.vue` (template lines 38-46, script line 71, CSS lines 281-319)

- [ ] **Step 1: Modify SettingsPage.vue — remove the old overlay**

Delete the restart loading overlay from the template (lines 38-46):

```vue
    <!-- Restart loading overlay -->
    <Teleport to="body">
      <div v-if="restartingOverlay" class="restart-overlay">
        <div class="restart-overlay__content">
          <div class="restart-overlay__spinner"></div>
          <div class="restart-overlay__text">{{ t('settings.restartingPleaseWait') }}</div>
        </div>
      </div>
    </Teleport>
```

In the script destructure (line 71), remove `restartingOverlay`:

```ts
  navStack, currentCategory, pushNav, popNav,
  restartDialogVisible, changedColdFields, needsRestart,
  restarting, restartingOverlay,          // ← remove restartingOverlay
  handleRestartNeeded, handleRestart,
  checkAllGuards,
} = useSettingsNavigation()
```

becomes:

```ts
  navStack, currentCategory, pushNav, popNav,
  restartDialogVisible, changedColdFields, needsRestart,
  restarting,
  handleRestartNeeded, handleRestart,
  checkAllGuards,
} = useSettingsNavigation()
```

Delete the `.restart-overlay` CSS block (lines 281-319), i.e. everything from `/* Restart loading overlay */` through `.restart-overlay__text { ... }`. **Keep `@keyframes spin` (lines 276-279)** — it is still used by `.settings-restart-btn__icon--spin`.

- [ ] **Step 2: Modify App.vue — mount the unified overlay**

Add the import near the other component imports. In the `<script setup>` block, after the `AppHeader` usage imports (find `useGlobalEvents` import at line 339):

```ts
import ConnectionOverlay from './components/common/ConnectionOverlay.vue'
```

Add `<ConnectionOverlay />` in the authenticated branch, right after `<AppHeader ... />` (after line 19, before `<main>`):

```vue
      <AppHeader
        :project-root="projectRoot"
        :home-dir="homeDir"
        @open-project-dialog="handleOpenProjectDialog"
      />
      <ConnectionOverlay />
```

- [ ] **Step 3: Run the affected tests**

Run: `./scripts/vitest-run.sh web/src/components/settings/__tests__/SettingsPage.test.ts`
Expected: PASS (SettingsPage.test.ts mocks `useSettingsNavigation` fully; it never asserts on `.restart-overlay`, so removing the markup is safe).

Run: `./scripts/vitest-run.sh web/src/components/common/__tests__/ConnectionOverlay.test.ts web/src/components/common/__tests__/SystemResourcesPanel.test.ts`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/App.vue web/src/components/settings/SettingsPage.vue
git commit -m "feat(web): unify fullscreen status overlay, remove old restart overlay"
```

---

### Task 7: Full verification

- [ ] **Step 1: Run the complete frontend test suite**

Run: `./scripts/vitest-run.sh web/src`
Expected: PASS.

- [ ] **Step 2: Run lint**

Run: `npm run lint`
Expected: no errors (watch for unused `restartingOverlay` import/variable if left anywhere).

- [ ] **Step 3: Run typecheck**

Run: `npm run typecheck`
Expected: no errors.

- [ ] **Step 4: Run pre-push checks (optional, before pushing)**

Run: `./scripts/pre-push-checks.sh --skip-coverage`
Expected: PASS.

---

## Self-review notes

- **Spec coverage:** Reconnect mask (1.5s delay) → Task 4/5. Restart unified into same component → Task 2/5/6. First-load protection (`hasConnectedOnce`) → Task 1/4. Covers APP Header (z-index 9999 > header 1100) → Task 5/6. Removed old restart overlay → Task 6.
- **Type consistency:** `mode` is always `ConnectionOverlayMode` (`'restart' | 'reconnect' | null`). `hasConnectedOnce` returned from `useGlobalEvents` matches usage in the composable. `restartingOverlay` module ref is exported and shared.
- **Edge cases covered by tests:** cold start, transient blip recovery, reconnect clears mask, restart priority, restart clears on false.
