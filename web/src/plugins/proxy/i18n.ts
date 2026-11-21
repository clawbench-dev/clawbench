/**
 * Copy for the `proxy` plugin: the dock manager's one-line description and the
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
    desc: '通过 SSH 或流隧道把服务器端口映射到本地。',
    unavailable: '当前环境无法建立端口映射（SSH 与流隧道都不可用）。',
  },
  en: {
    desc: 'Forward server ports to your machine over SSH or the stream tunnel.',
    unavailable: 'Port mapping is unavailable here (neither SSH nor the stream tunnel can carry it).',
  },
} as const
