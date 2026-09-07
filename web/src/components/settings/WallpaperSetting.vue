<template>
  <div class="wallpaper-setting">
    <!-- ── Row 1: background image ─────────────────────────────── -->
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
      <div v-if="description" class="settings-item__desc">{{ description }}</div>
      <div v-if="error" class="wallpaper-error">{{ error }}</div>
    </div>

    <!-- ── Row 2: panel opacity ────────────────────────────────── -->
    <div class="settings-item" :class="{ 'settings-item--disabled': !wallpaperHasImage }">
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
          min="0.5"
          max="1"
          step="0.01"
          :disabled="!wallpaperHasImage"
          @input="onOpacityInput"
          @click.stop
        />
        <button v-if="panelOpacity !== 0.85" class="settings-item__slider-reset" @click.stop="resetOpacity" :title="t('settings.items.resetToDefault')">↺</button>
      </div>
      <div class="settings-item__desc">{{ t('settings.items.wallpaperPanelOpacityDesc') }}</div>
    </div>

    <!-- ── Row 3: gaussian blur ────────────────────────────────── -->
    <div class="settings-item" :class="{ 'settings-item--disabled': !wallpaperHasImage }">
      <div class="settings-item__left">
        <div class="settings-item__text">
          <span class="settings-item__label">{{ t('settings.items.wallpaperBlur') }}</span>
        </div>
      </div>
      <div class="settings-item__right">
        <span class="settings-item__slider-value">{{ blurDisplay }}</span>
        <input
          type="range"
          class="settings-item__slider"
          :value="wallpaperBlur"
          min="0"
          max="60"
          step="1"
          :disabled="!wallpaperHasImage"
          @input="onBlurInput"
          @click.stop
        />
        <button v-if="wallpaperBlur !== 0" class="settings-item__slider-reset" @click.stop="resetBlur" :title="t('settings.items.resetToDefault')">↺</button>
      </div>
      <div class="settings-item__desc">{{ t('settings.items.wallpaperBlurDesc') }}</div>
    </div>

    <!-- ── Row 4: edge fade ────────────────────────────────────── -->
    <div class="settings-item" :class="{ 'settings-item--disabled': !wallpaperHasImage }">
      <div class="settings-item__left">
        <div class="settings-item__text">
          <span class="settings-item__label">{{ t('settings.items.wallpaperEdgeFade') }}</span>
        </div>
      </div>
      <div class="settings-item__right">
        <label class="settings-item__switch" :class="{ 'settings-item__switch--disabled': !wallpaperHasImage }">
          <input
            type="checkbox"
            class="settings-item__switch-input"
            :checked="!!wallpaperEdgeFade"
            :disabled="!wallpaperHasImage"
            @change="onEdgeFadeChange"
            @click.stop
          />
          <span class="settings-item__switch-track"></span>
        </label>
      </div>
      <div class="settings-item__desc">{{ t('settings.items.wallpaperEdgeFadeDesc') }}</div>
    </div>
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
const { serverConfig, localConfig, loadConfig, patchConfig, setLocalConfig } = useSettingsConfig()

const fileInputRef = ref<HTMLInputElement | null>(null)
const busy = ref(false)
const error = ref('')

/** Wallpaper tri-state from the live server config. */
const state = computed<WallpaperState>(() =>
  resolveWallpaperState(serverConfig.value?.appearance as Record<string, unknown> | undefined)
)

/** Whether a wallpaper image is actually set (enables the per-option rows). */
const wallpaperHasImage = computed(() => state.value === 'set')

/** Panel opacity from config (0.5..1.0). */
const panelOpacity = computed(() => resolvePanelOpacity(serverConfig.value?.appearance as Record<string, unknown> | undefined))

const opacityDisplay = computed(() => `${Math.round(panelOpacity.value * 100)}%`)

/** Gaussian blur radius (px, local pref) + edge-fade switch (local pref). */
const wallpaperBlur = computed(() => Number(localConfig.wallpaperBlur || 0))
const wallpaperEdgeFade = computed(() => !!localConfig.wallpaperEdgeFade)
const blurDisplay = computed(() => (wallpaperBlur.value > 0 ? `${wallpaperBlur.value}px` : '0'))

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

/** Local display prefs change instantly (live preview) and persist debounced.
 *  setLocalConfig writes localStorage + the reactive singleton; App.vue's
 *  watcher on [localConfig.wallpaperBlur, wallpaperEdgeFade] re-applies the
 *  effect for the live preview. */
function onBlurInput(e: Event) {
  const v = Number((e.target as HTMLInputElement).value)
  const clamped = Math.min(60, Math.max(0, Math.round(v)))
  localConfig.wallpaperBlur = clamped // instant preview
  if (blurSaveTimer) clearTimeout(blurSaveTimer)
  blurSaveTimer = setTimeout(() => {
    setLocalConfig('wallpaperBlur', clamped)
  }, 250)
}

function resetBlur() {
  localConfig.wallpaperBlur = 0
  setLocalConfig('wallpaperBlur', 0)
  if (blurSaveTimer) clearTimeout(blurSaveTimer)
}

function onEdgeFadeChange(e: Event) {
  const checked = (e.target as HTMLInputElement).checked
  setLocalConfig('wallpaperEdgeFade', checked)
}

let opacitySaveTimer: ReturnType<typeof setTimeout> | null = null
let blurSaveTimer: ReturnType<typeof setTimeout> | null = null

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
  if (blurSaveTimer) clearTimeout(blurSaveTimer)
})
</script>

<style scoped>
/* ── Row layout aligned with the standard SettingsItem rows ────────────
   WallpaperSetting deliberately hand-rolls the SettingsItem row classes so the
   wallpaper row can host a custom right side (thumb + buttons) that the shared
   SettingsItem does not support. The rules below mirror SettingsItem.vue's
   scoped styles so the rows look identical to the rest of the settings list. */
.wallpaper-setting {
  background: transparent;
}

.wallpaper-setting > .settings-item {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
  padding: 8px 16px;
  min-height: 0;
  background: transparent;
  position: relative;
}

/* Divider between rows (last row has none). */
.wallpaper-setting > .settings-item::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 16px;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}
.wallpaper-setting > .settings-item:last-child::after {
  display: none;
}

/* Busy / unavailable rows fade out like standard disabled rows. */
.wallpaper-setting > .settings-item--disabled {
  opacity: 0.5;
  pointer-events: none;
}

.settings-item__left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 1;
  min-width: 0;
}

.settings-item__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.settings-item__label {
  font-size: 15px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-item__right {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

/* Description folds onto its own line below label+control (flex-wrap). */
.settings-item__desc {
  width: 100%;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.5;
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
.settings-item__slider-reset:active {
  color: var(--accent-color);
}

/* Thumbnail + actions on the wallpaper row */
.wallpaper-thumb-wrap {
  display: flex;
  align-items: center;
}

.wallpaper-thumb {
  width: 44px;
  height: 30px;
  border-radius: 8px;
  object-fit: cover;
  border: 1px solid var(--border-color);
  margin-right: 4px;
}

.wallpaper-thumb--empty {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  color: var(--text-muted);
}

.settings-item__action {
  padding: 7px 14px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
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
.settings-item__action:active {
  opacity: 0.85;
}
.settings-item__action:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.wallpaper-file-input {
  display: none;
}

.wallpaper-error {
  width: 100%;
  font-size: 12px;
  color: var(--color-red);
  line-height: 1.4;
  word-break: break-word;
}

/* iOS-style switch (mirrors SettingsItem) */
.settings-item__switch {
  position: relative;
  display: inline-block;
  width: 51px;
  height: 31px;
  cursor: pointer;
}

.settings-item__switch-input {
  opacity: 0;
  width: 0;
  height: 0;
  position: absolute;
}

.settings-item__switch-track {
  position: absolute;
  inset: 0;
  border-radius: 15.5px;
  background: var(--bg-tertiary);
  transition: background 0.2s ease;
}
.settings-item__switch-track::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 27px;
  height: 27px;
  border-radius: 50%;
  background: var(--bg-primary);
  transition: transform 0.2s ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
}
.settings-item__switch-input:checked + .settings-item__switch-track {
  background: var(--accent-color);
}
.settings-item__switch-input:checked + .settings-item__switch-track::after {
  transform: translateX(20px);
}
</style>
