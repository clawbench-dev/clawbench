<template>
  <button
    v-bind="$attrs"
    class="copy-btn"
    :class="{ 'is-copied': isCopied }"
    type="button"
    :title="titleLabel"
    :aria-label="titleLabel"
    @click="handleClick"
  >
    <!--
      The one copy button in the app.

      Feedback is an ICON SWAP — the glyph becomes a check for a moment — never
      a text label. The label variant is what this component exists to
      eliminate: the button is usually pinned to a container edge (or sits in a
      fixed-width toolbar), so a wider label grows it *into* whatever is beside
      it, and the only way to keep it clear is to reserve the widest
      translation up front (CJK "已复制" vs "コピーしました" differ by ~2x). A
      constant footprint has neither problem.

      It also does NOT toast. The check is the feedback.

      Two modes:

        uncontrolled — pass `text`, the button copies and flashes by itself
          <CopyButton :text="value" class="my-22px-btn" />

        controlled — pass `copied`; the host owns the clipboard write and the
        timing (used where the state already lives in a parent, e.g. the code
        preview toolbar)
          <CopyButton :copied="c" :title-key="k" :copied-key="ck" @click="doCopy" />

      The `copied` slot prop lets a caller with its own tooltip wiring read the
      state for the label:
          <CopyButton :text="v" v-slot="{ copied }"
                      @pointerenter="showTooltip($event, copied ? ck : k)">

      This comment lives INSIDE the root element on purpose: a comment BEFORE
      it makes the template a fragment, and the root attributes/classes then
      stop resolving on the component wrapper (the same trap QuoteCard
      documents).
    -->
    <Check v-if="isCopied" :size="size" />
    <slot v-else name="icon">
      <Copy :size="size" />
    </slot>
    <!-- Optional visible label. Some hosts (a command guide) need the button to
         say what it does; the label stays put while the GLYPH swaps, so the
         button's width is still constant. Hosts in dense toolbars omit it and
         rely on the title/aria-label. -->
    <span v-if="label" class="copy-btn-label">{{ label }}</span>
  </button>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Copy } from 'lucide-vue-next'
import { copyText } from '@/utils/clipboard'

const props = withDefaults(defineProps<{
  /**
   * Text to copy. Omit when the host owns the clipboard write (controlled
   * mode, or a caller that needs custom formatting).
   */
  text?: string
  /**
   * Controlled feedback state. When provided (including `false`) the component
   * stops managing its own timer and mirrors this value instead.
   */
  copied?: boolean
  /** Icon size in px. The button box is sized by the caller's class. */
  size?: number
  /**
   * Optional visible label. Hosts that need the button to say what it does
   * (a command guide) pass one; dense toolbars omit it and rely on the
   * title/aria-label. The label is already-translated text.
   *
   * The label does NOT change when copied — only the glyph swaps — so the
   * button's width stays constant either way.
   */
  label?: string
  /** i18n key for the idle label. */
  titleKey?: string
  /** i18n key for the copied label. */
  copiedKey?: string
  /** Milliseconds the check stays up (uncontrolled mode only). */
  duration?: number
}>(), {
  text: undefined,
  copied: undefined,
  size: 14,
  label: '',
  titleKey: 'common.copy',
  copiedKey: 'common.copied',
  duration: 1500,
})

const emit = defineEmits<{
  /** Fired on every click. In controlled mode this is where the host copies. */
  click: [event: MouseEvent]
}>()

defineOptions({ inheritAttrs: false })

const { t } = useI18n()

// Uncontrolled state. Only used when `props.copied` is not provided.
const selfCopied = ref(false)
let timer: ReturnType<typeof setTimeout> | null = null

const isCopied = computed(() => (props.copied !== undefined ? props.copied : selfCopied.value))

// Named `titleLabel`, not `label`: `label` is the PROP holding the optional
// visible button text, and a same-named computed would shadow it in the
// template (the visible label would render the i18n title instead).
const titleLabel = computed(() => t(isCopied.value ? props.copiedKey : props.titleKey))

function handleClick(event: MouseEvent) {
  // Self-manage only when the host has neither taken over the state nor the
  // clipboard write.
  //
  // Empty text means "there is nothing to copy" — no clipboard write and no
  // check. This is the contract every caller relies on: a message whose
  // copyable text is empty must not flash a success the user cannot see the
  // result of. (An empty string is never a meaningful copy.)
  //
  // The re-entrancy guard wraps ONLY the clipboard write — the `click` event
  // still fires on every press, because a host may use it for side effects
  // (closing a menu) that must not be dropped just because the check is up.
  if (props.copied === undefined && props.text && !selfCopied.value) {
    copyText(props.text, () => {
      selfCopied.value = true
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => {
        timer = null
        selfCopied.value = false
      }, props.duration)
    })
  }
  emit('click', event)
}

defineExpose({ copied: isCopied })
</script>

<style scoped>
/* Structural reset only. Wrapped in `:where()` so the rule has ZERO
   specificity: every host passes its own class (`.code-preview-btn`,
   `.item-copy-btn`, …) that sets size, colour and hover, and those must win
   regardless of which CSS chunk the bundler emits first. Without `:where()`
   this rule and a host class both weigh (0,1,0) and source order decides —
   which would silently strip a host's colour. */
:where(.copy-btn) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: none;
  background: transparent;
  color: inherit;
  cursor: pointer;
}

:where(.copy-btn) svg {
  display: block;
  flex-shrink: 0;
}

:where(.copy-btn-label) {
  white-space: nowrap;
}

/* The copied tint comes from the global `css/copy-button.css`
   (`button.copy-btn.is-copied`), NOT from here. A scoped declaration would
   compile to `.copy-btn.is-copied[data-v-xxxx]` — (0,3,0) — and would then beat
   any host that wants its own copied colour (the user-bubble overrides in
   markdown-common.css are (0,2,0)). Keeping the tint global and at (0,2,1)
   lets those host rules win when they need to. */
</style>
