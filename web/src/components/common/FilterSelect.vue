<template>
  <!-- A filter dropdown: `MenuSelect`'s behaviour (popup anchoring, outside
       click, exclusive open) wrapped in the app's pill trigger.

       Why a wrapper rather than another prop on MenuSelect: MenuSelect is
       also used as a plain form field and as a time picker (row editors,
       settings), where a square bordered box is the right look. Only the
       "filter bar" use wants a pill, so the pill lives here and MenuSelect
       keeps its neutral face.

       Everything a consumer has to know lives in the props below — which
       option means "no filter", whether that counts as a filter, and the
       caption. No consumer keeps its own copy of that bookkeeping, which is
       what made the forge chips and the notification dropdowns drift apart. -->
  <!-- The wrapper carries the pill's identity classes. They CANNOT ride on
       MenuSelect itself: a sub-component root that already has a static
       `class` keeps only that one, and Vue silently drops the incoming
       `:class` — so `is-active` would never reach the DOM and the tinted
       state would simply not render (verified in FilterSelect.test.ts). -->
  <div class="filter-select" :class="{ 'is-active': active }">
    <MenuSelect
      :model-value="modelValue"
      :options="internalOptions"
      :menu-width="menuWidth"
      :disabled="disabled"
      :multiple="multiple"
      :min-selected="minSelected"
      :trigger-title="triggerTitle"
      @update:model-value="onSelect"
    >
      <template #trigger="{ open }">
        <span class="filter-select-face">
          <span class="filter-select-caption">{{ caption }}</span>
          <span class="filter-select-value">{{ currentLabel }}</span>
          <ChevronDown class="filter-select-caret" :class="{ 'is-open': open }" :size="13" />
        </span>
      </template>
    </MenuSelect>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { ChevronDown } from 'lucide-vue-next'
import MenuSelect, { type MenuSelectOption, type MenuSelectValue } from '@/components/common/MenuSelect.vue'

const props = withDefaults(defineProps<{
  /** One value, or a list of values in `multiple` mode. */
  modelValue: string | number | (string | number)[]
  /** The real choices — no sentinel row needed: "show everything" is spelled
   *  by the `allLabel` prop, which this component prepends itself. */
  options: MenuSelectOption[]
  /** Caption printed before the value ("状态", "类型"). Omit for a bare value. */
  caption?: string
  /** What the dropdown reads when nothing is filtered. Required: it is the
   *  only way the control can know that `allValue` means "unfiltered". */
  allLabel: string
  /** The option value that means "no filter". Defaults to `''`. */
  allValue?: string | number
  disabled?: boolean
  menuWidth?: number
  /**
   * Allow several values at once (the usage panel's agent / dimension /
   * metric pickers). In this mode the "all" row is a real option the caller
   * supplies, not this component's reset — "several of them" and "none of
   * them" mean different things, so the control cannot invent that row.
   */
  multiple?: boolean
  /** `multiple` only: the selection may never drop below this count. */
  minSelected?: number
}>(), {
  caption: '',
  allValue: '',
  disabled: false,
  menuWidth: 200,
  multiple: false,
  minSelected: 0,
})

/**
 * Always a scalar. `multiple` is a template-level convenience for the usage
 * panel's multi pickers, not a mode this event reports on: `onSelect` below
 * collapses a list to its first value before emitting, so every caller may
 * type its handler `(v: string | number)` and never has to guard for an array
 * it cannot receive. (Multi-select consumers bind through `v-model` on their
 * own array state and read it on `change`, not through this event.)
 */
const emit = defineEmits<{
  'update:modelValue': [value: string | number]
  change: [value: MenuSelectValue]
}>()

/**
 * The rows actually rendered.
 *
 * Single: the component prepends the "all" row (picking it resets to
 * `allValue`), which is the whole point of the extraction — callers used to
 * hand-build that sentinel with the caption baked into the label.
 * Multiple: the caller's list is used as-is; see the `multiple` prop comment.
 */
const internalOptions = computed<MenuSelectOption[]>(() =>
  props.multiple
    ? props.options
    : [
        { value: props.allValue, label: props.allLabel },
        ...props.options.filter(o => o.value !== props.allValue),
      ],
)

const selectedValues = computed<(string | number)[]>(() =>
  Array.isArray(props.modelValue) ? props.modelValue : [props.modelValue],
)

const currentLabel = computed(() => {
  if (!props.multiple) {
    return internalOptions.value.find(o => o.value === props.modelValue)?.label ?? props.allLabel
  }
  // Same collapse rule as MenuSelect's own trigger: the first label plus a
  // count, with the full list in the tooltip.
  //
  // The caller's "all" sentinel is a state marker, not a choice: leaving it in
  // the count made a sentinel-plus-one selection read as "全部智能体 +1".
  const sel = selectedValues.value
    .filter(v => v !== props.allValue)
    .map(v => props.options.find(o => o.value === v))
    .filter((o): o is MenuSelectOption => !!o)
  if (sel.length === 0) return props.allLabel
  if (sel.length === 1) return sel[0].label
  return `${sel[0].label} +${sel.length - 1}`
})

/**
 * Whether the control is filtering anything.
 *
 * This is the "effective state" judgement the callers used to reimplement:
 * the pill tints only when the value is something other than "all", so an
 * untouched filter bar stays visually quiet while a narrowed one announces
 * itself even when the panel is too narrow to show the value in full.
 */
const active = computed(() => {
  if (!props.multiple) return props.modelValue !== props.allValue
  // Several values selected: "filtering" means the selection is narrower than
  // the full set. The "all" sentinel is excluded from BOTH sides: counting it
  // made a fresh multi-select (['']) look narrower than the full set, painting
  // the pill before the user touched anything.
  const candidates = props.options.filter(o => o.value !== props.allValue)
  const live = candidates.filter(o => selectedValues.value.includes(o.value))
  return live.length > 0 && live.length < candidates.length
})

const triggerTitle = computed(() => {
  const head = props.caption ? `${props.caption}: ` : ''
  return `${head}${currentLabel.value}`
})

function onSelect(v: MenuSelectValue) {
  // Forward the full payload on `change` (multi consumers need the whole
  // list), and keep `update:modelValue` scalar for everyone else.
  emit('change', v)
  emit('update:modelValue', Array.isArray(v) ? (v[0] ?? props.allValue) : v)
}
</script>

<style scoped>
/* The trigger is MenuSelect's <button>; these rules reshape it from a form
   field into the filter pill WITHOUT touching MenuSelect's own styles (scoped
   from here, so the other MenuSelect consumers are unaffected). */
.filter-select {
  display: inline-flex;
  min-width: 0;
}

.filter-select :deep(.menu-select) {
  /* Fill the wrapper so the whole pill is one hit target; the wrapper is what
     the caller's flex row sees. */
  width: 100%;
  gap: 0;
  padding: calc(4 * var(--dp)) calc(9 * var(--dp));
  border-radius: var(--radius-full);
  background: transparent;
  font-size: var(--font-size-sm);
  line-height: 1;
  color: var(--text-secondary);
  transition: background var(--duration-base) ease,
              border-color var(--duration-base) ease,
              color var(--duration-base) ease;
}

@media (hover: hover) {
  .filter-select:not(.is-active) :deep(.menu-select:not(.is-disabled):hover) {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
}

.filter-select :deep(.menu-select.is-open) {
  border-color: var(--accent-color);
  color: var(--text-primary);
}

/* Effective state: an accent OUTLINE plus accent text — no fill.
   A translucent accent fill was tried first and rejected on dark themes: the
   `--selection-bg` token is the accent at ~25% alpha, so an orange accent over
   a near-black surface composites to a muddy brown (#5e3e24 measured on
   Gruvbox Dark), which read as "disabled", not "filtering". An outline carries
   the same meaning at any theme accent and leaves the surface alone, so it
   cannot turn to mud. */
.filter-select.is-active :deep(.menu-select) {
  border-color: var(--accent-color);
  background: transparent;
  color: var(--accent-color);
}

@media (hover: hover) {
  /* Keep the outline visible while hovering: the neutral hover rule above
     repaints the background, and a filled pill again reads as a button. */
  .filter-select.is-active :deep(.menu-select:not(.is-disabled):hover) {
    background: transparent;
    color: var(--accent-color);
  }
}

.filter-select.is-active :deep(.menu-select:focus-visible) {
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}

.filter-select-face {
  display: inline-flex;
  align-items: center;
  gap: calc(5 * var(--dp));
  min-width: 0;
}

/* The caption is the quiet half of "状态: 开启" — it never competes with the
   value for attention, but it is still selectable text, so a narrow bar can
   show its value clipped AND its meaning. */
.filter-select-caption {
  color: var(--text-muted);
  white-space: nowrap;
}

.filter-select.is-active .filter-select-caption {
  color: inherit;
  opacity: 0.75;
}

.filter-select-value {
  font-weight: var(--font-weight-medium);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.filter-select-caret {
  flex-shrink: 0;
  color: var(--text-muted);
  transition: transform var(--duration-base) ease;
}

.filter-select.is-active .filter-select-caret {
  color: inherit;
}

.filter-select-caret.is-open {
  transform: rotate(180deg);
}
</style>
