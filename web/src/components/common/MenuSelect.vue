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
    :title="triggerTitle"
    @click.stop="toggle"
  >
    <!-- Default trigger: label + caret. A caller that needs a richer face
         (the mail bar's account avatar / folder + unread badge) supplies the
         slot; the button, popup anchoring and exclusive-open behaviour stay
         exactly the same either way. -->
    <slot name="trigger" :open="show" :label="triggerLabel">
      <span class="menu-select-label">{{ triggerLabel }}</span>
      <ChevronDown class="menu-select-caret" :size="14" />
    </slot>
  </button>

  <PopupMenu
    :show="show"
    :target-element="anchorRef"
    :max-width="menuWidth"
    :min-width="appSurface ? 0 : menuMinWidth"
    :max-height="maxHeight"
    :menu-items-count="options.length"
    :app-surface="appSurface"
    :close-on-click="!multiple"
    @update:show="onShowUpdate"
  >
    <!-- The popup's min-width (the trigger width) keeps the menu lined up with
         the control it belongs to instead of shrinking to its narrowest item. -->
    <div class="menu-select-list" :class="{ 'app-menu-column': appSurface }" :style="listStyle">
      <div v-if="menuTitle" class="app-menu-title">{{ menuTitle }}</div>
      <div :class="appSurface ? 'app-menu-scroll' : ''">
        <slot name="options">
          <button
            v-for="opt in options"
            :key="String(opt.value)"
            type="button"
            class="menu-select-item"
            :class="{ active: isSelected(opt.value), 'is-locked': isLocked(opt.value) }"
            :title="opt.label"
            :disabled="isLocked(opt.value)"
            :role="multiple ? 'menuitemcheckbox' : 'menuitem'"
            :aria-checked="multiple ? isSelected(opt.value) : undefined"
            @click="choose(opt.value)"
          >
            <span class="menu-select-item-label">{{ opt.label }}</span>
            <Check v-if="isSelected(opt.value)" class="menu-select-item-check" :size="14" />
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

/** A selection: one value in single mode, a list in `multiple` mode. */
export type MenuSelectValue = string | number | (string | number)[]

const props = withDefaults(defineProps<{
  modelValue: MenuSelectValue
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
  /**
   * Allow several options to be selected at once. The menu stays open while
   * rows are toggled (it closes on outside click / the trigger / Escape), and
   * the trigger collapses the selection to "first label +N".
   */
  multiple?: boolean
  /** `multiple` only: never allow the selection to shrink below this count.
   *  Rows that would break the floor are rendered disabled. */
  minSelected?: number
}>(), {
  placeholder: '',
  disabled: false,
  block: false,
  maxHeight: 320,
  menuWidth: 260,
  appSurface: false,
  menuTitle: '',
  multiple: false,
  minSelected: 0,
})

const emit = defineEmits<{
  'update:modelValue': [value: MenuSelectValue]
  change: [value: MenuSelectValue]
}>()

const anchorRef = ref<HTMLElement | null>(null)
const show = ref(false)
const menuMinWidth = ref(0)

const selectedValues = computed<(string | number)[]>(() =>
  Array.isArray(props.modelValue) ? props.modelValue : [props.modelValue],
)

const selectedOptions = computed<MenuSelectOption[]>(() =>
  // Ordered by the selection, not the option list: the trigger collapses a
  // multi selection to its first label, which should be the value the user
  // chose first, not whichever row happens to sort first in the menu.
  selectedValues.value
    .map(v => props.options.find(o => o.value === v))
    .filter((o): o is MenuSelectOption => !!o),
)

const triggerLabel = computed(() => {
  const sel = selectedOptions.value
  if (sel.length === 0) return props.placeholder
  if (!props.multiple || sel.length === 1) return sel[0].label
  // Compact multi-selection: the first label plus a count. The full list is on
  // the trigger's `title`, so a hover (or long-press) still reads it.
  return `${sel[0].label} +${sel.length - 1}`
})

const triggerTitle = computed(() =>
  selectedOptions.value.map(o => o.label).join(' / ') || props.placeholder,
)

function isSelected(value: string | number): boolean {
  return selectedValues.value.includes(value)
}

/** A row whose removal would break `minSelected` — rendered disabled. */
function isLocked(value: string | number): boolean {
  return props.multiple
    && selectedValues.value.length <= props.minSelected
    && isSelected(value)
}

/**
 * Inline style for the popup body.
 *
 * The min-width only makes sense for the app-header surface, which is a card
 * with its own padding: it still needs a floor, otherwise it shrinks to the
 * width of the widest row's *content* — and a stacked project row's path is
 * clipped to a few characters ("tmp/nr…") instead of being readable.
 * `menuWidth` is the popup's MAX width, so it doubles as a sensible target here.
 *
 * The plain list needs no width of its own: MenuSelect passes the trigger width
 * to the popup ROOT as `min-width`. Putting it on the list instead made the list
 * 2px wider than the root's content box (the root's border eats into its
 * max-width under `box-sizing: border-box`), and the root — which is the scroll
 * container — painted a horizontal scrollbar under the menu.
 */
const listStyle = computed(() =>
  props.appSurface ? { minWidth: `${props.menuWidth}px` } : undefined,
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
  // trigger it drops out of. The popup clamps this to its own max width.
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
  if (isLocked(value)) return
  if (props.multiple) {
    const current = selectedValues.value
    const next = current.includes(value)
      ? current.filter(v => v !== value)
      : [...current, value]
    if (next.length === 0) return
    // Deliberately stays open: toggling three columns should not cost three
    // visits to the trigger. PopupMenu is told not to close on content clicks.
    emit('update:modelValue', next)
    emit('change', next)
    return
  }
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
  .menu-select-item:not(.is-locked):hover {
    background: var(--bg-tertiary, #eef1f4);
  }
}

/* `multiple` floor: the last selected row is disabled so the selection can
   never drop below the consumer's minimum. */
.menu-select-item.is-locked {
  opacity: var(--opacity-disabled);
  cursor: not-allowed;
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
