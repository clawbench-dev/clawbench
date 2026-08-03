/**
 * highlight.js selective language registration.
 *
 * Instead of importing all ~190 languages, we register only the ~50 most
 * commonly used ones. Unregistered languages fall back to plain text
 * (HTML-escaped, no highlighting) to avoid pulling in the full language set.
 */

import hljs from 'highlight.js/lib/core'

// --- Tier 1: Core developer languages ---
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python from 'highlight.js/lib/languages/python'
import pythonRepl from 'highlight.js/lib/languages/python-repl'
import go from 'highlight.js/lib/languages/go'
import rust from 'highlight.js/lib/languages/rust'
import java from 'highlight.js/lib/languages/java'
import c from 'highlight.js/lib/languages/c'
import cpp from 'highlight.js/lib/languages/cpp'
import csharp from 'highlight.js/lib/languages/csharp'
import ruby from 'highlight.js/lib/languages/ruby'
import php from 'highlight.js/lib/languages/php'
import swift from 'highlight.js/lib/languages/swift'
import kotlin from 'highlight.js/lib/languages/kotlin'
import scala from 'highlight.js/lib/languages/scala'
import objectivec from 'highlight.js/lib/languages/objectivec'

// --- Tier 2: Scripting & functional ---
import bash from 'highlight.js/lib/languages/bash'
import shell from 'highlight.js/lib/languages/shell'
import perl from 'highlight.js/lib/languages/perl'
import lua from 'highlight.js/lib/languages/lua'
import dart from 'highlight.js/lib/languages/dart'
import r from 'highlight.js/lib/languages/r'
import elixir from 'highlight.js/lib/languages/elixir'
import erlang from 'highlight.js/lib/languages/erlang'
import haskell from 'highlight.js/lib/languages/haskell'
import clojure from 'highlight.js/lib/languages/clojure'
import ocaml from 'highlight.js/lib/languages/ocaml'
import fsharp from 'highlight.js/lib/languages/fsharp'
import groovy from 'highlight.js/lib/languages/groovy'

// --- Tier 3: Web & markup ---
import xmlLang from 'highlight.js/lib/languages/xml'
import css from 'highlight.js/lib/languages/css'
import scss from 'highlight.js/lib/languages/scss'
import less from 'highlight.js/lib/languages/less'
import json from 'highlight.js/lib/languages/json'
import yaml from 'highlight.js/lib/languages/yaml'
import markdown from 'highlight.js/lib/languages/markdown'
import graphql from 'highlight.js/lib/languages/graphql'
import handlebars from 'highlight.js/lib/languages/handlebars'

// --- Tier 4: Data & config ---
import sql from 'highlight.js/lib/languages/sql'
import diff from 'highlight.js/lib/languages/diff'
import dockerfile from 'highlight.js/lib/languages/dockerfile'
import makefile from 'highlight.js/lib/languages/makefile'
import ini from 'highlight.js/lib/languages/ini'
import nginx from 'highlight.js/lib/languages/nginx'
import protobuf from 'highlight.js/lib/languages/protobuf'
import cmake from 'highlight.js/lib/languages/cmake'
import gradle from 'highlight.js/lib/languages/gradle'

// --- Tier 5: Specialized but common ---
import glsl from 'highlight.js/lib/languages/glsl'
import latex from 'highlight.js/lib/languages/latex'
import matlab from 'highlight.js/lib/languages/matlab'
import powershell from 'highlight.js/lib/languages/powershell'
import vim from 'highlight.js/lib/languages/vim'
import wasm from 'highlight.js/lib/languages/wasm'
import verilog from 'highlight.js/lib/languages/verilog'
import vhdl from 'highlight.js/lib/languages/vhdl'

// Register all languages
const languages: Record<string, typeof javascript> = {
    javascript,
    typescript,
    python,
    'python-repl': pythonRepl,
    go,
    rust,
    java,
    c,
    cpp,
    csharp,
    ruby,
    php,
    swift,
    kotlin,
    scala,
    objectivec,
    bash,
    shell,
    perl,
    lua,
    dart,
    r,
    elixir,
    erlang,
    haskell,
    clojure,
    ocaml,
    fsharp,
    groovy,
    html: xmlLang,
    css,
    scss,
    less,
    json,
    yaml,
    xml: xmlLang,
    vue: xmlLang,
    markdown,
    graphql,
    handlebars,
    sql,
    diff,
    dockerfile,
    makefile,
    ini,
    nginx,
    protobuf,
    cmake,
    gradle,
    glsl,
    latex,
    matlab,
    powershell,
    vim,
    wasm,
    verilog,
    vhdl,
}

for (const [name, lang] of Object.entries(languages)) {
    hljs.registerLanguage(name, lang)
}

/**
 * Highlight code with a specific language.
 * Falls back to plain text (no highlighting) for unregistered languages.
 */
export function highlightCode(code: string, language: string, ignoreIllegals = true): string {
    if (language && hljs.getLanguage(language)) {
        return hljs.highlight(code, { language, ignoreIllegals }).value
    }
    // Fallback: no highlighting for unregistered languages
    return escapeHtml(code)
}

function escapeHtml(text: string): string {
    return text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
}

export { hljs }
