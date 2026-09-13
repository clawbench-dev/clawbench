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
    v-model:show="show"
    :target-element="anchorRef"
    :max-width="menuWidth"
    :max-height="maxHeight"
    :menu-items-count="options.length"
  >
    <!-- min-width matches the trigger so the open menu lines up with the
         control it belongs to instead of shrinking to its narrowest item. -->
    <div class="menu-select-list" :style="{ minWidth: `${menuMinWidth}px` }">
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
    </div>
  </PopupMenu>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { Check, ChevronDown } from 'lucide-vue-next'
import PopupMenu from '@/components/common/PopupMenu.vue'

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
}>(), {
  placeholder: '',
  disabled: false,
  block: false,
  maxHeight: 320,
  menuWidth: 260,
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

function toggle() {
  if (props.disabled) return
  if (!show.value) {
    // Measure before the popup positions itself (it reads geometry on the next
    // animation frame), so the menu can never come out narrower than the
    // trigger it drops out of.
    menuMinWidth.value = anchorRef.value?.offsetWidth ?? 0
  }
  show.value = !show.value
}

function choose(value: string | number) {
  show.value = false
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
  gap: 6px;
  padding: 8px 10px;
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
  box-shadow: 0 0 0 3px rgba(0, 102, 204, 0.1);
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

.menu-select-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
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
