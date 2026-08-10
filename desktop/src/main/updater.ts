import { app } from 'electron'
import https from 'node:https'
import path from 'node:path'
import fs from 'node:fs'
import os from 'node:os'
import { getDesktopPkg, latestUrl, rewriteTarball, parseNpmLatest } from '../shared/registry'
import { verifyIntegrity } from '../shared/integrity'

export function isChinaMainland(): boolean {
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  return ['Asia/Shanghai', 'Asia/Chongqing', 'Asia/Urumqi', 'Asia/Harbin'].includes(tz)
}

export async function checkForUpdate(): Promise<{ hasUpdate: boolean; version: string; tarball: string }> {
  const pkg = getDesktopPkg(process.platform as NodeJS.Platform, process.arch)
  if (!pkg) return { hasUpdate: false, version: '', tarball: '' }
  const china = isChinaMainland()
  const json = await httpGet(latestUrl(pkg, china))
  const info = parseNpmLatest(json)
  const current = app.getVersion()
  const hasUpdate = current !== info.version
  return { hasUpdate, version: info.version, tarball: rewriteTarball(info.tarball, china) }
}

function httpGet(url: string): Promise<string> {
  return new Promise((resolve, reject) => {
    https.get(url, (res) => {
      let data = ''
      res.on('data', c => { data += c })
      res.on('end', () => resolve(data))
    }).on('error', reject)
  })
}

export async function downloadAndInstall(tarballUrl: string, version: string, integrity: string): Promise<string> {
  const buf = await httpGetBuffer(tarballUrl)
  if (integrity && !verifyIntegrity(buf, integrity)) throw new Error('integrity verification failed')
  const destDir = path.join(os.homedir(), '.clawbench-desktop', `app-${version}`)
  fs.mkdirSync(destDir, { recursive: true })
  return destDir
}

function httpGetBuffer(url: string): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    https.get(url, (res) => {
      const chunks: Buffer[] = []
      res.on('data', c => chunks.push(c))
      res.on('end', () => resolve(Buffer.concat(chunks)))
    }).on('error', reject)
  })
}
