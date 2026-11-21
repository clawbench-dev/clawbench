<template>
  <ModalDialog
    :open="open"
    :title="t('settings.items.agentAvatarTitle')"
    :z-index="2500"
    size="wide"
    @close="handleClose"
  >
    <div class="avatar-picker__body">
      <!-- The body is a tile grid; preview tiles rather than a centred ring. -->
      <PanelSkeleton v-if="loading" class="avatar-picker__skeleton" variant="grid" :rows="8" :label="t('settings.items.agentAvatarLoading')" />

      <div v-else-if="loadError" class="avatar-picker__error">
        <span>{{ t('settings.items.agentAvatarLoadFailed') }}</span>
        <button class="fbtn" @click="initLib">{{ t('common.retry') }}</button>
      </div>

      <template v-else>
        <!-- Current avatar that could not be matched to a tile. Shown so Save
             cannot silently overwrite it with the default bottts + name. -->
        <div v-if="selection === 'current'" class="avatar-picker__current">
          <img
            class="avatar-picker__current-img"
            :src="currentSrc"
            :style="{ backgroundColor: currentBg }"
            alt=""
          />
          <div class="avatar-picker__current-text">
            <div class="avatar-picker__label">{{ t('settings.items.agentAvatarCurrent') }}</div>
            <div class="avatar-picker__hint">{{ t('settings.items.agentAvatarPickNew') }}</div>
          </div>
        </div>

        <!-- Seed controls (global: changing it re-renders every tile) -->
        <div class="avatar-picker__seed-row">
          <label class="avatar-picker__label">{{ t('settings.items.agentAvatarSeed') }}</label>
          <input
            v-model="seed"
            type="text"
            class="avatar-picker__input"
            :placeholder="agentName"
            @input="markStyleSelection"
            @keydown.enter="save"
          />
          <button class="fbtn avatar-picker__shuffle" @click="shuffle">
            {{ t('settings.items.agentAvatarShuffle') }}
          </button>
        </div>

        <!-- Upload: cropped and resized in the browser, saved as a data URI.
             The original photo never enters the database or a share payload. -->
        <div class="avatar-picker__upload-row">
          <button class="fbtn" :disabled="uploading" @click="fileInput?.click()">
            {{ uploading ? t('settings.items.agentAvatarUploading') : t('settings.items.agentAvatarUpload') }}
          </button>
          <span class="avatar-picker__hint">{{ t('settings.items.agentAvatarUploadHint') }}</span>
          <input
            ref="fileInput"
            class="avatar-picker__file"
            type="file"
            accept="image/png,image/jpeg,image/webp"
            @change="onFileChange"
          />
        </div>
        <div v-if="uploadError" class="avatar-picker__error-text">{{ uploadError }}</div>

        <!-- The uploaded image is one more selectable tile, not a separate mode. -->
        <div v-if="selection === 'upload' && uploaded" class="avatar-picker__uploaded">
          <button type="button" class="avatar-picker__tile is-selected" @click="selectUploaded">
            <img class="avatar-picker__tile-img" :src="uploaded" :alt="t('settings.items.agentAvatarUploaded')" />
            <span class="avatar-picker__tile-name">{{ t('settings.items.agentAvatarUploaded') }}</span>
          </button>
        </div>

        <!-- The selected avatar at the sizes it actually appears in the app. -->
        <div v-if="previewSrc" class="avatar-picker__size-preview">
          <span class="avatar-picker__label">{{ t('settings.items.agentAvatarSizePreview') }}</span>
          <img
            v-for="px in [16, 20, 24]"
            :key="px"
            :src="previewSrc"
            :style="{ width: px + 'px', height: px + 'px' }"
            alt=""
          />
        </div>

        <!-- Grouped styles; click a tile to select -->
        <div v-for="group in AVATAR_STYLE_GROUPS" :key="group.id" class="avatar-picker__group">
          <div class="avatar-picker__group-title">{{ t(`settings.items.agentAvatarGroup.${group.id}`) }}</div>
          <div class="avatar-picker__grid">
            <button
              v-for="s in group.styles"
              :key="s"
              type="button"
              class="avatar-picker__tile"
              :class="{ 'is-selected': selection === 'style' && s === style }"
              :data-style="s"
              @click="selectStyle(s)"
            >
              <img class="avatar-picker__tile-img" :src="tileSrc(s)" :alt="styleLabel(s)" />
              <span class="avatar-picker__tile-name">{{ styleLabel(s) }}</span>
            </button>
          </div>
        </div>

        <div v-if="error" class="avatar-picker__error-text">{{ error }}</div>
      </template>
    </div>

    <template #footer>
      <button class="fbtn avatar-picker__clear" @click="clear">
        {{ t('settings.items.agentAvatarClear') }}
      </button>
      <button class="fbtn" @click="handleClose">
        {{ t('common.cancel') }}
      </button>
      <button
        class="fbtn fbtn-primary"
        :disabled="loading || !!loadError || !canSave"
        @click="save"
      >
        {{ t('settings.items.agentAvatarSave') }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { ref, shallowRef, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import { loadAvatarKit, type AvatarKit } from '@/utils/lazyAvatar'
import { avatarToSrc } from '@/utils/avatarSrc'
import {
  AVATAR_STYLES,
  AVATAR_STYLE_GROUPS,
  avatarBackgroundFor,
  avatarStyleLabelKey,
  type AvatarSaveSpec,
  type AvatarStyle,
} from '@/utils/avatarPolicy'
import { fileToAvatarDataUri, AvatarImageError } from '@/utils/avatarImage'
import { appLog } from '@/utils/appLog'

const props = defineProps<{
  open: boolean
  agentName: string
  /** The avatar currently stored on the agent ("" = built-in icon). */
  currentAvatar?: string
  /** Which style / seed produced it (empty for uploads and legacy avatars). */
  currentStyle?: string
  currentSeed?: string
}>()

const emit = defineEmits<{
  close: []
  saved: [spec: AvatarSaveSpec]
}>()

const { t } = useI18n()

type Selection = 'style' | 'upload' | 'current'

const loading = ref(false)
const loadError = ref(false)
// shallowRef, NOT ref: the kit holds DiceBear `Style` instances that use
// `#private` fields. A deep reactive proxy would wrap them and any access to a
// private member throws "Cannot read private member from an object whose class
// did not declare it". shallowRef keeps the instances untouched.
const kit = shallowRef<AvatarKit | null>(null)
const style = ref<AvatarStyle>('bottts')
const seed = ref('')
const error = ref('')
const selection = ref<Selection>('style')
const uploaded = ref('')
const uploading = ref(false)
const uploadError = ref('')
const fileInput = ref<HTMLInputElement | null>(null)

let unregisterBack: (() => void) | null = null

/** The seed every tile is rendered with. Shared by the grid, the upload
 *  background and the saved provenance so all three agree. */
const effectiveSeed = computed(() => seed.value || props.agentName || 'agent')

// All tiles render synchronously off the kit + current seed. `seed` is a
// dependency so changing it recomputes every tile at once — the whole point of
// this grid.
const tileSvgs = computed<Partial<Record<AvatarStyle, string>>>(() => {
  const k = kit.value
  if (!k) return {}
  const s = effectiveSeed.value
  const out: Partial<Record<AvatarStyle, string>> = {}
  for (const name of AVATAR_STYLES) {
    const styleInstance = k.styles[name]
    if (!styleInstance) continue // still loading
    try {
      out[name] = new k.Avatar(styleInstance, {
        seed: s,
        backgroundColor: [avatarBackgroundFor(s)],
      }).toString()
    } catch (err) {
      appLog.w('AgentAvatar', `failed to render style ${name}`, err)
    }
  }
  return out
})

function tileSrc(s: AvatarStyle): string {
  const svg = tileSvgs.value[s]
  return svg ? avatarToSrc(svg) : ''
}

const currentSrc = computed(() => avatarToSrc(props.currentAvatar || ''))
const currentBg = computed(() => avatarBackgroundFor(props.agentName || 'agent'))

const selectedAvatar = computed(() => {
  if (selection.value === 'upload') return uploaded.value
  if (selection.value === 'style') return tileSvgs.value[style.value] || ''
  return ''
})

const canSave = computed(() => selection.value !== 'current' && !!selectedAvatar.value)

/** Selected avatar at real list sizes, so a tile that only looks good large is
 *  exposed before saving. */
const previewSrc = computed(() => avatarToSrc(
  selection.value === 'current' ? (props.currentAvatar || '') : selectedAvatar.value,
))

function styleLabel(s: string): string {
  return t(avatarStyleLabelKey(s))
}

function selectStyle(s: AvatarStyle) {
  style.value = s
  selection.value = 'style'
  uploaded.value = ''
}

function selectUploaded() {
  selection.value = 'upload'
}

/** Decide what the picker should show for the avatar already stored on the agent. */
function syncCurrentSelection() {
  const current = props.currentAvatar || ''
  if (!current) {
    selection.value = 'style'
    return
  }
  if (current.startsWith('data:image/')) {
    uploaded.value = current
    selection.value = 'upload'
    return
  }
  // Prefer the stored provenance: it tells us exactly which tile/seed produced
  // this image, so the picker can reopen on it instead of guessing.
  const knownStyle = props.currentStyle as AvatarStyle | undefined
  if (knownStyle && AVATAR_STYLES.includes(knownStyle) && props.currentSeed) {
    style.value = knownStyle
    seed.value = props.currentSeed
    selection.value = 'style'
    return
  }
  // Legacy avatar (no provenance): match against the tiles rendered with the
  // default seed. If it does not match, Save must stay disabled until the user
  // makes a new choice — that is the whole point of the guard.
  const match = AVATAR_STYLES.find((name) => tileSvgs.value[name] === current)
  if (match) {
    style.value = match
    selection.value = 'style'
  } else {
    selection.value = 'current'
  }
}

async function initLib() {
  loadError.value = false
  loading.value = true
  kit.value = null
  try {
    kit.value = await loadAvatarKit()
    syncCurrentSelection()
  } catch (err) {
    appLog.w('AgentAvatar', 'failed to load DiceBear kit', err)
    loadError.value = true
  } finally {
    loading.value = false
  }
}

async function onFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  // Clear the input so selecting the same file again still fires `change`.
  input.value = ''
  if (!file) return

  uploadError.value = ''
  uploading.value = true
  try {
    uploaded.value = await fileToAvatarDataUri(file, avatarBackgroundFor(effectiveSeed.value))
    selection.value = 'upload'
  } catch (err) {
    uploaded.value = ''
    uploadError.value = uploadErrorMessage(err)
    selection.value = props.currentAvatar ? 'current' : 'style'
  } finally {
    uploading.value = false
  }
}

function uploadErrorMessage(err: unknown): string {
  if (err instanceof AvatarImageError) {
    if (err.code === 'unsupported') return t('settings.items.agentAvatarUploadUnsupported')
    if (err.code === 'too-large') return t('settings.items.agentAvatarUploadTooLarge')
  }
  return t('settings.items.agentAvatarUploadFailed')
}

function markStyleSelection() {
  // Editing the seed / shuffling is an explicit "make me a new one" action, so
  // it is safe to re-enable Save even when the old avatar could not be matched.
  if (selection.value === 'current') selection.value = 'style'
}

function shuffle() {
  seed.value = Math.random().toString(36).slice(2, 10)
  markStyleSelection()
}

function save() {
  if (!canSave.value) return
  if (selection.value === 'upload') {
    // An uploaded image is its own source: there is no style/seed to reopen.
    emit('saved', { avatar: uploaded.value, style: '', seed: '' })
    return
  }
  emit('saved', {
    avatar: selectedAvatar.value,
    style: style.value,
    seed: effectiveSeed.value,
  })
}

function clear() {
  emit('saved', { avatar: '', style: '', seed: '' })
}

function handleClose() {
  emit('close')
}

// (Re)initialize every time the dialog opens: reset transient state, load the
// kit lazily, then render the grid.
watch(() => props.open, (open) => {
  if (open) {
    error.value = ''
    loadError.value = false
    kit.value = null
    style.value = 'bottts'
    selection.value = 'style'
    uploaded.value = ''
    uploading.value = false
    uploadError.value = ''
    // Prefer a stable seed so re-opening shows the same avatars; fall back to name.
    seed.value = props.agentName || 'agent'
    unregisterBack = registerBackHandler({
      id: 'agent-avatar-picker',
      canGoBack: () => true,
      goBack: () => handleClose(),
      priority: PRIORITY_OVERLAY,
    })
    void initLib()
  } else if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
}, { immediate: true })

onBeforeUnmount(() => {
  if (unregisterBack) { unregisterBack(); unregisterBack = null }
})
</script>

<style scoped>
.avatar-picker__body {
  display: flex;
  flex-direction: column;
  padding: var(--space-5) var(--space-7) var(--space-3);
  min-height: calc(120 * var(--dp));
}

/* The placeholder grid carries its own inset; strip the body's so the tiles
   land where the real ones will. */
.avatar-picker__skeleton {
  margin: calc(var(--space-4) * -1) calc(var(--space-4) * -1);
}

.avatar-picker__seed-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-bottom: var(--space-5);
  flex-shrink: 0;
}

.avatar-picker__current {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-4);
  margin-bottom: var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
}

.avatar-picker__current-img {
  width: calc(40 * var(--dp));
  height: calc(40 * var(--dp));
  border-radius: var(--radius-sm);
  object-fit: contain;
  flex-shrink: 0;
}

.avatar-picker__current-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.avatar-picker__hint {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  line-height: 1.4;
}

.avatar-picker__upload-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
  flex-wrap: wrap;
  flex-shrink: 0;
}

.avatar-picker__file {
  display: none;
}

.avatar-picker__uploaded {
  display: flex;
  justify-content: center;
  margin-bottom: var(--space-4);
}

.avatar-picker__uploaded .avatar-picker__tile {
  width: calc(96 * var(--dp));
}

.avatar-picker__label {
  font-size: var(--font-size-md);
  color: var(--text-secondary);
  white-space: nowrap;
}

.avatar-picker__input {
  flex: 1;
  min-width: 0;
  height: calc(30 * var(--dp));
  padding: 0 var(--space-6);
  font-size: var(--font-size-xl);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  outline: none;
  box-sizing: border-box;
}

.avatar-picker__input:focus {
  border-color: var(--accent-color);
}

.avatar-picker__shuffle {
  flex-shrink: 0;
}

.avatar-picker__group {
  margin-bottom: var(--space-4);
}

.avatar-picker__group-title {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
  margin-bottom: var(--space-3);
}

.avatar-picker__size-preview {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
  padding-bottom: var(--space-4);
  border-bottom: 1px solid var(--border-color);
}

.avatar-picker__size-preview img {
  border-radius: 20%;
  object-fit: contain;
  flex-shrink: 0;
}

.avatar-picker__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(calc(72 * var(--dp)), 1fr));
  gap: var(--space-4);
}

.avatar-picker__tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: calc(4 * var(--dp));
  padding: var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}

.avatar-picker__tile:hover {
  border-color: var(--accent-color);
}

.avatar-picker__tile.is-selected {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 1px var(--accent-color) inset;
  background: color-mix(in srgb, var(--accent-color) 10%, var(--bg-primary));
}

.avatar-picker__tile-img {
  width: calc(48 * var(--dp));
  height: calc(48 * var(--dp));
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  object-fit: contain;
  display: block;
}

.avatar-picker__tile-name {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  text-align: center;
  line-height: 1.2;
  overflow-wrap: anywhere;
}

.avatar-picker__error,
.avatar-picker__error-text {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  font-size: var(--font-size-md);
  color: #e74c3c;
  padding: var(--space-4) var(--space-6);
  background: rgba(231, 76, 60, 0.1);
  border-radius: var(--radius-sm);
}

.avatar-picker__error-text {
  margin-top: var(--space-4);
}

.avatar-picker__clear {
  margin-right: auto;
}
</style>
