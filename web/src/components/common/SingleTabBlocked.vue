<template>
  <div class="single-tab-block" role="alertdialog" aria-modal="true">
    <div class="single-tab-block__card">
      <div class="single-tab-block__icon" aria-hidden="true">
        <MonitorX :size="44" />
      </div>
      <h1 class="single-tab-block__title">{{ t('singleTab.title') }}</h1>
      <p class="single-tab-block__desc">{{ t('singleTab.description') }}</p>
      <p class="single-tab-block__hint">{{ t('singleTab.hint') }}</p>
      <button class="single-tab-block__btn" type="button" @click="reload">
        {{ t('singleTab.reload') }}
      </button>
    </div>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { MonitorX } from 'lucide-vue-next'

const { t } = useI18n()

function reload() {
  window.location.reload()
}
</script>

<style scoped>
.single-tab-block {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-6, 24px);
  background: var(--bg-primary, #1d2021);
  color: var(--text-primary, #ebdbb2);
  z-index: 2147483647;
}

.single-tab-block__card {
  max-width: 420px;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-4, 16px);
}

.single-tab-block__icon {
  color: var(--text-muted, #928374);
  display: flex;
}

.single-tab-block__title {
  margin: 0;
  font-size: var(--font-size-xl, 20px);
  font-weight: 600;
}

.single-tab-block__desc,
.single-tab-block__hint {
  margin: 0;
  font-size: var(--font-size-md, 14px);
  line-height: 1.6;
  color: var(--text-secondary, #d5c4a1);
}

.single-tab-block__hint {
  color: var(--text-muted, #928374);
}

.single-tab-block__btn {
  margin-top: var(--space-2, 8px);
  padding: 8px 20px;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: var(--font-size-md, 14px);
  /* The accent token is --accent-color, not --accent. The old name was never
     declared anywhere, so this always fell through to its fallback — a
     hard-coded gruvbox green that ignored the user's theme entirely (this
     screen is the second-tab block, rendered before the app's own theme
     plumbing runs).
     The fallback must stay: --accent-color is declared only under
     [data-theme="…"], and index.html's inline bootstrap is what sets that
     attribute. Keeping a neutral blue here means the button is still legible
     if the screen ever renders before that script has run. */
  background: var(--accent-color, #4a90d9);
  color: var(--bg-primary, #1d2021);
}

.single-tab-block__btn:hover {
  opacity: 0.9;
}
</style>
