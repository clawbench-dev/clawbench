<template>
  <!-- The user's own skill directories. A list, because skills often live in
       several places (a personal collection plus a shared team checkout). Each
       row is an absolute path, like fonts.dir. -->
  <div class="skills-dirs-setting">
    <div class="skills-hint">{{ t('settings.items.skillsDirsDesc') }}</div>

    <div v-for="(dir, idx) in dirs" :key="dir + idx" class="skills-dir">
      <input
        class="skills-input"
        :value="dir"
        @change="onDirChange(idx, ($event.target as HTMLInputElement).value)"
      />
      <button class="fbtn" @click="removeDir(idx)">
        <Trash2 :size="13" />
        {{ t('settings.items.skillsRepoRemove') }}
      </button>
    </div>

    <div class="skills-dir-add">
      <input
        v-model="newDir"
        class="skills-input"
        :placeholder="t('settings.items.skillsDirPlaceholder')"
      />
      <button class="fbtn fbtn-primary" :disabled="!newDir.trim() || dirsSaving" @click="addDir">
        {{ t('settings.items.skillsRepoAdd') }}
      </button>
    </div>

    <div v-if="dirsError" class="skills-error">
      <AlertCircle :size="13" />
      <span>{{ dirsError }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, Trash2 } from 'lucide-vue-next'
import '@/assets/modal-footer-btn.css'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useSkillsState, loadSkills } from '@/composables/useSkillsState'

const { t } = useI18n()
const { getServerValueWithDefault, setServerValue } = useSettingsConfig()
const { dirs, dirsError, dirsSaving } = useSkillsState()

const newDir = ref('')

function load() {
  return loadSkills(getServerValueWithDefault('skills.enabled') !== false)
}

/**
 * Persist the whole directory list. The endpoint replaces the array wholesale,
 * so every mutation sends the full list.
 */
async function saveDirs(next: string[]) {
  dirsSaving.value = true
  dirsError.value = ''
  try {
    await setServerValue('skills.dirs', next)
    newDir.value = ''
    await load()
  } catch (err) {
    dirsError.value = err instanceof Error ? err.message : String(err)
  } finally {
    dirsSaving.value = false
  }
}

async function onDirChange(idx: number, value: string) {
  const next = [...dirs.value]
  next[idx] = value.trim()
  await saveDirs(next)
}

async function addDir() {
  const dir = newDir.value.trim()
  if (!dir) return
  await saveDirs([...dirs.value, dir])
}

async function removeDir(idx: number) {
  await saveDirs(dirs.value.filter((_, i) => i !== idx))
}

onMounted(load)
</script>

<style scoped>
.skills-dirs-setting {
  /* The card body already provides the row inset; custom blocks pad their own
     text to the same 16px so they line up with the SettingsItem rows. */
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.skills-hint {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.skills-dir,
.skills-dir-add {
  display: flex;
  gap: var(--space-4);
  align-items: center;
  flex-wrap: wrap;
}
/* Input geometry follows the settings-panel convention (30px control height,
   same as .fbtn / the switch row), matching ForgeCredentialsRow's field. */
.skills-input {
  flex: 1;
  min-width: 150px;
  box-sizing: border-box;
  height: 30px;
  padding: 0 var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
}
.skills-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 2px var(--focus-ring);
}
.skills-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--color-red);
  font-size: var(--font-size-sm);
}
</style>
