<template>
  <ModalDialog :open="open" :title="t('kb.pickTitle')" @close="$emit('close')">
    <template #header>
      <BookOpen :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ t('kb.pickTitle') }}</span>
    </template>

    <div class="kb-pick">
      <div v-if="loading" class="kb-pick-hint">{{ t('common.loading') }}</div>
      <template v-else>
        <label class="kb-pick-field">
          <span class="kb-pick-label">{{ t('kb.pickCollection') }}</span>
          <select v-model="selected" class="kb-pick-select" data-role="kb-pick-collection">
            <option v-for="c in collections" :key="c.id" :value="c.id">{{ c.name }}</option>
            <option v-if="allowCreate" value="__new__">{{ t('kb.pickNew') }}</option>
          </select>
          <span v-if="collections.length === 0 && !allowCreate" class="kb-pick-hint">
            {{ t('kb.pickEmpty') }}
          </span>
        </label>
        <label v-if="selected === '__new__'" class="kb-pick-field">
          <span class="kb-pick-label">{{ t('kb.nameLabel') }}</span>
          <input
            v-model="newName"
            class="kb-pick-input"
            data-role="kb-pick-name"
            maxlength="100"
            :placeholder="t('kb.namePlaceholder')"
            @keydown.enter="confirm"
          />
        </label>
      </template>
      <div v-if="error" class="kb-pick-error" role="alert">{{ error }}</div>
    </div>

    <template #footer>
      <button class="fbtn" type="button" @click="$emit('close')">{{ t('common.cancel') }}</button>
      <button class="fbtn fbtn-primary" type="button" :disabled="busy || loading || !canConfirm" @click="confirm">
        {{ busy ? t('common.loading') : t('kb.pickConfirm') }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { BookOpen } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import {
  kbCreateCollection,
  type KBCollection,
} from '@/composables/useKnowledgeBase'
import { apiGet } from '@/utils/api'
import { store } from '@/stores/app'

const props = withDefaults(defineProps<{
  open: boolean
  /** Prefills the name when the user chooses "new collection" (e.g. a folder name). */
  initialName?: string
  /** Move mode: only existing libraries, no "new" option. */
  allowCreate?: boolean
  /** Hide this library (the one a document is already in). */
  excludeId?: number
}>(), { allowCreate: true })

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm', payload: { id: number; name: string; created: boolean }): void
}>()

const { t } = useI18n()

const collections = ref<KBCollection[]>([])
const selected = ref<number | '__new__' | ''>('')
const newName = ref('')
const loading = ref(false)
const busy = ref(false)
const error = ref('')

const canConfirm = computed(() => {
  if (selected.value === '') return false
  return selected.value !== '__new__' || newName.value.trim().length > 0
})

watch(
  () => props.open,
  (open) => {
    if (open) void load()
  },
  { immediate: true },
)

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  newName.value = props.initialName?.trim() ?? ''
  try {
    const res = await apiGet<{ collections: KBCollection[] }>('/api/kb/collections')
    collections.value = (res.collections ?? []).filter((c) => c.id !== props.excludeId)
    if (collections.value.length === 0) {
      selected.value = props.allowCreate === false ? '' : '__new__'
      return
    }
    // Prefer the collection of the project the user is working in.
    const project = collections.value.find((c) => c.scope === 'project')
    selected.value = (project ?? collections.value[0]).id
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

async function confirm(): Promise<void> {
  if (!canConfirm.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    if (selected.value === '') return
    if (selected.value === '__new__') {
      const scope = store.state.projectRoot ? 'project' : 'global'
      const created = await kbCreateCollection(newName.value.trim(), '', scope)
      emit('confirm', { id: created.id, name: created.name, created: true })
      return
    }
    const target = collections.value.find((c) => c.id === selected.value)
    if (!target) return
    emit('confirm', { id: target.id, name: target.name, created: false })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
/* ModalDialog's body has no padding of its own. */
.kb-pick {
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.kb-pick-hint {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

.kb-pick-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.kb-pick-label {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.kb-pick-select,
.kb-pick-input {
  width: 100%;
  height: calc(34 * var(--dp));
  padding: 0 var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
}

.kb-pick-error {
  color: var(--color-red);
  font-size: var(--font-size-sm);
}
</style>
