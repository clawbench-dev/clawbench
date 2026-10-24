<template>
  <Teleport to="body">
    <Transition name="toast">
      <div
        v-if="toast.visible.value"
        ref="rootEl"
        :class="['toast', `toast-${toast.type.value}`]"
        @click="handleClick"
        @mouseenter="toast.pause()"
        @mouseleave="toast.resume()"
      >
        <LoadingIndicator v-if="toast.type.value === 'loading'" class="toast-spinner" size="sm" inline />
        <span v-else class="toast-icon" aria-hidden="true">
          <component :is="resolvedIcon" :size="16" />
        </span>
        <span class="toast-text">{{ toast.message.value }}</span>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { computed, ref } from 'vue'
import LoadingIndicator from './LoadingIndicator.vue'
import { resolveToastIcon } from './toastIcons'

const props = defineProps({
    toast: {
        type: Object,
        required: true,
    },
})

const rootEl = ref(null)

// The icon is part of the toast's built-in structure: a semantic key when the
// caller supplies one, otherwise the default glyph for the toast type. Never
// empty — see toastIcons.ts.
const resolvedIcon = computed(() => resolveToastIcon(props.toast.icon.value, props.toast.type.value))

// The toast text is selectable so it can be copied. A click that ends a text
// selection must not also dismiss (and fire onClick) — otherwise copying the
// message closes it mid-copy. Only a selection inside THIS toast counts; a
// selection left elsewhere on the page must not swallow the dismissal.
function handleClick() {
    const sel = window.getSelection()
    if (sel && !sel.isCollapsed && rootEl.value && sel.anchorNode && rootEl.value.contains(sel.anchorNode)) return
    if (props.toast.onClick.value) props.toast.onClick.value()
    props.toast.dismiss()
}
</script>

<style>
.toast {
    position: fixed;
    top: calc(8 * var(--dp) + var(--header-safe-area-top, 0px));
    left: 0;
    right: 0;
    margin: 0 auto;
    background: color-mix(in srgb, var(--accent-color) 85%, var(--bg-tertiary));
    color: #fff;
    border-radius: var(--radius-full);
    padding: var(--space-3) calc(14 * var(--dp));
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--font-size-md);
    font-weight: var(--font-weight-medium);
    box-shadow: var(--shadow-md);
    cursor: pointer;
    z-index: var(--z-popover);
    white-space: normal;
    width: fit-content;
    min-width: calc(80 * var(--dp));
    max-width: 88vw;
    text-align: left;
    line-height: var(--line-height-snug);
    -webkit-tap-highlight-color: transparent;
    /* Selectable so the user can copy the message; the auto-dismiss is paused
       while the pointer is over the toast (see @mouseenter) to leave time. */
    user-select: text;
    transition: opacity var(--duration-fast), transform var(--duration-fast);
}

.toast-error {
    background: color-mix(in srgb, var(--color-red, #ef4444) 78%, var(--bg-tertiary));
}

[data-theme-base="dark"] .toast-error {
    background: color-mix(in srgb, var(--color-red, #ef4444) 55%, var(--bg-tertiary));
}

.toast-success {
    background: color-mix(in srgb, var(--color-success, #22c55e) 78%, var(--bg-tertiary));
}

[data-theme-base="dark"] .toast-success {
    background: color-mix(in srgb, var(--color-success, #22c55e) 55%, var(--bg-tertiary));
}

.toast-info {
    background: color-mix(in srgb, var(--color-info, var(--accent-color)) 78%, var(--bg-tertiary));
}

/* Loading: an in-progress action, not a result. Same accent recipe as info,
   but the caller pairs it with duration: 0 so it stays until the action ends
   and a success/error toast replaces it. */
.toast-loading {
    background: color-mix(in srgb, var(--color-info, var(--accent-color)) 78%, var(--bg-tertiary));
}

.toast-spinner {
    flex-shrink: 0;
    /* The default ring uses --border-color gray, which is invisible on the
       saturated accent backdrop; override both halves so the ring reads. */
    --li-track-color: color-mix(in srgb, #fff 35%, transparent);
    --li-color: #fff;
}

[data-theme-base="dark"] .toast-info {
    background: color-mix(in srgb, var(--color-info, var(--accent-color)) 55%, var(--bg-tertiary));
}

[data-theme-base="dark"] .toast {
    background: color-mix(in srgb, var(--accent-color) 40%, var(--bg-tertiary));
    color: var(--text-primary);
    box-shadow: 0 calc(4 * var(--dp)) calc(16 * var(--dp)) rgba(0, 0, 0, 0.5);
}

/* On dark themes the toast backdrop is a muted accent mix with --text-primary
   text, so a white ring would wash out on light accents — follow the text
   colour instead. */
[data-theme-base="dark"] .toast-spinner {
    --li-track-color: color-mix(in srgb, var(--text-primary) 30%, transparent);
    --li-color: var(--text-primary);
}

.toast:active {
    opacity: var(--opacity-hover);
    transform: scale(0.97);
}

/* Icon slot: the lucide glyph inherits the toast's text colour. Fixed box so
   every toast's text starts at the same x regardless of glyph shape. */
.toast-icon {
    display: inline-flex;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
}

.toast-text {
    flex: 1;
    min-width: 0;
    overflow-wrap: break-word;
}

.toast-enter-active,
.toast-leave-active {
    transition: opacity 0.25s ease, transform 0.25s ease;
}

.toast-enter-from,
.toast-leave-to {
    opacity: 0;
    transform: translateY(calc(-12 * var(--dp)));
}
</style>
