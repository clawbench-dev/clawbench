import type { Extension } from '@codemirror/state'
import type { Language } from '@codemirror/language'
import { StreamLanguage } from '@codemirror/language'
// High-frequency languages: static imports (always needed)
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { yaml } from '@codemirror/lang-yaml'
import { xml } from '@codemirror/lang-xml'
import { html } from '@codemirror/lang-html'
import { css } from '@codemirror/lang-css'
import { markdown } from '@codemirror/lang-markdown'
import { go } from '@codemirror/lang-go'
import { python } from '@codemirror/lang-python'

/** Lazy-loaded language factory: returns a Promise of Extension. */
type LangFactory = () => Extension | Promise<Extension>

/**
 * Wrap a CodeMirror 5-style stream parser as a CodeMirror 6 extension.
 *
 * `@codemirror/legacy-modes` ships the long tail of grammars that never got a
 * Lezer rewrite. They are line-oriented (no syntax tree), so they cannot fold
 * or recover from errors like `@codemirror/lang-*` can — but they colour code,
 * which is the whole job here. Every entry below is lazy-loaded, so none of
 * this costs the first screen anything.
 */
const stream = (load: () => Promise<unknown>): LangFactory => () =>
    load().then((parser) => StreamLanguage.define(parser as never))

/**
 * GraphQL has no official Lezer grammar, and the only community package
 * (`cm6-graphql`) drags in the full `graphql` runtime (~6.5MB unpacked) just
 * for highlighting. A `simpleMode` state machine covers the syntax that
 * actually appears in a `.graphql` file without that weight.
 */
const graphqlMode = stream(async () => {
    const { simpleMode } = await import('@codemirror/legacy-modes/mode/simple-mode')
    return simpleMode({
        start: [
            { regex: /#[^\n]*/, token: 'comment' },
            { regex: /"""/, token: 'string', next: 'blockString' },
            { regex: /"/, token: 'string', next: 'string' },
            {
                regex: /\b(query|mutation|subscription|fragment|on|type|interface|union|enum|input|scalar|schema|extend|directive|implements|repeatable)\b/,
                token: 'keyword',
            },
            { regex: /\b(true|false|null)\b/, token: 'atom' },
            { regex: /[-+]?\d+(\.\d+)?([eE][-+]?\d+)?/, token: 'number' },
            { regex: /\$[_A-Za-z][_0-9A-Za-z]*/, token: 'variableName.special' },
            { regex: /@[_A-Za-z][_0-9A-Za-z]*/, token: 'meta' },
            { regex: /[_A-Za-z][_0-9A-Za-z]*/, token: 'variableName' },
            { regex: /[{}()[\]:,!$@=|&]/, token: 'punctuation' },
            { regex: /\s+/, token: null },
        ],
        string: [
            { regex: /[^"\\]+/, token: 'string' },
            { regex: /\\./, token: 'string.special' },
            { regex: /"/, token: 'string', next: 'start' },
        ],
        blockString: [
            { regex: /"""/, token: 'string', next: 'start' },
            { regex: /[^"]+/, token: 'string' },
            { regex: /"/, token: 'string' },
        ],
    })
})

export const LANG_EXT: Record<string, LangFactory> = {
    // Static imports (high-frequency)
    javascript: () => javascript(),
    typescript: () => javascript({ typescript: true }),
    json: () => json(),
    yaml: () => yaml(),
    xml: () => xml(),
    html: () => html(),
    css: () => css(),
    markdown: () => buildMarkdownExtension(),
    go: () => go(),
    python: () => python(),
    // Lazy imports (official @codemirror/lang-*)
    rust: () => import('@codemirror/lang-rust').then(m => m.rust()),
    java: () => import('@codemirror/lang-java').then(m => m.java()),
    c: () => import('@codemirror/lang-cpp').then(m => m.cpp()),
    cpp: () => import('@codemirror/lang-cpp').then(m => m.cpp()),
    sql: () => import('@codemirror/lang-sql').then(m => m.sql()),
    php: () => import('@codemirror/lang-php').then(m => m.php()),
    vue: () => import('@codemirror/lang-vue').then(m => m.vue()),
    less: () => import('@codemirror/lang-less').then(m => m.less()),
    // `.scss` is the brace syntax; `.sass` is the indented one.
    scss: () => import('@codemirror/lang-sass').then(m => m.sass()),
    sass: () => import('@codemirror/lang-sass').then(m => m.sass({ indented: true })),
    liquid: () => import('@codemirror/lang-liquid').then(m => m.liquid()),
    angular: () => import('@codemirror/lang-angular').then(m => m.angular()),
    wast: () => import('@codemirror/lang-wast').then(m => m.wast()),
    // Lazy imports (community packages)
    bash: () => import('@codincod/codemirror-lang-shell').then(m => m.shell()),
    shell: () => import('@codincod/codemirror-lang-shell').then(m => m.shell()),
    lua: () => import('@codincod/codemirror-lang-lua').then(m => m.lua()),
    swift: () => import('@codincod/codemirror-lang-swift').then(m => m.swift()),
    kotlin: () => import('@codincod/codemirror-lang-kotlin').then(m => m.kotlin()),
    scala: () => import('@codincod/codemirror-lang-scala').then(m => m.scala()),
    ruby: () => import('codemirror-lang-ruby').then(m => m.ruby()),
    diff: () => import('codemirror-lang-diff').then(m => m.diff()),
    csharp: () => import('@replit/codemirror-lang-csharp').then(m => m.csharp()),
    perl: () => import('codemirror-lang-perl').then(m => m.perl()),
    makefile: () => import('codemirror-lang-makefile').then(m => m.makefile()),
    r: () => import('codemirror-lang-r').then(m => m.r()),

    // ── Legacy stream modes (no Lezer grammar exists) ────────────────────────
    graphql: graphqlMode,
    dockerfile: stream(() => import('@codemirror/legacy-modes/mode/dockerfile').then(m => m.dockerFile)),
    nginx: stream(() => import('@codemirror/legacy-modes/mode/nginx').then(m => m.nginx)),
    powershell: stream(() => import('@codemirror/legacy-modes/mode/powershell').then(m => m.powerShell)),
    toml: stream(() => import('@codemirror/legacy-modes/mode/toml').then(m => m.toml)),
    ini: stream(() => import('@codemirror/legacy-modes/mode/properties').then(m => m.properties)),
    properties: stream(() => import('@codemirror/legacy-modes/mode/properties').then(m => m.properties)),
    latex: stream(() => import('@codemirror/legacy-modes/mode/stex').then(m => m.stex)),
    protobuf: stream(() => import('@codemirror/legacy-modes/mode/protobuf').then(m => m.protobuf)),
    cmake: stream(() => import('@codemirror/legacy-modes/mode/cmake').then(m => m.cmake)),
    clojure: stream(() => import('@codemirror/legacy-modes/mode/clojure').then(m => m.clojure)),
    groovy: stream(() => import('@codemirror/legacy-modes/mode/groovy').then(m => m.groovy)),
    gradle: stream(() => import('@codemirror/legacy-modes/mode/groovy').then(m => m.groovy)),
    haskell: stream(() => import('@codemirror/legacy-modes/mode/haskell').then(m => m.haskell)),
    ocaml: stream(() => import('@codemirror/legacy-modes/mode/mllike').then(m => m.oCaml)),
    fsharp: stream(() => import('@codemirror/legacy-modes/mode/mllike').then(m => m.fSharp)),
    sml: stream(() => import('@codemirror/legacy-modes/mode/mllike').then(m => m.sml)),
    erlang: stream(() => import('@codemirror/legacy-modes/mode/erlang').then(m => m.erlang)),
    elm: stream(() => import('@codemirror/legacy-modes/mode/elm').then(m => m.elm)),
    scheme: stream(() => import('@codemirror/legacy-modes/mode/scheme').then(m => m.scheme)),
    lisp: stream(() => import('@codemirror/legacy-modes/mode/commonlisp').then(m => m.commonLisp)),
    julia: stream(() => import('@codemirror/legacy-modes/mode/julia').then(m => m.julia)),
    crystal: stream(() => import('@codemirror/legacy-modes/mode/crystal').then(m => m.crystal)),
    d: stream(() => import('@codemirror/legacy-modes/mode/d').then(m => m.d)),
    haxe: stream(() => import('@codemirror/legacy-modes/mode/haxe').then(m => m.haxe)),
    livescript: stream(() => import('@codemirror/legacy-modes/mode/livescript').then(m => m.liveScript)),
    coffeescript: stream(() => import('@codemirror/legacy-modes/mode/coffeescript').then(m => m.coffeeScript)),
    delphi: stream(() => import('@codemirror/legacy-modes/mode/pascal').then(m => m.pascal)),
    cobol: stream(() => import('@codemirror/legacy-modes/mode/cobol').then(m => m.cobol)),
    fortran: stream(() => import('@codemirror/legacy-modes/mode/fortran').then(m => m.fortran)),
    tcl: stream(() => import('@codemirror/legacy-modes/mode/tcl').then(m => m.tcl)),
    sas: stream(() => import('@codemirror/legacy-modes/mode/sas').then(m => m.sas)),
    smalltalk: stream(() => import('@codemirror/legacy-modes/mode/smalltalk').then(m => m.smalltalk)),
    stylus: stream(() => import('@codemirror/legacy-modes/mode/stylus').then(m => m.stylus)),
    pug: stream(() => import('@codemirror/legacy-modes/mode/pug').then(m => m.pug)),
    velocity: stream(() => import('@codemirror/legacy-modes/mode/velocity').then(m => m.velocity)),
    puppet: stream(() => import('@codemirror/legacy-modes/mode/puppet').then(m => m.puppet)),
    nsis: stream(() => import('@codemirror/legacy-modes/mode/nsis').then(m => m.nsis)),
    http: stream(() => import('@codemirror/legacy-modes/mode/http').then(m => m.http)),
    gherkin: stream(() => import('@codemirror/legacy-modes/mode/gherkin').then(m => m.gherkin)),
    cypher: stream(() => import('@codemirror/legacy-modes/mode/cypher').then(m => m.cypher)),
    turtle: stream(() => import('@codemirror/legacy-modes/mode/turtle').then(m => m.turtle)),
    sparql: stream(() => import('@codemirror/legacy-modes/mode/sparql').then(m => m.sparql)),
    dtd: stream(() => import('@codemirror/legacy-modes/mode/dtd').then(m => m.dtd)),
    verilog: stream(() => import('@codemirror/legacy-modes/mode/verilog').then(m => m.verilog)),
    vhdl: stream(() => import('@codemirror/legacy-modes/mode/vhdl').then(m => m.vhdl)),
    glsl: stream(() => import('@codemirror/legacy-modes/mode/clike').then(m => m.shader)),
    objectivec: stream(() => import('@codemirror/legacy-modes/mode/clike').then(m => m.objectiveC)),
    dart: stream(() => import('@codemirror/legacy-modes/mode/clike').then(m => m.dart)),
    ebnf: stream(() => import('@codemirror/legacy-modes/mode/ebnf').then(m => m.ebnf)),
    idl: stream(() => import('@codemirror/legacy-modes/mode/idl').then(m => m.idl)),
    xquery: stream(() => import('@codemirror/legacy-modes/mode/xquery').then(m => m.xQuery)),
    matlab: stream(() => import('@codemirror/legacy-modes/mode/octave').then(m => m.octave)),
    vbnet: stream(() => import('@codemirror/legacy-modes/mode/vb').then(m => m.vb)),
    vbscript: stream(() => import('@codemirror/legacy-modes/mode/vbscript').then(m => m.vbScript)),
    webidl: stream(() => import('@codemirror/legacy-modes/mode/webidl').then(m => m.webIDL)),
    yacas: stream(() => import('@codemirror/legacy-modes/mode/yacas').then(m => m.yacas)),
    z80: stream(() => import('@codemirror/legacy-modes/mode/z80').then(m => m.z80)),
    troff: stream(() => import('@codemirror/legacy-modes/mode/troff').then(m => m.troff)),
    ttcn: stream(() => import('@codemirror/legacy-modes/mode/ttcn').then(m => m.ttcn)),
    sieve: stream(() => import('@codemirror/legacy-modes/mode/sieve').then(m => m.sieve)),
    oz: stream(() => import('@codemirror/legacy-modes/mode/oz').then(m => m.oz)),
    pegjs: stream(() => import('@codemirror/legacy-modes/mode/pegjs').then(m => m.pegjs)),
    pig: stream(() => import('@codemirror/legacy-modes/mode/pig').then(m => m.pig)),
    ntriples: stream(() => import('@codemirror/legacy-modes/mode/ntriples').then(m => m.ntriples)),
    mumps: stream(() => import('@codemirror/legacy-modes/mode/mumps').then(m => m.mumps)),
    modelica: stream(() => import('@codemirror/legacy-modes/mode/modelica').then(m => m.modelica)),
    mirc: stream(() => import('@codemirror/legacy-modes/mode/mirc').then(m => m.mirc)),
    mathematica: stream(() => import('@codemirror/legacy-modes/mode/mathematica').then(m => m.mathematica)),
    textile: stream(() => import('@codemirror/legacy-modes/mode/textile').then(m => m.textile)),
    tiddlywiki: stream(() => import('@codemirror/legacy-modes/mode/tiddlywiki').then(m => m.tiddlyWiki)),
    ecl: stream(() => import('@codemirror/legacy-modes/mode/ecl').then(m => m.ecl)),
    eiffel: stream(() => import('@codemirror/legacy-modes/mode/eiffel').then(m => m.eiffel)),
    factor: stream(() => import('@codemirror/legacy-modes/mode/factor').then(m => m.factor)),
    dylan: stream(() => import('@codemirror/legacy-modes/mode/dylan').then(m => m.dylan)),
    asn1: stream(() => import('@codemirror/legacy-modes/mode/asn1').then(m => m.asn1)),
    brainfuck: stream(() => import('@codemirror/legacy-modes/mode/brainfuck').then(m => m.brainfuck)),
    apl: stream(() => import('@codemirror/legacy-modes/mode/apl').then(m => m.apl)),
    gas: stream(() => import('@codemirror/legacy-modes/mode/gas').then(m => m.gas)),
    forth: stream(() => import('@codemirror/legacy-modes/mode/forth').then(m => m.forth)),
    twig: stream(() => import('@codemirror/legacy-modes/mode/jinja2').then(m => m.jinja2)),
}

/**
 * Languages that register completion sources in their language data
 * (via `language.data.of({ autocomplete: source })`). When `autocompletion()`
 * is enabled for these languages, it reads those sources automatically.
 */
const COMPLETION_LANGS: Set<string> = new Set([
    'javascript', 'typescript', 'html', 'css', 'python', 'sql', 'go',
    'less', 'sass', 'scss', 'liquid', 'markdown',
])

/**
 * Markdown with nested syntax highlighting inside fenced code blocks.
 * Mirrors the browse mode where hljs highlights code fences inside markdown.
 * Only uses static imports for code-fence languages (available immediately).
 */
export function buildMarkdownExtension(): Extension {
    const codeLangs: Record<string, () => Extension> = {
        javascript: () => javascript(),
        typescript: () => javascript({ typescript: true }),
        js: () => javascript(),
        ts: () => javascript({ typescript: true }),
        json: () => json(),
        yaml: () => yaml(),
        yml: () => yaml(),
        xml: () => xml(),
        html: () => html(),
        css: () => css(),
        go: () => go(),
        golang: () => go(),
        python: () => python(),
        py: () => python(),
        markdown: () => markdown(),
        md: () => markdown(),
    }
    return markdown({
        codeLanguages: (info) => {
            const factory = codeLangs[info.toLowerCase()]
            const lang = factory ? factory() : null
            return lang && 'language' in lang ? (lang as { language: Language }).language : null
        },
    })
}

/**
 * Build the language extension for a given file language identifier.
 * Returns a Promise because most language packages are lazy-loaded.
 * Resolves to an empty array (plain text fallback) for unknown languages.
 */
export async function buildLangExtension(fileLang: string): Promise<Extension> {
    const factory = LANG_EXT[fileLang]
    if (!factory) return []
    try {
        return await factory()
    } catch {
        // A failed chunk fetch (offline, stale hash) degrades to plain text
        // rather than leaving the viewer stuck without a language.
        return []
    }
}

/**
 * Build a completion extension for a given language.
 * Returns an empty array for languages without a built-in completion source.
 *
 * Note: We do NOT use `override` here. Language packages register their completion
 * sources via `language.data.of({ autocomplete: source })`, and `autocompletion()`
 * reads those by default. Using `override` would replace the default source
 * collection and lose keyword completions (e.g. JS `const`/`continue`).
 */
export async function buildCompletionExtension(fileLang: string): Promise<Extension[]> {
    if (!COMPLETION_LANGS.has(fileLang)) return []
    const { autocompletion } = await import('@codemirror/autocomplete')
    return [autocompletion()]
}
