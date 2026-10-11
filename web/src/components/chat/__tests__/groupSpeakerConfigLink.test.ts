import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Wiring guard: a group-chat speaker header click must open that agent's config
 * page AND record the chat as the jump origin, so Back returns to the
 * conversation.
 *
 * The three hops (ChatMessageItem → ChatMessageList → ChatPanelContent →
 * App.vue) are plain `$emit` forwards, which no unit test exercises end to end
 * (App.vue has no mount-level test — it is a single large component). A dropped
 * `@open-agent-config` on any one hop silently turns the avatar into a dead
 * button, and a dropped `beginExternalJump` silently sends Back to the wrong
 * tab. Both are asserted here at the source level.
 *
 * The emitted VALUE (the real agent id, not the member row id) is pinned by a
 * behavior test in ChatMessageItem.group.test.ts, so this file does not repeat
 * it.
 */

function read(rel: string): string {
  return readFileSync(join(__dirname, '..', '..', '..', rel), 'utf8')
}

/** Strip comments so an assertion cannot be satisfied by prose alone.
 *
 *  HTML comments matter here: these are `.vue` files, and commenting out a
 *  binding (`<!-- @open-agent-config="openAgentConfig" -->`) is a plausible
 *  "temporarily disable while debugging" edit that would otherwise still
 *  satisfy the template assertions below. */
function stripComments(src: string): string {
  return src
    .replace(/<!--[\s\S]*?-->/g, '')    // HTML comments (Vue templates)
    .replace(/\/\*[\s\S]*?\*\//g, '')   // block comments
    .replace(/^\s*\/\/.*$/gm, '')       // whole-line comments
}

describe('group speaker → agent config wiring', () => {
  const item = stripComments(read('components/chat/ChatMessageItem.vue'))
  const list = stripComments(read('components/chat/ChatMessageList.vue'))
  const panel = stripComments(read('components/chat/ChatPanelContent.vue'))
  const app = stripComments(read('App.vue'))

  it('ChatMessageItem emits open-agent-config from the avatar button', () => {
    expect(item).toMatch(/defineEmits\(\[[^\]]*'open-agent-config'/)
    expect(item).toMatch(/@click\.stop="\$emit\('open-agent-config',\s*speaker\.agentId\)"/)
  })

  it('ChatMessageList re-emits open-agent-config', () => {
    expect(list).toMatch(/defineEmits\(\[[^\]]*'open-agent-config'/)
    expect(list).toMatch(/@open-agent-config="\$emit\('open-agent-config',\s*\$event\)"/)
  })

  it('ChatPanelContent re-emits open-agent-config', () => {
    expect(panel).toMatch(/defineEmits\(\[[^\]]*'open-agent-config'/)
    expect(panel).toMatch(/@open-agent-config="\(agentId\)\s*=>\s*\$emit\('open-agent-config',\s*agentId\)"/)
  })

  it('App.vue binds the handler on ChatPanelContent', () => {
    expect(app).toMatch(/@open-agent-config="openAgentConfig"/)
  })

  it('App.vue handler records the chat origin, then deep-links into the agent detail page', () => {
    const start = app.indexOf('function openAgentConfig(')
    expect(start, 'openAgentConfig must exist').toBeGreaterThan(-1)
    const region = app.slice(start, app.indexOf('\n}\n', start))
    // Back must return to the conversation, not whatever tab preceded the jump.
    expect(region).toMatch(/beginExternalJump\('chat'/)
    // The settings detail page is a lazily-mounted sub-page, so the target
    // travels through the module-level pending category.
    expect(region).toMatch(/setPendingSettingsCategory\(`agents:\$\{agentId\}`\)/)
    expect(region).toMatch(/switchTab\('settings'\)/)
  })
})
