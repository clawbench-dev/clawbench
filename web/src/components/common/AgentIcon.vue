<template>
    <img
        v-if="avatarSrc"
        class="agent-icon-img agent-icon-avatar"
        :class="sizeClass"
        :src="avatarSrc"
        :style="avatarBg ? { backgroundColor: avatarBg } : undefined"
        :alt="name || backend"
    />
    <img v-else-if="imageUrl" class="agent-icon-img" :class="sizeClass" :src="imageUrl" :alt="name || backend" />
    <svg v-else-if="processedSvg" class="agent-icon-svg" :class="[sizeClass, svgData!.needsBg ? 'agent-icon-bg' : '', svgData!.monoCssClass]" :viewBox="svgData!.viewBox" role="img" :aria-label="name || backend" v-html="processedSvg" />
    <span v-else class="agent-icon-initial" :class="sizeClass">{{ initial }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { getAgentSvg } from '@/utils/agentIcons'
import { avatarToSrc } from '@/utils/avatarSrc'
import { avatarBackgroundFor } from '@/utils/avatarPolicy'

// Per-instance unique suffix to avoid SVG gradient ID collisions when
// multiple AgentIcon instances render on the same page. Without this,
// gradient URLs in one <svg> can resolve to a <defs> in a different <svg>,
// causing wrong colors/shapes.
const uid = `_${Math.random().toString(36).slice(2, 8)}`

/** Size steps. Each maps to a token via a CSS class, so callers never pass raw
 *  pixels. Chosen to match the text each icon sits beside: sm=14 (dense meta),
 *  md=18 (default rows), lg=24 (larger list/avatar contexts), xl=40
 *  (settings-detail preview), xxl=56 (`--empty-icon-size`, the one size every
 *  empty state draws its glyph at — an avatar standing in for that glyph has to
 *  match it, and no step of the text-adjacent ladder comes close). */
export type AgentIconSize = 'sm' | 'md' | 'lg' | 'xl' | 'xxl'

const props = withDefaults(defineProps<{
    backend: string
    name?: string
    size?: AgentIconSize
    /** Raw SVG string or a `data:image/...` URI for a custom avatar ("" = built-in icon). */
    avatar?: string
}>(), {
    size: 'md',
})

const sizeClass = computed(() => `agent-icon--${props.size}`)

const svgData = computed(() => getAgentSvg(props.backend))

// A backend whose mark only exists as a raster image (the built-in agent uses
// the ClawBench product logo, which has no SVG source in the repo).
const imageUrl = computed(() => svgData.value?.imageUrl ?? '')

// A custom avatar is rendered as an <img> with a data URI, NOT via v-html:
// the <img> context is sandboxed (no script execution, no external loads) and
// sidesteps the scoped-CSS red line. `avatarToSrc` passes an uploaded raster
// data URI through untouched and encodes only raw SVG.
const avatarSrc = computed(() => avatarToSrc(props.avatar || ''))

// A generated avatar already carries its own background. This CSS fallback is
// for avatars stored BEFORE backgrounds were baked in: without it a transparent
// character style floats over the chat bubble and looks thin. The colour is
// derived from the same name so it stays stable per agent/member.
const avatarBg = computed(() => (props.avatar ? avatarBackgroundFor(props.name || 'agent') : ''))

// Replace all ID references (id="...", url(#...", href="#...") in SVG content
// that match known gradient ID patterns (lobe-icons-* or ai-* prefixes)
// with unique-per-instance versions to prevent cross-SVG gradient collisions.
const processedSvg = computed(() => {
    const data = svgData.value
    if (!data) return null
    // A raster-backed entry has no inline SVG at all (see AgentSvgData.imageUrl).
    // Returning null keeps this branch from rendering an empty <svg>, which
    // would occupy the box and hide the image branch behind it.
    if (!data.svg) return null
    // Two-step replacement to handle different ID boundary characters:
    // 1. id="..." and href="#..." — ID ends at quote ("')
    // 2. url(#...) — ID ends at closing paren )
    // Must be separate because [^"]+ would greedily include ')' inside url(#...),
    // causing mismatch between defs id= and url(# references.
    let s = data.svg
    s = s.replace(/(id="|href="#)(lobe-icons-[^"]+|ai-[^"]+)(["'])/g, `$1$2${uid}$3`)
    s = s.replace(/(url\(#)(lobe-icons-[^)]+|ai-[^)]+)(\))/g, `$1$2${uid}$3`)
    return s
})

const initial = computed(() => {
    if (props.name) return props.name.charAt(0).toUpperCase()
    return props.backend ? props.backend.charAt(0).toUpperCase() : '?'
})
</script>

<style scoped>
/* Size classes set --ai-size from a token and drive the box. Written as
   element-agnostic rules so the same class sizes whichever of the three render
   paths (svg / img / initial span) is active. A single source (--ai-size) also
   lets the initial glyph scale from the box without repeating the calc per
   class. */
.agent-icon--sm { --ai-size: var(--icon-size-sm); }
.agent-icon--md { --ai-size: var(--icon-size-md); }
.agent-icon--lg { --ai-size: var(--icon-size-lg); }
.agent-icon--xl { --ai-size: var(--icon-size-xl); }
/* Deliberately the empty-state token, not a rung of the --icon-size ladder:
   the welcome card's avatar IS that empty state's glyph and must not drift from
   the rest of the app's empty states. */
.agent-icon--xxl { --ai-size: var(--empty-icon-size); }

.agent-icon-svg,
.agent-icon-img,
.agent-icon-initial {
    width: var(--ai-size);
    height: var(--ai-size);
}

.agent-icon-svg {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    line-height: 1;
}

/* User-configured custom avatar (DiceBear SVG or an uploaded raster, rendered
   as a data-URI <img>). The inline background fills the transparent parts of a
   line-art avatar; a baked-in background simply covers it. */
.agent-icon-img {
    display: inline-block;
    flex-shrink: 0;
    border-radius: 20%;
    object-fit: contain;
}

.agent-icon-avatar {
    background-clip: padding-box;
}

/* Contrasting background for monochrome icons that would be
   invisible on same-colored backgrounds. Uses --bg-tertiary which
   automatically adapts: light=#e9ecef, dark=#21262d */
.agent-icon-bg {
    border-radius: 20%;
    background: var(--bg-tertiary);
}

.agent-icon-initial {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    line-height: 1;
    border-radius: 20%;
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    font-weight: var(--font-weight-semibold);
    /* Glyph scales with the box; 0.55 matches the previous ratio. */
    font-size: calc(var(--ai-size) * 0.55);
}
</style>
