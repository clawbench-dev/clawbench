import { describe, expect, it, vi, afterEach } from 'vitest'

// The real module no longer uses import.meta.glob — icons are static assets
// at /material-icons/<name>.svg, resolved by URL. getIconUrl() issues a HEAD
// fetch to verify the asset exists; we stub the global fetch here.
import {
  getFileIconName,
  getFolderIconName,
  getIconUrl,
  getFileIconUrl,
  getFolderIconUrl,
} from '@/utils/materialIcons'

function stubFetch(ok: boolean) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok }))
}

afterEach(() => {
  vi.unstubAllGlobals()
})

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

  it('returns default for files with no extension', () => {
    expect(getFileIconName('Makefile')).toBe('makefile')
  })

  it('returns default for files starting with dot', () => {
    expect(getFileIconName('.gitignore')).toBe('git')
  })

  it('handles Windows-style backslash paths', () => {
    expect(getFileIconName('C:\\Users\\dev\\main.go')).toBe('go')
    expect(getFileIconName('src\\components\\App.vue')).toBe('vue')
  })

  it('resolves double extension (e.g., .yml.dist → yaml)', () => {
    // file.yml.dist: fullExt="dist" → not found, doubleExt="yml.dist" → "yaml"
    expect(getFileIconName('config.yml.dist')).toBe('yaml')
  })

  it('full extension match takes priority over double extension', () => {
    expect(getFileIconName('data.json')).toBe('json')
  })

  it('returns default for single dot prefix with no real extension', () => {
    expect(getFileIconName('.unknown_hidden')).toBe('file')
  })

  it('handles deeply nested paths', () => {
    expect(getFileIconName('/a/b/c/d/e/f/g/main.go')).toBe('go')
  })

  it('is case-insensitive for file names', () => {
    expect(getFileIconName('DOCKERFILE')).toBe('docker')
    expect(getFileIconName('package.JSON')).toBe('nodejs')
  })

  it('file name match takes priority over extension match', () => {
    expect(getFileIconName('package.json')).toBe('nodejs')
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

  it('is case-insensitive for folder names', () => {
    expect(getFolderIconName('SRC')).toBe('folder-src')
    expect(getFolderIconName('Dist')).toBe('folder-dist')
  })

  it('default open parameter is false', () => {
    expect(getFolderIconName('src')).toBe('folder-src')
    expect(getFolderIconName('src')).not.toBe('folder-src-open')
  })
})

// URL-related tests use distinct icon names so module-level caching between
// tests cannot mask the behavior under test.
describe('getIconUrl', () => {
  it('returns the static URL when the icon asset exists', async () => {
    stubFetch(true)
    const url = await getIconUrl('go')
    expect(url).toBe('/material-icons/go.svg')
  })

  it('returns undefined for icons missing from the package', async () => {
    stubFetch(false)
    const url = await getIconUrl('nonexistent-icon-xyz')
    expect(url).toBeUndefined()
  })

  it('caches the result and does not re-fetch on second call', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const first = await getIconUrl('python')
    const second = await getIconUrl('python')
    expect(first).toBe('/material-icons/python.svg')
    expect(second).toBe(first)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('dedups concurrent loads for the same icon', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const [a, b, c] = await Promise.all([
      getIconUrl('rust'),
      getIconUrl('rust'),
      getIconUrl('rust'),
    ])
    expect(a).toBe('/material-icons/rust.svg')
    expect(b).toBe(a)
    expect(c).toBe(a)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})

describe('getFileIconUrl', () => {
  it('returns empty string when no icon URL is available', async () => {
    stubFetch(false)
    // data.xyz resolves to the default "file" icon, which has not been cached
    // by earlier tests, so the fetch stub decides the outcome.
    const url = await getFileIconUrl('data.xyz')
    expect(url).toBe('')
  })

  it('resolves the icon URL for a known file type', async () => {
    stubFetch(true)
    const url = await getFileIconUrl('main.cpp')
    expect(url).toBe('/material-icons/cpp.svg')
  })
})

describe('getFolderIconUrl', () => {
  it('returns empty string when no icon URL is available', async () => {
    stubFetch(false)
    const url = await getFolderIconUrl('src')
    expect(url).toBe('')
  })

  it('passes open parameter correctly', async () => {
    stubFetch(true)
    const closedUrl = await getFolderIconUrl('dist', false)
    const openUrl = await getFolderIconUrl('dist', true)
    expect(closedUrl).toBe('/material-icons/folder-dist.svg')
    expect(openUrl).toBe('/material-icons/folder-dist-open.svg')
  })
})
