<template>
    <svg v-if="processedSvg" class="agent-icon-svg" :class="{ 'agent-icon-bg': svgData!.needsBg }" :style="style" :viewBox="svgData!.viewBox" role="img" :aria-label="name || backend" v-html="processedSvg" />
    <span v-else class="agent-icon-initial" :style="initialStyle">{{ initial }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { getAgentSvg } from '@/utils/agentIcons'

// Per-instance unique suffix to avoid SVG gradient ID collisions when
// multiple AgentIcon instances render on the same page. Without this,
// url(#ai-cb-g) in one <svg> can resolve to a <defs> in a different <svg>,
// causing wrong colors/shapes (especially visible for CodeBuddy, Copilot, Codex).
const uid = `_${Math.random().toString(36).slice(2, 8)}`

const props = withDefaults(defineProps<{
    backend: string
    name?: string
    size?: number
}>(), {
    size: 16,
})

const svgData = computed(() => getAgentSvg(props.backend))

// Replace all `ai-` prefixed IDs in defs and references (id="ai-...", url(#ai-...", href="#ai-...")
// with unique-per-instance versions to prevent cross-SVG gradient collisions.
const processedSvg = computed(() => {
    const data = svgData.value
    if (!data) return null
    return data.svg.replace(/(id="ai-|url\(#ai-|href="#ai-)([^")]+)/g, `$1$2${uid}`)
})

const style = computed(() => ({
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

/* Contrasting background for icons with dark/light fills that would be
   invisible on same-colored backgrounds (opencode, codex, mimo, pi) */
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
    font-weight: 600;
}
</style>
