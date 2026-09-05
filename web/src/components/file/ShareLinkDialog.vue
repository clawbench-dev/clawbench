<template>
  <ModalDialog :open="open" :title="t('shareDialog.title')" :z-index="2500" :max-width="520" @close="$emit('close')">
    <div class="share-dialog-body">
      <template v-if="!file">
        <div class="share-dialog-hint">{{ t('shareDialog.noFile') }}</div>
      </template>

      <template v-else-if="busy">
        <div class="share-dialog-hint">{{ t('common.loading') }}</div>
      </template>

      <template v-else-if="errorMsg && !linkUrl">
        <div class="share-dialog-error">{{ errorMsg }}</div>
      </template>

      <template v-else>
        <!-- File identity: prominent name over a muted full path -->
        <div class="share-dialog-file-block">
          <div class="share-dialog-file-name" :title="file.name">{{ file.name }}</div>
          <div class="share-dialog-file-path" :title="file.path">{{ file.path }}</div>
        </div>

        <!-- Info notice with an embedded, icon-less security warning -->
        <div class="share-notice share-notice-info">
          <Info :size="15" class="share-notice-icon" />
          <div class="share-notice-content">
            <span class="share-notice-text">{{ linkUrl ? t('shareDialog.active') : t('shareDialog.explain') }}</span>
            <div class="share-notice-divider" />
            <span class="share-notice-warning">{{ t('shareDialog.securityHint') }}</span>
          </div>
        </div>
      </template>

      <!-- Link bar -->
      <div v-if="linkUrl" class="share-dialog-link-bar">
        <div class="share-dialog-link-input-wrap">
          <input
            ref="linkInputRef"
            class="share-dialog-link-input"
            type="text"
            :value="linkUrl"
            readonly
            spellcheck="false"
            @focus="$event.target.select()"
          />
          <button
            class="share-dialog-link-btn"
            :title="t('shareDialog.copyTip')"
            :aria-label="t('shareDialog.copyTip')"
            @click="copyLink"
          >
            <Copy :size="14" />
          </button>
          <button
            class="share-dialog-link-btn"
            :title="t('shareDialog.regenerateTip')"
            :aria-label="t('shareDialog.regenerateTip')"
            :disabled="creating"
            @click="regenerateLink"
          >
            <RefreshCw v-if="creating" :size="14" class="share-dialog-spin" />
            <RefreshCw v-else :size="14" />
          </button>
        </div>
        <div v-if="errorMsg" class="share-dialog-error">{{ errorMsg }}</div>
      </div>
    </div>

    <!-- Footer actions -->
    <template #footer>
      <template v-if="!file || busy || (errorMsg && !linkUrl)">
        <span />
      </template>
      <template v-else-if="!linkUrl">
        <button class="share-dialog-primary" :disabled="creating" @click="createLink">
          <Link2 :size="14" />
          {{ creating ? t('common.loading') : t('shareDialog.generate') }}
        </button>
      </template>
      <template v-else>
        <a
          class="share-dialog-btn"
          :href="linkUrl"
          target="_blank"
          rel="noopener noreferrer"
          :title="t('shareDialog.openPage')"
        >
          <ExternalLink :size="14" />
          {{ t('shareDialog.openPage') }}
        </a>
        <button class="share-dialog-secondary danger" @click="revokeLink">
          <Trash2 :size="14" />
          {{ t('shareDialog.revoke') }}
        </button>
      </template>
    </template>
  </ModalDialog>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, ExternalLink, Info, Link2, RefreshCw, Trash2 } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast.ts'
import { useFileShare } from '@/composables/useFileShare.ts'
import { copyText } from '@/utils/clipboard.ts'

const props = defineProps({
  open: Boolean,
  file: Object,
})

const { t } = useI18n()
const dialog = useDialog()
const toast = useToast()
const { markShared, markUnshared } = useFileShare()

const busy = ref(false)
const creating = ref(false)
const errorMsg = ref('')
const linkUrl = ref('')
const linkInputRef = ref(null)

// Build the absolute link from the server-returned path.
function toAbsoluteUrl(path) {
  return window.location.origin + path
}

watch(() => props.open, async (isOpen) => {
  if (!isOpen || !props.file?.path) return
  await loadStatus()
}, { immediate: true })

async function loadStatus() {
  busy.value = true
  errorMsg.value = ''
  linkUrl.value = ''
  try {
    const resp = await fetch(`/api/share?path=${encodeURIComponent(props.file.path)}`)
    if (!resp.ok) throw new Error(resp.statusText)
    const data = await resp.json()
    if (data.path) {
      linkUrl.value = toAbsoluteUrl(data.path)
      markShared(props.file.path)
    } else {
      markUnshared(props.file.path)
    }
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function createLink() {
  creating.value = true
  errorMsg.value = ''
  try {
    const resp = await fetch('/api/share', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: props.file.path }),
    })
    if (!resp.ok) {
      const err = await resp.json().catch(() => ({}))
      throw new Error(err.error || resp.statusText)
    }
    const data = await resp.json()
    linkUrl.value = toAbsoluteUrl(data.path)
    markShared(props.file.path)
    copyLink()
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : String(err)
  } finally {
    creating.value = false
  }
}

function copyLink() {
  if (!linkUrl.value) return
  copyText(linkUrl.value, () => {
    toast.show(t('common.copied'), { icon: '✅', type: 'success', duration: 2000 })
  })
}

async function regenerateLink() {
  // Rotating the token invalidates the old link — confirm before acting.
  const confirmed = await dialog.confirm(t('shareDialog.confirmRegenerate'), { dangerous: true })
  if (!confirmed) return
  await createLink()
}

async function revokeLink() {
  const confirmed = await dialog.confirm(t('shareDialog.confirmRevoke'), { dangerous: true })
  if (!confirmed) return
  errorMsg.value = ''
  try {
    const resp = await fetch(`/api/share?path=${encodeURIComponent(props.file.path)}`, { method: 'DELETE' })
    if (!resp.ok) throw new Error(resp.statusText)
    linkUrl.value = ''
    markUnshared(props.file.path)
    toast.show(t('shareDialog.revoked'), { icon: '🔗', type: 'success', duration: 2000 })
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : String(err)
  }
}
</script>

<style scoped>
.share-dialog-body {
  padding: 12px 16px 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.share-dialog-hint {
  font-size: 13px;
  color: var(--text-secondary, #57606a);
  line-height: 1.5;
}
.share-dialog-error {
  font-size: 13px;
  color: #cf222e;
  word-break: break-word;
}

/* ── File identity: prominent name over a muted full path ── */
.share-dialog-file-block {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  padding-bottom: 2px;
}
.share-dialog-file-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary, #1f2328);
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.share-dialog-file-path {
  font-size: 11px;
  color: var(--text-muted, #8b949e);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  line-height: 1.5;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Combined notice: info box with an embedded, icon-less warning row ── */
.share-notice {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 9px 11px;
  border-radius: 8px;
  font-size: 12.5px;
  line-height: 1.55;
}
.share-notice-icon {
  flex-shrink: 0;
  margin-top: 1px;
}
.share-notice-info {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 9%, var(--bg-primary, #fff));
}
:root[data-theme-base='dark'] .share-notice-info {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, var(--bg-primary, #fff));
}
.share-notice-info > .share-notice-icon {
  color: var(--accent-color, #0066cc);
}
.share-notice-content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.share-notice-text {
  color: var(--text-secondary, #57606a);
  word-break: break-word;
}
.share-notice-divider {
  height: 1px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, transparent);
}
.share-notice-warning {
  font-weight: 500;
  color: #b45309;
  word-break: break-word;
}
:root[data-theme-base='dark'] .share-notice-warning {
  color: #fcd34d;
}

/* ── Link bar: full-width row whose input embeds two icon buttons ── */
.share-dialog-link-bar {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.share-dialog-link-input-wrap {
  position: relative;
  display: flex;
  align-items: center;
}
.share-dialog-link-input {
  flex: 1;
  min-width: 0;
  width: 100%;
  padding: 7px 84px 7px 10px; /* right padding clears the two embedded buttons */
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  background: var(--bg-primary);
  color: var(--text-primary);
  outline: none;
}
.share-dialog-link-input:focus {
  border-color: var(--accent-color, #0066cc);
}
.share-dialog-link-btn {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  right: 4px;
  width: 34px;
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--text-secondary, #666);
  cursor: pointer;
}
.share-dialog-link-btn + .share-dialog-link-btn {
  right: 38px;
}
.share-dialog-link-btn:disabled { opacity: 0.5; cursor: default; }
.share-dialog-spin {
  animation: share-dialog-spin 0.8s linear infinite;
}
@keyframes share-dialog-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* ── Buttons (live in the footer now) ── */
.share-dialog-primary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 8px 18px;
  background: var(--accent-color, #0066cc);
  color: #fff;
  border: none;
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
}
.share-dialog-primary:disabled { opacity: 0.6; cursor: default; }
.share-dialog-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 7px 14px;
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
  text-decoration: none;
}
.share-dialog-secondary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 7px 14px;
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
}
.share-dialog-secondary.danger { color: #cf222e; }
@media (hover: hover) {
  .share-dialog-primary:hover { filter: brightness(1.1); }
  .share-dialog-btn:hover, .share-dialog-secondary:hover { background: var(--bg-secondary); }
  .share-dialog-link-btn:hover { background: var(--bg-tertiary, #f0f0f0); color: var(--accent-color, #0066cc); }
  .share-dialog-secondary.danger:hover { background: #fef2f2; }
}
</style>
