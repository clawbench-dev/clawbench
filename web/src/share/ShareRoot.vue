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
    <div v-if="loading" class="share-root-hint">
      <LoadingIndicator size="md" />
    </div>

    <div v-else-if="error" class="share-root-error">
      <FileX2 :size="40" />
      <div class="share-root-error-title">{{ t('share.invalidTitle') }}</div>
      <div class="share-root-error-desc">{{ error }}</div>
    </div>

    <SessionShareView v-else-if="kind === 'session'" />
    <ShareView v-else-if="kind === 'file'" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileX2 } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ShareView from './ShareView.vue'
import SessionShareView from './SessionShareView.vue'
import { setShareToken, shareApiUrl } from './shareMode'

const { t } = useI18n()

const loading = ref(true)
const error = ref('')
const kind = ref<'file' | 'session' | ''>('')

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
    kind.value = data.kind === 'session' ? 'session' : 'file'
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
  padding: 32px;
  color: var(--text-muted, #656d76);
  text-align: center;
}

.share-root-error-title {
  font-size: var(--font-size-lg, 16px);
  font-weight: 600;
  color: var(--text-primary, #1f2328);
}
</style>
