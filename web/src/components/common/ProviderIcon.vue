<template>
    <span v-if="fullSvg" class="provider-icon-wrap" v-html="fullSvg" />
    <span v-else class="provider-icon-initial" :style="initialStyle">{{ initial }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { getModelProvider, getProviderFullSvg } from '@/utils/providerIcons'

const props = withDefaults(defineProps<{
    /** Model name to detect provider from */
    modelName: string
    /** Display name for aria-label and initial-letter fallback */
    name?: string
    /** Icon size in pixels */
    size?: number
}>(), {
    size: 16,
})

const providerId = computed(() => getModelProvider(props.modelName))

/** Full SVG string rendered via v-html on a <span>.
 *  Avoids SVG-namespace issues with v-html on <svg> elements
 *  that cause blank rendering in some mobile WebViews. */
const fullSvg = computed(() => {
    if (!providerId.value) return null
    return getProviderFullSvg(providerId.value, props.size, [], props.name || props.modelName)
})

const initial = computed(() => {
    if (props.name) return props.name.charAt(0).toUpperCase()
    return props.modelName ? props.modelName.charAt(0).toUpperCase() : '?'
})

const initialStyle = computed(() => ({
    width: `${props.size}px`,
    height: `${props.size}px`,
    fontSize: `${Math.max(props.size * 0.55, 8)}px`,
}))
</script>

<style scoped>
.provider-icon-wrap {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    line-height: 1;
}

/* v-html rendered SVG inherits display from .provider-icon-wrap span.
   These :deep selectors style the inner SVG element. */
.provider-icon-wrap :deep(.provider-icon-svg) {
    display: block;
    flex-shrink: 0;
}

.provider-icon-wrap :deep(.provider-icon-bg) {
    border-radius: 20%;
    background: var(--bg-tertiary);
}

.provider-icon-initial {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    line-height: 1;
    border-radius: 20%;
    background: color-mix(in srgb, var(--text-secondary) 18%, transparent);
    color: var(--text-primary);
    font-weight: 600;
}
</style>
