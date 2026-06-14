<template>
  <Teleport to="body">
    <Transition name="toast">
      <div v-if="toast.visible.value" :class="['toast', `toast-${toast.type.value}`]" @click="toast.onClick.value ? (toast.onClick.value(), toast.dismiss()) : toast.dismiss()">
        <span v-if="toast.icon.value" class="toast-icon">{{ toast.icon.value }}</span>
        <span class="toast-text">{{ toast.message.value }}</span>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
defineProps({
    toast: {
        type: Object,
        required: true,
    },
})
</script>

<style>
.toast {
    position: fixed;
    top: calc(8px + var(--header-safe-area-top, 0px));
    left: 0;
    right: 0;
    margin: 0 auto;
    background: color-mix(in srgb, var(--accent-color) 85%, var(--bg-tertiary));
    color: #fff;
    border-radius: 16px;
    padding: 6px 14px;
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    font-weight: 500;
    box-shadow: var(--shadow-md);
    cursor: pointer;
    z-index: 9999;
    white-space: normal;
    width: fit-content;
    min-width: 80px;
    max-width: 88vw;
    text-align: left;
    line-height: 1.4;
    -webkit-tap-highlight-color: transparent;
    user-select: none;
    transition: opacity 0.1s, transform 0.1s;
}

.toast-error {
    background: color-mix(in srgb, #ef4444 78%, var(--bg-tertiary));
}

[data-theme="dark"] .toast-error {
    background: color-mix(in srgb, #ef4444 55%, var(--bg-tertiary));
}

.toast-success {
    background: color-mix(in srgb, #22c55e 78%, var(--bg-tertiary));
}

[data-theme="dark"] .toast-success {
    background: color-mix(in srgb, #22c55e 55%, var(--bg-tertiary));
}

.toast-info {
    background: color-mix(in srgb, var(--accent-color) 78%, var(--bg-tertiary));
}

[data-theme="dark"] .toast-info {
    background: color-mix(in srgb, var(--accent-color) 55%, var(--bg-tertiary));
}

[data-theme="dark"] .toast {
    background: color-mix(in srgb, var(--accent-color) 40%, var(--bg-tertiary));
    color: var(--text-primary);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.5);
}

.toast:active {
    opacity: 0.8;
    transform: scale(0.97);
}

.toast-icon {
    font-size: 16px;
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
    transform: translateY(-12px);
}
</style>
