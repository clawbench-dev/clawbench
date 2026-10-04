<template>
  <ModalDialog
    :open="open"
    :title="t('settings.items.agentAvatarTitle')"
    :z-index="2500"
    :max-width="520"
    @close="handleClose"
  >
    <div class="avatar-picker__body">
      <LoadingIndicator
        v-if="loading"
        :label="t('settings.items.agentAvatarLoading', { done: loadedCount, total: AVATAR_STYLES.length })"
      />

      <div v-else-if="loadError" class="avatar-picker__error">
        <span>{{ t('settings.items.agentAvatarLoadFailed') }}</span>
        <button class="fbtn" @click="initLib">{{ t('common.retry') }}</button>
      </div>

      <template v-else>
        <!-- Seed controls (global: changing it re-renders every tile) -->
        <div class="avatar-picker__seed-row">
          <label class="avatar-picker__label">{{ t('settings.items.agentAvatarSeed') }}</label>
          <input
            v-model="seed"
            type="text"
            class="avatar-picker__input"
            :placeholder="agentName"
            @keydown.enter="save"
          />
          <button class="fbtn avatar-picker__shuffle" @click="shuffle">
            {{ t('settings.items.agentAvatarShuffle') }}
          </button>
        </div>

        <!-- All styles at once; click a tile to select -->
        <div class="avatar-picker__grid">
          <button
            v-for="s in AVATAR_STYLES"
            :key="s"
            type="button"
            class="avatar-picker__tile"
            :class="{ 'is-selected': s === style }"
            :data-style="s"
            @click="selectStyle(s)"
          >
            <img class="avatar-picker__tile-img" :src="tileSrc(s)" :alt="styleLabel(s)" />
            <span class="avatar-picker__tile-name">{{ styleLabel(s) }}</span>
          </button>
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
        :disabled="loading || !!loadError || !selectedSvg"
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
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import { loadAvatarKit, AVATAR_STYLES, type AvatarStyle, type AvatarKit } from '@/utils/lazyAvatar'
import { appLog } from '@/utils/appLog'

const props = defineProps<{
  open: boolean
  agentName: string
}>()

const emit = defineEmits<{
  close: []
  saved: [svg: string]
}>()

const { t } = useI18n()

const loading = ref(false)
const loadError = ref(false)
const loadedCount = ref(0)
// shallowRef, NOT ref: the kit holds DiceBear `Style` instances that use
// `#private` fields. A deep reactive proxy would wrap them and any access to a
// private member throws "Cannot read private member from an object whose class
// did not declare it". shallowRef keeps the instances untouched.
const kit = shallowRef<AvatarKit | null>(null)
const style = ref<AvatarStyle>('bottts')
const seed = ref('')
const error = ref('')

let unregisterBack: (() => void) | null = null

// All tiles render synchronously off the kit + current seed. `seed` is a
// dependency so changing it recomputes every tile at once — the whole point of
// this grid.
const tileSvgs = computed<Partial<Record<AvatarStyle, string>>>(() => {
  const k = kit.value
  if (!k) return {}
  const s = seed.value || props.agentName || 'agent'
  const out: Partial<Record<AvatarStyle, string>> = {}
  for (const name of AVATAR_STYLES) {
    const styleInstance = k.styles[name]
    if (!styleInstance) continue // still loading
    try {
      out[name] = new k.Avatar(styleInstance, { seed: s }).toString()
    } catch (err) {
      appLog.w('AgentAvatar', `failed to render style ${name}`, err)
    }
  }
  return out
})

function toDataUri(svg: string): string {
  return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg)
}

function tileSrc(s: AvatarStyle): string {
  const svg = tileSvgs.value[s]
  return svg ? toDataUri(svg) : ''
}

const selectedSvg = computed(() => tileSvgs.value[style.value] || '')

function styleLabel(s: string): string {
  return s.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase())
}

function selectStyle(s: AvatarStyle) {
  style.value = s
}

async function initLib() {
  loadError.value = false
  loading.value = true
  loadedCount.value = 0
  kit.value = null
  try {
    // onProgress drives the loading label's done/total counter.
    const loaded = await loadAvatarKit((done) => { loadedCount.value = done })
    kit.value = loaded
  } catch (err) {
    appLog.w('AgentAvatar', 'failed to load DiceBear kit', err)
    loadError.value = true
  } finally {
    loading.value = false
  }
}

function shuffle() {
  seed.value = Math.random().toString(36).slice(2, 10)
}

function save() {
  if (!selectedSvg.value) return
  emit('saved', selectedSvg.value)
}

function clear() {
  emit('saved', '')
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
    loadedCount.value = 0
    kit.value = null
    style.value = 'bottts'
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
  min-height: 120px;
}

.avatar-picker__seed-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-bottom: var(--space-5);
  flex-shrink: 0;
}

.avatar-picker__label {
  font-size: var(--font-size-md);
  color: var(--text-secondary);
  white-space: nowrap;
}

.avatar-picker__input {
  flex: 1;
  min-width: 0;
  height: 30px;
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

.avatar-picker__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(72px, 1fr));
  gap: var(--space-4);
}

.avatar-picker__tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
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
  width: 48px;
  height: 48px;
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
