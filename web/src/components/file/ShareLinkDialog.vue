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
        <button class="fbtn fbtn-primary" :disabled="creating" @click="createLink">
          <Link2 :size="14" />
          {{ creating ? t('common.loading') : t('shareDialog.generate') }}
        </button>
      </template>
      <template v-else>
        <a
          class="fbtn"
          :href="linkUrl"
          target="_blank"
          rel="noopener noreferrer"
          :title="t('shareDialog.openPage')"
        >
          <ExternalLink :size="14" />
          {{ t('shareDialog.openPage') }}
        </a>
        <button class="fbtn fbtn-danger" @click="revokeLink">
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
// Shared notice + link-bar chrome. Imported (not global) so only the two share
// dialogs load it; see the file header for why it cannot be scoped.
import '@/assets/share-dialog.css'

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

/* ── File identity: prominent name over a muted full path ── */
.share-dialog-file-block {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
  padding-bottom: var(--space-1);
}
.share-dialog-file-name {
  font-size: var(--font-size-xl);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1f2328);
  line-height: var(--line-height-snug);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.share-dialog-file-path {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #8b949e);
  font-family: var(--font-mono);
  line-height: var(--line-height-normal);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

</style>
