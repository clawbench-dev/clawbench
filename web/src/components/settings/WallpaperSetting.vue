<template>
  <div class="wallpaper-setting">
    <!-- Wallpaper row: preview thumb + set/remove actions -->
    <div class="settings-item" :class="{ 'settings-item--disabled': busy }">
      <div class="settings-item__left">
        <div class="settings-item__text">
          <span class="settings-item__label">{{ t('settings.items.wallpaper') }}</span>
        </div>
      </div>
      <div class="settings-item__right">
        <div v-if="state !== 'unset'" class="wallpaper-thumb-wrap">
          <img
            v-if="state === 'set'"
            :src="wallpaperImageUrl()"
            class="wallpaper-thumb"
            :alt="t('settings.items.wallpaperPreview')"
          />
          <div v-else class="wallpaper-thumb wallpaper-thumb--empty">{{ t('settings.items.wallpaperLoading') }}</div>
        </div>
        <button v-if="state === 'set'" class="settings-item__action" :disabled="busy" @click.stop="handleRemove">
          {{ t('settings.items.wallpaperRemove') }}
        </button>
        <button class="settings-item__action settings-item__action--primary" :disabled="busy" @click.stop="triggerUpload">
          {{ state === 'set' ? t('settings.items.wallpaperReplace') : t('settings.items.wallpaperUpload') }}
        </button>
        <input ref="fileInputRef" type="file" accept=".png,.jpg,.jpeg,.gif,.webp,.svg,image/png,image/jpeg,image/gif,image/webp,image/svg+xml" class="wallpaper-file-input" @change="onFileSelected" />
      </div>
    </div>
    <div v-if="description" class="settings-item__desc">{{ description }}</div>
    <div v-if="error" class="wallpaper-error">{{ error }}</div>

    <!-- Panel opacity slider row (disabled unless a wallpaper is set) -->
    <div class="settings-item" :class="{ 'settings-item--disabled': state !== 'set' }">
      <div class="settings-item__left">
        <div class="settings-item__text">
          <span class="settings-item__label">{{ t('settings.items.wallpaperPanelOpacity') }}</span>
        </div>
      </div>
      <div class="settings-item__right">
        <span class="settings-item__slider-value">{{ opacityDisplay }}</span>
        <input
          type="range"
          class="settings-item__slider"
          :value="panelOpacity"
          min="0.7"
          max="1"
          step="0.01"
          :disabled="state !== 'set'"
          @input="onOpacityInput"
          @click.stop
        />
        <button v-if="panelOpacity !== 0.85" class="settings-item__slider-reset" @click.stop="resetOpacity" :title="t('settings.items.resetToDefault')">↺</button>
      </div>
    </div>
    <div class="settings-item__desc">{{ t('settings.items.wallpaperPanelOpacityDesc') }}</div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'
import {
  uploadWallpaper,
  clearWallpaper,
  wallpaperImageUrl,
  resolveWallpaperState,
  resolvePanelOpacity,
  applyWallpaper,
  applyWallpaperScrim,
  currentThemeIsDark,
  type WallpaperState,
} from '@/utils/themeBackground'
import { useSettingsConfig } from '@/composables/useSettingsConfig'

defineProps<{ description?: string }>()

const { t } = useI18n()
const toast = useToast()
const { serverConfig, localConfig, loadConfig, patchConfig } = useSettingsConfig()

const fileInputRef = ref<HTMLInputElement | null>(null)
const busy = ref(false)
const error = ref('')

/** Wallpaper tri-state from the live server config. */
const state = computed<WallpaperState>(() =>
  resolveWallpaperState(serverConfig.value?.appearance as Record<string, unknown> | undefined)
)

/** Panel opacity from config (0.7..1.0). */
const panelOpacity = computed(() => resolvePanelOpacity(serverConfig.value?.appearance as Record<string, unknown> | undefined))

const opacityDisplay = computed(() => `${Math.round(panelOpacity.value * 100)}%`)

/** Re-apply the wallpaper effect (after upload/remove) with current theme.
 *  forceBust is set after an upload so a same-name replacement (new bytes under
 *  the immutable cache) is re-fetched. */
function refreshEffect(forceBust = false) {
  const appearance = (serverConfig.value?.appearance as Record<string, unknown> | undefined) ?? {}
  applyWallpaper(
    (appearance.wallpaper_file as string) ?? '',
    resolvePanelOpacity(appearance),
    currentThemeIsDark(String(localConfig.theme ?? 'auto')),
    forceBust,
  )
}

/** Re-sync from server after a successful write so rows + effect agree.
 *  forceBust: re-fetch the image even if the file name is unchanged (upload
 *  replaced bytes under the same immutable-cached file name). */
async function reloadFromServer(forceBust = false) {
  await loadConfig()
  refreshEffect(forceBust)
}

function triggerUpload() {
  error.value = ''
  fileInputRef.value?.click()
}

async function onFileSelected(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // allow re-selecting the same file
  if (!file) return
  busy.value = true
  error.value = ''
  try {
    await uploadWallpaper(file)
    await reloadFromServer(true)
    toast.show(t('settings.items.wallpaperSetOk'), { icon: '🖼️', type: 'success', duration: 2500 })
  } catch {
    error.value = t('settings.items.wallpaperUploadFailed')
  } finally {
    busy.value = false
  }
}

async function handleRemove() {
  busy.value = true
  error.value = ''
  try {
    await clearWallpaper()
    await reloadFromServer()
    toast.show(t('settings.items.wallpaperRemoved'), { icon: 'ℹ️', type: 'info', duration: 2000 })
  } catch {
    error.value = t('settings.items.wallpaperRemoveFailed')
  } finally {
    busy.value = false
  }
}

function onOpacityInput(e: Event) {
  const v = Number((e.target as HTMLInputElement).value)
  applyWallpaper(
    (serverConfig.value?.appearance as Record<string, unknown> | undefined)?.wallpaper_file as string ?? '',
    v,
    currentThemeIsDark(String(localConfig.theme ?? 'auto')),
  )
  // Persist with debounce; patchConfig() reloads serverConfig, which the
  // App.vue watcher turns into a fresh applyWallpaper (alpha + state).
  if (opacitySaveTimer) clearTimeout(opacitySaveTimer)
  opacitySaveTimer = setTimeout(() => {
    void patchConfig({ appearance: { panel_opacity: v } })
      .catch(() => {
        error.value = t('settings.items.wallpaperSaveFailed')
        void loadConfig()
      })
  }, 350)
}

function resetOpacity() {
  onOpacityInput({ target: { value: '0.85' } } as unknown as Event)
}

let opacitySaveTimer: ReturnType<typeof setTimeout> | null = null

// Keep the scrim in sync when the theme changes while this panel is open.
function onThemeChange() {
  applyWallpaperScrim(currentThemeIsDark(String(localConfig.theme ?? 'auto')))
}

onMounted(() => {
  window.addEventListener('clawbench-theme-change', onThemeChange)
})

onUnmounted(() => {
  window.removeEventListener('clawbench-theme-change', onThemeChange)
  if (opacitySaveTimer) clearTimeout(opacitySaveTimer)
})
</script>

<style scoped>
.wallpaper-setting {
  background: transparent;
}

.wallpaper-setting > .settings-item {
  background: transparent;
  padding: 8px 16px;
  position: relative;
}

/* Divider consistent with sibling settings rows */
.wallpaper-setting > .settings-item::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 16px;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.wallpaper-thumb-wrap {
  display: flex;
  align-items: center;
}

.wallpaper-thumb {
  width: 40px;
  height: 28px;
  border-radius: 5px;
  object-fit: cover;
  border: 1px solid var(--border-color);
  margin-right: 8px;
}

.wallpaper-thumb--empty {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  color: var(--text-muted);
}

.settings-item__action {
  padding: 5px 12px;
  border: 1px solid var(--border-color);
  border-radius: 7px;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
  margin-left: 4px;
  white-space: nowrap;
}

.settings-item__action--primary {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
  font-weight: 500;
}

.settings-item__action:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.wallpaper-file-input {
  display: none;
}

.wallpaper-error {
  font-size: 12px;
  color: var(--color-red);
  padding: 4px 16px 8px;
  line-height: 1.4;
  word-break: break-word;
}

.settings-item__slider-value {
  font-size: 13px;
  color: var(--text-secondary);
  min-width: 36px;
  text-align: right;
}

.settings-item__slider {
  width: 120px;
  cursor: pointer;
  accent-color: var(--accent-color);
}

.settings-item__slider-reset {
  font-size: 14px;
  color: var(--text-muted);
  background: none;
  border: none;
  cursor: pointer;
  padding: 2px 4px;
  line-height: 1;
}
</style>
