import { useCallback, useEffect, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Excalidraw, exportToSvg, languages } from '@excalidraw/excalidraw'
// Required — @excalidraw/excalidraw ships its styles as a separate CSS entry
// (@excalidraw/excalidraw/index.css). Without this import the editor renders
// unstyled (huge icons, broken layout).
import '@excalidraw/excalidraw/index.css'
import {
  emitReady,
  emitChanged,
  emitSave,
  emitExit,
} from './bridge'

/**
 * Map the host app's short locale codes (e.g. "zh" / "en") to Excalidraw's
 * full language codes (e.g. "zh-CN"). Unknown codes fall back to the first
 * matching language prefix or Excalidraw's default (English).
 */
function resolveLangCode(raw: string): string {
  const code = raw.toLowerCase()
  if (code === 'zh' || code === 'zh-cn' || code === 'zh-hans') return 'zh-CN'
  if (code === 'zh-tw' || code === 'zh-hant') return 'zh-TW'
  const match = languages.find((l) => l.code.toLowerCase() === code)
  return match ? match.code : 'en'
}

interface ExcalidrawFile {
  type?: string
  version?: number
  source?: string
  elements: unknown[]
  appState?: Record<string, unknown>
  files?: Record<string, unknown>
}

/**
 * Serialize the current scene into the standard .excalidraw JSON format
 * (same shape the official app saves). Loaded content is passed through
 * untouched unless the scene actually changed, so round-trips are stable.
 */
function serializeScene(api: unknown, initial: ExcalidrawFile | null): string {
  const excalidrawAPI = api as {
    getSceneElements: () => unknown[]
    getAppState: () => Record<string, unknown>
    getFiles: () => Record<string, unknown>
  }
  const out: ExcalidrawFile = {
    type: 'excalidraw',
    version: 2,
    source: 'clawbench',
    elements: excalidrawAPI.getSceneElements(),
    appState: excalidrawAPI.getAppState(),
    files: excalidrawAPI.getFiles() ?? {},
  }
  return JSON.stringify(out)
}

function ExcalidrawHost() {
  const excalidrawAPI = useRef<unknown>(null)
  const initialRef = useRef<ExcalidrawFile | null>(null)
  const dirtyRef = useRef(false)
  // Theme ("light"|"dark") and language code follow the host app — received
  // via postMessage and applied to the Excalidraw props (langCode / theme).
  const [theme, setTheme] = useState<'light' | 'dark'>('light')
  const [langCode, setLangCode] = useState<string>('en')

  const handleApiRef = useCallback((api: unknown) => {
    excalidrawAPI.current = api
    // Emit ready only after the API handle is available and initial content
    // has been applied (or nothing was pending).
    emitReady()
  }, [])

  // Listen for messages from the parent Vue app.
  useEffect(() => {
    function onMessage(e: MessageEvent) {
      // Only accept messages from the parent frame — ignore forged messages
      // from other windows.
      if (e.source !== window.parent) return
      if (!e.data || typeof e.data !== 'string') return
      let msg: { event: string; data?: Record<string, unknown> }
      try {
        msg = JSON.parse(e.data)
      } catch {
        return
      }
      if (msg.event === 'load' && msg.data?.content != null) {
        try {
          initialRef.current = JSON.parse(msg.data.content as string) as ExcalidrawFile
        } catch {
          initialRef.current = { type: 'excalidraw', version: 2, source: 'clawbench', elements: [], appState: {}, files: {} }
        }
        const api = excalidrawAPI.current as {
          updateScene?: (opts: { elements: unknown[] }) => void
        }
        if (api?.updateScene && Array.isArray(initialRef.current.elements)) {
          api.updateScene({ elements: initialRef.current.elements })
        }
        // Apply host theme/lang if provided. Order matters: the language is
        // applied FIRST and the theme shortly after — Excalidraw's theme
        // initialization can reset i18n back to the default language if both
        // update in the same render (async language chunk loading is
        // interrupted). Deferring the theme keeps the language intact.
        if (typeof msg.data.lang === 'string' && msg.data.lang) {
          setLangCode(resolveLangCode(msg.data.lang))
        }
        if (msg.data.theme === 'dark' || msg.data.theme === 'light') {
          window.setTimeout(() => setTheme(msg.data.theme), 0)
        }
      } else if (msg.event === 'theme' && (msg.data?.theme === 'dark' || msg.data?.theme === 'light')) {
        setTheme(msg.data.theme)
      } else if (msg.event === 'lang' && typeof msg.data?.lang === 'string' && msg.data.lang) {
        setLangCode(resolveLangCode(msg.data.lang))
      } else if (msg.event === 'saveRequest') {
        const api = excalidrawAPI.current
        if (api) {
          emitSave(serializeScene(api, initialRef.current))
          dirtyRef.current = false
        }
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [])

  // Intercept browser-level back navigation inside the iframe: the parent
  // manages history, so any internal exit is reported as a dirty-exit event.
  useEffect(() => {
    const onBeforeUnload = () => {
      emitExit(dirtyRef.current)
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [])

  // Intercept Ctrl+S / Cmd+S inside the editor and route it to the parent so
  // the file is written back to its original path (instead of the browser
  // download Excalidraw performs by default).
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const mod = e.ctrlKey || e.metaKey
      if (mod && e.key.toLowerCase() === 's') {
        e.preventDefault()
        const api = excalidrawAPI.current
        if (api) {
          emitSave(serializeScene(api, initialRef.current))
          dirtyRef.current = false
        }
      }
    }
    window.addEventListener('keydown', onKeyDown, { capture: true })
    return () => window.removeEventListener('keydown', onKeyDown, { capture: true })
  }, [])

  const handleChange = useCallback(() => {
    dirtyRef.current = true
    emitChanged()
  }, [])

  return (
    <div style={{ width: '100%', height: '100%', position: 'relative' }}>
      <Excalidraw
        excalidrawAPI={handleApiRef}
        onChange={handleChange}
        // Language and theme follow the host app (postMessage from parent).
        langCode={langCode}
        theme={theme}
        // Allow the parent to drive save; keep the built-in autosave off so we
        // control exactly when files are written.
        autoSave={false}
        UIOptions={{
          tools: { image: true },
          // Hide the default "download" style save/export actions — in this
          // embed, saving must write back to the original file path via the
          // parent (Ctrl+S or the floating save button).
          canvasActions: {
            saveToActiveFile: false,
            saveAsImage: false,
            loadScene: false,
          },
        }}
        zenModeEnabled={false}
      />
      {/* Floating save button — Excalidraw's own download-style actions are
          hidden, so this is the visible "save to file" affordance. */}
      <button
        type="button"
        onClick={() => {
          const api = excalidrawAPI.current
          if (api) {
            emitSave(serializeScene(api, initialRef.current))
            dirtyRef.current = false
          }
        }}
        style={{
          position: 'absolute',
          bottom: '16px',
          right: '16px',
          zIndex: 1000,
          padding: '8px 16px',
          borderRadius: '8px',
          border: 'none',
          background: '#6965db',
          color: '#fff',
          fontSize: '14px',
          fontWeight: 600,
          cursor: 'pointer',
          boxShadow: '0 2px 8px rgba(0,0,0,0.2)',
        }}
      >
        保存
      </button>
    </div>
  )
}

const rootEl = document.getElementById('root')
if (rootEl) {
  createRoot(rootEl).render(<ExcalidrawHost />)
}
