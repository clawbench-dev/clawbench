<template>
  <Transition name="up-fade">
    <div v-if="visible" class="up-overlay" @click.self="dismiss">
      <div class="up-panel">
        <div class="up-header">
          <h3>{{ t('upgrade.newVersion') }}</h3>
        </div>
        <p class="up-body">{{ t('upgrade.promptMessage', { version: latestVersion, currentVersion }) }}</p>
        <div class="up-version-badge">v{{ latestVersion }}</div>
        <p v-if="isDocker" class="up-docker-hint">
          {{ t('upgrade.dockerHintBody') }}
          <span class="up-docker-restart">{{ t('upgrade.dockerHintRestart') }}</span>
        </p>
        <a v-if="releaseNotesUrl" class="up-release-link" :href="releaseNotesUrl" target="_blank" rel="noopener noreferrer">
          {{ t('upgrade.releaseNotes', { version: latestVersion }) }}
        </a>
        <div class="up-footer">
          <button class="up-upgrade" @click="upgradeNow">{{ t('upgrade.upgradeNow') }}</button>
          <button class="up-skip" @click="skipVersion">{{ t('upgrade.skipVersion') }}</button>
          <button class="up-later" @click="dismiss">{{ t('upgrade.remindLater') }}</button>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useUpgrade } from '@/composables/useUpgrade'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

const { t } = useI18n()
const { skipVersion: doSkip, startUpgrade, releaseNotesUrl, isDocker } = useUpgrade()

const visible = ref(false)
const latestVersion = ref('')
const currentVersion = ref('')
let unregisterBack: (() => void) | null = null

defineExpose({ show })

function show(latest: string, current: string) {
  latestVersion.value = latest
  currentVersion.value = current
  visible.value = true
}

async function upgradeNow() {
  visible.value = false
  // startUpgrade may open a confirmation dialog for an unverified release
  // before it does anything, so await it rather than firing and forgetting.
  await startUpgrade()
}

function skipVersion() {
  doSkip(latestVersion.value)
  visible.value = false
}

function dismiss() {
  visible.value = false
}

// Register back handler when overlay opens, unregister on close
watch(visible, (v) => {
  if (v) {
    unregisterBack = registerBackHandler({
      id: 'upgrade-prompt',
      canGoBack: () => visible.value,
      goBack: () => dismiss(),
      priority: PRIORITY_OVERLAY,
    })
  } else if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
})
</script>

<style scoped>
.up-overlay {
  position: fixed;
  inset: 0;
  z-index: var(--z-overlay-raised);
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--bg-primary) 80%, transparent);
  backdrop-filter: blur(calc(4 * var(--dp)));
  -webkit-backdrop-filter: blur(calc(4 * var(--dp)));
  padding: var(--space-7);
}

.up-panel {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  width: 100%;
  max-width: calc(380 * var(--dp));
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}

.up-header {
  padding: calc(14 * var(--dp)) var(--space-7) var(--space-4);
}

.up-header h3 {
  margin: 0;
  font-size: var(--font-size-2xl);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
}

.up-body {
  margin: 0 var(--space-7) var(--space-4);
  font-size: var(--font-size-md);
  color: var(--text-secondary);
  line-height: var(--line-height-relaxed);
}

.up-version-badge {
  display: inline-block;
  margin: 0 var(--space-7) var(--space-6);
  padding: var(--space-2) var(--space-6);
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
  color: var(--accent-color);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-bold);
  font-variant-numeric: tabular-nums;
}

.up-release-link {
  display: block;
  margin: 0 var(--space-7) var(--space-6);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--accent-color);
  text-decoration: none;
  cursor: pointer;
}

.up-docker-hint {
  margin: 0 var(--space-7) var(--space-5);
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  line-height: var(--line-height-normal);
}

.up-docker-restart {
  display: block;
  margin-top: var(--space-2);
  color: var(--color-orange);
}

.up-release-link:hover {
  text-decoration: underline;
}

/* The footer is a COLUMN: primary action full width, the two dismissals side by
   side beneath it. Same shape as WelcomeOverlay's footer (确定 full width, then
   重新扫描 / 不再提示), so the two first-run dialogs read alike.
   
   Why not one row with wrapping: the panel is at most 380px wide, so the footer
   has 348px of content box, and the three labels cannot share it in every
   locale. English needs ~470px ("Upgrade now" + "Skip this version" + "Remind me
   later" at 14px, plus padding and gaps), Chinese ~230px. A wrapping row
   therefore laid out DIFFERENTLY per language — one row in Chinese, two in
   English — and, worse, an earlier `@media (max-width: 400px)` override keyed
   the two-row layout to the VIEWPORT, which never fires on desktop: English on a
   1280px screen still got a squeezed two-row footer while Chinese got one row.
   
   A fixed column is language- and viewport-independent, which is the property
   that actually matters here. `flex-wrap` is kept so an unusually long locale
   wraps its own row instead of overflowing the panel. */
.up-footer {
  padding: var(--space-4) var(--space-7) calc(14 * var(--dp));
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-4);
}

/* Full width, so the primary action never competes with the labels beside it.
   `white-space: nowrap` still applies: a CJK string's min-content width is one
   character, so without it a narrow panel would stack the label vertically. */
.up-upgrade {
  flex: 1 1 100%;
  padding: var(--space-4) var(--space-7);
  border: none;
  border-radius: var(--radius-sm);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
  cursor: pointer;
  transition: opacity var(--duration-slow);
}

@media (hover: hover) {
  .up-upgrade:hover { opacity: var(--opacity-hover); }
}

/* The dismissals share the second row. `flex-basis: 0` with `min-width: 0` lets
   them divide the width evenly; without the explicit `min-width` a long label
   sets the flex item's automatic minimum to its full text width and the even
   split collapses. `flex-wrap` on the footer still moves one down if a locale's
   pair genuinely cannot fit. */
.up-skip {
  flex: 1 1 0;
  min-width: 0;
  padding: var(--space-4) var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
  cursor: pointer;
  transition: all var(--duration-slow);
}

@media (hover: hover) {
  .up-skip:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}

.up-later {
  flex: 1 1 0;
  min-width: 0;
  padding: var(--space-4) var(--space-6);
  border: none;
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-md);
  white-space: nowrap;
  cursor: pointer;
}

.up-fade-enter-active { transition: opacity var(--duration-slow) ease; }
.up-fade-leave-active { transition: opacity var(--duration-base) ease; }
.up-fade-enter-from, .up-fade-leave-to { opacity: 0; }
</style>
