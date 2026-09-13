<template>
    <svg v-if="processedSvg" class="agent-icon-svg" :class="[svgData!.needsBg ? 'agent-icon-bg' : '', svgData!.monoCssClass]" :style="svgStyle" :viewBox="svgData!.viewBox" role="img" :aria-label="name || backend" v-html="processedSvg" />
    <span v-else class="agent-icon-initial" :style="initialStyle">{{ initial }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { getAgentSvg } from '@/utils/agentIcons'

// Per-instance unique suffix to avoid SVG gradient ID collisions when
// multiple AgentIcon instances render on the same page. Without this,
// gradient URLs in one <svg> can resolve to a <defs> in a different <svg>,
// causing wrong colors/shapes.
const uid = `_${Math.random().toString(36).slice(2, 8)}`

const props = withDefaults(defineProps<{
    backend: string
    name?: string
    size?: number
}>(), {
    size: 16,
})

const svgData = computed(() => getAgentSvg(props.backend))

// Replace all ID references (id="...", url(#...", href="#...") in SVG content
// that match known gradient ID patterns (lobe-icons-* or ai-* prefixes)
// with unique-per-instance versions to prevent cross-SVG gradient collisions.
const processedSvg = computed(() => {
    const data = svgData.value
    if (!data) return null
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

const svgStyle = computed(() => ({
    width: `${props.size}px`,
    height: `${props.size}px`,
}))

const initial = computed(() => {
    if (props.name) return props.name.charAt(0).toUpperCase()
    return props.backend ? props.backend.charAt(0).toUpperCase() : '?'
})

const initialStyle = computed(() => ({
    width: `${props.size}px`,
    height: `${props.size}px`,
    fontSize: `${Math.max(props.size * 0.55, 8)}px`,
}))
</script>

<style scoped>
.agent-icon-svg {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    line-height: 1;
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
}
</style>
