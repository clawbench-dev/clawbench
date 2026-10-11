<template>
  <div class="model-row" :class="{ 'model-row--active': active }">
    <ProviderIcon :model-name="providerName" :name="name" :size="iconSize" />
    <span class="model-row__labels">
      <span class="model-row__name">{{ name }}</span>
      <span v-if="showId" class="model-row__id">{{ displayId }}</span>
    </span>
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import { getModelProvider } from '@/utils/providerIcons'
import { formatModelIdForDisplay } from '@/utils/modelDisplay'

/**
 * Shared model row: provider icon + a two-line name/id label + a trailing slot.
 *
 * Both the session model picker (chat/SessionDrawer.vue) and the agent config's
 * preferred-model select (settings/SettingsItem.vue) render the same row, so the
 * markup and the two-line layout live here once. Callers own the surrounding
 * list chrome (search, refresh, selection state) and pass their trailing content
 * — default badge, set-default star, selection check — through the slot.
 */
const props = withDefaults(defineProps<{
  /** Display name of the model (the primary line). */
  name: string
  /** Raw wire id; shown as a subdued second line when it differs from `name`. */
  modelId?: string
  /** Bold the name — used for the currently-selected row. */
  active?: boolean
  /** Provider icon size in px. */
  iconSize?: number
}>(), {
  modelId: '',
  active: false,
  iconSize: 16,
})

/**
 * The string handed to ProviderIcon for provider detection.
 *
 * The display name is tried first, but gateway models carry their provider in
 * the *id*, not the name (e.g. name "OpenCode Zen/Big Pickle" with id
 * "opencode/big-pickle"), so fall back to the id whenever the name yields no
 * known provider. Detection order is irrelevant when neither matches.
 */
const providerName = computed(() => {
  if (props.name && getModelProvider(props.name)) return props.name
  if (props.modelId && getModelProvider(props.modelId)) return props.modelId
  return props.name || props.modelId
})

/**
 * Only render the id line when it adds information. When the id already is the
 * display name (backends that report no separate name), showing it twice is
 * noise. JSON-shaped ACP wire ids are formatted for display (see modelDisplay).
 */
const showId = computed(() => !!props.modelId && props.modelId !== props.name)
const displayId = computed(() => formatModelIdForDisplay(props.modelId))
</script>

<style scoped>
.model-row {
  /* Grow to fill the host row so the trailing slot reaches the right edge. */
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: var(--space-4);
}

.model-row__labels {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.model-row__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-md);
  color: var(--text-primary);
}

.model-row--active .model-row__name {
  font-weight: var(--font-weight-semibold);
}

/* The raw model id, on its own line below the name, so two models that share a
   display name (e.g. deepseek-v4-pro / deepseek-v4-pro-exclusive) stay
   distinguishable. Hidden when the name already IS the id. */
.model-row__id {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
  font-size: var(--font-size-2xs);
  color: var(--text-muted, #999);
  opacity: var(--opacity-soft);
}
</style>
