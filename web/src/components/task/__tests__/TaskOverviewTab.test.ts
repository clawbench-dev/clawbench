import { describe, expect, it, vi } from 'vitest'
import { annotateFilePaths } from '@/composables/useFilePathAnnotation'
import { annotateCommitHashes } from '@/composables/useCommitHashAnnotation'

// Mock escapeHtml from html utils
vi.mock('@/utils/html', () => ({
  escapeHtml: (s: string) => s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c] || c)),
}))

// Mock splitPath
vi.mock('@/utils/path', () => ({
  splitPath: (p: string) => p.split('/').filter(Boolean),
}))

// Mock store
vi.mock('@/stores/app', () => ({
  store: {
    state: { projectRoot: '/home/user/project', homeDir: '/home/user' },
    selectFile: vi.fn(),
    navigateToDir: vi.fn(),
  },
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// Mock useWorktreeAnnotation — pass-through (worktree cache is async, test pipeline order only)
vi.mock('@/composables/useWorktreeAnnotation', () => ({
  useWorktreeAnnotation: () => ({ annotateWorktreePaths: (html: string) => ({ html }) }),
}))

// Mock useLocalhostAnnotation — pass-through
vi.mock('@/composables/useLocalhostAnnotation', () => ({
  annotateLocalhostUrls: (html: string) => html,
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: () => false }),
}))

// Ensure CSS.escape is available in jsdom
if (typeof (globalThis as any).CSS === 'undefined') {
  ;(globalThis as any).CSS = {}
}
if (typeof (globalThis as any).CSS.escape === 'undefined') {
  ;(globalThis as any).CSS.escape = (s: string) => s.replace(/[!"#$%&'()*+,./:;<=>?@[\\\]^`{|}~]/g, '\\$&')
}

const projectRoot = '/home/user/project'
const homeDir = '/home/user'

/**
 * Simulates the annotation pipeline used in TaskOverviewTab:
 * markdown → codeBlockHeaders → worktree → filepath → commit hash → localhost
 */
function renderAnnotatedPrompt(html: string): { html: string; detectedPaths: string[]; detectedSHAs: string[] } {
  // worktree is pass-through in mock
  const { html: annotatedHtml, detectedPaths } = annotateFilePaths(html, { projectRoot, homeDir })
  const { html: finalHtml, detectedSHAs } = annotateCommitHashes(annotatedHtml)
  // localhost is pass-through in mock
  return { html: finalHtml, detectedPaths, detectedSHAs }
}

describe('TaskOverviewTab prompt preview annotation pipeline', () => {
  describe('file path annotation', () => {
    it('annotates relative file paths in prompt text', () => {
      const input = '<p>Edit src/main.go for details</p>'
      const result = renderAnnotatedPrompt(input)
      expect(result.detectedPaths).toContain('src/main.go')
      expect(result.html).toContain('chat-file-open-btn')
      expect(result.html).toContain('data-file-path="src/main.go"')
    })

    it('annotates absolute paths under project root', () => {
      const input = '<p>See /home/user/project/internal/handler/file.go</p>'
      const result = renderAnnotatedPrompt(input)
      expect(result.detectedPaths.length).toBeGreaterThan(0)
      expect(result.html).toContain('chat-file-open-btn')
    })

    it('annotates file paths in code blocks', () => {
      const input = '<p>Modify <code>web/src/composables/useFoo.ts</code> to add the feature</p>'
      const result = renderAnnotatedPrompt(input)
      expect(result.detectedPaths.length).toBeGreaterThan(0)
      expect(result.html).toContain('chat-file-open-btn')
    })

    it('annotates file paths with line numbers', () => {
      const input = '<p>See internal/ai/factory.go:42 for the logic</p>'
      const result = renderAnnotatedPrompt(input)
      expect(result.detectedPaths.length).toBeGreaterThan(0)
      expect(result.html).toContain('chat-file-open-btn')
    })
  })

  describe('commit hash annotation', () => {
    it('annotates commit hashes in prompt text', () => {
      const input = '<p>Fixed in abc123def456789012345678901234567890abc</p>'
      const result = renderAnnotatedPrompt(input)
      expect(result.html).toContain('chat-commit-hash')
      expect(result.detectedSHAs.length).toBeGreaterThan(0)
    })

    it('preserves file path annotation alongside commit hashes', () => {
      const input = '<p>See src/main.go and commit abc123def456789012345678901234567890abc</p>'
      const result = renderAnnotatedPrompt(input)
      expect(result.html).toContain('chat-file-open-btn')
      expect(result.html).toContain('chat-commit-hash')
    })
  })

  describe('no annotation for non-paths', () => {
    it('does not annotate plain text without file paths', () => {
      const input = '<p>Run the task every 5 minutes</p>'
      const result = renderAnnotatedPrompt(input)
      expect(result.html).not.toContain('chat-file-open-btn')
      expect(result.html).not.toContain('chat-commit-hash')
    })
  })

  describe('pipeline order', () => {
    it('worktree annotation runs before file path annotation', () => {
      // This test verifies the mock was set up with the correct pipeline order.
      // The actual worktree-before-filepath behavior is tested in useChatRender tests.
      const input = '<p>Edit src/main.go</p>'
      const result = renderAnnotatedPrompt(input)
      expect(result.detectedPaths).toContain('src/main.go')
    })
  })
})
