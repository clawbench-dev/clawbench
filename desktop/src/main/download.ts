import { dialog, shell } from 'electron'
import fs from 'node:fs'
import path from 'node:path'
import http from 'node:http'
import https from 'node:https'
import { getStore } from './store'

function pickSavePath(defaultName: string): Promise<string | null> {
  return dialog.showSaveDialog({ defaultPath: defaultName }).then(r => r.canceled || !r.filePath ? null : r.filePath)
}

function resolveLocalFileUrl(filePath: string): string {
  const base = getStore().get('serverUrl') || ''
  if (filePath.startsWith('/')) {
    return `${base}/api/local-file/?download=1&path=${encodeURIComponent(filePath)}`
  }
  return `${base}/api/local-file/${filePath.split('/').map(encodeURIComponent).join('/')}?download=1`
}

async function fetchToFile(url: string, dest: string): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const lib = url.startsWith('https:') ? https : http
    lib.get(url, (res: import('node:http').IncomingMessage) => {
      if (res.statusCode && res.statusCode >= 400) { res.resume(); reject(new Error(`HTTP ${res.statusCode}`)); return }
      const f = fs.createWriteStream(dest)
      res.pipe(f).on('finish', () => { f.close(); resolve() }).on('error', reject)
    }).on('error', reject)
  })
}

export async function downloadFileByPathTo(filePath: string, dest: string): Promise<void> {
  await fetchToFile(resolveLocalFileUrl(filePath), dest)
}

export async function downloadFileByPath(filePath: string): Promise<void> {
  const name = path.basename(filePath)
  const dest = await pickSavePath(name)
  if (!dest) return
  await downloadFileByPathTo(filePath, dest)
  shell.showItemInFolder(dest)
}

export async function downloadByUrl(url: string, fileName: string): Promise<void> {
  const dest = await pickSavePath(fileName || path.basename(url))
  if (!dest) return
  await new Promise<void>((resolve, reject) => {
    https.get(url, (res) => {
      const f = fs.createWriteStream(dest)
      res.pipe(f).on('finish', () => { f.close(); resolve() }).on('error', reject)
    }).on('error', reject)
  })
  shell.showItemInFolder(dest)
}

export async function downloadBlob(base64: string, fileName: string): Promise<void> {
  const dest = await pickSavePath(fileName)
  if (!dest) return
  fs.writeFileSync(dest, Buffer.from(base64, 'base64'))
  shell.showItemInFolder(dest)
}
