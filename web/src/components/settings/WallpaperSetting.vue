<template>
  <div class="wallpaper-setting">
    <!-- ── Row 1: global switch ────────────────────────────────── -->
    <div class="settings-item">
      <div class="settings-item__left">
        <div class="settings-item__text">
          <span class="settings-item__label">{{ t('settings.items.wallpaperEnable') }}</span>
        </div>
      </div>
      <div class="settings-item__right">
        <label class="settings-item__switch">
          <input
            type="checkbox"
            class="settings-item__switch-input"
            :checked="enabled"
            :disabled="busy"
            @change="onEnabledChange"
            @click.stop
          />
          <span class="settings-item__switch-track"></span>
        </label>
      </div>
      <div class="settings-item__desc">{{ t('settings.items.wallpaperEnableDesc') }}</div>
    </div>

    <!-- ── Row 2: source mode ──────────────────────────────────── -->
    <div class="settings-item" :class="{ 'settings-item--disabled': !enabled || busy }">
      <div class="settings-item__left">
        <div class="settings-item__text">
          <span class="settings-item__label">{{ t('settings.items.wallpaperSource') }}</span>
        </div>
      </div>
      <div class="settings-item__right">
        <div class="wallpaper-mode">
          <button
            class="wallpaper-mode__btn"
            :class="{ 'wallpaper-mode__btn--active': mode === 'local' }"
            :disabled="!enabled || busy"
            @click.stop="onSelectMode('local')"
          >
            {{ t('settings.items.wallpaperModeLocal') }}
          </button>
          <button
            class="wallpaper-mode__btn"
            :class="{ 'wallpaper-mode__btn--active': mode === 'bing' }"
            :disabled="!enabled || busy"
            @click.stop="onSelectMode('bing')"
          >
            {{ t('settings.items.wallpaperModeBing') }}
          </button>
        </div>
      </div>
      <div class="settings-item__desc">{{ t('settings.items.wallpaperSourceDesc') }}</div>
    </div>

    <!-- ── Bing section ────────────────────────────────────────── -->
    <template v-if="mode === 'bing'">
      <div class="settings-item" :class="{ 'settings-item--disabled': !enabled || busy }">
        <div class="settings-item__left">
          <div class="settings-item__text">
            <span class="settings-item__label">{{ t('settings.items.wallpaperBingFollow') }}</span>
          </div>
        </div>
        <div class="settings-item__right">
          <button
            class="settings-item__action"
            :disabled="!enabled || busy || syncing"
            @click.stop="onSyncBing"
          >
            {{ syncing ? t('settings.items.wallpaperBingSyncing') : t('settings.items.wallpaperBingSync') }}
          </button>
        </div>
        <div class="settings-item__desc">{{ t('settings.items.wallpaperBingFollowDesc') }}</div>
      </div>

      <div class="settings-item" :class="{ 'settings-item--disabled': !enabled }">
        <div class="settings-item__left">
          <div class="settings-item__text">
            <span class="settings-item__label">{{ t('settings.items.wallpaperBingStatus') }}</span>
            <span v-if="bingStatus.last_success_date" class="settings-item__meta">
              {{ t('settings.items.wallpaperBingSyncedAt', { date: bingStatus.last_success_date }) }}
            </span>
            <span v-else class="settings-item__meta">{{ t('settings.items.wallpaperBingNoImage') }}</span>
            <span v-if="bingStatus.title" class="settings-item__meta">{{ bingStatus.title }}</span>
            <!-- Photographer credit: required attribution, shown in the panel only. -->
            <span v-if="bingStatus.copyright" class="wallpaper-credit">{{ bingStatus.copyright }}</span>
          </div>
        </div>
        <div v-if="bingStatus.file" class="wallpaper-thumb-wrap">
          <img :src="galleryImageUrl(bingStatus.file, bingStatus.abs_path)" class="wallpaper-thumb" :alt="bingStatus.title || t('settings.items.wallpaperPreview')" />
        </div>
      </div>

      <div v-if="bingStatus.last_error" class="settings-item">
        <div class="wallpaper-error">{{ t('settings.items.wallpaperBingFailed') }}: {{ bingStatus.last_error }}</div>
      </div>
    </template>

    <!-- ── Local gallery section ───────────────────────────────── -->
    <template v-else>
      <div class="settings-item" :class="{ 'settings-item--disabled': !enabled || busy }">
        <div class="settings-item__left">
          <div class="settings-item__text">
            <span class="settings-item__label">{{ t('settings.items.wallpaperGallery') }}</span>
            <span class="settings-item__meta">{{ t('settings.items.wallpaperGalleryCount', { count: galleryItems.length, max: maxGalleryItems }) }}</span>
          </div>
        </div>
        <div class="settings-item__right">
          <button class="settings-item__action settings-item__action--primary" :disabled="!enabled || busy || atLimit" @click.stop="triggerUpload">
            {{ t('settings.items.wallpaperGalleryUpload') }}
          </button>
          <input
            ref="fileInputRef"
            type="file"
            multiple
            accept=".png,.jpg,.jpeg,.gif,.webp,.svg,image/png,image/jpeg,image/gif,image/webp,image/svg+xml"
            class="wallpaper-file-input"
            @change="onFilesSelected"
          />
        </div>
        <div class="settings-item__desc">{{ t('settings.items.wallpaperGalleryDesc') }}</div>
      </div>

      <div class="settings-item settings-item--gallery">
        <div v-if="galleryItems.length === 0" class="wallpaper-gallery-empty">
          {{ t('settings.items.wallpaperGalleryEmpty') }}
        </div>
        <div v-else class="wallpaper-gallery">
          <div
            v-for="item in galleryItems"
            :key="item.file"
            class="wallpaper-gallery__item"
            :class="{ 'wallpaper-gallery__item--active': item.file === selected }"
            :title="item.name"
          >
            <img
              :src="galleryImageUrl(item.file, item.abs_path)"
              class="wallpaper-gallery__thumb"
              :alt="item.name"
              @click="onSelectItem(item.file)"
            />
            <button
              class="wallpaper-gallery__delete"
              :disabled="busy"
              :title="t('settings.items.wallpaperGalleryDelete')"
              @click.stop="onDeleteItem(item.file)"
            >
              ×
            </button>
            <span v-if="item.file === selected" class="wallpaper-gallery__badge">✓</span>
          </div>
        </div>
        <div v-if="atLimit" class="settings-item__desc">{{ t('settings.items.wallpaperGalleryLimit', { max: maxGalleryItems }) }}</div>
      </div>
    </template>

    <!-- ── Display options ─────────────────────────────────────── -->
    <div class="settings-item" :class="{ 'settings-item--disabled': !hasActiveWallpaper }">
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
          :disabled="!hasActiveWallpaper"
          @input="onOpacityInput"
          @click.stop
        />
        <button v-if="panelOpacity !== 0.85" class="settings-item__slider-reset" @click.stop="resetOpacity" :title="t('settings.resetToDefault')">↺</button>
      </div>
      <div class="settings-item__desc">{{ t('settings.items.wallpaperPanelOpacityDesc') }}</div>
    </div>

    <div class="settings-item" :class="{ 'settings-item--disabled': !hasActiveWallpaper }">
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
          :disabled="!hasActiveWallpaper"
          @input="onBlurInput"
          @click.stop
        />
        <button v-if="wallpaperBlur !== 0" class="settings-item__slider-reset" @click.stop="resetBlur" :title="t('settings.resetToDefault')">↺</button>
      </div>
      <div class="settings-item__desc">{{ t('settings.items.wallpaperBlurDesc') }}</div>
    </div>

    <div class="settings-item" :class="{ 'settings-item--disabled': !hasActiveWallpaper }">
      <div class="settings-item__left">
        <div class="settings-item__text">
          <span class="settings-item__label">{{ t('settings.items.wallpaperEdgeFade') }}</span>
        </div>
      </div>
      <div class="settings-item__right">
        <label class="settings-item__switch" :class="{ 'settings-item__switch--disabled': !hasActiveWallpaper }">
          <input
            type="checkbox"
            class="settings-item__switch-input"
            :checked="!!wallpaperEdgeFade"
            :disabled="!hasActiveWallpaper"
            @change="onEdgeFadeChange"
            @click.stop
          />
          <span class="settings-item__switch-track"></span>
        </label>
      </div>
      <div class="settings-item__desc">{{ t('settings.items.wallpaperEdgeFadeDesc') }}</div>
    </div>

    <div v-if="error" class="settings-item">
      <div class="wallpaper-error">{{ error }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'
import {
  uploadGalleryImages,
  deleteGalleryItem,
  selectGalleryItem,
  setWallpaperMode,
  syncBingNow,
  fetchBingStatus,
  galleryImageUrl,
  invalidateGalleryImageUrls,
  resolveWallpaperState,
  resolveWallpaperMode,
  resolveWallpaperEnabled,
  resolveGalleryItems,
  resolveGallerySelected,
  resolveBingStatus,
  type BingStatus,
  resolvePanelOpacity,
  applyWallpaper,
  applyWallpaperScrim,
  currentThemeIsDark,
  bingMktForLocale,
  type WallpaperMode,
} from '@/utils/themeBackground'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { appLog } from '@/utils/appLog'

defineProps<{ description?: string }>()

const { t } = useI18n()
const toast = useToast()
const { serverConfig, localConfig, loadConfig, patchConfig, setLocalConfig } = useSettingsConfig()

/** Matches the server-side cap in internal/wallpaper (MaxGalleryItems). */
const maxGalleryItems = 50
/** Matches the server-side per-request cap (MaxUploadsPerRequest). */
const maxUploadsPerRequest = 10

const fileInputRef = ref<HTMLInputElement | null>(null)
const busy = ref(false)
const syncing = ref(false)
const error = ref('')

// Set on unmount so a detached background refresh stops before touching state
// that no longer has a component behind it.
let unmounted = false

const appearance = computed(() => serverConfig.value?.appearance as Record<string, unknown> | undefined)

/** Wallpaper tri-state from the live server config. */
const state = computed(() => resolveWallpaperState(appearance.value))
const enabled = computed(() => resolveWallpaperEnabled(appearance.value))
const mode = computed<WallpaperMode>(() => resolveWallpaperMode(appearance.value))
const galleryItems = computed(() => resolveGalleryItems(appearance.value))
const selected = computed(() => resolveGallerySelected(appearance.value))
const bingStatus = computed(() => resolveBingStatus(appearance.value))

const atLimit = computed(() => galleryItems.value.length >= maxGalleryItems)

/** Whether any wallpaper is actually displayed (drives the display rows). */
const hasActiveWallpaper = computed(() => state.value === 'set' && enabled.value)

/** Panel opacity from config (0.5..1.0). */
const panelOpacity = computed(() => resolvePanelOpacity(appearance.value))
const opacityDisplay = computed(() => `${Math.round(panelOpacity.value * 100)}%`)

/** Gaussian blur radius (px, local pref) + edge-fade switch (local pref). */
const wallpaperBlur = computed(() => Number(localConfig.wallpaperBlur || 0))
const wallpaperEdgeFade = computed(() => !!localConfig.wallpaperEdgeFade)
const blurDisplay = computed(() => (wallpaperBlur.value > 0 ? `${wallpaperBlur.value}px` : '0'))

/** Re-apply the wallpaper effect with the current theme. */
function refreshEffect() {
  applyWallpaper(
    (appearance.value?.active_file as string) ?? '',
    resolvePanelOpacity(appearance.value),
    currentThemeIsDark(String(localConfig.theme ?? 'auto')),
  )
}

/**
 * Re-sync from the server after a successful write so rows + effect agree.
 * Thumbnail URLs are invalidated for files that no longer exist (they were
 * deleted) — the cached URL for a still-present file stays valid, which is what
 * keeps a re-render from re-downloading the whole gallery.
 */
async function reloadFromServer() {
  const before = galleryItems.value.map((it) => it.file)
  await loadConfig()
  const after = new Set(galleryItems.value.map((it) => it.file))
  const removed = before.filter((f) => !after.has(f))
  if (removed.length > 0) invalidateGalleryImageUrls(removed)
  refreshEffect()
}

/** Keep the persisted Bing market in step with the UI language. */
async function syncBingMktWithLocale() {
  const desired = bingMktForLocale(String(localConfig.locale ?? 'zh'))
  if (bingStatus.value.mkt === desired) return
  try {
    await patchConfig({ appearance: { bing: { mkt: desired } } })
  } catch {
    // A failed market sync must not surface as an error — the next fetch falls
    // back to the server default.
    appLog.w('Wallpaper', `failed to sync bing mkt to ${desired}`)
  }
}

function triggerUpload() {
  error.value = ''
  fileInputRef.value?.click()
}

async function onFilesSelected(e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = '' // allow re-selecting the same files
  if (files.length === 0) return

  if (files.length > maxUploadsPerRequest) {
    error.value = t('settings.items.wallpaperUploadTooMany', { max: maxUploadsPerRequest })
    return
  }
  if (galleryItems.value.length + files.length > maxGalleryItems) {
    error.value = t('settings.items.wallpaperGalleryLimit', { max: maxGalleryItems })
    return
  }

  busy.value = true
  error.value = ''
  try {
    const result = await uploadGalleryImages(files)
    await reloadFromServer()
    if (result.errors.length > 0) {
      error.value = t('settings.items.wallpaperUploadPartial', {
        ok: result.items.length,
        failed: result.errors.length,
      })
    } else {
      toast.show(t('settings.items.wallpaperSetOk'), { icon: '🖼️', type: 'success', duration: 2500 })
    }
  } catch {
    error.value = t('settings.items.wallpaperUploadFailed')
  } finally {
    busy.value = false
  }
}

async function onDeleteItem(name: string) {
  busy.value = true
  error.value = ''
  try {
    await deleteGalleryItem(name)
    await reloadFromServer()
  } catch {
    error.value = t('settings.items.wallpaperRemoveFailed')
  } finally {
    busy.value = false
  }
}

async function onSelectItem(name: string) {
  if (name === selected.value) return
  busy.value = true
  error.value = ''
  try {
    await selectGalleryItem(name)
    await reloadFromServer()
  } catch {
    error.value = t('settings.items.wallpaperSetFailed')
  } finally {
    busy.value = false
  }
}

async function onSelectMode(next: 'local' | 'bing') {
  if (next === mode.value) return
  busy.value = true
  error.value = ''
  try {
    await setWallpaperMode({ mode: next })
    await reloadFromServer()
  } catch {
    error.value = t('settings.items.wallpaperSaveFailed')
  } finally {
    // Release the UI as soon as the switch itself is done. The switch is a
    // single fast config write; waiting for the image download here held the
    // whole panel disabled for up to the poll budget, which is what made
    // changing the source feel stuck.
    busy.value = false
  }
  if (next === 'bing') {
    // Follow the fetch in the background: the preview fills in on its own once
    // the image lands, and the user can keep interacting meanwhile.
    void followBingFetch()
  }
}

/** In-flight guard so rapid source toggles cannot stack overlapping polls. */
let followingBing = false

/**
 * Watch a Bing fetch to completion without blocking the UI, then refresh the
 * preview. Runs detached from the caller so no click handler awaits it.
 *
 * Does nothing when an image is already cached for today: the server skips the
 * fetch in that case, so polling would spin until the budget expired for a
 * result that was never coming.
 */
async function followBingFetch() {
  if (followingBing) return
  followingBing = true
  try {
    await syncBingMktWithLocale()
    if (bingStatus.value.last_success_date === todayStamp()) return
    await pollBingUntilSettled()
    if (unmounted) return
    await reloadFromServer()
  } catch {
    // A background refresh failure must not surface as an error; the next
    // scheduled fetch or a manual 获取 recovers.
    appLog.w('Wallpaper', 'background bing refresh failed')
  } finally {
    followingBing = false
  }
}

async function onEnabledChange(e: Event) {
  const checked = (e.target as HTMLInputElement).checked
  busy.value = true
  error.value = ''
  try {
    await setWallpaperMode({ enabled: checked })
    await reloadFromServer()
    if (checked && mode.value === 'bing') await syncBingMktWithLocale()
  } catch {
    error.value = t('settings.items.wallpaperSaveFailed')
  } finally {
    busy.value = false
  }
}

/**
 * Poll until the Bing fetch settles or the wait budget runs out.
 * Returns whether it settled (today's image present or an error reported).
 */
async function pollBingUntilSettled(): Promise<{ status: BingStatus; settled: boolean }> {
  const deadline = Date.now() + 15000
  let status = bingStatus.value
  let settled = status.last_success_date === todayStamp()
  while (Date.now() < deadline && !settled && !unmounted) {
    await new Promise((r) => setTimeout(r, 1000))
    if (unmounted) break
    status = await fetchBingStatus()
    settled = !!status.last_error || status.last_success_date === todayStamp()
  }
  return { status, settled }
}

/** Trigger a Bing fetch and poll until it settles or the wait budget runs out. */
async function onSyncBing() {
  syncing.value = true
  error.value = ''
  try {
    await syncBingMktWithLocale()
    await syncBingNow()
    // The fetch runs in the background worker; poll for the outcome. A slow
    // fetch may outlast the budget, in which case the result is genuinely
    // unknown — report that rather than claiming success.
    const { status, settled } = await pollBingUntilSettled()
    await reloadFromServer()
    if (status.last_error) {
      error.value = t('settings.items.wallpaperBingFailed')
    } else if (settled) {
      toast.show(t('settings.items.wallpaperBingSynced'), { icon: '🌅', type: 'success', duration: 2500 })
    } else {
      // Still fetching server-side; the image will appear once it lands.
      toast.show(t('settings.items.wallpaperBingPending'), { icon: '⏳', type: 'info', duration: 3000 })
    }
  } catch {
    error.value = t('settings.items.wallpaperBingFailed')
  } finally {
    syncing.value = false
  }
}

/** Today as yyyymmdd, matching the server's LastSuccessDate format. */
function todayStamp(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}`
}

function onOpacityInput(e: Event) {
  const v = Number((e.target as HTMLInputElement).value)
  applyWallpaper(
    (appearance.value?.active_file as string) ?? '',
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
  // Follow the UI language for the Bing market once the panel is open.
  if (mode.value === 'bing' && enabled.value) void syncBingMktWithLocale()
})

onUnmounted(() => {
  unmounted = true
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

  /* One source of truth for both preview thumbnails: the Bing preview and the
     gallery tiles must render at the same size, so neither can drift from the
     other. Change these two values to resize both together. */
  --wallpaper-thumb-w: 72px;
  --wallpaper-thumb-h: 54px;
}

.wallpaper-setting > .settings-item {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  padding: var(--space-4) var(--space-7);
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
  opacity: var(--opacity-muted);
  pointer-events: none;
}

.settings-item__left {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-shrink: 1;
  min-width: 0;
}

.settings-item__text {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.settings-item__label {
  font-size: var(--font-size-lg);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-item__meta {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  word-break: break-word;
}

/* Photographer credit for the Bing image — panel-only attribution. */
.wallpaper-credit {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  line-height: var(--line-height-snug);
  word-break: break-word;
}

.settings-item__right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
}

/* Description folds onto its own line below label+control (flex-wrap). */
.settings-item__desc {
  width: 100%;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
  word-break: break-word;
}

.settings-item__slider-value {
  font-size: var(--font-size-md);
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
  font-size: var(--font-size-lg);
  color: var(--text-muted);
  background: none;
  border: none;
  cursor: pointer;
  padding: var(--space-1) var(--space-2);
  line-height: 1;
}
.settings-item__slider-reset:active {
  color: var(--accent-color);
}

/* ── Source mode segmented control ─────────────────────────────── */
.wallpaper-mode {
  display: inline-flex;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.wallpaper-mode__btn {
  padding: var(--space-3) 14px;
  border: none;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
  white-space: nowrap;
}
.wallpaper-mode__btn + .wallpaper-mode__btn {
  border-left: 1px solid var(--border-color);
}
.wallpaper-mode__btn--active {
  background: var(--accent-color);
  color: #fff;
}
.wallpaper-mode__btn:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

/* ── Gallery grid ──────────────────────────────────────────────── */
.settings-item--gallery {
  display: block;
}

/* minmax(shared-width, 1fr) so the columns absorb the leftover width and the
   last column reaches the right edge. A fixed track (repeat(auto-fill, 72px))
   packed from the left and left a ragged gap whenever the panel width was not
   an exact multiple of the tile + gap.
   The shared width stays the *minimum*, so a tile never renders smaller than
   the Bing preview, and because auto-fill picks the largest column count that
   fits, the per-tile stretch is bounded by one tile+gap divided by the count
   (a few px in practice) rather than growing without limit. */
.wallpaper-gallery {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(var(--wallpaper-thumb-w), 1fr));
  gap: var(--space-4);
  width: 100%;
}

.wallpaper-gallery__item {
  position: relative;
  border-radius: var(--radius-sm);
  overflow: hidden;
  aspect-ratio: 4 / 3;
  /* The selection ring is drawn with an inset shadow rather than a border so it
     does not consume layout space: a 2px border would shrink the photo to
     68x50 while the Bing preview renders 72x54, leaving the two previews
     visibly different sizes. */
  box-shadow: inset 0 0 0 2px transparent;
}
.wallpaper-gallery__item--active {
  box-shadow: inset 0 0 0 2px var(--accent-color);
}

.wallpaper-gallery__thumb {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  cursor: pointer;
}

.wallpaper-gallery__delete {
  position: absolute;
  top: 2px;
  right: 2px;
  width: 20px;
  height: 20px;
  border: none;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  font-size: var(--font-size-lg);
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}
.wallpaper-gallery__delete:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

.wallpaper-gallery__badge {
  position: absolute;
  bottom: 2px;
  left: 2px;
  padding:0 var(--space-2);
  border-radius: var(--radius-sm);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-xs);
  line-height: 16px;
}

.wallpaper-gallery-empty {
  width: 100%;
  padding: var(--space-6) 0;
  text-align: center;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}

/* Thumbnail + actions on the wallpaper row */
.wallpaper-thumb-wrap {
  display: flex;
  align-items: center;
}

/* Matches a gallery tile exactly, so the Bing preview and the uploaded-image
   previews read as the same control rather than two different ones. */
.wallpaper-thumb {
  width: var(--wallpaper-thumb-w);
  height: var(--wallpaper-thumb-h);
  border-radius: var(--radius-sm);
  object-fit: cover;
  border: 1px solid var(--border-color);
  margin-right: var(--space-2);
}

.settings-item__action {
  padding: 7px 14px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
  margin-left: var(--space-2);
  white-space: nowrap;
}

.settings-item__action--primary {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
  font-weight: var(--font-weight-medium);
}
.settings-item__action:active {
  opacity: var(--opacity-hover);
}
.settings-item__action:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

.wallpaper-file-input {
  display: none;
}

.wallpaper-error {
  width: 100%;
  font-size: var(--font-size-sm);
  color: var(--color-red);
  line-height: var(--line-height-snug);
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
  border-radius: var(--radius-lg);
  background: var(--bg-tertiary);
  transition: background var(--duration-slow) ease;
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
  transition: transform var(--duration-slow) ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
}
.settings-item__switch-input:checked + .settings-item__switch-track {
  background: var(--accent-color);
}
.settings-item__switch-input:checked + .settings-item__switch-track::after {
  transform: translateX(20px);
}
</style>
