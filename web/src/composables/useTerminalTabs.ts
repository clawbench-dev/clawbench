import { ref, reactive, computed, markRaw, type Ref } from 'vue'
import { useTerminalSession, type TerminalErrorMessages } from './useTerminalSession'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import type { Terminal as TerminalType } from '@xterm/xterm'
import { stripSyncOutput } from '@/utils/terminalSessionUtils'

export interface TerminalTab {
  id: string
  sessionId: string
  cwd: string
  title: string
  session: ReturnType<typeof useTerminalSession>
  xterm: TerminalType | null
  fitAddon: FitAddon | null
  container: HTMLElement | null
}

let tabCounter = 0

function generateTabId(): string {
  return `tab-${++tabCounter}-${Date.now()}`
}

/** Extract last directory segment from a path for tab title. */
function cwdToTitle(cwd: string): string {
  if (!cwd) return 'Terminal'
  const cleaned = cwd.replace(/\/+$/, '')
  const last = cleaned.split('/').pop() || ''
  return last || 'Terminal'
}

export function useTerminalTabs(
  getWsUrl: (cwd?: string) => string,
  opts: {
    fontSize: Ref<number>
    getXtermTheme: () => Record<string, unknown>
    errorMessages: TerminalErrorMessages
    /** Called when a tab is closed but the WS was already disconnected.
     *  The backend PTY session needs to be killed via HTTP API. */
    onCloseSessionViaHttp?: (sessionId: string) => void
    onExit?: (tabId: string) => void
    onError?: (tabId: string, message: string, code: string) => void
    onAutoExec?: (tabId: string, command: string) => void
    toast?: (msg: string, opts?: Record<string, unknown>) => void
    /** Transform keyboard input through modifier key processing (Ctrl/Alt/Shift combos) */
    processInput?: (data: string) => string
  },
) {
  const tabs: Ref<TerminalTab[]> = ref([])
  const activeTabId = ref('')

  const activeTab = computed(() =>
    tabs.value.find((t) => t.id === activeTabId.value) || null,
  )

  function createXtermInstance(): { term: TerminalType; fit: FitAddon } {
    const term = new Terminal({
      theme: opts.getXtermTheme(),
      fontSize: opts.fontSize.value,
      fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace",
      cursorBlink: true,
      convertEol: false,
      scrollback: 5000,
      rightClickSelectsWord: true,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.loadAddon(new WebLinksAddon())
    ;(term as any).fitAddon = fit
    return { term, fit }
  }

  function createTab(cwd?: string): TerminalTab {
    const id = generateTabId()
    const resolvedCwd = cwd || ''
    const title = cwdToTitle(resolvedCwd)
    const session = useTerminalSession(() => getWsUrl(resolvedCwd), opts.errorMessages)
    const { term, fit } = createXtermInstance()

    // Mark xterm/addon instances as raw so Vue's reactive() does NOT wrap
    // them in a Proxy.  xterm.js accesses internal services (coreService,
    // bufferService, renderService…) via `this` — a Proxy intercepts every
    // property access, which can break the rendering pipeline for TUI apps
    // that use alternate screen buffers and rapid updates (e.g. OpenCode).
    const tab = reactive<TerminalTab>({
      id,
      sessionId: '',
      cwd: resolvedCwd,
      title,
      session,
      xterm: markRaw(term),
      fitAddon: markRaw(fit),
      container: null,
    })

    // Wire session callbacks
    // On reconnect, the backend sends a replay buffer then suppresses output
    // until the first resize completes (to avoid duplicate prompts from
    // SIGWINCH). The frontend just clears the terminal and writes the replay.
    // Strip DEC mode 2026 (Synchronized Output) from PTY output before
    // writing to xterm.js. TUI apps (vim, OpenCode/Bubble Tea) send
    // \x1b[?2026h before each rendered frame and \x1b[?2026l after.
    // xterm.js buffers all rendering while mode 2026 is active, only
    // flushing on mode-off or a 1-second safety timeout.  In a remote
    // terminal the round-trip latency and WriteBuffer's 12 ms time-slicing
    // can cause the safety timeout to fire while the next \x1b[?2026h is
    // already queued — the renderer sees sync-on again and skips the
    // frame, leaving the alternate screen frozen (key input reaches the
    // PTY but the response is never rendered).  Stripping the sequences
    // is safe: the local xterm.js renderer has no perceptible benefit
    // from batched updates since we are streaming data over a WebSocket.
    session.setCallbacks({
      onOutput: (data: string) => {
        term.write(stripSyncOutput(data))
      },
      onReplay: (data: string) => {
        // Clear xterm buffer and replace with replay data — discards any
        // stale content left over from before the disconnect.
        // The backend suppresses output after replay until the first resize
        // (triggered by fit()) completes, so no duplicate prompt appears.
        term.reset()
        term.write(stripSyncOutput(data))
      },
      onStatus: (status: { running: boolean; cwd: string }) => {
        if (status.cwd) {
          tab.cwd = status.cwd
          tab.title = cwdToTitle(status.cwd)
        }
      },
      onExit: () => {
        opts.onExit?.(id)
      },
      onError: (message: string, code: string) => {
        opts.onError?.(id, message, code)
      },
    })

    // Handle terminal input — transform through modifier key processing
    term.onData((data) => {
      const transformed = opts.processInput ? opts.processInput(data) : data
      if (transformed) session.sendInput(transformed)
    })

    // Send resize to backend
    term.onResize(({ cols, rows }) => {
      session.sendResize(cols, rows)
    })

    // Watch session ID updates — synced manually via syncTabSessionId()

    tabs.value.push(tab as unknown as TerminalTab)
    activeTabId.value = id
    return tab as unknown as TerminalTab
  }

  function closeTab(id: string): { switchToId: string | null } {
    const idx = tabs.value.findIndex((t) => t.id === id)
    if (idx === -1) return { switchToId: null }

    const tab = tabs.value[idx]

    // Save sessionId and wsOpen before sendClose clears them
    const sessionId = tab.session.sessionId as unknown as string
    const wasOpen = tab.session.wsOpen as unknown as boolean

    // Kill PTY: send WS close message, or fall back to HTTP API if WS is disconnected
    tab.session.sendClose()
    if (sessionId && !wasOpen) {
      // WS was already closed — sendClose couldn't reach the backend.
      // Use HTTP API to kill the orphaned PTY session.
      opts.onCloseSessionViaHttp?.(sessionId)
    }

    // Dispose xterm
    if (tab.xterm) {
      tab.xterm.dispose()
    }

    // Remove from array
    tabs.value.splice(idx, 1)

    // If we closed the active tab, switch to adjacent
    if (tabs.value.length > 0 && activeTabId.value === id) {
      const newIdx = Math.min(idx, tabs.value.length - 1)
      activeTabId.value = tabs.value[newIdx].id
    }

    return { switchToId: tabs.value.length > 0 ? activeTabId.value : null }
  }

  function switchTab(id: string) {
    const tab = tabs.value.find((t) => t.id === id)
    if (!tab) return

    activeTabId.value = id

    // Fit the newly active terminal after a tick (DOM needs to be visible)
    if (tab.fitAddon && tab.container) {
      requestAnimationFrame(() => {
        try {
          tab.fitAddon?.fit()
        } catch {
          // May fail if container is not yet visible
        }
      })
    }

    // Focus the terminal
    tab.xterm?.focus()
  }

  /** Update tab CWD and title (called when session status reports new cwd). */
  function updateTabCwd(id: string, cwd: string) {
    const tab = tabs.value.find((t) => t.id === id)
    if (tab) {
      tab.cwd = cwd
      tab.title = cwdToTitle(cwd)
    }
  }

  /** Sync session ID from session composable to tab record. */
  function syncTabSessionId(id: string) {
    const tab = tabs.value.find((t) => t.id === id)
    if (tab) {
      tab.sessionId = tab.session.sessionId as unknown as string
    }
  }

  /** Mount an xterm instance to its container element. */
  function mountTabXterm(tab: TerminalTab, container: HTMLElement) {
    if (!tab.xterm) return

    // Always record the container, even if xterm is already open
    tab.container = container

    if (tab.xterm.element) return // Already mounted

    tab.xterm.open(container)
    // Note: wheel/contextmenu handlers are added by TerminalPanelContent.mountTabToContainer()
  }

  /** Disconnect all tabs (called when terminal panel becomes inactive). */
  function disconnectAll() {
    for (const tab of tabs.value) {
      tab.session.disconnect()
      // Sync session ID after disconnect (it's preserved for reconnect)
      syncTabSessionId(tab.id)
    }
  }

  /** Connect a specific tab (called when switching to it or panel becomes active). */
  async function connectTab(id: string) {
    const tab = tabs.value.find((t) => t.id === id)
    if (!tab) return

    syncTabSessionId(tab.id)

    if ((tab.session.connectionState as unknown as string) === 'disconnected') {
      try {
        await tab.session.connect()
        syncTabSessionId(tab.id)
      } catch {
        // Error will be shown via overlay
      }
    }
  }

  /** Connect the active tab (called when panel becomes active). */
  async function connectActiveTab() {
    if (activeTab.value) {
      await connectTab(activeTab.value.id)
    }
  }

  /** Dispose all tabs (called on component unmount). */
  function disposeAll() {
    for (const tab of tabs.value) {
      const sessionId = tab.session.sessionId as unknown as string
      const wasOpen = tab.session.wsOpen as unknown as boolean
      tab.session.sendClose()
      // If WS was disconnected, sendClose couldn't reach the backend.
      // Use HTTP API to kill the PTY session.
      if (sessionId && !wasOpen) {
        opts.onCloseSessionViaHttp?.(sessionId)
      }
      if (tab.xterm) {
        tab.xterm.dispose()
      }
    }
    tabs.value = []
    activeTabId.value = ''
  }

  /** Update font size on all xterm instances. */
  function updateFontSize(size: number) {
    for (const tab of tabs.value) {
      if (tab.xterm) {
        tab.xterm.options.fontSize = size
      }
    }
  }

  /** Update theme on all xterm instances. */
  function updateTheme(theme: Record<string, unknown>) {
    for (const tab of tabs.value) {
      if (tab.xterm) {
        tab.xterm.options.theme = theme
      }
    }
  }

  /** Get a tab by its local ID. */
  function getTab(id: string): TerminalTab | undefined {
    return tabs.value.find((t) => t.id === id)
  }

  // Initialize with one default tab
  createTab()

  return {
    tabs,
    activeTabId,
    activeTab,
    createTab,
    closeTab,
    switchTab,
    updateTabCwd,
    syncTabSessionId,
    mountTabXterm,
    disconnectAll,
    connectTab,
    connectActiveTab,
    disposeAll,
    updateFontSize,
    updateTheme,
    getTab,
  }
}
