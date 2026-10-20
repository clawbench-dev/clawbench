/**
 * highlight.js core: the language set loaded on the FIRST screen.
 *
 * `highlight.ts` re-exports everything here and adds the lazy tier on top; the
 * split exists so that the eager bundle only carries the languages a chat
 * message or a file preview realistically needs, while the long tail
 * (highlight.js ships 384 languages, ~870KB minified) is fetched on demand.
 *
 * Adding a language here means paying for it on every page load. Put anything
 * that is not everyday work (protobuf, nix, julia, …) in `highlightExtra.ts`
 * instead.
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
import cmake from 'highlight.js/lib/languages/cmake'
import gradle from 'highlight.js/lib/languages/gradle'

// `plaintext` has no grammar (313 bytes, produces no spans) but is the most
// common file type there is — .txt/.log/.csv/config all resolve to it. Keeping
// it eager avoids a pointless chunk request for every plain file opened.
import plaintext from 'highlight.js/lib/languages/plaintext'

// --- Tier 5: Specialized but common ---
import glsl from 'highlight.js/lib/languages/glsl'
import latex from 'highlight.js/lib/languages/latex'
import matlab from 'highlight.js/lib/languages/matlab'
import powershell from 'highlight.js/lib/languages/powershell'
import vim from 'highlight.js/lib/languages/vim'
import wasm from 'highlight.js/lib/languages/wasm'
import verilog from 'highlight.js/lib/languages/verilog'
import vhdl from 'highlight.js/lib/languages/vhdl'

/** Languages registered at import time — see `EAGER_LANGUAGES`. */
const eagerLanguages: Record<string, typeof javascript> = {
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
    plaintext,
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

for (const [name, lang] of Object.entries(eagerLanguages)) {
    hljs.registerLanguage(name, lang)
}

// Aliases whose target is already eager. highlight.js derives aliases from the
// grammar itself, but these three names are what our file-type table emits and
// none of them is a declared alias upstream:
//   sass   → scss (indented syntax shares the tokenizer closely enough)
//   wast   → wasm (the text format IS what a .wat/.wast file contains)
//   proto  → protobuf is a real upstream alias, but protobuf itself is lazy,
//            so it is registered in highlightExtra.ts together with it.
hljs.registerAliases(['sass'], { languageName: 'scss' })
hljs.registerAliases(['wast'], { languageName: 'wasm' })

/** Names available without any lazy loading. */
export const EAGER_LANGUAGES: readonly string[] = Object.keys(eagerLanguages)

export { hljs }
