import { describe, expect, it } from 'vitest'
import { slugify, extractToc } from '@/utils/toc.ts'

describe('slugify', () => {
  it('lowercases text', () => {
    expect(slugify('Hello World')).toBe('hello-world')
  })

  it('replaces spaces with dashes', () => {
    expect(slugify('section one')).toBe('section-one')
  })

  it('removes leading and trailing dashes', () => {
    expect(slugify('--hello--')).toBe('hello')
  })

  it('keeps Chinese characters', () => {
    expect(slugify('配置选项')).toBe('配置选项')
  })

  it('replaces special characters with dashes', () => {
    expect(slugify('hello@world!')).toBe('hello-world')
  })

  it('handles empty string', () => {
    expect(slugify('')).toBe('')
  })

  it('handles numbers', () => {
    expect(slugify('Step 1')).toBe('step-1')
  })

  it('handles multiple consecutive special chars', () => {
    expect(slugify('a!!!b')).toBe('a-b')
  })

  it('handles mixed Chinese and English', () => {
    expect(slugify('配置 Options')).toBe('配置-options')
  })

  it('handles underscores (kept as word chars)', () => {
    expect(slugify('my_variable')).toBe('my_variable')
  })

  it('handles hyphens (treated as word chars)', () => {
    expect(slugify('already-dashed')).toBe('already-dashed')
  })

  it('handles dots', () => {
    expect(slugify('v1.0.0')).toBe('v1-0-0')
  })

  it('handles tabs and newlines as whitespace', () => {
    expect(slugify('hello\tworld\nfoo')).toBe('hello-world-foo')
  })

  it('handles only special characters', () => {
    expect(slugify('@#$%')).toBe('')
  })

  it('handles parentheses', () => {
    expect(slugify('func(arg)')).toBe('func-arg')
  })

  it('handles square brackets', () => {
    expect(slugify('array[0]')).toBe('array-0')
  })
})

describe('extractToc - markdown', () => {
  it('extracts markdown headers', () => {
    const content = '# Title\n## Section 1\n### Subsection\n## Section 2'
    const toc = extractToc(content, 'markdown')
    expect(toc).toHaveLength(4)
    expect(toc[0].level).toBe(1)
    expect(toc[0].text).toBe('Title')
    expect(toc[1].level).toBe(2)
    expect(toc[1].text).toBe('Section 1')
    expect(toc[2].level).toBe(3)
    expect(toc[2].text).toBe('Subsection')
  })

  it('returns empty for empty markdown', () => {
    const toc = extractToc('', 'markdown')
    expect(toc).toEqual([])
  })

  it('returns empty for markdown with no headers', () => {
    const toc = extractToc('Just some text\nNo headers here', 'markdown')
    expect(toc).toEqual([])
  })

  it('generates correct slug IDs for markdown', () => {
    const toc = extractToc('# Hello World', 'markdown')
    expect(toc[0].id).toBe('hello-world')
  })

  it('calculates correct line numbers for markdown', () => {
    const content = 'line 1\nline 2\n## Header on line 3'
    const toc = extractToc(content, 'markdown')
    expect(toc[0].line).toBe(3)
  })

  it('extracts h4-h6 headers', () => {
    const content = '#### Deep Header\n##### Deeper\n###### Deepest'
    const toc = extractToc(content, 'markdown')
    expect(toc).toHaveLength(3)
    expect(toc[0].level).toBe(4)
    expect(toc[1].level).toBe(5)
    expect(toc[2].level).toBe(6)
  })

  it('generates correct slug for Chinese headers', () => {
    const toc = extractToc('# 配置选项', 'markdown')
    expect(toc[0].id).toBe('配置选项')
  })

  it('does not filter # in code blocks (known limitation of simple regex)', () => {
    const content = '```python\n# This is a comment\n```\n## Real Header'
    const toc = extractToc(content, 'markdown')
    expect(toc.length).toBeGreaterThanOrEqual(1)
    const realHeaders = toc.filter(t => t.text === 'Real Header')
    expect(realHeaders).toHaveLength(1)
  })
})

// Languages with tree-sitter tags query (go, python, typescript, rust, java, c, cpp,
// ruby, swift, kotlin, scala, lua, css, dart, zig, nim, r, etc.) are handled by the
// backend /api/file/symbols endpoint. These tests only cover the regex fallback patterns
// for languages WITHOUT tree-sitter tags query.

describe('extractToc - regex fallback languages', () => {
  it('extracts PHP class and function symbols', () => {
    const content = 'class UserService {\n  public static function find() {\nfunction helper() {'
    const toc = extractToc(content, 'php')
    expect(toc.length).toBeGreaterThanOrEqual(2)
    const texts = toc.map(t => t.text)
    expect(texts).toContain('UserService')
    expect(texts).toContain('helper')
  })

  it('extracts PHP abstract class and interface', () => {
    const content = 'abstract class BaseHandler {\ninterface Logger {'
    const toc = extractToc(content, 'php')
    const texts = toc.map(t => t.text)
    expect(texts).toContain('BaseHandler')
    expect(texts).toContain('Logger')
  })

  it('extracts Bash function symbols', () => {
    const content = 'build() {\nfunction deploy() {'
    const toc = extractToc(content, 'bash')
    expect(toc.length).toBeGreaterThanOrEqual(1)
    const texts = toc.map(t => t.text)
    expect(texts).toContain('build')
  })

  it('extracts SQL CREATE TABLE symbols', () => {
    const content = 'CREATE TABLE users (\n  id INT PRIMARY KEY\n);\nCREATE VIEW active_users AS SELECT * FROM users;'
    const toc = extractToc(content, 'sql')
    expect(toc.length).toBeGreaterThanOrEqual(1)
    const texts = toc.map(t => t.text)
    expect(texts.some(t => t.startsWith('users') || t.includes('users'))).toBe(true)
  })

  it('extracts Nginx server and location blocks', () => {
    const content = 'server {\n  listen 80;\n  location /api {\n  upstream backend {'
    const toc = extractToc(content, 'nginx')
    expect(toc.length).toBeGreaterThanOrEqual(2)
    const texts = toc.map(t => t.text)
    expect(texts).toContain('server')
    expect(texts).toContain('/api')
  })

  it('extracts INI section headers', () => {
    const content = '[database]\nhost=localhost\n[server]\nport=8080'
    const toc = extractToc(content, 'ini')
    expect(toc).toHaveLength(2)
    const texts = toc.map(t => t.text)
    expect(texts).toContain('database')
    expect(texts).toContain('server')
  })

  it('extracts Dockerfile instructions', () => {
    const content = 'FROM golang:1.21\nRUN go build\nCMD ["./app"]'
    const toc = extractToc(content, 'dockerfile')
    expect(toc.length).toBeGreaterThanOrEqual(1)
  })

  it('extracts Vue template/script/style sections', () => {
    const content = '<template>\n  <div/>\n</template>\n<script setup>\n</script>\n<style scoped>\n</style>'
    const toc = extractToc(content, 'vue')
    expect(toc.length).toBeGreaterThanOrEqual(2)
    const texts = toc.map(t => t.text)
    expect(texts).toContain('template')
    expect(texts).toContain('script')
  })

  it('extracts GraphQL type and field symbols', () => {
    const content = 'type Query {\n  users: [User]\n}\ninput CreateUserInput {\n  name: String\n}\nenum Role {\n  ADMIN\n}'
    const toc = extractToc(content, 'graphql')
    expect(toc.length).toBeGreaterThanOrEqual(3)
    const texts = toc.map(t => t.text)
    expect(texts).toContain('Query')
    expect(texts).toContain('CreateUserInput')
    expect(texts).toContain('Role')
  })

  it('extracts YAML key extraction', () => {
    const content = 'server:\n  port: 8080\n  host: localhost'
    const toc = extractToc(content, 'yaml')
    expect(toc.length).toBeGreaterThanOrEqual(1)
  })

  it('extracts JSON key extraction', () => {
    const content = '{\n  "name": "test",\n  "version": "1.0"\n}'
    const toc = extractToc(content, 'json')
    expect(toc.length).toBeGreaterThanOrEqual(1)
  })

  it('extracts TOML section headers', () => {
    const content = '[dependencies]\nserde = "1.0"\n[[bin]]\nname = "app"\n[profile.release]'
    const toc = extractToc(content, 'toml')
    expect(toc.length).toBeGreaterThanOrEqual(2)
    const texts = toc.map(t => t.text)
    expect(texts).toContain('dependencies')
    expect(texts).toContain('bin')
  })

  it('extracts Makefile targets', () => {
    const content = 'build:\n\tgo build\n test:\n\tgo test'
    const toc = extractToc(content, 'makefile')
    expect(toc.length).toBeGreaterThanOrEqual(1)
    const texts = toc.map(t => t.text)
    expect(texts).toContain('build')
  })

  it('returns empty for unknown language with no extractable content', () => {
    const toc = extractToc('just some random text', 'unknown')
    expect(toc).toEqual([])
  })

  it('sorts code symbols by line number', () => {
    const content = 'function later() {}\nfunction first() {}'
    const toc = extractToc(content, 'bash')
    if (toc.length >= 2) {
      expect(toc[0].line).toBeLessThanOrEqual(toc[1].line)
    }
  })
})

describe('extractTocGeneric (fallback for unknown lang)', () => {
  it('extracts key-value pairs by indentation', () => {
    const content = 'root:\n  child1: value\n  child2:\n    grandchild: val'
    const toc = extractToc(content, 'unknown_lang')
    expect(toc.length).toBeGreaterThanOrEqual(1)
    const texts = toc.map(t => t.text)
    expect(texts).toContain('root')
  })

  it('skips comment lines starting with //', () => {
    const content = '// this is a comment\nkey: value'
    const toc = extractToc(content, 'unknown_lang')
    const texts = toc.map(t => t.text)
    expect(texts).not.toContain('this')
  })

  it('skips comment lines starting with #', () => {
    const content = '# this is a comment\nkey: value'
    const toc = extractToc(content, 'unknown_lang')
    const texts = toc.map(t => t.text)
    expect(texts).not.toContain('this')
  })

  it('skips comment lines starting with <!--', () => {
    const content = '<!-- HTML comment -->\nkey: value'
    const toc = extractToc(content, 'unknown_lang')
    const texts = toc.map(t => t.text)
    expect(texts).not.toContain('!--')
  })

  it('skips keys shorter than 2 characters', () => {
    const content = '{\n  a: 1,\n  ab: 2,\n}'
    const toc = extractToc(content, 'unknown_lang')
    const texts = toc.map(t => t.text)
    expect(texts).not.toContain('a')
    expect(texts).toContain('ab')
  })

  it('skips { as key', () => {
    const content = '{: something\nkey: value'
    const toc = extractToc(content, 'unknown_lang')
    const texts = toc.map(t => t.text)
    expect(texts).not.toContain('{')
  })

  it('skips [ as key', () => {
    const content = '[: something\nkey: value'
    const toc = extractToc(content, 'unknown_lang')
    const texts = toc.map(t => t.text)
    expect(texts).not.toContain('[')
  })

  it('limits results to 150 entries', () => {
    const lines = Array.from({ length: 200 }, (_, i) => `key${i}: value`)
    const content = lines.join('\n')
    const toc = extractToc(content, 'unknown_lang')
    expect(toc.length).toBeLessThanOrEqual(150)
  })

  it('calculates indent levels correctly', () => {
    const content = 'root:\n  child:\n    grandchild:'
    const toc = extractToc(content, 'unknown_lang')
    expect(toc).toHaveLength(3)
    expect(toc[0].level).toBe(1)
    expect(toc[1].level).toBe(2)
    expect(toc[2].level).toBe(3)
  })

  it('skips lines that do not end with : , { [ (', () => {
    const content = 'just a plain line\nkey: value'
    const toc = extractToc(content, 'unknown_lang')
    const texts = toc.map(t => t.text)
    expect(texts).not.toContain('just')
  })

  it('skips lines with indent > 6', () => {
    const content = 'root:\n        tooDeep: value'
    const toc = extractToc(content, 'unknown_lang')
    const texts = toc.map(t => t.text)
    expect(texts).toContain('root')
    expect(texts).not.toContain('tooDeep')
  })

  it('returns empty for content with no extractable keys', () => {
    const toc = extractToc('just plain text\nno keys here', 'unknown_lang')
    expect(toc).toEqual([])
  })
})

describe('slugify edge cases', () => {
  it('handles pure Chinese with punctuation', () => {
    expect(slugify('你好，世界！')).toBe('你好-世界')
  })

  it('handles Unicode emoji', () => {
    const result = slugify('hello 🚀 world')
    expect(result).toBe('hello-world')
  })

  it('handles very long string', () => {
    const long = 'a'.repeat(10000)
    const result = slugify(long)
    expect(result).toBe(long.toLowerCase())
    expect(result).toHaveLength(10000)
  })

  it('handles mixed upper and lower case', () => {
    expect(slugify('FooBARbaz')).toBe('foobarbaz')
  })

  it('handles Chinese with English and special chars', () => {
    expect(slugify('第1章：简介（Overview）')).toBe('第1章-简介-overview')
  })
})
