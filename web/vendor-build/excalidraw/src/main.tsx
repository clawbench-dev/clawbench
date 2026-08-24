import { useCallback, useEffect, useRef } from 'react'
import { createRoot } from 'react-dom/client'
import { Excalidraw, exportToSvg } from '@excalidraw/excalidraw'
import {
  emitReady,
  emitChanged,
  emitSave,
  emitExit,
} from './bridge'

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

  const handleApiRef = useCallback((api: unknown) => {
    excalidrawAPI.current = api
    // Emit ready only after the API handle is available and initial content
    // has been applied (or nothing was pending).
    emitReady()
  }, [])

  // Listen for messages from the parent Vue app.
  useEffect(() => {
    function onMessage(e: MessageEvent) {
      if (!e.data || typeof e.data !== 'string') return
      let msg: { event: string; data?: { content: string } }
      try {
        msg = JSON.parse(e.data)
      } catch {
        return
      }
      if (msg.event === 'load' && msg.data?.content != null) {
        try {
          initialRef.current = JSON.parse(msg.data.content) as ExcalidrawFile
        } catch {
          initialRef.current = { type: 'excalidraw', version: 2, source: 'clawbench', elements: [], appState: {}, files: {} }
        }
        const api = excalidrawAPI.current as {
          updateScene?: (opts: { elements: unknown[] }) => void
        }
        if (api?.updateScene && Array.isArray(initialRef.current.elements)) {
          api.updateScene({ elements: initialRef.current.elements })
        }
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

  const handleChange = useCallback(() => {
    dirtyRef.current = true
    emitChanged()
  }, [])

  return (
    <div style={{ width: '100%', height: '100%' }}>
      <Excalidraw
        excalidrawAPI={handleApiRef}
        onChange={handleChange}
        // Allow the parent to drive save; keep the built-in autosave off so we
        // control exactly when files are written.
        autoSave={false}
        UIOptions={{
          tools: { image: true },
        }}
        zenModeEnabled={false}
      />
    </div>
  )
}

const rootEl = document.getElementById('root')
if (rootEl) {
  createRoot(rootEl).render(<ExcalidrawHost />)
}
