<template>
  <div class="skills-scan">
    <div class="skills-desc">{{ description }}</div>

    <!-- Master switch for the whole feature. Off stops injecting the skill
         table into any system prompt; scanning and the discovered listing keep
         working, so this is deliberately NOT a per-source switch. -->
    <SettingsItem
      :label="t('settings.items.skillsEnabled')"
      :description="t('settings.items.skillsEnabledDesc')"
      type="switch"
      :model-value="enabled"
      @update:model-value="onToggleEnabled"
    />

    <!-- Project skills come from the working tree, which may be a repository
         the user has never read, and their description is copied into the
         system prompt on every turn. That is a different risk from a skill the
         user configured themselves, so it gets its own switch and is off by
         default. The listing below still shows them either way. -->
    <SettingsItem
      :label="t('settings.items.skillsProjectAutoInject')"
      :description="t('settings.items.skillsProjectAutoInjectDesc')"
      type="switch"
      :model-value="projectAutoInject"
      @update:model-value="onToggleProjectAutoInject"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingsItem from './SettingsItem.vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useSkillsState, loadSkills } from '@/composables/useSkillsState'

defineProps<{ description?: string }>()

const { t } = useI18n()
const { getServerValueWithDefault, setServerValue } = useSettingsConfig()

// Shared with the sibling cards (dirs / repos / discovered) so they all read one
// GET /api/skills response and a change in any card is reflected in the others.
const { enabled, projectAutoInject } = useSkillsState()

function load() {
  return loadSkills(getServerValueWithDefault('skills.enabled') !== false)
}

async function onToggleEnabled(v: unknown) {
  enabled.value = Boolean(v)
  try {
    await setServerValue('skills.enabled', enabled.value)
  } finally {
    await load()
  }
}

async function onToggleProjectAutoInject(v: unknown) {
  projectAutoInject.value = Boolean(v)
  try {
    await setServerValue('skills.project_auto_inject', projectAutoInject.value)
  } finally {
    await load()
  }
}

onMounted(load)
</script>

<style scoped>
.skills-scan {
  /* No horizontal padding: the switch row is a SettingsItem, which already
     carries `padding: 12px 16px`, and the card's rows align on that 16px inset.
     Adding a second inset here pushed the switch label to 32px — visibly deeper
     than every neighbouring row. */
  padding: var(--space-6) 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}
.skills-desc {
  /* Align with the SettingsItem rows above/below (their 16px inset). */
  padding: 0 var(--space-7);
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
</style>
