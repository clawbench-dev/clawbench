<template>
  <div class="model-row" :class="{ 'model-row--active': active }">
    <ProviderIcon :model-name="providerName" :name="name" :size="iconSize" />
    <span class="model-row__labels">
      <span class="model-row__name">{{ name }}</span>
      <span v-if="showId" class="model-row__id">{{ displayId }}</span>
    </span>
    <!-- Cost tag: the agent's own billing multiplier when it publishes one
         (issue #657), otherwise the catalog's USD price (issue #643). The two
         measure different things, so only one is shown — a narrow row cannot
         carry both, and the multiplier is the number the user can compare
         against the agent's own client. Sits after the labels so it never
         pushes the name off its ellipsis budget. -->
    <span v-if="creditsLabel" class="model-row__credits" :title="creditsTitle">{{ creditsLabel }}</span>
    <span v-else-if="priceLabel" class="model-row__price" :title="priceTitle">{{ priceLabel }}</span>
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import { getModelProvider } from '@/utils/providerIcons'
import { formatModelIdForDisplay } from '@/utils/modelDisplay'
import { formatCreditsLabel } from '@/utils/modelCredits'
import { useI18n } from 'vue-i18n'
import type { ModelPrice } from '@/utils/modelPrice'

/**
 * Shared model row: provider icon + a two-line name/id label + an optional
 * price tag + a trailing slot.
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
  /** Per-million-token list price, when the catalog knows one. */
  price?: ModelPrice | null
  /** The agent's own billing multiplier, verbatim ("x0.06 credits"). */
  credits?: string | null
}>(), {
  modelId: '',
  active: false,
  iconSize: 16,
  price: null,
  credits: '',
})

const { t } = useI18n()

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

/**
 * Compact price tag: "$3 / $15" for a price published by the model's own
 * vendor, "~$3 / $15" when it is a consensus across resellers (see
 * model.LookupModelPrice). Rendered only when a trustworthy price exists —
 * an agent whose ids the catalog does not know shows nothing rather than a
 * guess.
 */
const priceLabel = computed(() => {
  const p = props.price
  if (!p) return ''
  // Two decimals for dollar-and-up, three below that, and significant digits
  // for the sub-cent prices some models carry — a plain round would print
  // "$0", which reads as free rather than cheap.
  const fmt = (v: number) => {
    if (v >= 1) return String(Math.round(v * 100) / 100)
    if (v >= 0.01) return String(Math.round(v * 1000) / 1000)
    return String(Number(v.toPrecision(2)))
  }
  return `${p.approx ? '~' : ''}$${fmt(p.input)} / $${fmt(p.output)}`
})

const priceTitle = computed(() =>
  t(props.price?.approx ? 'chat.sessionSetting.priceApproxHint' : 'chat.sessionSetting.priceHint'),
)

/**
 * Compact multiplier tag, e.g. "x0.06" — the same form the agent's own client
 * shows, so the two can be compared at a glance (issue #657). Empty when the
 * agent publishes no multiplier, which is the case for every backend except
 * CodeBuddy today.
 */
const creditsLabel = computed(() => formatCreditsLabel(props.credits))
const creditsTitle = computed(() => t('chat.sessionSetting.creditsHint'))
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

/* Price tag. Never shrinks and never wraps: the name above it already
   ellipsizes, so this is the one element that must stay legible. */
.model-row__price,
.model-row__credits {
  flex: none;
  font-size: var(--font-size-2xs);
  font-variant-numeric: tabular-nums;
  color: var(--text-secondary);
  opacity: var(--opacity-muted, 0.75);
  white-space: nowrap;
}
</style>
