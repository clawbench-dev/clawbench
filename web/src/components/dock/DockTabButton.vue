<template>
  <!-- Wide dock and the narrow bottom dock share one shape: a round icon button
       with a live dot and/or a count badge hanging off its corner. -->
  <div v-if="variant !== 'popup'" class="dock-btn-wrap">
    <button
      class="dock-btn"
      :data-tab="tab"
      :class="buttonClass"
      v-dock-tip="variant === 'wide' ? title : ''"
      :aria-label="title"
      :title="variant === 'wide' ? undefined : title"
      @click.stop="emit('click', $event as MouseEvent)"
    >
      <component :is="icon" />
    </button>
    <span v-if="showLiveDot" class="dock-badge dock-badge--live" />
    <span
      v-if="showCount"
      class="dock-badge dock-badge-count count-badge"
      :class="{ 'dock-badge-pop': popping }"
      @animationend="onBadgeAnimEnd"
    >{{ formattedCount }}</span>
  </div>

  <!-- The narrow dock's overflow popup renders a labelled row instead; its
       count sits INSIDE the row and is shown even while the tab is active. -->
  <button
    v-else
    class="dock-overflow-item"
    :data-tab="tab"
    :class="{ active }"
    @click.stop="emit('click', $event as MouseEvent)"
  >
    <component :is="icon" :size="16" />
    <span>{{ title }}</span>
    <span
      v-if="count > 0"
      class="dock-overflow-count count-badge count-badge--md"
      :class="{ 'dock-badge-pop': popping }"
      @animationend="onBadgeAnimEnd"
    >{{ formattedCount }}</span>
  </button>
</template>

<script setup lang="ts">
import { computed, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { Clock, FolderOpen } from 'lucide-vue-next'
import {
  dockTabIcon,
  dockTabTitleKey,
  resolveDockBadge,
  resolveDockIcon,
  resolveDockPlugin,
  resolveDockTitle,
} from '@/composables/dockPluginRuntime'
import { formatBadgeCount } from '@/utils/format'
import { useBadgePop } from '@/composables/useBadgePop'

/**
 * ONE dock tab button for every surface: the wide vertical dock, the narrow
 * bottom dock's inline row and the overflow popup. Icon, title, badge, live
 * dot, count-change animation and the state classes all come from the plugin's
 * own declaration — App.vue renders a button per tab and never names a plugin.
 *
 * What the button LOOKS like stays in `assets/dock.css` (global: scoped rules
 * cannot reach a child component's inner elements); this component decides only
 * WHICH class is present.
 */
const props = withDefaults(
  defineProps<{
    tab: string
    variant: 'wide' | 'inline' | 'popup'
    /** Whether the tab is the active one on this surface (drives the class). */
    active?: boolean
    /**
     * Whether the user is looking at this tab on this surface, even if the pane
     * is collapsed — a badge on the tab you are reading carries no information,
     * so the unread/running/dot states hide. Defaults to `active`. The wide
     * dock passes it separately: a collapsed pane still means "not looking at
     * the left column", so the badge stays hidden.
     */
    suppressBadge?: boolean
  }>(),
  { active: false, suppressBadge: undefined },
)

const emit = defineEmits<{ click: [event: MouseEvent] }>()

const { t } = useI18n()

const plugin = computed(() => resolveDockPlugin(props.tab))
const appearance = computed(() => plugin.value?.appearance)

/** Static manifest icon; `resolveDockIcon` only overrides state-dependent ones. */
function staticIcon(): Component | undefined {
  return dockTabIcon(props.tab)
}

const icon = computed<Component>(() => resolveDockIcon(props.tab) ?? staticIcon() ?? fallbackIcon())

/** The wide dock's fallback matches its own head icon; the bottom dock's is Clock. */
function fallbackIcon(): Component {
  return props.variant === 'wide' ? FolderOpen : Clock
}

const title = computed(() => {
  const runtimeTitle = resolveDockTitle(props.tab)
  if (runtimeTitle) return runtimeTitle
  const key = dockTabTitleKey(props.tab)
  return key ? t(key) : ''
})

// Reactive reads: the badge source touches the store, so this computed is what
// makes the badge (and its animation) track the plugin's data.
const badge = computed(() => resolveDockBadge(props.tab))
const count = computed(() => badge.value?.count ?? 0)
const formattedCount = computed(() => formatBadgeCount(count.value))

/** The user is reading this tab on this surface: badges and dots say nothing. */
const suppressed = computed(() => props.suppressBadge ?? props.active)

const showLiveDot = computed(
  () => props.variant !== 'popup' && !!appearance.value?.liveDot?.() && !suppressed.value,
)
const showCount = computed(() =>
  props.variant === 'popup' ? count.value > 0 : count.value > 0 && !suppressed.value,
)

const buttonClass = computed(() => ({
  active: props.active,
  'has-unread': !!appearance.value?.unread?.() && !suppressed.value,
  'just-completed': !!appearance.value?.justCompleted?.() && !suppressed.value,
  'has-running': !!appearance.value?.running?.() && !suppressed.value,
}))

const { popping, onBadgeAnimEnd } = useBadgePop(() => count.value)
</script>
