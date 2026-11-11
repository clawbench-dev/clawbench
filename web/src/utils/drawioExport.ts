import { getNative } from '@/utils/clawbenchNative'

/**
 * Turning a drawio export into a file the user can keep.
 *
 * drawio answers `{action:'export'}` with a data URI (base64) for images and a
 * raw XML string for the `xml` format. Two destinations are offered: the
 * browser/device download, and the project directory the diagram lives in.
 */

export interface DrawioExportFormat {
  /** drawio's own format id, sent verbatim in the export request. */
  id: 'png' | 'svg' | 'xml'
  /** File extension for the produced artifact. */
  ext: string
  mime: string
  /** i18n key for the picker label. */
  labelKey: string
}

/**
 * Only formats the open-source editor actually answers.
 *
 * PDF is deliberately absent: asking for it comes back as an SVG, because this
 * build has no PDF renderer — offering it would hand the user a .pdf file whose
 * bytes are SVG.
 */
export const DRAWIO_EXPORT_FORMATS: DrawioExportFormat[] = [
  { id: 'png', ext: 'png', mime: 'image/png', labelKey: 'file.drawioExport.formatPng' },
  { id: 'svg', ext: 'svg', mime: 'image/svg+xml', labelKey: 'file.drawioExport.formatSvg' },
  { id: 'xml', ext: 'drawio', mime: 'application/xml', labelKey: 'file.drawioExport.formatXml' },
]

/** `docs/flow.drawio` + png → `flow.png` (same directory, same base name). */
export function exportedFileName(sourceName: string, ext: string): string {
  const base = (sourceName || 'diagram').replace(/\.[^.]+$/, '')
  return `${base}.${ext}`
}

/** Split `data:<mime>;base64,<payload>` into its two halves. */
export function parseDataUri(uri: string): { mime: string; base64: string } {
  const match = /^data:([^;,]+)?(?:;charset=[^;,]+)?;base64,(.*)$/s.exec(uri)
  if (!match) throw new Error('not a base64 data URI')
  return { mime: match[1] || 'application/octet-stream', base64: match[2] }
}

function base64ToArrayBuffer(base64: string): ArrayBuffer {
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes.buffer as ArrayBuffer
}

/** The exported bytes, whatever shape drawio answered in. */
export function exportPayload(data: string): { blob: Blob; base64: string | null } {
  // XML comes back as plain text; images as a base64 data URI.
  if (!data.startsWith('data:')) {
    return { blob: new Blob([data], { type: 'application/xml' }), base64: null }
  }
  const { mime, base64 } = parseDataUri(data)
  return { blob: new Blob([base64ToArrayBuffer(base64)], { type: mime }), base64 }
}

/**
 * Hand the file to the user's device.
 *
 * The Android shell takes base64 directly (`downloadBlob`), which avoids
 * materialising a blob URL the WebView cannot follow; the browser gets an
 * object URL on an anchor.
 */
export function downloadExport(data: string, filename: string) {
  const { blob, base64 } = exportPayload(data)
  const native = getNative()
  if (native?.downloadBlob && base64) {
    void native.downloadBlob(base64, filename)
    return
  }
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  setTimeout(() => {
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }, 1000)
}

/**
 * Write the artifact next to the diagram, on the server.
 *
 * Goes through the upload endpoint rather than the text-write one: exports are
 * binary, and that endpoint already resolves name conflicts by appending a
 * number instead of silently overwriting a previous export.
 */
export async function saveExportToProject(
  data: string,
  filename: string,
  dir: string,
): Promise<string> {
  const { blob } = exportPayload(data)
  const form = new FormData()
  form.append('file', blob, filename)
  form.append('dir', dir)

  const resp = await fetch('/api/upload/file', { method: 'POST', body: form })
  const body = (await resp.json().catch(() => ({}))) as { path?: string; error?: string }
  if (!resp.ok || !body.path) {
    throw new Error(body.error || `HTTP ${resp.status}`)
  }
  return body.path
}
