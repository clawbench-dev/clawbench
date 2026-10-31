<template>
    <!-- `tile` is for lists and cards: the logo sits on a uniform rounded
         square so a column of brand marks reads as one system. Inline uses
         (model chips, agent rows) keep the bare logo so it can sit in a line
         of text without adding a box. -->
    <span v-if="tile" class="provider-icon-tile" :style="tileStyle">
        <span v-if="fullSvg" class="provider-icon-wrap" v-html="fullSvg" />
        <span v-else class="provider-icon-initial">{{ initial }}</span>
    </span>
    <span v-else-if="fullSvg" class="provider-icon-wrap" v-html="fullSvg" />
    <span v-else class="provider-icon-initial" :style="initialStyle">{{ initial }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { getModelProvider, getProviderFullSvg, resolveProviderIconId } from '@/utils/providerIcons'

const props = withDefaults(defineProps<{
    /** Model name to detect provider from */
    modelName?: string
    /** Explicit provider id; wins over `modelName` when both are given. */
    providerId?: string
    /** Display name for aria-label and initial-letter fallback */
    name?: string
    /** Icon size in pixels (the tile size when `tile` is set) */
    size?: number
    /** Render the logo on a rounded tile instead of bare */
    tile?: boolean
}>(), {
    size: 16,
    tile: false,
})

const providerId = computed(() =>
    props.providerId ? resolveProviderIconId(props.providerId) : getModelProvider(props.modelName ?? ''),
)

/** The logo occupies ~62% of the tile: brand marks have wildly different
 *  internal padding, and a fixed inset is what makes them look equal-weight. */
const logoSize = computed(() => (props.tile ? Math.max(Math.round(props.size * 0.62), 8) : props.size))

/** Full SVG string rendered via v-html on a <span>.
 *  Avoids SVG-namespace issues with v-html on <svg> elements
 *  that cause blank rendering in some mobile WebViews. */
const fullSvg = computed(() => {
    if (!providerId.value) return null
    return getProviderFullSvg(
        providerId.value,
        logoSize.value,
        [],
        props.name || props.modelName || props.providerId,
        { background: !props.tile },
    )
})

const initial = computed(() => {
    const label = props.name || props.modelName || props.providerId || ''
    return label ? label.charAt(0).toUpperCase() : '?'
})

const tileStyle = computed(() => ({
    width: `${props.size}px`,
    height: `${props.size}px`,
    fontSize: `${Math.max(Math.round(props.size * 0.42), 9)}px`,
}))

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
    font-weight: var(--font-weight-semibold);
}

/* The tile: one radius, one inset ring, one neutral wash for every brand.
   Colour logos carry their own paint; monochrome ones inherit --text-primary
   from the row, so both read correctly across the 36 themes. */
.provider-icon-tile {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--text-secondary) 10%, transparent);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--text-secondary) 10%, transparent);
    line-height: 1;
    overflow: hidden;
}

.provider-icon-tile .provider-icon-initial {
    width: auto;
    height: auto;
    background: transparent;
    color: var(--text-secondary);
}
</style>
