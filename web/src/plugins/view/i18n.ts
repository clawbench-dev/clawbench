/**
 * Copy for the `view` plugin: the dock manager's one-line description and the
 * reason shown when this host cannot run it.
 *
 * Kept next to the plugin so a new plugin carries its own copy: the global
 * `i18n/locales/*.ts` tables used to hold a hand-maintained `dockPlugins.desc`
 * entry per plugin, which is one more file to remember when adding one.
 *
 * Shared wording ("保存"/"取消", and the generic "current host unsupported")
 * stays in the global tables — it is not this plugin's property.
 */
export default {
  zh: {
    desc: '预览当前打开的文件。',
  },
  en: {
    desc: 'Preview the file you have open.',
  },
} as const
