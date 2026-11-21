/**
 * Copy for the `forge` plugin: the dock manager's one-line description and the
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
    desc: '查看 GitHub、GitLab、Gitea 的议题、合并请求和流水线（代码托管集成）。',
    unavailable: '服务端没有启用代码托管集成。',
  },
  en: {
    desc: 'Issues, merge requests and pipelines from GitHub, GitLab and Gitea / Forgejo.',
    unavailable: 'Software Forge is not enabled on the server.',
  },
} as const
