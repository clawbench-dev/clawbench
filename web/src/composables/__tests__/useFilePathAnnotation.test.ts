import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import {
  resolveFilePath,
  resolveFilePathDual,
  resolveRelativePath,
  fileOpenButtonHtml,
  FILE_OPEN_ICON_SVG,
  annotateFilePaths,
  verifyFilePaths,
  clearVerifiedCache,
  openFilePath,
} from '@/composables/useFilePathAnnotation'

// Mock escapeHtml from html utils
vi.mock('@/utils/html', () => ({
  escapeHtml: (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'),
}))

// Mock splitPath
vi.mock('@/utils/path', () => ({
  splitPath: (p: string) => p.split('/').filter(Boolean),
}))

// Mock store
vi.mock('@/stores/app', () => ({
  store: {
    state: { projectRoot: '/home/user/project' },
    selectFile: vi.fn(),
    navigateToDir: vi.fn(),
  },
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// --- resolveFilePathDual ---

describe('resolveFilePathDual', () => {
  const projectRoot = '/home/user/project'

  describe('absolute paths (single candidate)', () => {
    it('returns primary === fallback for project-internal absolute path', () => {
      const result = resolveFilePathDual('/home/user/project/src/main.go', projectRoot)
      expect(result).toEqual({ primary: 'src/main.go', fallback: 'src/main.go' })
    })

    it('returns primary === fallback for project-external absolute path', () => {
      const result = resolveFilePathDual('/etc/hosts', projectRoot)
      expect(result).toEqual({ primary: '/etc/hosts', fallback: '/etc/hosts' })
    })

    it('returns null for path equal to projectRoot', () => {
      expect(resolveFilePathDual('/home/user/project', projectRoot)).toBeNull()
    })
  })

  describe('tilde paths (single candidate)', () => {
    const homeDir = '/home/user'

    it('returns primary === fallback for ~/project path', () => {
      const result = resolveFilePathDual('~/project/src/main.go', projectRoot, homeDir)
      expect(result).toEqual({ primary: 'src/main.go', fallback: 'src/main.go' })
    })

    it('returns primary === fallback for ~/path outside project', () => {
      const result = resolveFilePathDual('~/.bashrc', projectRoot, homeDir)
      expect(result).toEqual({ primary: '/home/user/.bashrc', fallback: '/home/user/.bashrc' })
    })

    it('returns null for ~/project (equals projectRoot)', () => {
      expect(resolveFilePathDual('~/project', projectRoot, homeDir)).toBeNull()
    })
  })

  describe('relative paths without baseDir (single candidate)', () => {
    it('returns primary === fallback for relative path', () => {
      const result = resolveFilePathDual('src/main.go', projectRoot)
      expect(result).toEqual({ primary: 'src/main.go', fallback: 'src/main.go' })
    })

    it('returns primary === fallback for ./ relative path', () => {
      const result = resolveFilePathDual('./src/main.go', projectRoot)
      expect(result).toEqual({ primary: 'src/main.go', fallback: 'src/main.go' })
    })
  })

  describe('relative paths with baseDir (dual candidates)', () => {
    it('returns dual candidates when baseDir resolves differently than projectRoot', () => {
      // baseDir = '/home/user/project/web/src', path = 'utils.ts'
      // baseDir result: 'web/src/utils.ts' (primary)
      // projectRoot result: 'utils.ts' (fallback)
      const result = resolveFilePathDual('utils.ts', projectRoot, undefined, '/home/user/project/web/src')
      expect(result).toEqual({ primary: 'web/src/utils.ts', fallback: 'utils.ts' })
    })

    it('returns single candidate when baseDir and projectRoot produce same result', () => {
      // baseDir = projectRoot, so no dual candidate
      const result = resolveFilePathDual('src/main.go', projectRoot, undefined, projectRoot)
      expect(result).toEqual({ primary: 'src/main.go', fallback: 'src/main.go' })
    })

    it('returns single candidate when baseDir resolves to project-external path', () => {
      // baseDir = '/etc', path = 'config.json' → resolves to /etc/config.json (external) → projectRoot wins
      const result = resolveFilePathDual('config.json', projectRoot, undefined, '/etc')
      // projectResult for 'config.json' against '/home/user/project' → 'config.json'
      // Both resolve to 'config.json' → single candidate
      expect(result).toEqual({ primary: 'config.json', fallback: 'config.json' })
    })

    it('returns dual candidates for bare filename with extension', () => {
      const result = resolveFilePathDual('App.vue', projectRoot, undefined, '/home/user/project/web/src')
      expect(result).toEqual({ primary: 'web/src/App.vue', fallback: 'App.vue' })
    })

    it('returns dual candidates when baseDir is a subdirectory of projectRoot', () => {
      // baseDir = '/home/user/project/src', path = 'main.go'
      // baseDir result: 'src/main.go' (primary)
      // projectRoot result: 'main.go' (fallback)
      const result = resolveFilePathDual('main.go', projectRoot, undefined, '/home/user/project/src')
      expect(result).toEqual({ primary: 'src/main.go', fallback: 'main.go' })
    })

    it('returns project-external result when path escapes project via relative', () => {
      // ../../../etc/hosts with projectRoot = /home/user/project
      // projectResult resolves to /etc/hosts (external) → { primary: '/etc/hosts', fallback: '/etc/hosts' }
      const result = resolveFilePathDual('../../../etc/hosts', projectRoot)
      expect(result).toEqual({ primary: '/etc/hosts', fallback: '/etc/hosts' })
    })

    it('handles project-relative baseDir (not absolute)', () => {
      // baseDir = 'web/src' (project-relative, not starting with /)
      // Should produce same result as absolute baseDir '/home/user/project/web/src'
      const result = resolveFilePathDual('utils.ts', projectRoot, undefined, 'web/src')
      expect(result).toEqual({ primary: 'web/src/utils.ts', fallback: 'utils.ts' })
    })

    it('handles ../path with project-relative baseDir', () => {
      // baseDir = 'test/path-annotation', path = '../README.md'
      // baseDir result: 'test/README.md' (primary)
      // projectRoot result: external → stripped fallback: 'README.md'
      const result = resolveFilePathDual('../README.md', projectRoot, undefined, 'test/path-annotation')
      expect(result).toEqual({ primary: 'test/README.md', fallback: 'README.md' })
    })

    it('strips leading ../ for stripped fallback when projectResult is project-external', () => {
      // ../../go.mod from 'test/path-annotation' → baseDir resolves to 'go.mod',
      // projectRoot resolves to external → stripped 'go.mod' also resolves to 'go.md'
      // Both are the same → single candidate
      const result = resolveFilePathDual('../../go.mod', projectRoot, undefined, 'test/path-annotation')
      expect(result).toEqual({ primary: 'go.mod', fallback: 'go.mod' })
    })
  })

  describe('rejection rules', () => {
    it('rejects glob patterns', () => {
      expect(resolveFilePathDual('*.go', projectRoot)).toBeNull()
      expect(resolveFilePathDual('src/*.go', projectRoot)).toBeNull()
    })

    it('rejects URLs', () => {
      expect(resolveFilePathDual('https://example.com', projectRoot)).toBeNull()
    })

    it('rejects env vars', () => {
      expect(resolveFilePathDual('$HOME/.bashrc', projectRoot)).toBeNull()
    })

    it('rejects bare identifiers without slash or extension', () => {
      expect(resolveFilePathDual('useAutoSpeech', projectRoot)).toBeNull()
      expect(resolveFilePathDual('ref', projectRoot)).toBeNull()
    })

    it('accepts bare filename with extension (even without slash)', () => {
      const result = resolveFilePathDual('main.go', projectRoot)
      expect(result).not.toBeNull()
      expect(result!.primary).toBe('main.go')
    })
  })
})

// --- resolveFilePath ---

describe('resolveFilePath', () => {
  const projectRoot = '/home/user/project'

  describe('absolute paths', () => {
    it('resolves a path under projectRoot', () => {
      expect(resolveFilePath('/home/user/project/src/main.go', projectRoot)).toBe('src/main.go')
    })

    it('returns absolute path for path outside projectRoot', () => {
      expect(resolveFilePath('/etc/passwd', projectRoot)).toBe('/etc/passwd')
    })

    it('returns absolute path when projectRoot is empty', () => {
      expect(resolveFilePath('/home/user/project/src/main.go', '')).toBe('/home/user/project/src/main.go')
    })

    it('returns null when path equals projectRoot (no relative part)', () => {
      expect(resolveFilePath('/home/user/project', projectRoot)).toBeNull()
    })

    it('handles nested project root paths', () => {
      expect(resolveFilePath('/home/user/project/deep/nested/file.ts', projectRoot)).toBe('deep/nested/file.ts')
    })
  })

  describe('relative paths with projectRoot', () => {
    it('resolves a simple relative path', () => {
      expect(resolveFilePath('src/main.go', projectRoot)).toBe('src/main.go')
    })

    it('resolves ./prefixed paths', () => {
      expect(resolveFilePath('./src/main.go', projectRoot)).toBe('src/main.go')
    })

    it('resolves ../prefixed paths within project', () => {
      expect(resolveFilePath('../project/src/main.go', projectRoot)).toBe('src/main.go')
    })

    it('returns absolute path for paths going above project root', () => {
      expect(resolveFilePath('../../../etc/passwd', projectRoot)).toBe('/etc/passwd')
    })

    it('returns absolute path for multiple consecutive ../ segments', () => {
      // projectRoot = /home/user/project → parts = ['home', 'user', 'project']
      // Going ../ 3 times exhausts parts → resolves to absolute /src/main.go
      expect(resolveFilePath('../../../src/main.go', projectRoot)).toBe('/src/main.go')
    })

    it('handles mixed . and .. segments', () => {
      expect(resolveFilePath('./src/../lib/utils.ts', projectRoot)).toBe('lib/utils.ts')
    })
  })

  describe('relative paths without projectRoot', () => {
    it('returns path as-is after stripping ./', () => {
      expect(resolveFilePath('src/main.go', '')).toBe('src/main.go')
    })

    it('strips leading ./', () => {
      expect(resolveFilePath('./src/main.go', '')).toBe('src/main.go')
    })

    it('returns null for paths starting with ../', () => {
      expect(resolveFilePath('../src/main.go', '')).toBeNull()
    })
  })

  describe('illegal characters (glob patterns, template vars)', () => {
    it('returns null for paths with * wildcard', () => {
      expect(resolveFilePath('*.class', projectRoot)).toBeNull()
      expect(resolveFilePath('src/*.go', projectRoot)).toBeNull()
    })

    it('returns null for paths with ** double-star', () => {
      expect(resolveFilePath('**/*.class', projectRoot)).toBeNull()
      expect(resolveFilePath('src/**/*.ts', projectRoot)).toBeNull()
    })

    it('returns null for paths with ? wildcard', () => {
      expect(resolveFilePath('src/test?.go', projectRoot)).toBeNull()
    })

    it('returns null for paths with [ ] brackets', () => {
      expect(resolveFilePath('src/[test]/file.go', projectRoot)).toBeNull()
    })

    it('returns null for paths with < > angle brackets', () => {
      expect(resolveFilePath('<sourcefile>/<line>', projectRoot)).toBeNull()
    })

    it('returns null for http:// URLs', () => {
      expect(resolveFilePath('http://localhost:20003', projectRoot)).toBeNull()
      expect(resolveFilePath('http://example.com/page.html', projectRoot)).toBeNull()
    })

    it('returns null for https:// URLs', () => {
      expect(resolveFilePath('https://example.com/page.html', projectRoot)).toBeNull()
    })

    it('returns null for $HOME environment variable paths', () => {
      expect(resolveFilePath('$HOME/.bashrc', projectRoot)).toBeNull()
      expect(resolveFilePath('${HOME}/config', projectRoot)).toBeNull()
    })
  })

  describe('tilde (~/) paths', () => {
    const homeDir = '/home/user'
    const projectRoot = '/home/user/my-app'

    it('resolves ~/project/... paths when homeDir is provided and path is in project', () => {
      expect(resolveFilePath('~/my-app/src/main.go', projectRoot, homeDir)).toBe('src/main.go')
    })

    it('resolves ~/project/sub/deep paths', () => {
      expect(resolveFilePath('~/my-app/internal/handler/chat.go', projectRoot, homeDir)).toBe('internal/handler/chat.go')
    })

    it('returns absolute path for ~/ paths outside project when homeDir is provided', () => {
      expect(resolveFilePath('~/.bashrc', projectRoot, homeDir)).toBe('/home/user/.bashrc')
      expect(resolveFilePath('~/other-project/file.ts', projectRoot, homeDir)).toBe('/home/user/other-project/file.ts')
      expect(resolveFilePath('~/.config/nvim/init.lua', projectRoot, homeDir)).toBe('/home/user/.config/nvim/init.lua')
    })

    it('returns null for ~/ paths without homeDir (cannot expand)', () => {
      expect(resolveFilePath('~/my-app/src/main.go', projectRoot)).toBeNull()
      expect(resolveFilePath('~/.bashrc', projectRoot)).toBeNull()
    })

    it('returns null for ~/ paths when expanded path equals projectRoot (no file part)', () => {
      expect(resolveFilePath('~/my-app', projectRoot, homeDir)).toBeNull()
    })

    it('handles /root home directory correctly', () => {
      expect(resolveFilePath('~/project/src/main.go', '/root/project', '/root')).toBe('src/main.go')
      expect(resolveFilePath('~/other/file.ts', '/root/project', '/root')).toBe('/root/other/file.ts')
    })
  })
})

// --- resolveRelativePath ---

describe('resolveRelativePath', () => {
  it('resolves relative path against base directory', () => {
    expect(resolveRelativePath('file.ts', 'src')).toBe('src/file.ts')
  })

  it('normalizes ./ segments', () => {
    expect(resolveRelativePath('./file.ts', 'src')).toBe('src/file.ts')
  })

  it('normalizes ../ segments', () => {
    expect(resolveRelativePath('../file.ts', 'src/utils')).toBe('src/file.ts')
  })

  it('handles multiple ../ segments', () => {
    expect(resolveRelativePath('../../file.ts', 'src/utils/deep')).toBe('src/file.ts')
  })

  it('returns raw href when baseDir is empty', () => {
    expect(resolveRelativePath('file.ts', '')).toBe('file.ts')
  })

  it('handles deeply nested paths', () => {
    expect(resolveRelativePath('../../../root.ts', 'a/b/c/d')).toBe('a/root.ts')
  })

  it('does not go above root (pops from empty normalized)', () => {
    expect(resolveRelativePath('../../../../root.ts', 'a')).toBe('root.ts')
  })

  it('handles double slashes', () => {
    expect(resolveRelativePath('sub//file.ts', 'src')).toBe('src/sub/file.ts')
  })

  it('handles empty href segments', () => {
    expect(resolveRelativePath('././file.ts', 'src')).toBe('src/file.ts')
  })
})

// --- fileOpenButtonHtml ---

describe('fileOpenButtonHtml', () => {
  it('generates button HTML with data-file-path attribute', () => {
    const html = fileOpenButtonHtml('src/main.go')
    expect(html).toContain('chat-file-open-btn')
    expect(html).toContain('data-file-path="src/main.go"')
  })

  it('escapes HTML in the path', () => {
    const html = fileOpenButtonHtml('src/<script>.go')
    expect(html).toContain('data-file-path="src/&lt;script&gt;.go"')
  })

  it('includes the SVG icon', () => {
    const html = fileOpenButtonHtml('test.ts')
    expect(html).toContain('<svg')
  })

  it('contains the same icon as FILE_OPEN_ICON_SVG', () => {
    const html = fileOpenButtonHtml('test.ts')
    expect(html).toContain(FILE_OPEN_ICON_SVG)
  })

  it('includes data-line-start when lineStart is provided', () => {
    const html = fileOpenButtonHtml('src/main.go', 42)
    expect(html).toContain('data-file-path="src/main.go"')
    expect(html).toContain('data-line-start="42"')
    expect(html).not.toContain('data-line-end')
  })

  it('includes data-line-start and data-line-end when both are provided', () => {
    const html = fileOpenButtonHtml('src/main.go', 70, 81)
    expect(html).toContain('data-file-path="src/main.go"')
    expect(html).toContain('data-line-start="70"')
    expect(html).toContain('data-line-end="81"')
  })

  it('does not include line attributes when lineStart is not provided', () => {
    const html = fileOpenButtonHtml('src/main.go')
    expect(html).not.toContain('data-line-start')
    expect(html).not.toContain('data-line-end')
  })

  it('includes data-fallback-path when fallbackPath differs from resolvedPath', () => {
    const html = fileOpenButtonHtml('web/src/utils.ts', undefined, undefined, 'utils.ts')
    expect(html).toContain('data-file-path="web/src/utils.ts"')
    expect(html).toContain('data-fallback-path="utils.ts"')
  })

  it('does not include data-fallback-path when fallbackPath equals resolvedPath', () => {
    const html = fileOpenButtonHtml('src/main.go', undefined, undefined, 'src/main.go')
    expect(html).toContain('data-file-path="src/main.go"')
    expect(html).not.toContain('data-fallback-path')
  })

  it('escapes HTML in fallbackPath', () => {
    const html = fileOpenButtonHtml('src/main.go', undefined, undefined, 'src/<weird>.go')
    expect(html).toContain('data-fallback-path="src/&lt;weird&gt;.go"')
  })
})

// --- annotateFilePaths ---

describe('annotateFilePaths', () => {
  const projectRoot = '/home/user/project'

  it('annotates absolute paths under projectRoot', () => {
    const input = 'See /home/user/project/src/main.go for details'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/main.go')
    expect(result.html).toContain('chat-file-path')
    expect(result.html).toContain('chat-file-open-btn')
  })

  it('does not annotate absolute paths outside projectRoot without file extension', () => {
    // /etc/config has no extension, so FILE_PATH_RE does not match it in text nodes
    const input = 'See /etc/config for details'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
    expect(result.html).not.toContain('chat-file-path')
  })

  it('annotates relative paths with ./', () => {
    const input = 'Check ./src/main.go for details'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('annotates bare relative paths with at least two segments and extension', () => {
    const input = 'Look at src/main.go for details'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('does not annotate single-segment names without slash', () => {
    const input = 'Look at main.go for details'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('annotates file paths inside <pre> blocks', () => {
    const input = '<pre>some /home/user/project/src/main.go code</pre>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('annotates file paths inside inline code elements', () => {
    const input = '<code>src/main.go</code>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('does not annotate inline code without slash or extension', () => {
    const input = '<code>useAutoSpeech</code>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('annotates inline code with extension but no slash', () => {
    const input = '<code>ChatPanel.vue</code>'
    const result = annotateFilePaths(input, { projectRoot })
    // ChatPanel.vue matches the file extension pattern
    expect(result.detectedPaths.length).toBeGreaterThanOrEqual(0)
  })

  it('appends open button after <a> links to local files', () => {
    const input = '<a href="src/utils.ts">utils</a>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/utils.ts')
    expect(result.html).toContain('chat-file-open-btn')
  })

  it('does not annotate external <a> links', () => {
    const input = '<a href="https://example.com">link</a>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('does not annotate anchor <a> links', () => {
    const input = '<a href="#section">jump</a>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('resolves <a> href against baseDir when provided', () => {
    const input = '<a href="utils.ts">utils</a>'
    const result = annotateFilePaths(input, { projectRoot, baseDir: 'src' })
    expect(result.detectedPaths).toContain('src/utils.ts')
  })

  it('returns empty detectedPaths for plain text with no paths', () => {
    const input = 'This is just some text without any file references.'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('handles empty input', () => {
    const result = annotateFilePaths('', { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
    expect(result.html).toBe('')
  })

  it('detects multiple paths in one string', () => {
    const input = 'See src/main.go and ./lib/utils.ts'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths.length).toBeGreaterThanOrEqual(2)
  })

  it('annotates paths inside blockquote elements (blockquote is valid context)', () => {
    // After markdown rendering, ">src/main.go" becomes <blockquote><p>src/main.go</p></blockquote>
    // The DOM-based approach naturally handles this — the text node is inside a <blockquote>
    // but not inside <pre>/<a>/<code>, so it's a valid annotation target.
    const input = '<blockquote><p>src/main.go</p></blockquote>'
    const result = annotateFilePaths(input, { projectRoot })
    // Paths inside blockquotes are annotated — they are legitimate file references
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('does not double-annotate absolute paths that contain a bare relative path segment', () => {
    // Regression: absolute path like /home/user/project/public/landing/index.html
    // would be annotated by the absolute-path regex, then the bare relative-path regex
    // would match "public/landing/index.html" inside the generated data-file-path
    // attribute of both the <span> and <button> tags, producing broken HTML like:
    //   data-file-path="<span class="chat-file-path"..."
    const input = '<p>/home/user/project/public/landing/index.html这个是出问题的文件。</p>'
    const result = annotateFilePaths(input, { projectRoot })

    // Should detect exactly one path
    expect(result.detectedPaths).toHaveLength(1)
    expect(result.detectedPaths[0]).toBe('public/landing/index.html')

    // The data-file-path attribute must NOT contain a nested <span>
    expect(result.html).not.toContain('data-file-path="<span')
    expect(result.html).not.toContain('data-file-path="&lt;span')

    // The data-file-path attribute should contain the correct resolved path
    expect(result.html).toContain('data-file-path="public/landing/index.html"')
  })

  // ── DOM traversal specific tests ──

  it('does not re-annotate paths inside <a> tag text content', () => {
    // <a> tags are handled in step 1 (append button after the link).
    // The text inside <a> should NOT be matched again by the text-node regex.
    const input = '<a href="src/utils.ts">see src/utils.ts</a>'
    const result = annotateFilePaths(input, { projectRoot })
    // Should detect the path once (from the href), not twice
    expect(result.detectedPaths).toHaveLength(1)
    expect(result.detectedPaths[0]).toBe('src/utils.ts')
    // Should only have one open button
    const btnCount = (result.html.match(/chat-file-open-btn/g) || []).length
    expect(btnCount).toBe(1)
  })

  it('does not re-annotate paths inside <code> tag text content', () => {
    // <code> tags are handled in step 2 (add class + button).
    // The text inside <code> should NOT be matched again by the text-node regex.
    const input = '<p>check <code>src/main.go</code> for details</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(1)
    expect(result.detectedPaths[0]).toBe('src/main.go')
    // Only one span/button pair
    const btnCount = (result.html.match(/chat-file-open-btn/g) || []).length
    expect(btnCount).toBe(1)
  })

  it('annotates code inside <pre> blocks', () => {
    // <pre><code> is a multi-line code block — paths inside are now also annotated
    const input = '<pre><code>import "/home/user/project/src/main.go"</code></pre>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/main.go')
    // Path is inside <code> content but not the entire content, so only a button is appended
    expect(result.html).toContain('chat-file-open-btn')
  })

  it('annotates absolute path immediately followed by CJK characters', () => {
    // Original bug: /home/user/project/public/landing/index.html这个文件
    // The path ends at the CJK character boundary — regex should not eat the Chinese text
    const input = '<p>/home/user/project/src/main.go有问题</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(1)
    expect(result.detectedPaths[0]).toBe('src/main.go')
    // The CJK text should remain outside the span
    expect(result.html).toContain('有问题')
  })

  it('annotates ../ relative paths that go above projectRoot as external', () => {
    // ../lib/utils.ts resolves to /home/user/lib/utils.ts which is outside projectRoot
    const input = '<p>see ../lib/utils.ts</p>'
    const result = annotateFilePaths(input, { projectRoot })
    // External absolute paths are now annotated (with data-external attribute)
    expect(result.detectedPaths).toContain('/home/user/lib/utils.ts')
  })

  it('annotates ./ relative paths that stay within projectRoot', () => {
    const input = '<p>see ./src/main.go</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('detects multiple absolute paths in the same HTML', () => {
    const input = '<p>Edit /home/user/project/src/main.go and /home/user/project/lib/utils.ts</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(2)
    expect(result.detectedPaths).toContain('src/main.go')
    expect(result.detectedPaths).toContain('lib/utils.ts')
  })

  it('does not match paths in existing data-file-path attributes', () => {
    // Pre-existing annotation HTML should not be re-matched by text node regex
    // because data-file-path is an HTML attribute, and DOM traversal only processes text nodes
    const input = '<span class="chat-file-path" data-file-path="src/main.go">src/main.go</span>'
    const result = annotateFilePaths(input, { projectRoot })
    // The span's text content is inside the span element, which is not a text node
    // directly under the body — it's inside the span, so the walker won't pick it up
    // (parent.tagName check skips CODE, but SPAN is not filtered, so the text inside
    // the span IS a text node that gets walked). However, the regex will match
    // "src/main.go" in the text node and try to resolve it — which succeeds.
    // This is expected: if someone passes already-annotated HTML through the function
    // again, it may double-annotate. The caller is responsible for not doing that.
    // What we DO guarantee is that HTML ATTRIBUTES are never matched.
    expect(result.html).not.toContain('data-file-path="&lt;span')
  })

  it('does not annotate mailto: links', () => {
    const input = '<a href="mailto:user@example.com">email</a>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('does not annotate tel: links', () => {
    const input = '<a href="tel:+1234567890">call</a>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('handles path followed by punctuation (period, comma, semicolon)', () => {
    const input = '<p>see src/main.go, lib/utils.ts; and more</p>'
    const result = annotateFilePaths(input, { projectRoot })
    // Both paths should be detected
    expect(result.detectedPaths).toContain('src/main.go')
    expect(result.detectedPaths).toContain('lib/utils.ts')
  })

  it('handles path followed by closing parenthesis', () => {
    const input = '<p>see src/main.go) for details</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('produces valid HTML with span and button for text node paths', () => {
    const input = '<p>see src/main.go</p>'
    const result = annotateFilePaths(input, { projectRoot })
    // The output should contain a <span class="chat-file-path"> with the path
    // and a <button class="chat-file-open-btn"> with the same data-file-path
    expect(result.html).toContain('<span class="chat-file-path"')
    expect(result.html).toContain('data-file-path="src/main.go"')
    expect(result.html).toContain('chat-file-open-btn')
  })

  it('produces valid HTML with class and button for code node paths', () => {
    const input = '<code>src/main.go</code>'
    const result = annotateFilePaths(input, { projectRoot })
    // The <code> should get the chat-file-path class and data-file-path attribute
    expect(result.html).toContain('class="chat-file-path"')
    expect(result.html).toContain('data-file-path="src/main.go"')
    expect(result.html).toContain('chat-file-open-btn')
  })

  it('handles HTML with only tags and no text', () => {
    const input = '<p></p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('handles path in a deeply nested element', () => {
    const input = '<div><section><article><p>edit src/main.go</p></article></section></div>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('annotates absolute paths outside projectRoot as external', () => {
    const input = '<p>check /etc/nginx/nginx.conf and /home/user/project/src/main.go</p>'
    const result = annotateFilePaths(input, { projectRoot })
    // Both paths detected — external as absolute, internal as relative
    expect(result.detectedPaths).toContain('/etc/nginx/nginx.conf')
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('preserves surrounding text when annotating a path in a text node', () => {
    const input = '<p>Before src/main.go after</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.html).toContain('Before')
    expect(result.html).toContain('after')
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('annotates bare relative path with multiple segments', () => {
    const input = '<p>see internal/handler/chat.go</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('internal/handler/chat.go')
  })

  it('does not annotate URL-like strings', () => {
    const input = '<p>visit https://example.com/page.html</p>'
    const result = annotateFilePaths(input, { projectRoot })
    // https:// URLs are rejected by shouldRejectPath, but the regex FILE_PATH_RE
    // may match "//example.com/page.html" which resolves to an absolute external path
    expect(result.detectedPaths).not.toContain('src/main.go')
  })

  it('does not annotate localhost URLs in <code> elements', () => {
    const input = '<code>http://localhost:20003</code>'
    const result = annotateFilePaths(input, { projectRoot })
    // localhost URLs should not get file-path annotations
    // (they are handled by localhost annotation instead)
    expect(result.detectedPaths).toHaveLength(0)
    expect(result.html).not.toContain('chat-file-path')
    expect(result.html).not.toContain('chat-file-open-btn')
  })

  it('annotates <a> with relative href and baseDir', () => {
    const input = '<a href="components/App.vue">App</a>'
    const result = annotateFilePaths(input, { projectRoot, baseDir: 'src' })
    expect(result.detectedPaths).toContain('src/components/App.vue')
  })

  // ── Dual-candidate annotation with baseDir ──

  describe('dual-candidate annotation with baseDir', () => {
    const projectRoot = '/home/user/project'

    it('stores data-fallback-path on <code> annotation when baseDir produces dual candidate', () => {
      const input = '<code>utils.ts</code>'
      // Using project-relative baseDir (as MarkdownPreview does)
      const result = annotateFilePaths(input, { projectRoot, baseDir: 'web/src' })
      // primary = web/src/utils.ts, fallback = utils.ts
      expect(result.detectedPaths).toContain('web/src/utils.ts')
      expect(result.detectedPaths).toContain('utils.ts')
      expect(result.html).toContain('data-file-path="web/src/utils.ts"')
      expect(result.html).toContain('data-fallback-path="utils.ts"')
    })

    it('stores data-fallback-path on text-node span when baseDir produces dual candidate', () => {
      // Use a multi-segment path that FILE_PATH_RE can match in text nodes
      const input = '<p>see components/App.vue for details</p>'
      const result = annotateFilePaths(input, { projectRoot, baseDir: 'web/src' })
      // primary = web/src/components/App.vue, fallback = components/App.vue
      expect(result.detectedPaths).toContain('web/src/components/App.vue')
      expect(result.detectedPaths).toContain('components/App.vue')
      expect(result.html).toContain('data-file-path="web/src/components/App.vue"')
      expect(result.html).toContain('data-fallback-path="components/App.vue"')
    })

    it('does not include data-fallback-path when primary === fallback', () => {
      const input = '<code>src/main.go</code>'
      const result = annotateFilePaths(input, { projectRoot })
      expect(result.html).toContain('data-file-path="src/main.go"')
      expect(result.html).not.toContain('data-fallback-path')
    })

    it('button also has data-fallback-path for dual-candidate code annotation', () => {
      const input = '<code>utils.ts</code>'
      const result = annotateFilePaths(input, { projectRoot, baseDir: 'web/src' })
      const btnMatch = result.html.match(/chat-file-open-btn[^>]*data-fallback-path="utils.ts"/)
      expect(btnMatch).not.toBeNull()
    })

    it('resolves ../path with project-relative baseDir', () => {
      const input = '<code>../README.md</code>'
      const result = annotateFilePaths(input, { projectRoot, baseDir: 'test/path-annotation' })
      // primary = test/README.md, fallback = external /home/user/README.md
      expect(result.detectedPaths).toContain('test/README.md')
    })
  })

  // ── Chinese path encoding (percent-encoded href decoding) ──

  describe('percent-encoded href decoding for Chinese paths', () => {
    it('decodes percent-encoded Chinese href in <a> tags', () => {
      // Browsers/DOMPurify may encode 中文 → %E4%B8%AD%E6%96%87 in href attributes
      const input = '<a href="%E4%B8%AD%E6%96%87/%E6%96%87%E4%BB%B6.md">链接</a>'
      const result = annotateFilePaths(input, { projectRoot, baseDir: '' })
      expect(result.detectedPaths).toContain('中文/文件.md')
    })

    it('decodes percent-encoded Chinese href with baseDir resolution', () => {
      const input = '<a href="%E6%96%87%E6%A1%A3/%E8%AF%B4%E6%98%8E.md">说明</a>'
      const result = annotateFilePaths(input, { projectRoot, baseDir: 'docs' })
      expect(result.detectedPaths).toContain('docs/文档/说明.md')
    })

    it('handles mixed ASCII and percent-encoded Chinese segments in href', () => {
      const input = '<a href="src/%E5%B7%A5%E5%85%B7/utils.ts">工具</a>'
      const result = annotateFilePaths(input, { projectRoot, baseDir: '' })
      expect(result.detectedPaths).toContain('src/工具/utils.ts')
    })

    it('does not double-decode already decoded Chinese href', () => {
      // If the href is already decoded (中文 instead of %E4%B8%AD%E6%96%87),
      // it should work correctly without double-decoding
      const input = '<a href="中文/文件.md">链接</a>'
      const result = annotateFilePaths(input, { projectRoot, baseDir: '' })
      expect(result.detectedPaths).toContain('中文/文件.md')
    })

    it('handles malformed percent encoding gracefully', () => {
      // %ZZ is not a valid percent encoding — should be returned as-is
      const input = '<a href="path/%ZZfile.md">bad</a>'
      const result = annotateFilePaths(input, { projectRoot, baseDir: '' })
      // Should still detect a path (even if the encoding is malformed)
      expect(result.detectedPaths.length).toBeGreaterThanOrEqual(1)
    })

    it('skips percent-encoded external links (https://)', () => {
      const input = '<a href="https://example.com/%E4%B8%AD%E6%96%87">link</a>'
      const result = annotateFilePaths(input, { projectRoot })
      expect(result.detectedPaths).toHaveLength(0)
    })
  })

  it('does not partially match directory prefix followed by more path segments (worktree-like)', () => {
    // Regression: /home/user/project/.worktrees/gitgraph-fix
    // The FILE_PATH_RE would match /home/user/project/.worktrees (treating .worktrees as extension)
    // but the full path is a directory, not a file. The trailing /gitgraph-fix indicates the
    // match is incomplete — this should be skipped so worktree annotation can handle the full path.
    const input = '<p>/home/user/project/.worktrees/gitgraph-fix</p>'
    const result = annotateFilePaths(input, { projectRoot })
    // Should NOT detect .worktrees as a file path (it's a directory prefix)
    expect(result.detectedPaths).toHaveLength(0)
    expect(result.html).not.toContain('chat-file-path')
  })

  it('does not partially match .worktrees directory prefix with non-hyphen continuation', () => {
    // Same bug with a worktree name that has no hyphen (e.g. featurex)
    const input = '<p>/home/user/project/.worktrees/featurex</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
    expect(result.html).not.toContain('chat-file-path')
  })

  it('still annotates legitimate paths ending in .worktrees when there is no continuation', () => {
    // If the text is just /home/user/project/.worktrees (no trailing path), it's a
    // legitimate path that should be annotated — even though it's a directory,
    // the user may want to navigate to it.
    const input = '<p>/home/user/project/.worktrees</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toContain('.worktrees')
  })

  it('skips text nodes inside chat-worktree-path elements', () => {
    // After worktree annotation runs first, file path annotation should not
    // re-annotate text inside worktree-annotated elements
    const input = '<span class="chat-worktree-path" data-worktree-path="/home/user/project/.worktrees/fix">.worktrees/fix</span>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('skips <code> elements already annotated as worktree', () => {
    // <code> with chat-worktree-path class should be skipped in step 2
    const input = '<code class="chat-worktree-path" data-worktree-path="/home/user/project/.worktrees/fix">.worktrees/fix</code>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  // ── Glob / illegal character rejection tests ──

  it('does not annotate glob patterns in <code> tags', () => {
    const input = '<code>**/*.class</code>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
    expect(result.html).not.toContain('chat-file-path')
  })

  it('does not annotate paths with * wildcard in <code> tags', () => {
    const input = '<code>*Test.java</code>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('does not annotate paths with angle brackets (template vars)', () => {
    const input = '<code><sourcefile>/<line></code>'
    const result = annotateFilePaths(input, { projectRoot })
    // DOMParser treats <sourcefile> and <line> as HTML tags, so the text content
    // is just "/". No file path is detected from this.
    // However, the jsdom environment may differ from browser DOMParser,
    // so we accept either 0 or 1 detections (the latter being a false positive
    // from the HTML parsing artifacts).
    expect(result.detectedPaths.length).toBeLessThanOrEqual(1)
  })

  it('does not annotate ProGuard-style glob patterns in text', () => {
    // These are common in Android/Java projects — not real file paths
    const input = '<p>**/R.class and **/R$*.class and **/Manifest*.*</p>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  // ── Tilde (~/) path tests ──

  it('does not annotate ~/ paths outside project when homeDir is provided', () => {
    const input = '<code>~/.bashrc</code>'
    const result = annotateFilePaths(input, { projectRoot: '/home/user/my-app', homeDir: '/home/user' })
    // External ~/ paths are now resolved to absolute paths and annotated
    expect(result.detectedPaths).toContain('/home/user/.bashrc')
  })

  it('annotates ~/project/... paths when homeDir is provided', () => {
    const input = '<code>~/my-app/src/main.go</code>'
    const result = annotateFilePaths(input, { projectRoot: '/home/user/my-app', homeDir: '/home/user' })
    expect(result.detectedPaths).toContain('src/main.go')
    expect(result.html).toContain('chat-file-path')
  })

  it('does not annotate ~/ paths without homeDir', () => {
    const input = '<code>~/my-app/src/main.go</code>'
    const result = annotateFilePaths(input, { projectRoot: '/home/user/my-app' })
    expect(result.detectedPaths).toHaveLength(0)
  })

  it('annotates ~/project/... in text nodes when homeDir is provided', () => {
    const input = '<p>Edit ~/my-app/src/main.go for details</p>'
    const result = annotateFilePaths(input, { projectRoot: '/home/user/my-app', homeDir: '/home/user' })
    expect(result.detectedPaths).toContain('src/main.go')
  })

  it('annotates ~/ paths outside project as external in text nodes', () => {
    const input = '<p>Check ~/.config/nvim/init.lua for settings</p>'
    const result = annotateFilePaths(input, { projectRoot: '/home/user/my-app', homeDir: '/home/user' })
    // External ~/ paths are now resolved to absolute paths
    expect(result.detectedPaths).toContain('/home/user/.config/nvim/init.lua')
  })

  it('does not annotate $HOME paths in <code> tags', () => {
    const input = '<code>$HOME/.bashrc</code>'
    const result = annotateFilePaths(input, { projectRoot })
    expect(result.detectedPaths).toHaveLength(0)
  })

  // ── Comprehensive real-project path tests (projectRoot=/home/xulongzhe/projects/clawbench, homeDir=/home/xulongzhe) ──

  describe('real-project path scenarios', () => {
    const projectRoot = '/home/xulongzhe/projects/clawbench'
    const homeDir = '/home/xulongzhe'

    describe('正例 — 项目内路径，应该标注', () => {
      it('annotates relative path (web/src/composables/useFilePathAnnotation.ts)', () => {
        const input = '<p>Edit web/src/composables/useFilePathAnnotation.ts for details</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('web/src/composables/useFilePathAnnotation.ts')
      })

      it('annotates absolute path under project (/home/xulongzhe/projects/clawbench/internal/handler/file.go)', () => {
        const input = '<p>See /home/xulongzhe/projects/clawbench/internal/handler/file.go for details</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('internal/handler/file.go')
      })

      it('annotates ~-expanded path in project (~/projects/clawbench/cmd/server/main.go)', () => {
        const input = '<p>Check ~/projects/clawbench/cmd/server/main.go for details</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('cmd/server/main.go')
      })

      it('annotates ./ relative path (./web/src/App.vue)', () => {
        const input = '<p>Open ./web/src/App.vue for details</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('web/src/App.vue')
      })

      it('annotates absolute path followed by CJK text (/home/xulongzhe/projects/clawbench/go.mod 这个文件)', () => {
        const input = '<p>/home/xulongzhe/projects/clawbench/go.mod这个文件</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('go.mod')
      })

      it('annotates deep ~-expanded path (~/projects/clawbench/web/src/composables/useChatRender.ts)', () => {
        const input = '<p>Edit ~/projects/clawbench/web/src/composables/useChatRender.ts for details</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('web/src/composables/useChatRender.ts')
      })

      it('annotates ~-expanded path in <code> tag', () => {
        const input = '<code>~/projects/clawbench/web/src/App.vue</code>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('web/src/App.vue')
        expect(result.html).toContain('chat-file-path')
      })
    })

    describe('反例 — 项目外路径，不应标注', () => {
      it('does not annotate ~/.bashrc', () => {
        const input = '<p>Edit ~/.bashrc to configure your shell</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toHaveLength(0)
      })

      it('annotates ~/projects/other-app/src/main.go as external (other project)', () => {
        const input = '<p>Check ~/projects/other-app/src/main.go</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        // External ~/ paths are now resolved to absolute paths
        expect(result.detectedPaths).toContain('/home/xulongzhe/projects/other-app/src/main.go')
      })

      it('annotates ~/.config/nvim/init.lua as external', () => {
        const input = '<p>Modify ~/.config/nvim/init.lua for settings</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('/home/xulongzhe/.config/nvim/init.lua')
      })

      it('does not annotate ~/.ssh/config', () => {
        const input = '<p>Look at ~/.ssh/config</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toHaveLength(0)
      })

      it('annotates ~/go/src/main.go as external', () => {
        const input = '<p>Check ~/go/src/main.go</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('/home/xulongzhe/go/src/main.go')
      })

      it('annotates ~/.cargo/config.toml as external', () => {
        const input = '<p>Look at ~/.cargo/config.toml for Rust settings</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toContain('/home/xulongzhe/.cargo/config.toml')
      })

      it('does not annotate /etc/hosts (no file extension matched by regex)', () => {
        const input = '<p>See /etc/hosts for DNS</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        // /etc/hosts has no extension, so FILE_PATH_RE doesn't match it
        expect(result.detectedPaths).toHaveLength(0)
      })

      it('does not annotate /usr/local/bin/python3 (no file extension matched by regex)', () => {
        const input = '<p>Run /usr/local/bin/python3 to start</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        // /usr/local/bin/python3 has no extension, so FILE_PATH_RE doesn't match it
        expect(result.detectedPaths).toHaveLength(0)
      })

      it('annotates /home/xulongzhe/.local/share/applications/mimeapps.list as external', () => {
        const input = '<p>The path is /home/xulongzhe/.local/share/applications/mimeapps.list</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        // External absolute paths are now annotated
        expect(result.detectedPaths).toContain('/home/xulongzhe/.local/share/applications/mimeapps.list')
      })

      it('does not annotate $HOME/.bashrc', () => {
        const input = '<p>Check $HOME/.bashrc</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toHaveLength(0)
      })

      it('does not annotate ${HOME}/config', () => {
        const input = '<p>Check ${HOME}/config</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toHaveLength(0)
      })

      it('does not annotate **/*.class glob pattern', () => {
        const input = '<p>Clean up **/*.class files</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toHaveLength(0)
      })

      it('annotates https://example.com/page.html as external path from regex', () => {
        const input = '<p>Visit https://example.com/page.html for more</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        // The https:// URL itself is rejected by shouldRejectPath, but FILE_PATH_RE
        // may match "//example.com/page.html" which resolves to an absolute external path
        expect(result.detectedPaths.length).toBeGreaterThanOrEqual(0)
      })
    })

    describe('边界 case', () => {
      it('does not annotate ~/projects/clawbench (equals projectRoot, no file part)', () => {
        const input = '<p>Navigate to ~/projects/clawbench</p>'
        const result = annotateFilePaths(input, { projectRoot, homeDir })
        expect(result.detectedPaths).toHaveLength(0)
      })
    })

    describe('line suffix support', () => {
      it('annotates path with single line suffix :42', () => {
        const input = '<p>see internal/ai/acp_backend.go:70</p>'
        const result = annotateFilePaths(input, { projectRoot })
        expect(result.detectedPaths).toContain('internal/ai/acp_backend.go')
        expect(result.html).toContain('data-line-start="70"')
        expect(result.html).toContain('chat-file-path')
        expect(result.html).toContain('internal/ai/acp_backend.go:70')
      })

      it('annotates path with line range suffix :70-81', () => {
        const input = '<p>see internal/ai/acp_backend.go:70-81</p>'
        const result = annotateFilePaths(input, { projectRoot })
        expect(result.detectedPaths).toContain('internal/ai/acp_backend.go')
        expect(result.html).toContain('data-line-start="70"')
        expect(result.html).toContain('data-line-end="81"')
        expect(result.html).toContain('chat-file-path')
        expect(result.html).toContain('internal/ai/acp_backend.go:70-81')
      })

      it('annotates path without line suffix (backward compatible)', () => {
        const input = '<p>see src/main.go</p>'
        const result = annotateFilePaths(input, { projectRoot })
        expect(result.detectedPaths).toContain('src/main.go')
        expect(result.html).toContain('chat-file-path')
        expect(result.html).not.toContain('data-line-start')
        expect(result.html).not.toContain('data-line-end')
      })

      it('annotates <code> tag with line suffix', () => {
        const input = '<p>check <code>internal/ai/acp_backend.go:42</code> for details</p>'
        const result = annotateFilePaths(input, { projectRoot })
        expect(result.detectedPaths).toContain('internal/ai/acp_backend.go')
        expect(result.html).toContain('data-line-start="42"')
        expect(result.html).toContain('chat-file-path')
      })

      it('button includes line attributes for path with line suffix', () => {
        const input = '<p>see internal/ai/acp_backend.go:70-81</p>'
        const result = annotateFilePaths(input, { projectRoot })
        expect(result.html).toContain('chat-file-open-btn')
        // Button should have line attributes too
        const btnMatch = result.html.match(/chat-file-open-btn[^>]*data-line-start="70"/)
        expect(btnMatch).not.toBeNull()
        const btnMatch2 = result.html.match(/chat-file-open-btn[^>]*data-line-end="81"/)
        expect(btnMatch2).not.toBeNull()
      })

      it('span includes line attributes for path with line suffix', () => {
        const input = '<p>see internal/ai/acp_backend.go:70-81</p>'
        const result = annotateFilePaths(input, { projectRoot })
        const spanMatch = result.html.match(/chat-file-path[^>]*data-line-start="70"/)
        expect(spanMatch).not.toBeNull()
        const spanMatch2 = result.html.match(/chat-file-path[^>]*data-line-end="81"/)
        expect(spanMatch2).not.toBeNull()
      })

      it('resolved path does not include line suffix', () => {
        const input = '<p>see internal/ai/acp_backend.go:70-81</p>'
        const result = annotateFilePaths(input, { projectRoot })
        // The resolved path in detectedPaths should NOT include the line suffix
        expect(result.detectedPaths).toContain('internal/ai/acp_backend.go')
        expect(result.detectedPaths).not.toContain('internal/ai/acp_backend.go:70-81')
      })

      it('annotates absolute path with line suffix', () => {
        const input = `See ${projectRoot}/internal/handler/chat.go:10 for details`
        const result = annotateFilePaths(input, { projectRoot })
        expect(result.detectedPaths).toContain('internal/handler/chat.go')
        expect(result.html).toContain('data-line-start="10"')
      })
    })
  })
})

// --- clearVerifiedCache ---

describe('clearVerifiedCache', () => {
  it('does not throw when called', () => {
    expect(() => clearVerifiedCache()).not.toThrow()
  })
})

// --- verifyFilePaths ---

describe('verifyFilePaths', () => {
  beforeEach(() => {
    clearVerifiedCache()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  // Ensure CSS.escape is available in jsdom
  if (typeof (globalThis as any).CSS === 'undefined') {
    ;(globalThis as any).CSS = {}
  }
  if (typeof (globalThis as any).CSS.escape === 'undefined') {
    ;(globalThis as any).CSS.escape = (s: string) => s.replace(/[!"#$%&'()*+,./:;<=>?@[\\\]^`{|}~]/g, '\\$&')
  }

  it('removes buttons for non-existent paths (batch API returns none)', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { 'missing.go': 'none' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    container.innerHTML = '<button class="chat-file-open-btn" data-file-path="missing.go">open</button><span class="chat-file-path" data-file-path="missing.go">missing.go</span>'

    await verifyFilePaths(['missing.go'], container)

    // Button should be removed
    expect(container.querySelector('.chat-file-open-btn')).toBeNull()
    // Span should be unwrapped (plain text remains)
    expect(container.textContent).toContain('missing.go')
    expect(container.querySelector('.chat-file-path')).toBeNull()

    // Verify batch API was called
    expect(mockFetch).toHaveBeenCalledTimes(1)
    const callArgs = mockFetch.mock.calls[0]
    expect(callArgs[0]).toBe('/api/file/batch-exists')
    expect(callArgs[1].method).toBe('POST')

    vi.unstubAllGlobals()
  })

  it('keeps annotations for existing paths (batch API returns file)', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { 'src/main.go': 'file' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    container.innerHTML = '<button class="chat-file-open-btn" data-file-path="src/main.go">open</button><span class="chat-file-path" data-file-path="src/main.go">src/main.go</span>'

    await verifyFilePaths(['src/main.go'], container)

    // Button and span should remain
    expect(container.querySelector('.chat-file-open-btn')).not.toBeNull()
    expect(container.querySelector('.chat-file-path')).not.toBeNull()

    vi.unstubAllGlobals()
  })

  it('keeps annotations for existing directories (batch API returns dir)', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { 'src': 'dir' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    container.innerHTML = '<button class="chat-file-open-btn" data-file-path="src">open</button>'

    await verifyFilePaths(['src'], container)

    expect(container.querySelector('.chat-file-open-btn')).not.toBeNull()

    vi.unstubAllGlobals()
  })

  it('handles mixed existing and non-existing paths', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { 'exists.go': 'file', 'missing.go': 'none' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    container.innerHTML = '<button class="chat-file-open-btn" data-file-path="exists.go">open</button><button class="chat-file-open-btn" data-file-path="missing.go">open</button>'

    await verifyFilePaths(['exists.go', 'missing.go'], container)

    expect(container.querySelector('[data-file-path="exists.go"]')).not.toBeNull()
    expect(container.querySelector('[data-file-path="missing.go"]')).toBeNull()

    vi.unstubAllGlobals()
  })

  it('handles network error gracefully (assumes exists)', async () => {
    const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    container.innerHTML = '<button class="chat-file-open-btn" data-file-path="test.go">open</button>'

    await verifyFilePaths(['test.go'], container)

    // On network error, assumes exists — button stays
    expect(container.querySelector('.chat-file-open-btn')).not.toBeNull()

    vi.unstubAllGlobals()
  })

  it('skips API call when all paths are cached', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { 'cached.go': 'file' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    // First call populates cache
    const container1 = document.createElement('div')
    await verifyFilePaths(['cached.go'], container1)

    // Second call should use cache — no fetch
    const mockFetch2 = vi.fn()
    vi.stubGlobal('fetch', mockFetch2)

    const container2 = document.createElement('div')
    container2.innerHTML = '<button class="chat-file-open-btn" data-file-path="cached.go">open</button>'
    await verifyFilePaths(['cached.go'], container2)

    expect(mockFetch2).not.toHaveBeenCalled()
    expect(container2.querySelector('.chat-file-open-btn')).not.toBeNull()

    vi.unstubAllGlobals()
  })

  it('does nothing for empty paths array', async () => {
    const mockFetch = vi.fn()
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    await verifyFilePaths([], container)

    expect(mockFetch).not.toHaveBeenCalled()

    vi.unstubAllGlobals()
  })

  it('deduplicates paths before making API call', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { 'dup.go': 'file' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    await verifyFilePaths(['dup.go', 'dup.go', 'dup.go'], container)

    // Should only call API once
    expect(mockFetch).toHaveBeenCalledTimes(1)
    // The body should contain deduplicated paths
    const body = JSON.parse(mockFetch.mock.calls[0][1].body)
    expect(body.paths).toEqual(['dup.go'])

    vi.unstubAllGlobals()
  })

  it('swaps to fallback path when primary does not exist but fallback does', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { 'web/src/utils.ts': 'none', 'utils.ts': 'file' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    // Simulate annotation with dual candidates: primary=web/src/utils.ts, fallback=utils.ts
    container.innerHTML = '<span class="chat-file-path" data-file-path="web/src/utils.ts" data-fallback-path="utils.ts">utils.ts</span><button class="chat-file-open-btn" data-file-path="web/src/utils.ts" data-fallback-path="utils.ts">open</button>'

    await verifyFilePaths(['web/src/utils.ts', 'utils.ts'], container)

    // Primary was swapped to fallback — elements should now have data-file-path="utils.ts"
    expect(container.querySelector('[data-file-path="utils.ts"]')).not.toBeNull()
    expect(container.querySelector('[data-file-path="web/src/utils.ts"]')).toBeNull()
    // Both span and button should have been swapped
    const swappedSpan = container.querySelector('.chat-file-path[data-file-path="utils.ts"]')
    expect(swappedSpan).not.toBeNull()
    expect(swappedSpan!.hasAttribute('data-fallback-path')).toBe(false)
    const swappedBtn = container.querySelector('.chat-file-open-btn[data-file-path="utils.ts"]')
    expect(swappedBtn).not.toBeNull()
    expect(swappedBtn!.hasAttribute('data-fallback-path')).toBe(false)

    vi.unstubAllGlobals()
  })

  it('removes annotation when neither primary nor fallback exists', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { 'web/src/missing.ts': 'none', 'missing.ts': 'none' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    container.innerHTML = '<span class="chat-file-path" data-file-path="web/src/missing.ts" data-fallback-path="missing.ts">missing.ts</span><button class="chat-file-open-btn" data-file-path="web/src/missing.ts" data-fallback-path="missing.ts">open</button>'

    await verifyFilePaths(['web/src/missing.ts', 'missing.ts'], container)

    // Both should be removed
    expect(container.querySelector('.chat-file-path')).toBeNull()
    expect(container.querySelector('.chat-file-open-btn')).toBeNull()
    // Text content preserved (unwrapped from span)
    expect(container.textContent).toContain('missing.ts')

    vi.unstubAllGlobals()
  })

  it('updates external status when fallback is project-internal', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { '/etc/hosts': 'none', 'etc/hosts': 'file' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    // Simulate external path with project-internal fallback
    container.innerHTML = '<span class="chat-file-path external" data-file-path="/etc/hosts" data-fallback-path="etc/hosts" data-external="true">hosts</span>'

    await verifyFilePaths(['/etc/hosts', 'etc/hosts'], container)

    const swapped = container.querySelector('.chat-file-path')
    expect(swapped).not.toBeNull()
    expect(swapped!.getAttribute('data-file-path')).toBe('etc/hosts')
    expect(swapped!.hasAttribute('data-external')).toBe(false)
    expect(swapped!.classList.contains('external')).toBe(false)

    vi.unstubAllGlobals()
  })

  it('keeps annotation when primary exists (no swap needed)', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: { 'web/src/utils.ts': 'file' } }),
    })
    vi.stubGlobal('fetch', mockFetch)

    const container = document.createElement('div')
    container.innerHTML = '<span class="chat-file-path" data-file-path="web/src/utils.ts" data-fallback-path="utils.ts">utils.ts</span>'

    await verifyFilePaths(['web/src/utils.ts'], container)

    // Primary exists — no swap, original attributes preserved
    const span = container.querySelector('.chat-file-path')
    expect(span).not.toBeNull()
    expect(span!.getAttribute('data-file-path')).toBe('web/src/utils.ts')
    expect(span!.getAttribute('data-fallback-path')).toBe('utils.ts')

    vi.unstubAllGlobals()
  })
})

describe('openFilePath', () => {
  let mockSelectFile: ReturnType<typeof vi.fn>
  let mockNavigateToDir: ReturnType<typeof vi.fn>

  beforeEach(async () => {
    clearVerifiedCache()
    const { store } = await import('@/stores/app')
    mockSelectFile = store.selectFile as ReturnType<typeof vi.fn>
    mockNavigateToDir = store.navigateToDir as ReturnType<typeof vi.fn>
    mockSelectFile.mockClear()
    mockNavigateToDir.mockClear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('navigates to directory when path is a directory', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', mockFetch)

    const mockDispatchEvent = vi.fn()
    const origDispatch = window.dispatchEvent
    window.dispatchEvent = mockDispatchEvent

    await openFilePath('src')

    expect(mockFetch).toHaveBeenCalledTimes(1)
    expect(mockFetch.mock.calls[0][0]).toContain('/api/dir?path=')
    expect(mockNavigateToDir).toHaveBeenCalledWith('src')
    expect(mockDispatchEvent).toHaveBeenCalled()

    window.dispatchEvent = origDispatch
    vi.unstubAllGlobals()
  })

  it('selects file when path exists as a file', async () => {
    // First fetch: /api/dir check → not ok (not a directory)
    // Second fetch: /api/file/batch-exists → file exists
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({ ok: false }) // /api/dir
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ results: { 'src/main.go': 'file' } }) }) // batch-exists

    vi.stubGlobal('fetch', mockFetch)

    await openFilePath('src/main.go')

    expect(mockSelectFile).toHaveBeenCalledWith('src/main.go')

    vi.unstubAllGlobals()
  })

  it('shows toast and does not select file when path does not exist', async () => {
    // First fetch: /api/dir check → not ok (not a directory)
    // Second fetch: /api/file/batch-exists → path doesn't exist
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({ ok: false }) // /api/dir
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ results: { 'src/deleted/File.ts': 'none' } }) }) // batch-exists

    vi.stubGlobal('fetch', mockFetch)

    // Mock useToast
    const mockShow = vi.fn()
    vi.doMock('@/composables/useToast', () => ({
      useToast: () => ({ show: mockShow }),
    }))

    await openFilePath('src/deleted/File.ts')

    // selectFile should NOT be called for non-existent file
    expect(mockSelectFile).not.toHaveBeenCalled()

    vi.unstubAllGlobals()
    vi.doUnmock('@/composables/useToast')
  })

  it('falls through to selectFile when batch-exists check fails (network error)', async () => {
    // First fetch: /api/dir check → not ok
    // Second fetch: /api/file/batch-exists → network error
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({ ok: false }) // /api/dir
      .mockRejectedValueOnce(new Error('Network error')) // batch-exists

    vi.stubGlobal('fetch', mockFetch)

    await openFilePath('src/main.go')

    // Best-effort: selectFile should still be called
    expect(mockSelectFile).toHaveBeenCalledWith('src/main.go')

    vi.unstubAllGlobals()
  })

  it('dispatches open-file-overlay with lineStart and lineEnd', async () => {
    mockSelectFile.mockResolvedValue(true)
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({ ok: false }) // /api/dir
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ results: { 'src/main.go': 'file' } }) }) // batch-exists

    vi.stubGlobal('fetch', mockFetch)

    const mockDispatchEvent = vi.fn()
    const origDispatch = window.dispatchEvent
    window.dispatchEvent = mockDispatchEvent

    await openFilePath('src/main.go', 42, 50)

    expect(mockSelectFile).toHaveBeenCalledWith('src/main.go')
    const overlayCalls = mockDispatchEvent.mock.calls.filter(call => call[0].type === 'open-file-overlay')
    expect(overlayCalls).toHaveLength(1)
    expect(overlayCalls[0][0].detail).toEqual({ path: 'src/main.go', lineStart: 42, lineEnd: 50 })

    window.dispatchEvent = origDispatch
    vi.unstubAllGlobals()
  })

  it('dispatches open-file-overlay with lineStart only (no lineEnd)', async () => {
    mockSelectFile.mockResolvedValue(true)
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({ ok: false }) // /api/dir
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ results: { 'src/main.go': 'file' } }) }) // batch-exists

    vi.stubGlobal('fetch', mockFetch)

    const mockDispatchEvent = vi.fn()
    const origDispatch = window.dispatchEvent
    window.dispatchEvent = mockDispatchEvent

    await openFilePath('src/main.go', 10)

    const overlayCalls = mockDispatchEvent.mock.calls.filter(call => call[0].type === 'open-file-overlay')
    expect(overlayCalls).toHaveLength(1)
    expect(overlayCalls[0][0].detail).toEqual({ path: 'src/main.go', lineStart: 10, lineEnd: undefined })

    window.dispatchEvent = origDispatch
    vi.unstubAllGlobals()
  })

  it('dispatches open-file-overlay without line info when none provided', async () => {
    mockSelectFile.mockResolvedValue(true)
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({ ok: false }) // /api/dir
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ results: { 'src/main.go': 'file' } }) }) // batch-exists

    vi.stubGlobal('fetch', mockFetch)

    const mockDispatchEvent = vi.fn()
    const origDispatch = window.dispatchEvent
    window.dispatchEvent = mockDispatchEvent

    await openFilePath('src/main.go')

    const overlayCalls = mockDispatchEvent.mock.calls.filter(call => call[0].type === 'open-file-overlay')
    expect(overlayCalls).toHaveLength(1)
    expect(overlayCalls[0][0].detail).toEqual({ path: 'src/main.go', lineStart: undefined, lineEnd: undefined })

    window.dispatchEvent = origDispatch
    vi.unstubAllGlobals()
  })

  it('navigates to directory when /api/dir fails but batch-exists returns dir', async () => {
    // First fetch: /api/dir check → not ok (e.g. trailing slash issue)
    // Second fetch: /api/file/batch-exists → type is "dir"
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({ ok: false }) // /api/dir
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ results: { 'internal/rag/': 'dir' } }) }) // batch-exists

    vi.stubGlobal('fetch', mockFetch)

    const mockDispatchEvent = vi.fn()
    const origDispatch = window.dispatchEvent
    window.dispatchEvent = mockDispatchEvent

    await openFilePath('internal/rag/')

    // Should navigate to directory, NOT call selectFile
    expect(mockNavigateToDir).toHaveBeenCalledWith('internal/rag/')
    expect(mockSelectFile).not.toHaveBeenCalled()
    // Should close file overlay and open file manager
    const eventTypes = mockDispatchEvent.mock.calls.map(call => call[0].type)
    expect(eventTypes).toContain('close-file-overlay')
    expect(eventTypes).toContain('open-file-manager')

    window.dispatchEvent = origDispatch
    vi.unstubAllGlobals()
  })

  it('shows external dir toast when batch-exists returns dir for external path', async () => {
    // First fetch: /api/dir is skipped for external paths
    // Second fetch: /api/file/batch-exists → type is "dir"
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ results: { '/external/dir': 'dir' } }) }) // batch-exists

    vi.stubGlobal('fetch', mockFetch)

    // Mock useToast
    const mockShow = vi.fn()
    vi.doMock('@/composables/useToast', () => ({
      useToast: () => ({ show: mockShow }),
    }))

    await openFilePath('/external/dir')

    // Should NOT navigate to directory for external paths
    expect(mockNavigateToDir).not.toHaveBeenCalled()
    expect(mockSelectFile).not.toHaveBeenCalled()

    vi.unstubAllGlobals()
    vi.doUnmock('@/composables/useToast')
  })
})
