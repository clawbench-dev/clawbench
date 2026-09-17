<template>
  <div class="about-brand">
    <img class="about-brand__logo" src="/logo-128.png" alt="ClawBench" />
    <div class="about-brand__name">ClawBench</div>
    <div class="about-brand__slogan">{{ t('settings.items.aboutBrandSlogan') }}</div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<style scoped>
/* Sits directly on the settings page's grey background rather than inside a
   card: the brand block is a header for the page, and giving it the same white
   card treatment as the "Version"/"Actions" groups below would make three
   equally-weighted blocks with no visual entry point. */
.about-brand {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: var(--space-8) var(--space-4) var(--space-4);
}

.about-brand__logo {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  display: block;
  box-shadow: var(--shadow-md);
}

.about-brand__name {
  margin-top: var(--space-4);
  font-size: var(--font-size-2xl);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.about-brand__slogan {
  margin-top: var(--space-1);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
</style>
