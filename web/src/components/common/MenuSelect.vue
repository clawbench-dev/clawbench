<template>
  <!-- Trigger. Deliberately a <button> rather than a native <select>: the
       native control renders the OS picker, which looks nothing like the rest
       of the app on mobile. This reuses PopupMenu so positioning, viewport
       clamping and outside-click handling match every other in-app dropdown. -->
  <button
    ref="anchorRef"
    type="button"
    class="menu-select"
    :class="{ 'is-open': show, 'is-disabled': disabled, 'is-block': block }"
    :disabled="disabled"
    @click.stop="toggle"
  >
    <span class="menu-select-label">{{ selectedOption ? selectedOption.label : placeholder }}</span>
    <ChevronDown class="menu-select-caret" :size="14" />
  </button>

  <PopupMenu
    :show="show"
    :target-element="anchorRef"
    :max-width="menuWidth"
    :max-height="maxHeight"
    :menu-items-count="options.length"
    :app-surface="appSurface"
    @update:show="onShowUpdate"
  >
    <!-- min-width matches the trigger so the open menu lines up with the
         control it belongs to instead of shrinking to its narrowest item. -->
    <div class="menu-select-list" :class="{ 'app-menu-column': appSurface }" :style="listStyle">
      <div v-if="menuTitle" class="app-menu-title">{{ menuTitle }}</div>
      <div :class="appSurface ? 'app-menu-scroll' : ''">
        <slot name="options">
          <button
            v-for="opt in options"
            :key="String(opt.value)"
            type="button"
            class="menu-select-item"
            :class="{ active: opt.value === modelValue }"
            :title="opt.label"
            @click="choose(opt.value)"
          >
            <span class="menu-select-item-label">{{ opt.label }}</span>
            <Check v-if="opt.value === modelValue" class="menu-select-item-check" :size="14" />
          </button>
        </slot>
      </div>
    </div>
  </PopupMenu>
</template>

<script setup lang="ts">
import { computed, ref, onBeforeUnmount } from 'vue'
import { Check, ChevronDown } from 'lucide-vue-next'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { claimOpenMenu, releaseOpenMenu } from '@/utils/exclusiveMenu'

export interface MenuSelectOption {
  value: string | number
  label: string
}

const props = withDefaults(defineProps<{
  modelValue: string | number
  options: MenuSelectOption[]
  /** Shown when no option matches modelValue. */
  placeholder?: string
  disabled?: boolean
  /** Stretch the trigger to the container width (form fields); otherwise it
   *  sizes to its content (inline pickers such as a time selector). */
  block?: boolean
  maxHeight?: number
  menuWidth?: number
  /** Style the popup like the app-header dropdown (primary surface, pinned
   *  header + scrollable middle). Use with an `.app-menu-column` body. */
  appSurface?: boolean
  /** Optional heading rendered as an `.app-menu-title` above the list. */
  menuTitle?: string
}>(), {
  placeholder: '',
  disabled: false,
  block: false,
  maxHeight: 320,
  menuWidth: 260,
  appSurface: false,
  menuTitle: '',
})

const emit = defineEmits<{
  'update:modelValue': [value: string | number]
  change: [value: string | number]
}>()

const anchorRef = ref<HTMLElement | null>(null)
const show = ref(false)
const menuMinWidth = ref(0)

const selectedOption = computed(
  () => props.options.find(o => o.value === props.modelValue) ?? null,
)

/**
 * Inline style for the popup body.
 *
 * The min-width only makes sense for the plain list (where the menu should line
 * up with its trigger). On the app-header surface the panel is a card with its
 * own padding; it still needs a floor, otherwise it shrinks to the width of the
 * widest row's *content* — and a stacked project row's path is clipped to a few
 * characters ("tmp/nr…") instead of being readable. `menuWidth` is the popup's
 * MAX width, so it doubles as a sensible target here.
 */
const listStyle = computed(() =>
  props.appSurface ? { minWidth: `${props.menuWidth}px` } : { minWidth: `${menuMinWidth.value}px` },
)

/**
 * Exclusive-open coordination.
 *
 * A MenuSelect popup is teleported to <body>, so it is NOT a DOM sibling of any
 * other trigger — nothing about the markup stops a second one from opening on
 * top of the first. Without this, clicking trigger A then trigger B left BOTH
 * menus on screen (reproduced: two .popup-menu elements, overlapping).
 *
 * The slot lives in `@/utils/exclusiveMenu` (a real module) rather than here:
 * top-level bindings in `<script setup>` are compiled INTO setup(), so a `let`
 * in this file would be per-instance and coordinate nothing.
 */
const token = { close }

function close() {
  show.value = false
  releaseOpenMenu(token)
}

function toggle() {
  if (props.disabled) return
  if (show.value) {
    close()
    return
  }
  // Close whoever is open first, so at most one menu is ever on screen.
  claimOpenMenu(token)
  // Measure before the popup positions itself (it reads geometry on the next
  // animation frame), so the menu can never come out narrower than the
  // trigger it drops out of.
  menuMinWidth.value = anchorRef.value?.offsetWidth ?? 0
  show.value = true
}

function onShowUpdate(value: boolean) {
  if (value) show.value = true
  else close()
}

// If this trigger is unmounted while its menu is open (tab switch, v-if), drop
// the slot so a stale entry cannot block the next open.
onBeforeUnmount(() => {
  releaseOpenMenu(token)
})

function choose(value: string | number) {
  close()
  if (value === props.modelValue) return
  emit('update:modelValue', value)
  emit('change', value)
}
</script>

<style scoped>
.menu-select {
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  gap: calc(6 * var(--dp));
  padding: calc(8 * var(--dp)) calc(10 * var(--dp));
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: 0;
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-md);
  font-family: inherit;
  cursor: pointer;
  box-sizing: border-box;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.menu-select.is-block {
  display: flex;
  width: 100%;
}

.menu-select.is-disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

.menu-select:focus-visible,
.menu-select.is-open {
  outline: none;
  border-color: var(--accent-color, #0066cc);
  box-shadow: 0 0 0 calc(3 * var(--dp)) rgba(0, 102, 204, 0.1);
}

@media (hover: hover) {
  .menu-select:not(.is-disabled):hover {
    border-color: var(--accent-color, #0066cc);
  }
}

.menu-select-label {
  /* Two characters covers every time option (00–59, 1–31 padded visually), so
     the trigger does not resize as the selection moves between 1 and 2 digits. */
  min-width: 2ch;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: left;
}

.menu-select-caret {
  flex-shrink: 0;
  color: var(--text-muted, #9ca3af);
  transition: transform 0.2s ease;
}

.menu-select.is-open .menu-select-caret {
  transform: rotate(180deg);
}

.menu-select-list {
  display: flex;
  flex-direction: column;
}

/* Custom rows supplied through the `options` slot (the project picker reuses the
   app-header's global .app-menu-item look). Those global rules were written for
   <div> rows; slot rows here are <button>s, and a button is shrink-to-fit — so
   without this each row collapses to its own content inside a wider panel and
   the rows do not share a left edge. Scoped here (not in the consumer) because
   the popup is teleported to <body>: the consumer's `.notif-filters :deep(...)`
   would never match, as .notif-filters is not an ancestor of the menu. */
.menu-select-list :deep(.app-menu-item) {
  width: 100%;
  box-sizing: border-box;
  border: none;
  font-family: inherit;
  text-align: left;
}

/* A <button> carries a UA background that a <div> does not. Clear it on the
   INACTIVE rows only: a blanket `background: transparent` here would out-rank
   the global `.app-menu-item.active` accent fill (this selector is more
   specific), silently killing the "you are here" highlight. */
.menu-select-list :deep(.app-menu-item:not(.active)) {
  background: transparent;
}

.menu-select-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: calc(8 * var(--dp));
  width: 100%;
  padding: calc(8 * var(--dp)) calc(10 * var(--dp));
  border: none;
  background: transparent;
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-md);
  font-family: inherit;
  text-align: left;
  cursor: pointer;
}

@media (hover: hover) {
  .menu-select-item:hover {
    background: var(--bg-tertiary, #eef1f4);
  }
}

.menu-select-item.active {
  color: var(--accent-color, #0066cc);
  font-weight: var(--font-weight-medium);
}

.menu-select-item-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.menu-select-item-check {
  flex-shrink: 0;
  color: var(--accent-color, #0066cc);
}
</style>
