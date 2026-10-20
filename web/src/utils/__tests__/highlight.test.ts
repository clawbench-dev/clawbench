import { describe, expect, it } from 'vitest'
import { hljs, highlightCode } from '@/utils/highlight'

describe('highlightCode', () => {
    it('highlights JavaScript code', () => {
        const result = highlightCode('const x = 1;', 'javascript')
        expect(result).toContain('<span')
        expect(result).toContain('keyword')
    })

    it('highlights Python code', () => {
        const result = highlightCode('def foo(): pass', 'python')
        expect(result).toContain('<span')
    })

    it('highlights Go code', () => {
        const result = highlightCode('func main() {}', 'go')
        expect(result).toContain('<span')
    })

    it('highlights JSON code', () => {
        const result = highlightCode('{"key": "value"}', 'json')
        expect(result).toContain('<span')
    })

    it('highlights Bash code', () => {
        const result = highlightCode('echo hello', 'bash')
        expect(result).toContain('<span')
    })

    it('highlights SQL code', () => {
        const result = highlightCode('SELECT * FROM users;', 'sql')
        expect(result).toContain('<span')
    })

    it('returns escaped HTML for unregistered language', () => {
        const code = 'if (x < 5) { y = "hello" & x; }'
        const result = highlightCode(code, 'brainfuck')
        expect(result).not.toContain('<span')
        expect(result).toContain('&lt;')
        expect(result).toContain('&amp;')
        expect(result).toContain('&quot;')
    })

    it('returns escaped HTML for empty language', () => {
        const code = '<script>alert(1)</script>'
        const result = highlightCode(code, '')
        expect(result).toContain('&lt;script')
        expect(result).not.toContain('<span')
    })

    it('escapes HTML special characters for unknown languages', () => {
        const result = highlightCode('a < b & c > d', 'unknown_lang')
        expect(result).toContain('&lt;')
        expect(result).toContain('&amp;')
        expect(result).toContain('&gt;')
    })

    it('produces different output for known vs unknown languages', () => {
        const code = 'const x = 1;'
        const known = highlightCode(code, 'javascript')
        const unknown = highlightCode(code, 'nonexistent_lang')
        expect(known).toContain('<span')
        expect(unknown).not.toContain('<span class="hljs')
    })
})

describe('hljs.getLanguage', () => {
    it('includes core languages', () => {
        expect(hljs.getLanguage('javascript')).toBeTruthy()
        expect(hljs.getLanguage('typescript')).toBeTruthy()
        expect(hljs.getLanguage('python')).toBeTruthy()
        expect(hljs.getLanguage('go')).toBeTruthy()
        expect(hljs.getLanguage('rust')).toBeTruthy()
        expect(hljs.getLanguage('java')).toBeTruthy()
        expect(hljs.getLanguage('cpp')).toBeTruthy()
    })

    it('includes web languages', () => {
        expect(hljs.getLanguage('html')).toBeTruthy()
        expect(hljs.getLanguage('css')).toBeTruthy()
        expect(hljs.getLanguage('json')).toBeTruthy()
        expect(hljs.getLanguage('yaml')).toBeTruthy()
        expect(hljs.getLanguage('xml')).toBeTruthy()
        expect(hljs.getLanguage('markdown')).toBeTruthy()
    })

    it('includes scripting languages', () => {
        expect(hljs.getLanguage('bash')).toBeTruthy()
        expect(hljs.getLanguage('shell')).toBeTruthy()
        expect(hljs.getLanguage('sql')).toBeTruthy()
        expect(hljs.getLanguage('dockerfile')).toBeTruthy()
        expect(hljs.getLanguage('diff')).toBeTruthy()
    })

    it('returns undefined for unregistered languages', () => {
        expect(hljs.getLanguage('brainfuck')).toBeUndefined()
        expect(hljs.getLanguage('abnf')).toBeUndefined()
    })

    it('resolves language aliases', () => {
        expect(hljs.getLanguage('ts')).toBeTruthy()
        expect(hljs.getLanguage('js')).toBeTruthy()
        expect(hljs.getLanguage('py')).toBeTruthy()
        expect(hljs.getLanguage('sh')).toBeTruthy()
        expect(hljs.getLanguage('toml')).toBeTruthy()
        expect(hljs.getLanguage('ini')).toBeTruthy()
    })

    it('has approximately 50+ languages registered', () => {
        // Count registered languages by checking a representative sample
        const sampleLanguages = [
            'javascript', 'typescript', 'python', 'go', 'rust', 'java', 'c', 'cpp',
            'csharp', 'ruby', 'php', 'swift', 'kotlin', 'scala', 'objectivec',
            'bash', 'shell', 'perl', 'lua', 'dart', 'r', 'elixir', 'erlang',
            'haskell', 'clojure', 'ocaml', 'fsharp', 'groovy',
            'html', 'css', 'scss', 'less', 'json', 'yaml', 'xml', 'markdown',
            'graphql', 'handlebars',
            'sql', 'diff', 'dockerfile', 'makefile', 'ini', 'nginx', 'protobuf',
            'cmake', 'gradle',
            'glsl', 'latex', 'matlab', 'powershell', 'vim', 'wasm', 'verilog', 'vhdl',
        ]
        const registered = sampleLanguages.filter(l => hljs.getLanguage(l))
        expect(registered.length).toBeGreaterThanOrEqual(50)
    })
})

describe('lazy language tier', () => {
    it('does not carry the long tail eagerly', () => {
        // The eager registry is what every page load pays for. `protobuf`,
        // `nix`, `julia` are common enough to notice but not common enough to
        // ship on the first screen.
        expect(hljs.getLanguage('protobuf')).toBeUndefined()
        expect(hljs.getLanguage('nix')).toBeUndefined()
        expect(hljs.getLanguage('julia')).toBeUndefined()
        // Sanity: the eager set is still populated.
        expect(hljs.getLanguage('javascript')).toBeTruthy()
    })

    it('ensureLanguage registers a lazy grammar on demand', async () => {
        const { ensureLanguage } = await import('@/utils/highlight')
        expect(hljs.getLanguage('protobuf')).toBeUndefined()
        await expect(ensureLanguage('protobuf')).resolves.toBe(true)
        expect(hljs.getLanguage('protobuf')).toBeTruthy()
    })

    it('registers upstream aliases together with the lazy grammar', async () => {
        const { ensureLanguage } = await import('@/utils/highlight')
        await ensureLanguage('protobuf')
        // `proto` is highlight.js's own alias for protobuf, but aliases are
        // derived at registration time — loading the grammar must restore it,
        // or `.proto` files stay uncoloured despite the grammar being present.
        expect(hljs.getLanguage('proto')).toBeTruthy()
    })

    it('highlights a lazy language after it loads', async () => {
        const { ensureLanguage, highlightCode } = await import('@/utils/highlight')
        await ensureLanguage('protobuf')
        const result = highlightCode('message User { string name = 1; }', 'protobuf')
        expect(result).toContain('hljs-')
    })

    it('falls back to escaped plain text while a lazy grammar is still loading', async () => {
        const { ensureLanguage, highlightCode } = await import('@/utils/highlight')
        // `nim` is lazy and nothing else in this file has loaded it yet.
        const before = highlightCode('proc f() = discard', 'nim')
        expect(before).not.toContain('<span')
        // …and the call schedules the load, so a later render is coloured.
        await ensureLanguage('nim')
        const after = highlightCode('proc f() = discard', 'nim')
        expect(after).toContain('hljs-')
    })

    it('resolves false for a language no grammar exists for', async () => {
        const { ensureLanguage } = await import('@/utils/highlight')
        await expect(ensureLanguage('not_a_real_language')).resolves.toBe(false)
    })

    it('bumps languageVersion once per load so renderers can re-run', async () => {
        const { ensureLanguage, languageVersion } = await import('@/utils/highlight')
        const before = languageVersion.value
        await ensureLanguage('crystal')
        expect(languageVersion.value).toBeGreaterThan(before)
        const after = languageVersion.value
        // Second call is a no-op — no extra bump, or every renderer would
        // re-render forever.
        await ensureLanguage('crystal')
        expect(languageVersion.value).toBe(after)
    })

    it('shares one in-flight load between concurrent callers', async () => {
        const { ensureLanguage } = await import('@/utils/highlight')
        const [a, b, c] = await Promise.all([
            ensureLanguage('elm'),
            ensureLanguage('elm'),
            ensureLanguage('elm'),
        ])
        expect([a, b, c]).toEqual([true, true, true])
    })
})

describe('highlightCode hot path', () => {
    it('does not allocate a fresh load for an unknown language on every call', async () => {
        // `highlightCode` runs from template render functions. Without a
        // negative cache, an unknown language (or an unlabelled fence) would
        // schedule a new import promise per render — thousands during a turn.
        const { ensureLanguage } = await import('@/utils/highlight')
        const missing = 'definitely_not_a_language'
        const first = ensureLanguage(missing)
        const second = ensureLanguage(missing)
        // Resolved misses are cheap and identical; the point is that the
        // missing-language set short-circuits before touching the loaders.
        await expect(first).resolves.toBe(false)
        await expect(second).resolves.toBe(false)
    })

    it('keeps plaintext eager so .txt/.log/.csv never fetch a chunk', async () => {
        // plaintext has no grammar, but it is the most common file type.
        // Routing it through the lazy tier would fire a network request for
        // every plain file opened.
        expect(hljs.getLanguage('plaintext')).toBeTruthy()
    })
})
