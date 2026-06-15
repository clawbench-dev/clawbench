/**
 * Language-aware import path resolution for code viewer.
 *
 * Resolves import/require/use statements in Go, Rust, Python, PHP, and
 * Java/Kotlin to candidate file paths within the project. All candidates
 * are verified via batch-exists before showing as clickable links.
 *
 * Performance model mirrors useFilePathAnnotation:
 *   - Module-level config caches with in-flight dedup
 *   - Lazy config fetch (triggers on first import resolution for that language)
 *   - Optimistic annotation + async verification via verifyFilePaths()
 */
import { store } from '@/stores/app.ts'

// ─── Data Structures ─────────────────────────────────────────────

/** Result of resolving a single import statement */
export interface ImportResolution {
  /** Original import path text (e.g. "myproject/internal/service") */
  importPath: string
  /** Candidate project-relative paths in priority order */
  candidates: string[]
  /** HLJS span element containing this import path */
  span: HTMLElement
  /** Text inside the span to wrap with annotation */
  displayText: string
}

/** Parsed go.mod data */
interface GoModConfig {
  modulePath: string // e.g. "github.com/user/myproject"
}

/** Parsed composer.json autoload data */
interface ComposerAutoload {
  psr4: Map<string, string> // namespace prefix → directory, e.g. "App\\" → "src/"
}

/** Python source root info */
interface PythonConfig {
  sourceRoots: string[] // e.g. ["src", "."]
}

// ─── Config Cache ────────────────────────────────────────────────

const goModCache = new Map<string, GoModConfig>()
const composerJsonCache = new Map<string, ComposerAutoload>()
const pythonConfigCache = new Map<string, PythonConfig>()
const pendingFetches = new Map<string, Promise<any>>()

/**
 * Fetch a project config file, parse it, and cache the result.
 * Deduplicates in-flight requests. Returns null if fetch fails or parse fails.
 */
async function fetchProjectConfig<T>(
  projectRoot: string,
  configPath: string,
  parser: (text: string) => T | null,
  cache: Map<string, T>,
): Promise<T | null> {
  const cached = cache.get(projectRoot)
  if (cached !== undefined) return cached

  const key = `${projectRoot}:${configPath}`
  if (pendingFetches.has(key)) return pendingFetches.get(key) as Promise<T | null>

  const promise = (async () => {
    try {
      const resp = await fetch(`/api/file/${encodeURIComponent(configPath)}`)
      if (!resp.ok) {
        cache.set(projectRoot, null as any) // negative cache: won't retry
        return null
      }
      const text = await resp.text()
      const parsed = parser(text)
      if (parsed) {
        cache.set(projectRoot, parsed)
      } else {
        cache.set(projectRoot, null as any)
      }
      return parsed
    } catch {
      return null
    } finally {
      pendingFetches.delete(key)
    }
  })()

  pendingFetches.set(key, promise)
  return promise
}

/** Clear all config caches (on project switch). */
export function clearImportConfigCache(): void {
  goModCache.clear()
  composerJsonCache.clear()
  pythonConfigCache.clear()
  pendingFetches.clear()
}

// ─── Config Parsers ──────────────────────────────────────────────

function parseGoMod(text: string): GoModConfig | null {
  const match = text.match(/^module\s+(\S+)/m)
  if (!match) return null
  return { modulePath: match[1] }
}

function parseComposerJson(text: string): ComposerAutoload | null {
  try {
    const json = JSON.parse(text)
    const psr4 = new Map<string, string>()
    const autoload = json?.autoload?.['psr-4']
    if (autoload && typeof autoload === 'object') {
      for (const [ns, dir] of Object.entries(autoload)) {
        if (typeof dir === 'string') {
          // Normalize: "App\\" → "App\", dir "src/" → "src"
          psr4.set(ns.replace(/\\+$/, '\\'), dir.replace(/\/+$/, ''))
        } else if (Array.isArray(dir) && dir.length > 0 && typeof dir[0] === 'string') {
          psr4.set(ns.replace(/\\+$/, '\\'), dir[0].replace(/\/+$/, ''))
        }
      }
    }
    if (psr4.size === 0) return null
    return { psr4 }
  } catch {
    return null
  }
}

function parsePythonConfig(text: string): PythonConfig | null {
  // Try pyproject.toml (simple heuristic: look for packages or src layout)
  const sourceRoots: string[] = []
  // Check for src layout: [tool.setuptools.packages.find] where = ["src"]
  const srcMatch = text.match(/where\s*=\s*\[["']([^"']+)["']\]/)
  if (srcMatch) {
    sourceRoots.push(srcMatch[1])
  }
  // Check for package_dir
  const pkgDirMatch = text.match(/package_dir\s*=\s*\{["']["']\s*:\s*["']([^"']+)["']/)
  if (pkgDirMatch) {
    sourceRoots.push(pkgDirMatch[1])
  }
  // Fallback: always include project root
  if (sourceRoots.length === 0) {
    sourceRoots.push('.')
  }
  return { sourceRoots }
}

// ─── Helper: get file's project-relative directory ───────────────

function getFileDir(filePath: string | null): string {
  if (!filePath) return ''
  const lastSlash = filePath.lastIndexOf('/')
  return lastSlash > 0 ? filePath.slice(0, lastSlash) : ''
}

/** Resolve a relative path (with .. segments) against a base directory */
function resolveRelative(baseDir: string, relPath: string): string | null {
  const parts = baseDir.split('/').filter(Boolean)
  const segments = relPath.split('/')
  for (const seg of segments) {
    if (seg === '..') {
      if (parts.length > 0) parts.pop()
      else return null
    } else if (seg !== '.' && seg !== '') {
      parts.push(seg)
    }
  }
  return parts.join('/')
}

// ─── Per-Language Resolvers ──────────────────────────────────────

/**
 * Resolve Go import path.
 * e.g. "myproject/internal/service" → ["internal/service/", "internal/service/service.go"]
 * Directory path is primary (for navigation to dir), but also try common
 * .go filenames so batch-exists can find a direct file target.
 */
function resolveGoImport(
  importPath: string,
  _filePath: string | null,
  projectRoot: string,
  config: GoModConfig | null,
): string[] {
  if (!config) return []
  const { modulePath } = config

  // Must start with module path
  if (!importPath.startsWith(modulePath + '/')) return []
  // Strip module prefix
  const relPath = importPath.slice(modulePath.length + 1)
  if (!relPath) return []
  // Go import maps to a directory
  const candidates: string[] = [relPath + '/']
  // Also try the package-name.go file (e.g. internal/service → internal/service/service.go)
  const lastSegment = relPath.split('/').pop()
  if (lastSegment) {
    candidates.push(`${relPath}/${lastSegment}.go`)
  }
  // Also try doc.go which is a common Go package doc file
  candidates.push(`${relPath}/doc.go`)
  return candidates
}

// Regex to detect Go import statements
const GO_IMPORT_RE = /"([^"]+)"/

/**
 * Resolve Rust use path.
 * e.g. "crate::models::user::User" → ["src/models/user/User.rs", "src/models/user.rs", ...]
 */
function resolveRustUse(
  usePath: string,
  filePath: string | null,
  _projectRoot: string,
): string[] {
  const candidates: string[] = []

  // Determine base path
  let basePath: string
  const segments = usePath.split('::').filter(Boolean)

  if (segments[0] === 'crate') {
    // crate:: → src/
    basePath = 'src/' + segments.slice(1, -1).join('/')
  } else if (segments[0] === 'super') {
    // super:: → go up one directory per super
    let superCount = 0
    for (const seg of segments) {
      if (seg === 'super') superCount++
      else break
    }
    const fileDir = getFileDir(filePath)
    let base = fileDir
    for (let i = 0; i < superCount; i++) {
      const lastSlash = base.lastIndexOf('/')
      base = lastSlash > 0 ? base.slice(0, lastSlash) : ''
    }
    const remaining = segments.slice(superCount, -1)
    basePath = base + (remaining.length > 0 ? '/' + remaining.join('/') : '')
  } else if (segments[0] === 'self') {
    // self:: → current file's directory
    const fileDir = getFileDir(filePath)
    basePath = fileDir + (segments.length > 2 ? '/' + segments.slice(1, -1).join('/') : '')
  } else {
    // External crate — can't resolve
    return []
  }

  const lastSegment = segments[segments.length - 1]
  if (!lastSegment) return []

  // Last segment is likely a type/module name
  const isFirstUpper = /^[A-Z]/.test(lastSegment)

  if (isFirstUpper) {
    // Type name: try as file, then as module directory
    if (basePath) {
      candidates.push(`${basePath}/${lastSegment}.rs`)
      candidates.push(`${basePath}/${lastSegment}/mod.rs`)
    }
    // Also try the whole path as a module (last segment is a submodule)
    candidates.push(`${basePath}.rs`)
    candidates.push(`${basePath}/mod.rs`)
  } else {
    // Module name: try as file, then as directory with mod.rs
    candidates.push(`${basePath}/${lastSegment}.rs`)
    candidates.push(`${basePath}/${lastSegment}/mod.rs`)
  }

  // Filter out empty candidates and deduplicate
  return [...new Set(candidates.filter(c => c && !c.startsWith('/')))]
}

// Regex to detect Rust use statements
const RUST_USE_RE = /\buse\s+((?:crate|super|self)(?:::\w+)+)/

/**
 * Resolve Python import path.
 * e.g. "myapp.utils.helpers" → ["myapp/utils/helpers.py", "myapp/utils/helpers/__init__.py"]
 * e.g. ".sibling" (relative) → ["{currentDir}/sibling.py", ...]
 */
function resolvePythonImport(
  modulePath: string,
  isRelative: boolean,
  relativeDots: number,
  filePath: string | null,
  _projectRoot: string,
  config: PythonConfig | null,
): string[] {
  const candidates: string[] = []

  if (isRelative) {
    const fileDir = getFileDir(filePath)
    // Go up (relativeDots - 1) directories (. = current, .. = parent, etc.)
    let base = fileDir
    for (let i = 1; i < relativeDots; i++) {
      const lastSlash = base.lastIndexOf('/')
      base = lastSlash > 0 ? base.slice(0, lastSlash) : ''
    }
    if (modulePath) {
      const relPath = modulePath.replace(/\./g, '/')
      const resolved = resolveRelative(base, relPath)
      if (resolved) {
        candidates.push(resolved + '.py')
        candidates.push(resolved + '/__init__.py')
      }
    } else {
      // "from . import X" — X is a sibling module, handled by caller
    }
  } else {
    // Absolute import: try each source root
    const roots = config?.sourceRoots || ['.']
    const relPath = modulePath.replace(/\./g, '/')
    for (const root of roots) {
      const prefix = root === '.' ? '' : root + '/'
      candidates.push(prefix + relPath + '.py')
      candidates.push(prefix + relPath + '/__init__.py')
    }
  }

  return candidates.filter(c => c && !c.startsWith('/'))
}

/**
 * Resolve PHP use statement (PSR-4).
 * e.g. "App\Models\User" → ["src/Models/User.php"]
 */
function resolvePhpUse(
  namespace: string,
  _filePath: string | null,
  _projectRoot: string,
  config: ComposerAutoload | null,
): string[] {
  if (!config) return []

  // Find longest matching PSR-4 prefix
  let bestPrefix = ''
  let bestDir = ''
  for (const [prefix, dir] of config.psr4) {
    if (namespace.startsWith(prefix) && prefix.length > bestPrefix.length) {
      bestPrefix = prefix
      bestDir = dir
    }
  }

  if (!bestPrefix) return []

  // Strip prefix, convert \ to /, append .php
  const remaining = namespace.slice(bestPrefix.length).replace(/\\/g, '/')
  const fullPath = bestDir + '/' + remaining + '.php'

  // Normalize: remove double slashes
  return [fullPath.replace(/\/+/g, '/')]
}

// Regex for PHP use statements
const PHP_USE_RE = /\buse\s+(?:function\s+)?([A-Z][\w\\]+)/

/**
 * Resolve Java/Kotlin import path.
 * e.g. "com.example.model.User" → ["src/main/java/com/example/model/User.java", ...]
 */
function resolveJavaImport(
  fqn: string,
  _filePath: string | null,
  _projectRoot: string,
): string[] {
  // Skip wildcard imports
  if (fqn.endsWith('.*')) return []

  const candidates: string[] = []
  const pathFromRoot = fqn.replace(/\./g, '/')

  // Convention source roots
  const sourceRoots = [
    'src/main/java',
    'src/main/kotlin',
    'src/test/java',
    'src/test/kotlin',
  ]

  // Try each source root with both extensions
  for (const root of sourceRoots) {
    candidates.push(`${root}/${pathFromRoot}.java`)
    candidates.push(`${root}/${pathFromRoot}.kt`)
    if (candidates.length >= 6) break // limit
  }

  return candidates
}

// Regex for Java/Kotlin import statements
const JAVA_IMPORT_RE = /\bimport\s+(?:static\s+)?([\w]+(?:\.\w+)+)\s*;/

// ─── DOM Scanner ─────────────────────────────────────────────────

// Supported languages for import resolution
const LANG_RESOLVERS: Record<string, {
  lineRegex: RegExp
  extractPath: (match: RegExpMatchArray) => { path: string; isRelative?: boolean; relativeDots?: number } | null
  resolve: (path: string, isRelative: boolean, relativeDots: number, filePath: string | null, projectRoot: string) => string[]
  needsConfig: boolean
}> = {
  go: {
    // Go has two import forms:
    // 1. Single: import "path"
    // 2. Block: import (\n  "path1"\n  "path2"\n)
    // The lineRegex only handles form 1. Form 2 is handled specially
    // in resolveImportPathsFromDOM() via import block tracking.
    lineRegex: /\bimport\s+"([^"]+)"/,
    extractPath: (m) => ({ path: m[1] }),
    resolve(path, _isRel, _dots, _filePath, projectRoot) {
      const config = goModCache.get(projectRoot)
      return resolveGoImport(path, _filePath, projectRoot, config ?? null)
    },
    needsConfig: true,
  },
  rust: {
    lineRegex: RUST_USE_RE,
    extractPath: (m) => ({ path: m[1] }),
    resolve(path, _isRel, _dots, filePath, _projectRoot) {
      return resolveRustUse(path, filePath, _projectRoot)
    },
    needsConfig: false,
  },
  python: {
    lineRegex: /(?:from\s+(\.[\w.]*|\w[\w.]*)\s+import|import\s+([\w.]+))/,
    extractPath: (m) => {
      const rawPath = m[1] || m[2]
      if (!rawPath) return null
      const isRelative = rawPath.startsWith('.')
      const dotMatch = rawPath.match(/^(\.+)/)
      const relativeDots = dotMatch ? dotMatch[1].length : 0
      // For "from ..pkg import X", the path is "..pkg"
      // Strip leading dots for the module path part
      const path = isRelative ? rawPath.replace(/^\.+/, '') : rawPath
      return { path, isRelative, relativeDots }
    },
    resolve(path, isRelative, relativeDots, filePath, projectRoot) {
      const config = pythonConfigCache.get(projectRoot) ?? null
      return resolvePythonImport(path, isRelative, relativeDots, filePath, projectRoot, config)
    },
    needsConfig: false, // has fallback
  },
  php: {
    lineRegex: PHP_USE_RE,
    extractPath: (m) => ({ path: m[1] }),
    resolve(path, _isRel, _dots, _filePath, projectRoot) {
      const config = composerJsonCache.get(projectRoot) ?? null
      return resolvePhpUse(path, _filePath, projectRoot, config)
    },
    needsConfig: true,
  },
  java: {
    lineRegex: JAVA_IMPORT_RE,
    extractPath: (m) => ({ path: m[1] }),
    resolve(path, _isRel, _dots, filePath, _projectRoot) {
      return resolveJavaImport(path, filePath, _projectRoot)
    },
    needsConfig: false,
  },
  kotlin: {
    lineRegex: JAVA_IMPORT_RE, // same syntax
    extractPath: (m) => ({ path: m[1] }),
    resolve(path, _isRel, _dots, filePath, _projectRoot) {
      return resolveJavaImport(path, filePath, _projectRoot)
    },
    needsConfig: false,
  },
}

/**
 * Go-specific import resolution.
 * Handles both single-line imports and multi-line import blocks.
 */
function resolveGoImportPathsFromDOM(
  container: HTMLElement,
  filePath: string | null,
  projectRootPath: string,
): ImportResolution[] {
  const results: ImportResolution[] = []
  const config = goModCache.get(projectRootPath) ?? null

  // Trigger config fetch if needed
  triggerConfigFetch('go', projectRootPath)

  const lines = container.querySelectorAll('.code-line')
  let inImportBlock = false

  for (const line of lines) {
    const lineText = line.textContent || ''

    // Detect start of import block: "import ("
    if (/\bimport\s*\(/.test(lineText)) {
      inImportBlock = true
      continue
    }

    // Detect end of import block: ")"
    if (inImportBlock && /^\s*\)/.test(lineText)) {
      inImportBlock = false
      continue
    }

    // Single-line import: import "path"
    let importPath: string | null = null
    let targetSpan: HTMLElement | null = null

    if (!inImportBlock) {
      const singleMatch = lineText.match(/\bimport\s+"([^"]+)"/)
      if (singleMatch) {
        importPath = singleMatch[1]
      }
    }

    // Inside import block: line is just "path"
    if (inImportBlock && !importPath) {
      const blockMatch = lineText.match(/"([^"]+)"/)
      if (blockMatch) {
        importPath = blockMatch[1]
      }
    }

    if (!importPath) continue

    // Resolve the import path
    const candidates = resolveGoImport(importPath, filePath, projectRootPath, config)
    if (candidates.length === 0) continue

    // Find the .hljs-string span containing the import path
    const stringSpans = line.querySelectorAll('.hljs-string')
    for (const span of stringSpans) {
      const spanText = (span.textContent || '').replace(/^"/, '').replace(/"$/, '')
      if (spanText === importPath) {
        targetSpan = span as HTMLElement
        break
      }
    }

    if (!targetSpan) continue
    if (targetSpan.querySelector('.code-file-path')) continue
    if (targetSpan.hasAttribute('data-import-resolved')) continue

    results.push({
      importPath,
      candidates,
      span: targetSpan,
      displayText: importPath,
    })
  }

  return results
}

/**
 * Scan rendered code DOM for language-specific import statements
 * and resolve them to candidate file paths.
 *
 * SYNCHRONOUS — may trigger background config fetches on cache miss.
 * On cache miss, returns empty results (next render picks up).
 */
export function resolveImportPathsFromDOM(
  container: HTMLElement,
  lang: string,
  filePath: string | null,
  projectRoot: string,
): ImportResolution[] {
  const resolver = LANG_RESOLVERS[lang]
  if (!resolver) return []

  const projectRootPath = store.state.projectRoot
  const results: ImportResolution[] = []

  // Trigger config fetch if needed (async, won't block)
  if (resolver.needsConfig) {
    triggerConfigFetch(lang, projectRootPath)
  }

  // ── Go special handling: track import blocks ──
  // Go import blocks span multiple lines:
  //   import (
  //     "pkg1"
  //     "pkg2"
  //   )
  // We track whether we're inside an import block and treat each
  // quoted string as an import path.
  if (lang === 'go') {
    return resolveGoImportPathsFromDOM(container, filePath, projectRootPath)
  }

  // ── Standard line-by-line scan for other languages ──
  const lines = container.querySelectorAll('.code-line')
  for (const line of lines) {
    const lineText = line.textContent || ''
    const match = lineText.match(resolver.lineRegex)
    if (!match) continue

    const extracted = resolver.extractPath(match)
    if (!extracted) continue

    const candidates = resolver.resolve(
      extracted.path,
      extracted.isRelative ?? false,
      extracted.relativeDots ?? 0,
      filePath,
      projectRootPath,
    )
    if (candidates.length === 0) continue

    // Find the HLJS span that contains the import path text
    const importPath = extracted.path
    const spans = line.querySelectorAll('.hljs-string, .hljs-title, .hljs-type, .hljs-built_in')
    let targetSpan: HTMLElement | null = null

    for (const span of spans) {
      const spanText = span.textContent || ''
      if (spanText.includes(importPath) || spanText.includes(importPath.replace(/\\/g, ''))) {
        targetSpan = span as HTMLElement
        break
      }
    }

    // Fallback: if no HLJS span found, use the line's first suitable span
    if (!targetSpan) {
      // For Go/Rust/Python/Java, try broader search
      const allSpans = line.querySelectorAll('span')
      for (const span of allSpans) {
        const spanText = span.textContent || ''
        // Check if this span's text is a substring of the import path or vice versa
        if (spanText && importPath.includes(spanText.replace(/^['"`](.*)['"`]$/, '$1').trim())) {
          targetSpan = span as HTMLElement
          break
        }
      }
    }

    if (!targetSpan) continue

    // Skip already-annotated spans
    if (targetSpan.querySelector('.code-file-path')) continue
    if (targetSpan.hasAttribute('data-import-resolved')) continue

    // Determine the display text (what to wrap in the annotation)
    let displayText = importPath
    // For Go: the path is inside quotes, strip them for display text matching
    const spanText = targetSpan.textContent || ''
    const quoteMatch = spanText.match(/^['"`](.*)['"`]$/)
    if (quoteMatch && quoteMatch[1] === importPath) {
      displayText = importPath
    }

    results.push({
      importPath,
      candidates,
      span: targetSpan,
      displayText,
    })
  }

  return results
}

/**
 * Trigger background config fetch for a language.
 * Does not block — caller checks cache on next render.
 */
function triggerConfigFetch(lang: string, projectRoot: string): void {
  switch (lang) {
    case 'go':
      fetchProjectConfig(projectRoot, 'go.mod', parseGoMod, goModCache)
      break
    case 'php':
      fetchProjectConfig(projectRoot, 'composer.json', parseComposerJson, composerJsonCache)
      break
    case 'python':
      fetchProjectConfig(projectRoot, 'pyproject.toml', parsePythonConfig, pythonConfigCache)
      break
  }
}

// ─── Annotation Helpers ──────────────────────────────────────────

/**
 * Wrap the import path text within an HLJS span with a clickable annotation.
 * Marks the span with data-import-resolved to prevent double-annotation.
 */
export function annotateImportSpan(
  span: HTMLElement,
  displayText: string,
  resolvedPath: string,
): void {
  const innerHtml = span.innerHTML
  const escapedPath = displayText.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const pathRegex = new RegExp(`(${escapedPath})`)
  if (pathRegex.test(innerHtml)) {
    span.innerHTML = innerHtml.replace(
      pathRegex,
      `<span class="code-file-path" data-file-path="${resolvedPath}" data-import-candidate>$1</span>`,
    )
    span.setAttribute('data-import-resolved', '')
  }
}

/**
 * Verify import-resolved candidates AND generic code-file-path annotations
 * using batch-exists API. For import annotations with multiple candidates,
 * find the first existing candidate and update data-file-path.
 * If no candidate exists, remove the annotation.
 */
export async function verifyImportAnnotations(
  resolutions: ImportResolution[],
  container: HTMLElement,
): Promise<void> {
  if (resolutions.length === 0) return

  // Collect all candidate paths from import resolutions
  const allPaths = new Set<string>()
  for (const r of resolutions) {
    for (const c of r.candidates) {
      allPaths.add(c)
    }
  }

  // Also collect paths from generic Phase 2 .code-file-path annotations
  // (those without data-import-candidate) that haven't been verified yet
  const phase2Paths = new Set<string>()
  container.querySelectorAll('.code-file-path:not([data-import-candidate])').forEach((el) => {
    const path = el.getAttribute('data-file-path')
    if (path) phase2Paths.add(path)
  })

  // Merge all paths into one batch request
  for (const p of phase2Paths) allPaths.add(p)

  if (allPaths.size === 0) return

  try {
    const resp = await fetch('/api/file/batch-exists', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths: [...allPaths] }),
    })
    if (!resp.ok) return
    const data = await resp.json()

    // ── Handle import-candidate annotations (Phase 1) ──
    // For each resolution, find the best existing candidate
    // Prefer file over dir (e.g. for Go: prefer service.go over service/)
    for (const resolution of resolutions) {
      let bestCandidate: string | null = null
      let bestType: string | null = null
      for (const candidate of resolution.candidates) {
        const type = data.results?.[candidate]
        if (type === 'file') {
          // File found — use it immediately (preferred over dir)
          bestCandidate = candidate
          bestType = type
          break
        }
        if (type === 'dir' && !bestCandidate) {
          // Dir found — keep looking for a file candidate
          bestCandidate = candidate
          bestType = type
        }
      }

      // Find and update the annotated span for this resolution
      const spans = container.querySelectorAll('.code-file-path[data-import-candidate]')
      for (const span of spans) {
        const currentPath = span.getAttribute('data-file-path')
        // Match span belonging to this resolution
        if (!currentPath || !resolution.candidates.includes(currentPath)) continue

        if (bestCandidate) {
          // Update to the first existing candidate
          if (bestType === 'dir' && !bestCandidate.endsWith('/')) {
            span.setAttribute('data-file-path', bestCandidate + '/')
          } else {
            span.setAttribute('data-file-path', bestCandidate)
          }
          span.removeAttribute('data-import-candidate')
        } else {
          // No candidate exists — remove annotation, keep text
          const parent = span.parentNode
          if (parent) {
            while (span.firstChild) parent.insertBefore(span.firstChild, span)
            parent.removeChild(span)
          }
        }
      }
    }

    // ── Handle generic .code-file-path annotations (Phase 2) ──
    container.querySelectorAll('.code-file-path:not([data-import-candidate])').forEach((el) => {
      const path = el.getAttribute('data-file-path')
      if (!path) return
      const type = data.results?.[path]
      const exists = type === 'file' || type === 'dir'
      if (!exists) {
        // Remove annotation, keep text
        const parent = el.parentNode
        if (parent) {
          while (el.firstChild) parent.insertBefore(el.firstChild, el)
          parent.removeChild(el)
        }
      } else if (type === 'dir' && !path.endsWith('/')) {
        el.setAttribute('data-file-path', path + '/')
      }
    })
  } catch {
    // Network error — leave annotations as-is (best effort)
  }
}
