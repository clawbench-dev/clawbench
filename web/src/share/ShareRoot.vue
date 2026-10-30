<template>
  <!--
    Root dispatcher for the public share SPA (/share/{token}).

    A token addresses either a shared FILE or a shared CONVERSATION. The URL
    cannot tell them apart, so the kind is resolved from /api/share/{token}/meta
    before mounting the matching view. Keeping the two views as separate
    components (rather than branching inside one) means the file view stays
    exactly as it was and the session view is free to own the chat render chain.
  -->
  <div class="share-root">
    <div v-if="loading" class="share-root-hint share-root-hint--skeleton">
      <PanelSkeleton variant="text" :rows="8" :label="t('common.loading')" />
    </div>

    <div v-else-if="error" class="share-root-error">
      <FileX2 :size="40" />
      <div class="share-root-error-title">{{ t('share.invalidTitle') }}</div>
      <div class="share-root-error-desc">{{ error }}</div>
    </div>

    <LiveShareView v-else-if="kind === 'live'" />
    <SessionShareView v-else-if="kind === 'session'" />
    <ShareView v-else-if="kind === 'file'" />

    <!-- Download progress. Floats under the share topbar (44px, taller than the
         app's --header-height, hence the offset override) and is out of flow.
         The share SPA builds its own token-scoped download URLs, so it mounts
         the bar itself rather than relying on App.vue, which this entry does
         not use. -->
    <!-- Confirmation dialogs. The share SPA does not use App.vue, so it must
         mount this host itself — `useDialog()` is a module-level store with no
         renderer of its own. Without it, `confirm()` sets `visible` and returns
         a promise nothing ever resolves: the tap produces no dialog, no
         navigation, and a permanently pending promise. External links in a
         shared document go through that path (externalLinkClick.ts), so this
         host is what makes them tappable at all. -->
    <DialogOverlay />

    <DownloadProgressBar
      :visible="downloadVisible"
      :file-name="downloadFileName"
      :received="downloadReceived"
      :total="downloadTotal"
      :style="{ '--transfer-progress-top': 'calc(44px + var(--space-2, 4px))' }"
      @cancel="cancelDownload"
    />

    <!-- Full-screen table view. The share SPA does not use App.vue, so it must
         mount this host itself — `useBlockLandscape()` is module-level state
         with no renderer of its own. -->
    <BlockLandscape />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileX2 } from 'lucide-vue-next'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import DownloadProgressBar from '@/components/common/DownloadProgressBar.vue'
import DialogOverlay from '@/components/common/DialogOverlay.vue'
import BlockLandscape from '@/components/common/BlockLandscape.vue'
import ShareView from './ShareView.vue'
import LiveShareView from './LiveShareView.vue'
import SessionShareView from './SessionShareView.vue'
import { setShareToken, shareApiUrl } from './shareMode'
import { useDownloadProgress } from '@/composables/useDownloadProgress'

const { t } = useI18n()

const { downloadVisible, downloadFileName, downloadReceived, downloadTotal, cancelDownload } = useDownloadProgress()

const loading = ref(true)
const error = ref('')
const kind = ref<'file' | 'session' | 'live' | ''>('')

/** Extract the capability token from /share/{token}. */
function parseTokenFromPath(): string {
  const m = location.pathname.match(/^\/share\/([^/]+)\/?$/)
  return m ? decodeURIComponent(m[1]) : ''
}

onMounted(async () => {
  const token = parseTokenFromPath()
  if (!token) {
    error.value = t('share.invalidUrl')
    loading.value = false
    return
  }
  // The child views build token-scoped URLs, so the token must be installed
  // before they mount.
  setShareToken(token)
  try {
    const resp = await fetch(shareApiUrl('meta'))
    if (!resp.ok) {
      error.value = t('share.notFound')
      return
    }
    const data = await resp.json()
    // Three kinds now. Anything unrecognized falls back to 'file' — the
    // pre-existing behaviour — so an older server cannot break this page.
    kind.value =
      data.kind === 'session' ? 'session' : data.kind === 'live' ? 'live' : 'file'
  } catch {
    error.value = t('share.notFound')
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.share-root {
  height: 100%;
}

.share-root-hint,
.share-root-error {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-4);
  height: 100%;
  padding: calc(32 * var(--dp));
  color: var(--text-muted, #656d76);
  text-align: center;
}

.share-root-error-title {
  font-size: var(--font-size-lg, 16px);
  font-weight: 600;
  color: var(--text-primary, #1f2328);
}

/* The placeholder fills the width and left-aligns; the error state stays
   centred. */
.share-root-hint--skeleton {
  align-items: stretch;
  justify-content: flex-start;
  text-align: left;
}
</style>
