<template>
  <div class="overview-card">
    <h3 class="card-title">
      <Mail class="card-icon" :size="14" />
      <span class="card-title-text">{{ t('task.mail.title') }}</span>
    </h3>

    <div class="overview-row">
      <div class="overview-label">{{ t('task.mail.account') }}</div>
      <div class="overview-value">{{ accountLabel }}</div>
    </div>
    <div class="overview-row">
      <div class="overview-label">{{ t('task.mail.folder') }}</div>
      <div class="overview-value">{{ folderLabel }}</div>
    </div>

    <div class="overview-row">
      <div class="overview-label">{{ t('task.mail.gates') }}</div>
      <div class="overview-value">
        <div v-if="gateChips.length === 0" class="muted">{{ t('task.mail.noGates') }}</div>
        <div v-else class="mail-gate-chips">
          <span v-for="g in gateChips" :key="g" class="mail-gate-chip">{{ g }}</span>
        </div>
      </div>
    </div>

    <div class="mail-card-hint">{{ t('task.mail.hint') }}</div>
  </div>
</template>

<script setup lang="ts">
/**
 * Overview card for an email-triggered task.
 *
 * Separate from TaskScheduleCard / TaskEventCard for the same reason those two
 * are separate: almost no field is shared. Rendering the cron card here would
 * show an empty schedule and "next run: none", which is not what the task does.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Mail } from 'lucide-vue-next'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'

const props = defineProps<{ task: Record<string, unknown> }>()
const { t } = useI18n()

const accounts = ref<Array<{ id: number; name: string; email: string }>>([])
const folders = ref<Array<{ path: string; name: string }>>([])

const accountId = computed(() => Number(props.task.mailAccountId) || 0)
const folderPath = computed(() => String(props.task.mailFolder ?? ''))

const accountLabel = computed(() => {
  const hit = accounts.value.find((a) => a.id === accountId.value)
  if (hit) return hit.name || hit.email
  return accountId.value ? `#${accountId.value}` : '—'
})

const folderLabel = computed(() => {
  if (!folderPath.value) return t('task.form.mailFolderInbox')
  const hit = folders.value.find((f) => f.path === folderPath.value)
  return hit?.name || folderPath.value
})

const gateChips = computed(() => {
  const chips: string[] = []
  const from = String(props.task.mailFrom ?? '')
  const subject = String(props.task.mailSubject ?? '')
  const body = String(props.task.mailBody ?? '')
  if (from) chips.push(t('task.mail.gateFrom', { v: from }))
  if (subject) chips.push(t('task.mail.gateSubject', { v: subject }))
  if (body) chips.push(t('task.mail.gateBody', { v: body }))
  if (props.task.mailHasAttachment) chips.push(t('task.mail.gateAttachment'))
  return chips
})

onMounted(async () => {
  try {
    const data = await apiGet<{ accounts?: typeof accounts.value }>('/api/mail/accounts')
    accounts.value = Array.isArray(data.accounts) ? data.accounts : []
  } catch (e) {
    appLog.d('TaskMailCard', 'load accounts failed', e)
  }
  if (!accountId.value) return
  try {
    const data = await apiGet<{ folders?: typeof folders.value }>(
      `/api/mail/folders?accountId=${accountId.value}`,
    )
    folders.value = Array.isArray(data.folders) ? data.folders : []
  } catch (e) {
    appLog.d('TaskMailCard', 'load folders failed', e)
  }
})
</script>

<style scoped>
/* Row chrome is scoped per card in this folder (the shared part is only the
   outer .overview-card / .card-title), so it is declared here too. */
.overview-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
}
.overview-label {
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}
.overview-value {
  font-size: var(--font-size-md);
  color: var(--text-primary, #1a1a1a);
  text-align: right;
  word-break: break-word;
  min-width: 0;
}
.mail-card-hint {
  font-size: var(--font-size-xs, 11px);
  line-height: 1.6;
  color: var(--text-hint);
}
.mail-gate-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2, 4px);
}
.mail-gate-chip {
  display: inline-block;
  max-width: 100%;
  padding: calc(2 * var(--dp)) calc(7 * var(--dp));
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  border-radius: var(--radius-full, 999px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.muted {
  color: var(--text-hint);
}
</style>
