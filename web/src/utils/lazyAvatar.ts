/**
 * Lazy-loaded DiceBear avatar generator.
 *
 * Both `@dicebear/core` (~61 KB gzip) and each per-style JSON definition are
 * loaded via dynamic import() so they land in separate chunks and never touch
 * the entry bundle. They are only fetched when the avatar picker is opened.
 *
 * Style JSON subpaths resolve through the `@dicebear/styles` exports map, e.g.
 * `@dicebear/styles/bottts.json` -> `dist/bottts.min.json`. The ambient module
 * declaration in `types/dicebear-styles.d.ts` keeps these imports typed.
 */
import { retryableImport } from '@/utils/lazyMermaid'
import type { StyleDefinition } from '@dicebear/core'

type CoreModule = typeof import('@dicebear/core')

let _core: CoreModule | null = null
let _corePending: Promise<CoreModule> | null = null

/** Load (and cache) the DiceBear core module. Retries transient fetch failures. */
export async function getAvatarLib(): Promise<CoreModule> {
    if (_core) return _core
    if (_corePending) return _corePending
    _corePending = retryableImport(() => import('@dicebear/core')).then(mod => {
        _core = mod
        _corePending = null
        return _core
    }).catch(err => {
        _corePending = null
        throw err
    })
    return _corePending
}

/**
 * User-curated style set (40 styles). Each entry becomes its own lazy chunk and
 * is only fetched when selected in the picker, so the entry bundle is
 * unaffected. Extending this list only requires adding a matching loader below.
 */
export const AVATAR_STYLES = [
    'avataaars',
    'avataaars-neutral',
    'big-ears',
    'big-smile',
    'blobs',
    'bottts',
    'bottts-neutral',
    'cameo',
    'clay',
    'critters',
    'disco',
    'dylan',
    'glyphs',
    'icons',
    'identicon',
    'landscape',
    'line-face',
    'loops',
    'micah',
    'miniavs',
    'moods',
    'patchwork',
    'personas',
    'pixel-art',
    'pixel-art-neutral',
    'planets',
    'rings',
    'shape-grid',
    'shapes',
    'slice',
    'sprouts',
    'squircles',
    'stack',
    'stripes',
    'thumbs',
    'toon-head',
    'voxel-art',
    'voxel-bot',
    'waves',
    'weave',
] as const

export type AvatarStyle = typeof AVATAR_STYLES[number]

// Each entry becomes its own lazy chunk. Kept as an explicit map (not a
// template-literal import) so Vite can statically discover every subpath.
// The JSON module's inferred type is a narrow literal that does not exactly
// match StyleDefinition, so the loader is typed loosely and the definition is
// cast at the single use site.
const STYLE_LOADERS: Record<AvatarStyle, () => Promise<{ default: unknown }>> = {
    avataaars: () => import('@dicebear/styles/avataaars.json'),
    'avataaars-neutral': () => import('@dicebear/styles/avataaars-neutral.json'),
    'big-ears': () => import('@dicebear/styles/big-ears.json'),
    'big-smile': () => import('@dicebear/styles/big-smile.json'),
    blobs: () => import('@dicebear/styles/blobs.json'),
    bottts: () => import('@dicebear/styles/bottts.json'),
    'bottts-neutral': () => import('@dicebear/styles/bottts-neutral.json'),
    cameo: () => import('@dicebear/styles/cameo.json'),
    clay: () => import('@dicebear/styles/clay.json'),
    critters: () => import('@dicebear/styles/critters.json'),
    disco: () => import('@dicebear/styles/disco.json'),
    dylan: () => import('@dicebear/styles/dylan.json'),
    glyphs: () => import('@dicebear/styles/glyphs.json'),
    icons: () => import('@dicebear/styles/icons.json'),
    identicon: () => import('@dicebear/styles/identicon.json'),
    landscape: () => import('@dicebear/styles/landscape.json'),
    'line-face': () => import('@dicebear/styles/line-face.json'),
    loops: () => import('@dicebear/styles/loops.json'),
    micah: () => import('@dicebear/styles/micah.json'),
    miniavs: () => import('@dicebear/styles/miniavs.json'),
    moods: () => import('@dicebear/styles/moods.json'),
    patchwork: () => import('@dicebear/styles/patchwork.json'),
    personas: () => import('@dicebear/styles/personas.json'),
    'pixel-art': () => import('@dicebear/styles/pixel-art.json'),
    'pixel-art-neutral': () => import('@dicebear/styles/pixel-art-neutral.json'),
    planets: () => import('@dicebear/styles/planets.json'),
    rings: () => import('@dicebear/styles/rings.json'),
    'shape-grid': () => import('@dicebear/styles/shape-grid.json'),
    shapes: () => import('@dicebear/styles/shapes.json'),
    slice: () => import('@dicebear/styles/slice.json'),
    sprouts: () => import('@dicebear/styles/sprouts.json'),
    squircles: () => import('@dicebear/styles/squircles.json'),
    stack: () => import('@dicebear/styles/stack.json'),
    stripes: () => import('@dicebear/styles/stripes.json'),
    thumbs: () => import('@dicebear/styles/thumbs.json'),
    'toon-head': () => import('@dicebear/styles/toon-head.json'),
    'voxel-art': () => import('@dicebear/styles/voxel-art.json'),
    'voxel-bot': () => import('@dicebear/styles/voxel-bot.json'),
    waves: () => import('@dicebear/styles/waves.json'),
    weave: () => import('@dicebear/styles/weave.json'),
}

/**
 * A ready-to-render kit: core + every style definition loaded, with `Style`
 * instances pre-built (constructing a `Style` validates the schema, so we do it
 * once per style rather than on every render). Holding this lets the picker
 * render the whole style grid SYNCHRONOUSLY — no per-cell await — which is what
 * makes "change the seed → every preview updates instantly" possible.
 */
export interface AvatarKit {
    Avatar: typeof import('@dicebear/core')['Avatar']
    styles: Record<AvatarStyle, InstanceType<typeof import('@dicebear/core')['Style']>>
}

/** How many style chunks to fetch concurrently. 40 parallel imports would
 *  flood the connection; a small pool keeps the picker responsive. */
const LOAD_CONCURRENCY = 6

/** Load the DiceBear core plus all curated style definitions, then return a kit. */
export async function loadAvatarKit(): Promise<AvatarKit> {
    const core = await getAvatarLib()
    const { Style } = core
    const styles = {} as AvatarKit['styles']

    const queue = [...AVATAR_STYLES]
    async function worker() {
        for (;;) {
            const name = queue.shift()
            if (!name) return
            const mod = await retryableImport(STYLE_LOADERS[name])
            styles[name] = new Style(mod.default as StyleDefinition)
        }
    }
    await Promise.all(Array.from({ length: Math.min(LOAD_CONCURRENCY, AVATAR_STYLES.length) }, worker))

    return { Avatar: core.Avatar, styles }
}
