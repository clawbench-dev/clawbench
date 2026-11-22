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

/**
 * The same destination has two MORE entry points, both inside the chat pane:
 * the agent icon in the header and the big glyph in the empty state.
 *
 * They do not travel the same way — the header lives in App.vue (calls the
 * handler directly), the glyph lives four components down (emits) — and each is
 * a single binding that no mount-level test in this repo covers (App.vue has
 * none; the glyph's own emit value is pinned in ChatMessageList.agentWelcome).
 * A dropped binding on either turns it into a dead icon, which is the whole
 * failure mode this file exists for.
 */
describe('header icon / empty-state glyph → agent config wiring', () => {
  const app = stripComments(read('App.vue'))
  const list = stripComments(read('components/chat/ChatMessageList.vue'))

  it('the chat header agent icon AND name open the config page for the session agent', () => {
    // Scoped to the single-agent header branch: a bare `openAgentConfig(` search
    // would also match the handler definition and the ChatPanelContent binding.
    const region = app.slice(app.indexOf('class="bs-header-title"'), app.indexOf('bs-header-title-editable'))
    expect(region, 'the header icon block must exist').toContain('chat-title-agent-btn')
    expect(region).toMatch(/@click\.stop="openAgentConfig\(\s*sessionIdentity\.currentAgentId\.value\s*\)"/)
    // The name is INSIDE the same button, so clicking it hits the button. A
    // name left outside (as plain text next to the button) is the exact
    // regression to catch: it still looks like part of the control.
    expect(region).toMatch(/class="chat-title-agent-name">\{\{\s*sessionIdentity\.agentHeaderTitle\.value\s*\}\}/)
    const btnStart = region.indexOf('chat-title-agent-btn')
    const btnEnd = region.indexOf('</button>', btnStart)
    expect(region.slice(btnStart, btnEnd), 'the name must sit inside the button').toContain('chat-title-agent-name')
    // The control now has visible text, so it has to say what it does.
    expect(region).toContain(`:title="t('session.configAgent')"`)
    expect(region).toContain(`:aria-label="t('session.configAgent')"`)
  })

  it('the empty-state glyph emits open-agent-config with the agent id', () => {
    expect(list).toMatch(/@click="emit\('open-agent-config',\s*currentAgent\.id\)"/)
    // Without an id there is no page to open — the tile must stay inert rather
    // than become a button that does nothing.
    expect(list).toMatch(/v-if="currentAgent\.id"[\s\S]{0,400}agent-welcome-icon-btn/)
  })
})
