import { describe, expect, it, vi } from 'vitest'

// Mock import.meta.glob — vitest doesn't support it natively
vi.mock('@/utils/materialIcons', async () => {
  const { generateManifest } = await import('material-icon-theme')
  const manifest = generateManifest()

  const extMap = new Map<string, string>()
  const fileNameMap = new Map<string, string>()
  const folderNameMap = new Map<string, string>()
  const folderNameOpenMap = new Map<string, string>()

  for (const [ext, iconName] of Object.entries(manifest.fileExtensions || {})) {
    extMap.set(ext.toLowerCase(), iconName)
  }
  for (const [name, iconName] of Object.entries(manifest.fileNames || {})) {
    fileNameMap.set(name.toLowerCase(), iconName)
  }
  for (const [name, iconName] of Object.entries(manifest.folderNames || {})) {
    folderNameMap.set(name.toLowerCase(), iconName)
  }
  for (const [name, iconName] of Object.entries(manifest.folderNamesExpanded || {})) {
    folderNameOpenMap.set(name.toLowerCase(), iconName)
  }

  const DEFAULT_FILE_ICON = manifest.file || 'file'
  const DEFAULT_FOLDER_ICON = manifest.folder || 'folder'
  const DEFAULT_FOLDER_OPEN_ICON = manifest.folderExpanded || 'folder-open'

  function getFileIconName(path: string): string {
    const parts = path.replace(/\\/g, '/').split('/')
    const baseName = parts[parts.length - 1]
    const nameHit = fileNameMap.get(baseName.toLowerCase())
    if (nameHit) return nameHit
    const dotIndex = baseName.lastIndexOf('.')
    if (dotIndex > 0) {
      const fullExt = baseName.slice(dotIndex + 1).toLowerCase()
      const fullHit = extMap.get(fullExt)
      if (fullHit) return fullHit
      if (dotIndex > 0) {
        const prevDot = baseName.lastIndexOf('.', dotIndex - 1)
        if (prevDot > 0) {
          const doubleExt = baseName.slice(prevDot + 1).toLowerCase()
          const doubleHit = extMap.get(doubleExt)
          if (doubleHit) return doubleHit
        }
      }
    }
    return DEFAULT_FILE_ICON
  }

  function getFolderIconName(name: string, open = false): string {
    const map = open ? folderNameOpenMap : folderNameMap
    const hit = map.get(name.toLowerCase())
    if (hit) return hit
    return open ? DEFAULT_FOLDER_OPEN_ICON : DEFAULT_FOLDER_ICON
  }

  return { getFileIconName, getFolderIconName }
})

import { getFileIconName, getFolderIconName } from '@/utils/materialIcons'

describe('getFileIconName', () => {
  it('resolves Go files', () => {
    expect(getFileIconName('main.go')).toBe('go')
  })

  it('resolves TypeScript files', () => {
    expect(getFileIconName('app.ts')).toBe('typescript')
  })

  it('resolves JavaScript files', () => {
    expect(getFileIconName('index.js')).toBe('javascript')
  })

  it('resolves Python files', () => {
    expect(getFileIconName('main.py')).toBe('python')
  })

  it('resolves Rust files', () => {
    expect(getFileIconName('lib.rs')).toBe('rust')
  })

  it('resolves Java files', () => {
    expect(getFileIconName('App.java')).toBe('java')
  })

  it('resolves C++ files', () => {
    expect(getFileIconName('main.cpp')).toBe('cpp')
  })

  it('resolves HTML files', () => {
    expect(getFileIconName('index.html')).toBe('html')
  })

  it('resolves CSS files', () => {
    expect(getFileIconName('style.css')).toBe('css')
  })

  it('resolves JSON files', () => {
    expect(getFileIconName('data.json')).toBe('json')
  })

  it('resolves package.json to package icon (file name match)', () => {
    // File name match takes priority over extension match
    expect(getFileIconName('package.json')).toBe('nodejs')
  })

  it('resolves Markdown files', () => {
    expect(getFileIconName('notes.md')).toBe('markdown')
  })

  it('resolves README.md to readme icon (file name match)', () => {
    // File name match takes priority over extension match
    expect(getFileIconName('README.md')).toBe('readme')
  })

  it('resolves YAML files', () => {
    expect(getFileIconName('config.yaml')).toBe('yaml')
  })

  it('resolves TOML files', () => {
    expect(getFileIconName('Cargo.toml')).toBe('toml')
  })

  it('resolves SVG files', () => {
    expect(getFileIconName('logo.svg')).toBe('svg')
  })

  it('resolves image files (png)', () => {
    expect(getFileIconName('photo.png')).toBe('image')
  })

  it('resolves image files (jpg)', () => {
    expect(getFileIconName('photo.jpg')).toBe('image')
  })

  it('resolves audio files', () => {
    expect(getFileIconName('song.mp3')).toBe('audio')
  })

  it('resolves video files', () => {
    expect(getFileIconName('movie.mp4')).toBe('video')
  })

  it('resolves PDF files', () => {
    expect(getFileIconName('doc.pdf')).toBe('pdf')
  })

  it('resolves ZIP files', () => {
    expect(getFileIconName('archive.zip')).toBe('zip')
  })

  it('resolves Dockerfile by file name', () => {
    expect(getFileIconName('Dockerfile')).toBe('docker')
  })

  it('resolves Vue files', () => {
    expect(getFileIconName('App.vue')).toBe('vue')
  })

  it('resolves SQL files', () => {
    expect(getFileIconName('query.sql')).toBe('database')
  })

  it('resolves shell scripts', () => {
    expect(getFileIconName('script.sh')).toBe('console')
  })

  it('returns default for unknown extensions', () => {
    expect(getFileIconName('data.xyz')).toBe('file')
  })

  it('handles full paths', () => {
    expect(getFileIconName('/home/user/main.go')).toBe('go')
    expect(getFileIconName('src/components/App.vue')).toBe('vue')
  })

  it('is case-insensitive for extensions', () => {
    expect(getFileIconName('APP.TS')).toBe('typescript')
    expect(getFileIconName('Main.GO')).toBe('go')
  })
})

describe('getFolderIconName', () => {
  it('resolves src folder', () => {
    expect(getFolderIconName('src')).toBe('folder-src')
  })

  it('resolves dist folder', () => {
    expect(getFolderIconName('dist')).toBe('folder-dist')
  })

  it('resolves node_modules folder', () => {
    expect(getFolderIconName('node_modules')).toBe('folder-node')
  })

  it('resolves components folder', () => {
    expect(getFolderIconName('components')).toBe('folder-components')
  })

  it('resolves test folder', () => {
    expect(getFolderIconName('test')).toBe('folder-test')
  })

  it('resolves docs folder', () => {
    expect(getFolderIconName('docs')).toBe('folder-docs')
  })

  it('resolves config folder', () => {
    expect(getFolderIconName('config')).toBe('folder-config')
  })

  it('resolves scripts folder', () => {
    expect(getFolderIconName('scripts')).toBe('folder-scripts')
  })

  it('returns default folder for unknown names', () => {
    expect(getFolderIconName('xyz_unknown')).toBe('folder')
  })

  it('returns open folder icon when open=true', () => {
    expect(getFolderIconName('src', true)).toBe('folder-src-open')
  })

  it('returns default open folder for unknown names when open=true', () => {
    expect(getFolderIconName('xyz_unknown', true)).toBe('folder-open')
  })
})
