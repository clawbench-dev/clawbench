<template>
  <component
    :is="panelComponent"
    v-if="panelComponent"
    :active="active"
    v-bind="extraProps"
    v-on="events"
  />
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent'
import { resolveDockPlugin } from '@/composables/dockPluginRuntime'
import type { DockPanelHost, DockPanelHostExtras } from '@/composables/dockPluginTypes'
import type { DockTabId } from '@/composables/dockTabs'

/**
 * Generic host for a plugin's left-dock panel.
 *
 * The panel component, its extra props and its event listeners all come from
 * the plugin's `dock.ts` (`panel`). App.vue no longer imports optional panels
 * or writes a `<TabPanel>` block per plugin — it renders one host per tab.
 *
 * `active` is passed to every panel because every panel needs to know whether
 * it is the visible one. Everything else is plugin-specific and goes through
 * `panel.props` / `panel.on`, which receive the generic host plus the
 * per-plugin extras (a panel takes only its own group).
 */
const props = defineProps<{
  tabId: DockTabId
  active: boolean
  host: DockPanelHost
  /** Plugin-specific capabilities; a panel takes only its own group. */
  extras: DockPanelHostExtras
}>()

const panel = computed(() => resolveDockPlugin(props.tabId)?.panel)

const panelComponent = computed(() => {
  const descriptor = panel.value
  if (!descriptor) return null
  if (descriptor.component) return descriptor.component
  if (descriptor.load) {
    return defineAsyncComponent(buildAsyncComponentOptions({ loader: descriptor.load }))
  }
  return null
})

const extraProps = computed(() => panel.value?.props?.(props.host, props.extras) ?? {})
const events = computed(() => panel.value?.on?.(props.host, props.extras) ?? {})
</script>
