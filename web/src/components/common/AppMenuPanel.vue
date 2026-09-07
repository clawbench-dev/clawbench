<template>
  <Teleport to="body">
    <Transition name="dropdown">
      <div
        v-if="open"
        class="app-menu"
        :style="panelStyle"
        :ref="(el) => props.registerPanel(el as HTMLElement | null)"
      >
        <slot />
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
/**
 * Shared teleported dropdown chrome for the header's quick-index dropdowns
 * (project switch / recent files / branch). Renders the `.app-menu` panel
 * skeleton (Transition + fixed-position inline style) and forwards the panel
 * DOM node to the caller via `registerPanel` so the caller can wire keyboard
 * navigation (useMenuKeyboard) and re-position the panel after its content
 * settles.
 *
 * The title / item rows / footer markup is passed via the default slot — it
 * differs per menu, so only the chrome is shared here.
 */

const props = defineProps<{
  /** Reactive open state of the dropdown. */
  open: boolean
  /** Fixed-position CSS for the teleported panel. */
  panelStyle: Record<string, string>
  /** Callback that receives the rendered panel element (null when closed). */
  registerPanel: (el: HTMLElement | null) => void
}>()
</script>
