// @ts-check
/**
 * CodeBuddy Agent SDK — capability probe harness.
 *
 * Purpose: empirically answer "can ClawBench replace its CLI/ACP transport with
 * CodeBuddy's native Agent SDK?" by exercising every documented SDK capability
 * against a real backend and reporting, per capability point, what actually works.
 *
 * Output contract (consumed by internal/ai/codebuddy_sdk_integration_test.go):
 *   - one NDJSON line per test point on stdout:
 *       {"id":"basic_query","group":"basic","ok":true,"ms":1234,"detail":{...}}
 *   - a final line: {"id":"__summary__","ok":<all required passed>,"counts":{...}}
 *   - human-readable diagnostics go to stderr only (never stdout).
 *
 * A failing point never aborts the run: each is independently try/caught, so one
 * broken capability cannot mask the rest.
 *
 * Env:
 *   SDK_PROBE_STRICT=1   treat every point as required (default: only REQUIRED_POINTS)
 *   SDK_PROBE_ONLY=a,b   run only these point ids
 *   SDK_PROBE_MODEL=...  override the model used by every point
 *
 * Auth: inherited from the parent process. On this machine the working
 * combination is CODEBUDDY_INTERNET_ENVIRONMENT=cloudhosted plus
 * CODEBUDDY_ENTERPRISE_ENDPOINT. The SDK strips filesystem config by default,
 * so `settingSources: ['user']` is needed for the stored login to be visible.
 */

import { query, unstable_v2_createSession, unstable_v2_resumeSession, createSdkMcpServer, tool } from '@tencent-ai/agent-sdk';
import { z } from 'zod';
import fs from 'node:fs';
import path from 'node:path';

// ---------------------------------------------------------------------------
// Crash hardening
// ---------------------------------------------------------------------------
//
// The SDK has an unhandled-rejection hazard: a late MCP control message arriving
// after the transport closed makes ProcessTransport.writeLine throw
// "Transport not started" from an async callback, which by default kills the
// Node process and takes the whole probe with it. Observed in practice after a
// run using in-process SDK MCP servers.
//
// Record it as a finding and keep going — losing the remaining capability points
// would be worse than the underlying defect.
const sdkCrashFindings = [];
process.on('uncaughtException', (err) => {
  sdkCrashFindings.push({ kind: 'uncaughtException', error: String(err?.message || err) });
  log(`[harness] swallowed uncaughtException: ${err?.message || err}`);
});
process.on('unhandledRejection', (err) => {
  sdkCrashFindings.push({ kind: 'unhandledRejection', error: String(err?.message || err) });
  log(`[harness] swallowed unhandledRejection: ${err?.message || err}`);
});

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const STRICT = process.env.SDK_PROBE_STRICT === '1';
const ONLY = (process.env.SDK_PROBE_ONLY || '').split(',').map(s => s.trim()).filter(Boolean);
const MODEL = process.env.SDK_PROBE_MODEL || undefined;

/**
 * Points that MUST pass for the SDK to be a viable transport at all. Everything
 * else is recorded as an observation. Under SDK_PROBE_STRICT=1 every point is
 * required, which turns this probe into a regression gate after adoption.
 */
const REQUIRED_POINTS = new Set([
  'basic_query',
  'session_id_capture',
  'resume_context',
  'builtin_tool_use',
]);

/** Working directory for every point. Created by the Go test. */
const WORKDIR = process.env.SDK_PROBE_CWD || process.cwd();

/**
 * Auth env forwarded into the spawned CLI. The SDK merges options.env over
 * process.env, so passing the current values explicitly keeps the probe working
 * even when the Go test's environment is trimmed.
 */
const AUTH_ENV = {
  ...(process.env.CODEBUDDY_INTERNET_ENVIRONMENT
    ? { CODEBUDDY_INTERNET_ENVIRONMENT: process.env.CODEBUDDY_INTERNET_ENVIRONMENT }
    : {}),
  ...(process.env.CODEBUDDY_ENTERPRISE_ENDPOINT
    ? { CODEBUDDY_ENTERPRISE_ENDPOINT: process.env.CODEBUDDY_ENTERPRISE_ENDPOINT }
    : {}),
};

/** Baseline options every point starts from. */
function baseOptions(extra = {}) {
  return {
    cwd: WORKDIR,
    permissionMode: 'bypassPermissions',
    settingSources: ['user'],
    env: { ...AUTH_ENV },
    ...(MODEL ? { model: MODEL } : {}),
    ...extra,
  };
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

/** @type {Array<{id:string,group:string,ok:boolean,ms:number,required:boolean,detail:any}>} */
const results = [];
let currentGroup = 'misc';

function emit(obj) {
  process.stdout.write(JSON.stringify(obj) + '\n');
}

function log(...args) {
  process.stderr.write(args.map(String).join(' ') + '\n');
}

/**
 * Run one capability point. Never throws.
 * @param {string} id
 * @param {object} spec
 * @param {(ctx:any)=>Promise<any>} spec.run  returns `detail`; throw to fail
 * @param {string} [spec.group]
 * @param {number} [spec.timeoutMs]
 * @param {string} [spec.skipReason]  when set, record a skip instead of running
 */
async function point(id, spec) {
  if (ONLY.length && !ONLY.includes(id)) return;
  const group = spec.group || currentGroup;
  const required = STRICT || REQUIRED_POINTS.has(id);

  if (spec.skipReason) {
    const rec = { id, group, ok: false, skipped: true, ms: 0, required, detail: { skip_reason: spec.skipReason } };
    results.push(rec);
    emit(rec);
    log(`SKIP ${id}: ${spec.skipReason}`);
    return;
  }

  const t0 = Date.now();
  try {
    const detail = await withTimeout(spec.run(), spec.timeoutMs || 120_000, id);
    const rec = { id, group, ok: true, ms: Date.now() - t0, required, detail: detail ?? {} };
    results.push(rec);
    emit(rec);
    log(`PASS ${id} (${rec.ms}ms)`);
  } catch (err) {
    const rec = {
      id, group, ok: false, ms: Date.now() - t0, required,
      detail: { error: String(err && err.message ? err.message : err) },
    };
    results.push(rec);
    emit(rec);
    log(`FAIL ${id} (${rec.ms}ms): ${rec.detail.error}`);
  }
}

function withTimeout(promise, ms, id) {
  let timer;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`point ${id} timed out after ${ms}ms`)), ms);
  });
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer));
}

// ---------------------------------------------------------------------------
// Message helpers
// ---------------------------------------------------------------------------

/** Drain a Query/session stream, collecting a structured summary. */
async function drain(iter, { limitMs = 110_000 } = {}) {
  const types = {};
  const blocks = {};
  const toolCalls = [];
  const text = [];
  const thinking = [];
  let init = null, result = null, partialCount = 0;
  const streamEventTypes = {};

  const deadline = Date.now() + limitMs;
  for await (const m of iter) {
    types[m.type] = (types[m.type] || 0) + 1;
    if (m.type === 'system' && m.subtype === 'init') init = m;
    if (m.type === 'result') result = m;
    if (m.type === 'stream_event') {
      partialCount++;
      const t = m.event?.type || 'unknown';
      streamEventTypes[t] = (streamEventTypes[t] || 0) + 1;
    }
    if (m.type === 'assistant' && m.message?.content) {
      for (const b of m.message.content) {
        blocks[b.type] = (blocks[b.type] || 0) + 1;
        if (b.type === 'text') text.push(b.text);
        if (b.type === 'thinking') thinking.push(b.thinking);
        if (b.type === 'tool_use') toolCalls.push({ name: b.name, id: b.id, inputKeys: Object.keys(b.input || {}) });
      }
    }
    if (Date.now() > deadline) break;
  }
  return { types, blocks, toolCalls, text: text.join(''), thinking: thinking.join(''), init, result, partialCount, streamEventTypes };
}

function summarizeResult(r) {
  if (!r) return null;
  return {
    subtype: r.subtype,
    is_error: r.is_error ?? null,
    session_id: r.session_id ?? null,
    num_turns: r.num_turns ?? null,
    duration_ms: r.duration_ms ?? null,
    total_cost_usd: r.total_cost_usd ?? null,
    usage: r.usage ?? null,
    result_preview: typeof r.result === 'string' ? r.result.slice(0, 200) : null,
  };
}

function summarizeInit(i) {
  if (!i) return null;
  return {
    session_id: i.session_id,
    model: i.model,
    permissionMode: i.permissionMode,
    cwd: i.cwd ?? null,
    tools_count: (i.tools || []).length,
    tools_sample: (i.tools || []).slice(0, 15),
    slash_commands_count: (i.slash_commands || []).length,
    slash_commands_sample: (i.slash_commands || []).slice(0, 10),
    skills: i.skills ?? null,
    plugins: i.plugins ?? null,
    mcp_servers: i.mcp_servers ?? null,
    codebuddy_code_version: i.codebuddy_code_version ?? null,
  };
}

/**
 * Full key list of a system:init message. Used to discover which optional
 * fields this CLI build actually emits (skills/plugins are documented but were
 * observed absent, so the raw key set is the evidence).
 */
function initKeys(i) {
  return i ? Object.keys(i).sort() : [];
}

// ===========================================================================
// Group 1: basic
// ===========================================================================

async function runBasic() {
  currentGroup = 'basic';

  await point('basic_query', {
    group: 'basic',
    run: async () => {
      const r = await drain(query({ prompt: 'reply with exactly: SDK-PONG', options: baseOptions() }));
      if (!r.result) throw new Error('no result message');
      if (r.result.is_error) throw new Error(`result is_error: ${r.result.subtype}`);
      const ok = /SDK-PONG/i.test(r.text) || /SDK-PONG/i.test(String(r.result.result || ''));
      if (!ok) throw new Error(`expected SDK-PONG, got: ${JSON.stringify(r.text.slice(0, 120))}`);
      return { text: r.text.slice(0, 120), result: summarizeResult(r.result) };
    },
  });

  await point('message_types', {
    group: 'basic',
    run: async () => {
      const r = await drain(query({ prompt: 'say hi', options: baseOptions({ includePartialMessages: true }) }));
      return {
        message_types: r.types,
        content_blocks: r.blocks,
        partial_count: r.partialCount,
        stream_event_types: r.streamEventTypes,
        has_init: !!r.init,
        has_result: !!r.result,
      };
    },
  });

  await point('session_id_capture', {
    group: 'basic',
    run: async () => {
      const r = await drain(query({ prompt: 'say ok', options: baseOptions() }));
      const sid = r.init?.session_id || r.result?.session_id;
      if (!sid) throw new Error('no session_id on init or result');
      if (!/^[0-9a-f-]{36}$/i.test(sid)) throw new Error(`session_id not a uuid: ${sid}`);
      return { session_id: sid, from_init: r.init?.session_id ?? null, from_result: r.result?.session_id ?? null };
    },
  });

  await point('cwd_honored', {
    group: 'basic',
    run: async () => {
      const r = await drain(query({ prompt: 'run `pwd` and reply with only the absolute path', options: baseOptions() }));
      const got = r.text.trim();
      const ok = got.includes(WORKDIR);
      if (!ok) throw new Error(`expected pwd to contain ${WORKDIR}, got ${JSON.stringify(got.slice(0, 200))}`);
      return { expected: WORKDIR, observed: got.slice(0, 200) };
    },
  });
}

// ===========================================================================
// Group 2: session
// ===========================================================================

async function runSession() {
  currentGroup = 'session';

  // Shared across the session group: a first turn we then resume from.
  let seedSid = null;
  const SEED_TOKEN = 'ZEBRA-9174';

  await point('resume_context', {
    group: 'session',
    run: async () => {
      const first = await drain(query({
        prompt: `Remember this codeword for later: ${SEED_TOKEN}. Reply with just OK.`,
        options: baseOptions(),
      }));
      seedSid = first.init?.session_id || first.result?.session_id;
      if (!seedSid) throw new Error('seed turn produced no session_id');

      const second = await drain(query({
        prompt: 'What was the codeword I asked you to remember? Reply with only the codeword.',
        options: baseOptions({ resume: seedSid }),
      }));
      const ok = second.text.includes(SEED_TOKEN);
      if (!ok) throw new Error(`resume lost context; got ${JSON.stringify(second.text.slice(0, 200))}`);
      return { seed_session_id: seedSid, resumed_session_id: second.result?.session_id ?? null, recalled: ok };
    },
  });

  await point('fork_session', {
    group: 'session',
    skipReason: seedSid ? undefined : 'resume_context did not produce a seed session',
    run: async () => {
      const r = await drain(query({
        prompt: 'Reply with only the word FORKED.',
        options: baseOptions({ resume: seedSid, forkSession: true }),
      }));
      return {
        parent_session_id: seedSid,
        forked_session_id: r.result?.session_id ?? null,
        differs_from_parent: (r.result?.session_id ?? null) !== seedSid,
        text: r.text.slice(0, 120),
      };
    },
  });

  await point('continue_recent', {
    group: 'session',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions({ continue: true }) }));
      return { session_id: r.result?.session_id ?? null, subtype: r.result?.subtype ?? null };
    },
  });

  await point('persist_session_false', {
    group: 'session',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions({ persistSession: false }) }));
      const sid = r.result?.session_id ?? null;
      if (!sid) throw new Error('no session_id with persistSession:false');
      // The transcript should NOT be on disk. Reported, not asserted (path layout is
      // the CLI's business); the Go side can cross-check ~/.codebuddy/projects.
      return { session_id: sid, subtype: r.result?.subtype ?? null };
    },
  });

  await point('resume_at_message', {
    group: 'session',
    skipReason: 'resumeSessionAt needs a message id that the probe does not stably observe; deferred',
  });
}

// ===========================================================================
// Group 3: streaming
// ===========================================================================

async function runStreaming() {
  currentGroup = 'streaming';

  await point('include_partial_messages', {
    group: 'streaming',
    run: async () => {
      const r = await drain(query({
        prompt: 'Count from 1 to 5, one number per line.',
        options: baseOptions({ includePartialMessages: true }),
      }));
      if (r.partialCount === 0) throw new Error('includePartialMessages produced no stream_event messages');
      return { partial_count: r.partialCount, stream_event_types: r.streamEventTypes };
    },
  });

  await point('streaming_input', {
    group: 'streaming',
    run: async () => {
      async function* gen() {
        yield { type: 'user', message: { role: 'user', content: 'Reply with only the word STREAMED.' } };
      }
      const r = await drain(query({ prompt: gen(), options: baseOptions() }));
      if (!r.result) throw new Error('no result from streaming input');
      return { text: r.text.slice(0, 120), subtype: r.result.subtype };
    },
  });

  await point('interrupt', {
    group: 'streaming',
    run: async () => {
      const q = query({ prompt: 'Write a very long detailed essay about the history of computing, at least 2000 words.', options: baseOptions() });
      let sawOutput = false;
      const iter = q[Symbol.asyncIterator]();
      // Read a little, then interrupt.
      const t0 = Date.now();
      let interrupted = false;
      while (Date.now() - t0 < 20_000) {
        const { value, done } = await iter.next();
        if (done) break;
        if (value.type === 'assistant' || value.type === 'stream_event') sawOutput = true;
        if (sawOutput && !interrupted) {
          await q.interrupt();
          interrupted = true;
          break;
        }
      }
      return { saw_output_before_interrupt: sawOutput, interrupt_sent: interrupted };
    },
  });

  await point('abort_controller', {
    group: 'streaming',
    run: async () => {
      const ac = new AbortController();
      const q = query({ prompt: 'Count slowly to 100.', options: baseOptions({ abortController: ac }) });
      const t0 = Date.now();
      let aborted = false, sawAnything = false;
      try {
        for await (const m of q) {
          sawAnything = true;
          if (!aborted) { ac.abort(); aborted = true; }
          if (Date.now() - t0 > 15_000) break;
        }
      } catch (e) {
        return { aborted: true, saw_anything: sawAnything, error_on_abort: String(e?.message || e).slice(0, 200) };
      }
      return { aborted, saw_anything: sawAnything };
    },
  });
}

// ===========================================================================
// Group 4: permissions
// ===========================================================================

async function runPermissions() {
  currentGroup = 'permissions';

  await point('can_use_tool_allow', {
    group: 'permissions',
    run: async () => {
      const calls = [];
      const marker = path.join(WORKDIR, `perm-allow-${Date.now()}.txt`);
      const r = await drain(query({
        prompt: `Run the bash command \`echo PERM-ALLOW-OK > ${marker}\` and confirm it succeeded.`,
        options: baseOptions({
          permissionMode: 'default',
          canUseTool: async (toolName, input, opts) => {
            calls.push({ toolName, inputKeys: Object.keys(input || {}), toolUseID: opts?.toolUseID ?? null, hasSignal: !!opts?.signal });
            return { behavior: 'allow', updatedInput: input };
          },
        }),
      }));
      if (calls.length === 0) throw new Error('canUseTool was never invoked under permissionMode=default');
      // Filesystem proof the command actually ran — independent of model phrasing.
      if (!fs.existsSync(marker)) {
        throw new Error(`allowed command did not execute (marker ${marker} missing); text=${JSON.stringify(r.text.slice(0, 160))}`);
      }
      fs.unlinkSync(marker);
      return { callback_calls: calls, executed: true };
    },
  });

  await point('can_use_tool_deny', {
    group: 'permissions',
    run: async () => {
      const calls = [];
      const marker = path.join(WORKDIR, `perm-deny-${Date.now()}.txt`);
      const r = await drain(query({
        prompt: `Run the bash command \`echo PERM-DENY-OK > ${marker}\` and report whether it worked.`,
        options: baseOptions({
          permissionMode: 'default',
          canUseTool: async (toolName, input, opts) => {
            calls.push({ toolName, toolUseID: opts?.toolUseID ?? null });
            return { behavior: 'deny', message: 'denied by probe' };
          },
        }),
      }));
      if (calls.length === 0) throw new Error('canUseTool was never invoked');
      // Filesystem proof of NON-execution. Asserting on assistant text is wrong:
      // the model naturally echoes the command (and thus the marker) when
      // explaining that it was denied.
      if (fs.existsSync(marker)) {
        fs.unlinkSync(marker);
        throw new Error('denied command still executed — the side-effect file was created');
      }
      return { callback_calls: calls, denied: true, subtype: r.result?.subtype ?? null, text: r.text.slice(0, 160) };
    },
  });

  await point('can_use_tool_modify_input', {
    group: 'permissions',
    run: async () => {
      let modified = false;
      const r = await drain(query({
        prompt: 'Run the bash command `echo ORIGINAL-COMMAND` and report its output.',
        options: baseOptions({
          permissionMode: 'default',
          canUseTool: async (toolName, input) => {
            if (toolName === 'Bash' && typeof input?.command === 'string') {
              modified = true;
              return { behavior: 'allow', updatedInput: { ...input, command: 'echo REWRITTEN-COMMAND' } };
            }
            return { behavior: 'allow', updatedInput: input };
          },
        }),
      }));
      if (!modified) throw new Error('Bash tool was never offered to canUseTool for rewriting');
      const ok = /REWRITTEN-COMMAND/.test(r.text);
      if (!ok) throw new Error(`updatedInput not honored; text=${JSON.stringify(r.text.slice(0, 200))}`);
      return { modified_input: true, saw_rewritten: true };
    },
  });

  await point('permission_mode_plan', {
    group: 'permissions',
    // Read-only prompt on purpose: plan mode blocks writes, so asking it to
    // create a file makes the agent wait for an approval that never comes.
    timeoutMs: 70_000,
    run: async () => {
      const r = await drain(query({
        prompt: 'Reply with only the word PLANNED.',
        options: baseOptions({ permissionMode: 'plan' }),
      }), { limitMs: 60_000 });
      const mode = r.init?.permissionMode ?? null;
      if (mode !== 'plan') throw new Error(`expected permissionMode=plan on init, got ${JSON.stringify(mode)}`);
      // Plan mode must not have written anything.
      const writes = r.toolCalls.filter(t => t.name === 'Write' || t.name === 'Edit');
      return { init_mode: mode, write_attempts: writes.length, subtype: r.result?.subtype ?? null, text: r.text.slice(0, 120) };
    },
  });

  await point('permission_mode_bypass', {
    group: 'permissions',
    run: async () => {
      const r = await drain(query({
        prompt: 'Run `echo BYPASS-OK`.',
        options: baseOptions({ permissionMode: 'bypassPermissions' }),
      }));
      const ok = /BYPASS-OK/.test(r.text);
      if (!ok) throw new Error('bypassPermissions did not execute the tool');
      return { executed: true, init_mode: r.init?.permissionMode ?? null };
    },
  });

  await point('allowed_tools', {
    group: 'permissions',
    run: async () => {
      const r = await drain(query({
        prompt: 'Run `echo ALLOWED-OK`.',
        options: baseOptions({ allowedTools: ['Bash'] }),
      }));
      return { init_tools_count: (r.init?.tools || []).length, text: r.text.slice(0, 120) };
    },
  });

  await point('disallowed_tools', {
    group: 'permissions',
    run: async () => {
      const r = await drain(query({
        prompt: 'Run the bash command `echo SHOULD-NOT-RUN`.',
        options: baseOptions({ disallowedTools: ['Bash'] }),
      }));
      const ran = /SHOULD-NOT-RUN/.test(r.text);
      return { disallowed_honored: !ran, text: r.text.slice(0, 160), init_tools_count: (r.init?.tools || []).length };
    },
  });

  await point('tools_whitelist', {
    group: 'permissions',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions({ tools: [] }) }));
      return { init_tools_count: (r.init?.tools || []).length, note: 'tools:[] should disable builtins' };
    },
  });
}

// ===========================================================================
// Group 5: hooks
// ===========================================================================

async function runHooks() {
  currentGroup = 'hooks';

  await point('hook_pre_tool_use', {
    group: 'hooks',
    run: async () => {
      const seen = [];
      const r = await drain(query({
        prompt: 'Run `echo HOOK-PRE-OK`.',
        options: baseOptions({
          hooks: {
            PreToolUse: [{ matcher: 'Bash', hooks: [async (input) => {
              seen.push({ tool_name: input.tool_name, has_tool_input: !!input.tool_input, event: input.hook_event_name });
              return { continue: true };
            }] }],
          },
        }),
      }));
      if (seen.length === 0) throw new Error('PreToolUse hook never fired');
      return { hook_calls: seen, text: r.text.slice(0, 120) };
    },
  });

  await point('hook_post_tool_use', {
    group: 'hooks',
    run: async () => {
      const seen = [];
      const r = await drain(query({
        prompt: 'Run `echo POST-HOOK-ORIGINAL`.',
        options: baseOptions({
          hooks: {
            PostToolUse: [{ matcher: 'Bash', hooks: [async (input) => {
              seen.push({ tool_name: input.tool_name, event: input.hook_event_name });
              return { continue: true };
            }] }],
          },
        }),
      }));
      if (seen.length === 0) throw new Error('PostToolUse hook never fired');
      return { hook_calls: seen };
    },
  });

  await point('hook_post_tool_use_replace_output', {
    group: 'hooks',
    run: async () => {
      const r = await drain(query({
        prompt: 'Run `echo TOKEN-TO-BE-REPLACED` and then tell me exactly what the command output was.',
        options: baseOptions({
          hooks: {
            PostToolUse: [{ matcher: 'Bash', hooks: [async () => ({
              continue: true,
              hookSpecificOutput: { updatedToolOutput: 'REPLACED-BY-HOOK' },
            })] }],
          },
        }),
      }));
      const replaced = /REPLACED-BY-HOOK/.test(r.text);
      const original = /TOKEN-TO-BE-REPLACED/.test(r.text);
      if (!replaced) {
        throw new Error(
          `updatedToolOutput was NOT honored: model saw the original output ` +
          `(original_marker=${original}). PostToolUse output replacement is unavailable.`
        );
      }
      return { replaced: true, saw_original: original, text: r.text.slice(0, 200) };
    },
  });

  await point('hook_user_prompt_submit', {
    group: 'hooks',
    run: async () => {
      const seen = [];
      await drain(query({
        prompt: 'Reply with only OK.',
        options: baseOptions({
          hooks: {
            UserPromptSubmit: [{ hooks: [async (input) => {
              seen.push({ prompt: input.prompt, event: input.hook_event_name });
              return { continue: true };
            }] }],
          },
        }),
      }));
      if (seen.length === 0) throw new Error('UserPromptSubmit hook never fired');
      return { hook_calls: seen };
    },
  });

  await point('hook_session_start', {
    group: 'hooks',
    run: async () => {
      const seen = [];
      await drain(query({
        prompt: 'Reply with only OK.',
        options: baseOptions({
          hooks: {
            SessionStart: [{ hooks: [async (input) => {
              seen.push({ event: input.hook_event_name, cwd: input.cwd });
              return { continue: true };
            }] }],
          },
        }),
      }));
      return { hook_calls: seen, note: 'SessionStart may or may not fire depending on CLI version' };
    },
  });

  await point('hook_stop', {
    group: 'hooks',
    run: async () => {
      const seen = [];
      await drain(query({
        prompt: 'Reply with only OK.',
        options: baseOptions({
          hooks: {
            Stop: [{ hooks: [async (input) => {
              seen.push({ event: input.hook_event_name });
              return { continue: true };
            }] }],
          },
        }),
      }));
      return { hook_calls: seen };
    },
  });

  await point('hook_block_pre_tool', {
    group: 'hooks',
    run: async () => {
      const r = await drain(query({
        prompt: 'Run the bash command `echo BLOCKED-MARKER`.',
        options: baseOptions({
          hooks: {
            PreToolUse: [{ matcher: 'Bash', hooks: [async () => ({
              decision: 'block',
              reason: 'blocked by probe hook',
            })] }],
          },
        }),
      }));
      const ran = /BLOCKED-MARKER/.test(r.text);
      return { blocked: !ran, text: r.text.slice(0, 200) };
    },
  });
}

// ===========================================================================
// Group 6: tools
// ===========================================================================

async function runTools() {
  currentGroup = 'tools';

  await point('builtin_tool_use', {
    group: 'tools',
    run: async () => {
      const r = await drain(query({
        prompt: 'Run `echo BUILTIN-TOOL-OK` and report the output.',
        options: baseOptions(),
      }));
      if (!/BUILTIN-TOOL-OK/.test(r.text)) throw new Error(`bash tool did not run; text=${JSON.stringify(r.text.slice(0, 200))}`);
      return { tool_calls: r.toolCalls, text: r.text.slice(0, 160) };
    },
  });

  await point('builtin_tool_names', {
    group: 'tools',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions() }));
      return { tools: r.init?.tools || [], count: (r.init?.tools || []).length };
    },
  });

  await point('custom_mcp_tool', {
    group: 'tools',
    run: async () => {
      const server = createSdkMcpServer({
        name: 'probemath',
        version: '1.0.0',
        tools: [
          tool('add', 'Add two numbers and return the sum', { a: z.number(), b: z.number() }, async ({ a, b }) => ({
            content: [{ type: 'text', text: String(Number(a) + Number(b)) }],
          })),
        ],
      });
      const r = await drain(query({
        prompt: 'Use the probemath add tool to add 17 and 25, then reply with only the resulting number.',
        options: baseOptions({ mcpServers: { probemath: server } }),
      }));
      // Assert on the ACTUAL invocation, not on the number appearing in the text:
      // a model can compute 42 itself, which would mask a failed MCP server.
      const invoked = r.toolCalls.filter(t => t.name.includes('probemath') || t.name.startsWith('mcp__'));
      const connected = (r.init?.mcp_servers || []).some(s => s.name === 'probemath' && s.status === 'connected');
      if (!connected || invoked.length === 0) {
        throw new Error(
          `in-process SDK MCP server was not usable: connected=${connected}, ` +
          `init.mcp_servers=${JSON.stringify(r.init?.mcp_servers ?? null)}, ` +
          `tool_calls=${JSON.stringify(r.toolCalls.map(t => t.name))}, text=${JSON.stringify(r.text.slice(0, 200))}`
        );
      }
      return { connected, invoked_tools: invoked.map(t => t.name), text: r.text.slice(0, 160) };
    },
  });

  await point('mcp_stdio_server', {
    group: 'tools',
    run: async () => {
      const r = await drain(query({
        prompt: 'List the tools you have available whose names start with mcp__. Reply with only the count.',
        options: baseOptions({ mcpServers: { tavily: { type: 'stdio', command: 'npx', args: ['tavily-mcp'], env: {} } } }),
      }));
      return { init_mcp_servers: r.init?.mcp_servers ?? null, text: r.text.slice(0, 160) };
    },
  });

  await point('mcp_tool_naming', {
    group: 'tools',
    run: async () => {
      const server = createSdkMcpServer({
        name: 'nametest',
        tools: [tool('ping', 'Return pong', {}, async () => ({ content: [{ type: 'text', text: 'pong' }] }))],
      });
      const r = await drain(query({
        prompt: 'Call the nametest ping tool, then reply with only the word it returned.',
        options: baseOptions({ mcpServers: { nametest: server } }),
      }));
      const names = r.toolCalls.map(t => t.name);
      const prefixed = names.filter(n => n.startsWith('mcp__'));
      if (prefixed.length === 0) {
        throw new Error(
          `no mcp__<server>__<tool> name observed; tool calls were ${JSON.stringify(names)}. ` +
          'In-process SDK MCP tools were not invoked under their canonical prefixed name.'
        );
      }
      const ok = /pong/i.test(r.text);
      if (!ok) throw new Error(`tool ran but its result did not reach the answer: ${JSON.stringify(r.text.slice(0, 160))}`);
      return { observed_tool_names: names, prefixed_names: prefixed, text: r.text.slice(0, 120) };
    },
  });
}

// ===========================================================================
// Group 7: subagents
// ===========================================================================

async function runSubagents() {
  currentGroup = 'subagents';

  await point('agents_definition', {
    group: 'subagents',
    run: async () => {
      const r = await drain(query({
        prompt: 'Reply with only OK.',
        options: baseOptions({
          agents: {
            'probe-helper': {
              description: 'A helper used only by the SDK probe',
              prompt: 'You are a probe helper. Answer in one word.',
            },
          },
        }),
      }));
      return { subtype: r.result?.subtype ?? null, init_tools_count: (r.init?.tools || []).length };
    },
  });

  await point('subagent_task_tool', {
    group: 'subagents',
    run: async () => {
      const r = await drain(query({
        prompt: 'Use the Task tool to delegate this to a subagent: "reply with exactly SUBAGENT-OK". Then tell me the subagent output.',
        options: baseOptions({
          agents: {
            'probe-helper': {
              description: 'Delegate tiny questions here',
              prompt: 'You are a probe helper. Reply exactly as instructed.',
            },
          },
        }),
      }), { limitMs: 150_000 });
      return {
        tool_calls: r.toolCalls.map(t => t.name),
        used_task_tool: r.toolCalls.some(t => t.name === 'Task'),
        text: r.text.slice(0, 200),
      };
    },
  });
}

// ===========================================================================
// Group 8: model / thinking
// ===========================================================================

async function runModel() {
  currentGroup = 'model';

  await point('thinking_option', {
    group: 'model',
    run: async () => {
      const r = await drain(query({
        prompt: 'What is 17 * 23? Think it through, then reply with only the number.',
        options: baseOptions({ thinking: { type: 'enabled', budgetTokens: 4096 } }),
      }));
      return {
        content_blocks: r.blocks,
        thinking_block_present: (r.blocks['thinking'] || 0) > 0,
        thinking_chars: r.thinking.length,
        text: r.text.slice(0, 120),
      };
    },
  });

  await point('thinking_disabled', {
    group: 'model',
    run: async () => {
      const r = await drain(query({
        prompt: 'Reply with only OK.',
        options: baseOptions({ thinking: { type: 'disabled' } }),
      }));
      return { thinking_block_present: (r.blocks['thinking'] || 0) > 0, subtype: r.result?.subtype ?? null };
    },
  });

  await point('effort_option', {
    group: 'model',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions({ effort: 'low' }) }));
      return { subtype: r.result?.subtype ?? null };
    },
  });

  await point('get_available_models', {
    group: 'model',
    run: async () => {
      const q = query({ prompt: 'Reply with only OK.', options: baseOptions() });
      // supportedModels() requires an initialized connection; iterate first.
      const modelsPromise = (async () => {
        await q.connect().catch(() => {});
        return q.supportedModels();
      })();
      const models = await modelsPromise.catch(e => ({ error: String(e?.message || e) }));
      // Drain to let the process exit cleanly.
      await drain(q).catch(() => {});
      return { models: Array.isArray(models) ? models.slice(0, 10) : models, count: Array.isArray(models) ? models.length : null };
    },
  });

  await point('set_model', {
    group: 'model',
    run: async () => {
      const q = query({ prompt: 'Reply with only OK.', options: baseOptions() });
      await q.connect().catch(() => {});
      let err = null;
      try { await q.setModel(MODEL || 'deepseek-v4-flash'); } catch (e) { err = String(e?.message || e); }
      const cur = q.getModel?.() ?? null;
      await drain(q).catch(() => {});
      return { set_model_error: err, current_model: cur };
    },
  });

  await point('account_info', {
    group: 'model',
    run: async () => {
      const q = query({ prompt: 'Reply with only OK.', options: baseOptions() });
      await q.connect().catch(() => {});
      const info = await q.accountInfo().catch(e => ({ error: String(e?.message || e) }));
      await drain(q).catch(() => {});
      return { account_info: info };
    },
  });
}

// ===========================================================================
// Group 9: state
// ===========================================================================

async function runState() {
  currentGroup = 'state';

  await point('slash_commands', {
    group: 'state',
    run: async () => {
      const q = query({ prompt: 'Reply with only OK.', options: baseOptions() });
      await q.connect().catch(() => {});
      const cmds = await q.supportedCommands().catch(e => ({ error: String(e?.message || e) }));
      await drain(q).catch(() => {});
      return {
        count: Array.isArray(cmds) ? cmds.length : null,
        sample: Array.isArray(cmds) ? cmds.slice(0, 15) : cmds,
      };
    },
  });

  await point('skills_exposed', {
    group: 'state',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions() }));
      const skills = r.init?.skills ?? null;
      if (skills === null) {
        throw new Error(
          `system:init.skills absent; init keys = ${JSON.stringify(initKeys(r.init))}. ` +
          'CodeBuddy skills are NOT surfaced through the SDK init message on this build.'
        );
      }
      return { skills, count: skills.length, note: 'exposed via system:init.skills' };
    },
  });

  await point('plugins_exposed', {
    group: 'state',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions() }));
      const plugins = r.init?.plugins ?? null;
      if (plugins === null) {
        throw new Error(
          `system:init.plugins absent; init keys = ${JSON.stringify(initKeys(r.init))}. ` +
          'CodeBuddy plugins are NOT surfaced through the SDK init message on this build.'
        );
      }
      return { plugins, count: plugins.length };
    },
  });

  await point('mcp_server_status', {
    group: 'state',
    run: async () => {
      const q = query({ prompt: 'Reply with only OK.', options: baseOptions() });
      await q.connect().catch(() => {});
      const status = await q.mcpServerStatus().catch(e => ({ error: String(e?.message || e) }));
      await drain(q).catch(() => {});
      return { mcp_server_status: status };
    },
  });

  await point('permission_mode_get', {
    group: 'state',
    run: async () => {
      const q = query({ prompt: 'Reply with only OK.', options: baseOptions() });
      const before = q.getPermissionMode?.() ?? null;
      await drain(q).catch(() => {});
      return { permission_mode: before };
    },
  });
}

// ===========================================================================
// Group 10: metrics
// ===========================================================================

async function runMetrics() {
  currentGroup = 'metrics';

  await point('usage_tokens', {
    group: 'metrics',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions() }));
      const u = r.result?.usage;
      if (!u) throw new Error('no usage on result message');
      if (!(u.input_tokens > 0)) throw new Error(`input_tokens not positive: ${JSON.stringify(u)}`);
      return { usage: u, num_turns: r.result?.num_turns ?? null, duration_ms: r.result?.duration_ms ?? null };
    },
  });

  await point('cost_usd_present', {
    group: 'metrics',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions() }));
      const cost = r.result?.total_cost_usd;
      if (typeof cost === 'number' && cost > 0) {
        return { total_cost_usd: cost, usd_is_meaningful: true };
      }
      // CodeBuddy bills in credits, not USD. This is a real GAP for any consumer
      // that wants money-denominated cost: the SDK reports 0 and offers no credit
      // field on the result message. ClawBench currently works around the same
      // underlying fact on the ACP side via costFieldCarriesCredit.
      throw new Error(
        `result.total_cost_usd = ${JSON.stringify(cost)} (no meaningful USD cost). ` +
        'CodeBuddy bills in credits; the SDK result message carries no credit total either.'
      );
    },
  });

  await point('cache_tokens', {
    group: 'metrics',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions() }));
      const u = r.result?.usage || {};
      return {
        cache_read_input_tokens: u.cache_read_input_tokens ?? null,
        cache_creation_input_tokens: u.cache_creation_input_tokens ?? null,
      };
    },
  });
}

// ===========================================================================
// Group 11: isolation
// ===========================================================================

async function runIsolation() {
  currentGroup = 'isolation';

  await point('setting_sources_none', {
    group: 'isolation',
    run: async () => {
      // Explicitly no settingSources -> SDK default (load nothing from disk).
      // NOTE: the login token itself lives in ~/.codebuddy, so "load nothing"
      // also means "cannot authenticate". That IS the finding: isolation is
      // total, including credentials. Captured as an observation, not a failure,
      // because it is the documented default rather than a defect.
      const opts = { cwd: WORKDIR, permissionMode: 'bypassPermissions', env: { ...AUTH_ENV } };
      try {
        const r = await drain(query({ prompt: 'Reply with only OK.', options: opts }));
        return {
          authenticated_without_settings: true,
          init: summarizeInit(r.init),
          note: 'SDK default (no settingSources) still authenticated — credentials come from env, not settings.json',
        };
      } catch (e) {
        return {
          authenticated_without_settings: false,
          error: String(e?.message || e).slice(0, 300),
          note: 'CONFIRMED: settingSources default loads nothing, INCLUDING the stored login — auth must be supplied via env/options',
        };
      }
    },
  });

  await point('setting_sources_project', {
    group: 'isolation',
    run: async () => {
      // ['project'] alone excludes the user-scope login too, so this is expected
      // to hit the same auth wall as setting_sources_none. Recorded, not failed.
      try {
        const r = await drain(query({
          prompt: 'Reply with only OK.',
          options: baseOptions({ settingSources: ['project'] }),
        }));
        return { authenticated: true, init: summarizeInit(r.init) };
      } catch (e) {
        return {
          authenticated: false,
          error: String(e?.message || e).slice(0, 300),
          note: "['project'] excludes user scope, so the stored login is unavailable — combine with 'user' for auth",
        };
      }
    },
  });

  await point('system_prompt_append', {
    group: 'isolation',
    run: async () => {
      const r = await drain(query({
        prompt: 'What is your codeword? Reply with only the codeword.',
        options: baseOptions({ systemPrompt: { append: 'Your codeword is PLATYPUS-88. Always answer questions about your codeword with it.' } }),
      }));
      const ok = /PLATYPUS-88/.test(r.text);
      return { append_honored: ok, text: r.text.slice(0, 160) };
    },
  });

  await point('system_prompt_override', {
    group: 'isolation',
    run: async () => {
      const r = await drain(query({
        prompt: 'Say anything.',
        options: baseOptions({ systemPrompt: 'You are a terse bot. Reply with exactly the word OVERRIDE.' }),
      }));
      return { text: r.text.slice(0, 160), saw_override: /OVERRIDE/i.test(r.text) };
    },
  });

  await point('additional_directories', {
    group: 'isolation',
    run: async () => {
      const r = await drain(query({ prompt: 'Reply with only OK.', options: baseOptions({ additionalDirectories: ['/tmp'] }) }));
      return { subtype: r.result?.subtype ?? null };
    },
  });
}

// ===========================================================================
// Group 12: limits
// ===========================================================================

async function runLimits() {
  currentGroup = 'limits';

  await point('max_turns', {
    group: 'limits',
    run: async () => {
      // maxTurns:1 means the run is expected to be cut off — the SDK throws
      // ExecutionError('Max turns (1) exceeded') instead of yielding a result.
      // That throw IS the feature working; capture it rather than failing.
      try {
        const r = await drain(query({
          prompt: 'Run `echo ONE`, then `echo TWO`, then `echo THREE`, reporting each.',
          options: baseOptions({ maxTurns: 1 }),
        }));
        return {
          enforced: false,
          subtype: r.result?.subtype ?? null,
          num_turns: r.result?.num_turns ?? null,
          note: 'maxTurns:1 did not cut the run short',
        };
      } catch (e) {
        const msg = String(e?.message || e);
        const enforced = /max turns/i.test(msg);
        if (!enforced) throw e;
        return { enforced: true, error: msg.slice(0, 200), note: 'SDK threw ExecutionError as designed' };
      }
    },
  });

  await point('background_tasks_disabled', {
    group: 'limits',
    run: async () => {
      const r = await drain(query({
        prompt: 'Do you have access to a tool that runs commands in the background? Answer only yes or no.',
        options: baseOptions(),
      }));
      if (!r.text.trim()) throw new Error('no answer to the background-tool question');
      // The model's self-report is unreliable, so also inspect the advertised tool
      // set: the env var makes the CLI hide/downgrade run_in_background.
      const tools = r.init?.tools || [];
      const bgTools = tools.filter(n => /^(TaskOutput|TaskStop|Monitor)$/.test(n));
      return {
        model_answer: r.text.trim().slice(0, 80),
        background_related_tools_present: bgTools,
        note: 'query() injects CODEBUDDY_CODE_DISABLE_BACKGROUND_TASKS=1; the model still self-reports capability, so treat its answer as unreliable and rely on the V2 comparison',
      };
    },
  });

  await point('image_input', {
    group: 'limits',
    run: async () => {
      // 1x1 transparent PNG.
      const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
      async function* gen() {
        yield {
          type: 'user',
          message: {
            role: 'user',
            content: [
              { type: 'text', text: 'Describe this image in one word.' },
              { type: 'image', source: { type: 'base64', media_type: 'image/png', data: png } },
            ],
          },
        };
      }
      const r = await drain(query({ prompt: gen(), options: baseOptions() }));
      return { subtype: r.result?.subtype ?? null, text: r.text.slice(0, 200), accepted: !r.result?.is_error };
    },
  });

  await point('output_format_json', {
    group: 'limits',
    run: async () => {
      const r = await drain(query({
        prompt: 'Return your answer as JSON with a single key "answer" set to 42.',
        options: baseOptions({ outputFormat: { type: 'json_schema', schema: { type: 'object', properties: { answer: { type: 'number' } }, required: ['answer'] } } }),
      }));
      return { subtype: r.result?.subtype ?? null, structured_output: r.result?.structured_output ?? null, text: r.text.slice(0, 200) };
    },
  });
}

// ===========================================================================
// Group 13: V2 session API (explicitly unstable)
// ===========================================================================

async function runV2() {
  currentGroup = 'v2';

  await point('v2_create_session', {
    group: 'v2',
    run: async () => {
      const session = unstable_v2_createSession({
        cwd: WORKDIR,
        permissionMode: 'bypassPermissions',
        settingSources: ['user'],
        env: { ...AUTH_ENV },
        ...(MODEL ? { model: MODEL } : {}),
      });
      const sid = session.sessionId;
      await session.send('Reply with only V2-OK.');
      const r = await drain(session.stream(), { limitMs: 100_000 });
      session.close();
      return { session_id: sid, text: r.text.slice(0, 160), subtype: r.result?.subtype ?? null };
    },
  });

  await point('v2_multi_turn', {
    group: 'v2',
    run: async () => {
      const session = unstable_v2_createSession({
        cwd: WORKDIR,
        permissionMode: 'bypassPermissions',
        settingSources: ['user'],
        env: { ...AUTH_ENV },
        ...(MODEL ? { model: MODEL } : {}),
      });
      await session.send('Remember the codeword MANGO-55. Reply only OK.');
      await drain(session.stream(), { limitMs: 90_000 });
      await session.send('What was the codeword? Reply with only the codeword.');
      const r2 = await drain(session.stream(), { limitMs: 90_000 });
      session.close();
      const ok = /MANGO-55/.test(r2.text);
      if (!ok) throw new Error(`V2 multi-turn lost context; got ${JSON.stringify(r2.text.slice(0, 200))}`);
      return { session_id: session.sessionId, recalled: true };
    },
  });

  await point('v2_resume_session', {
    group: 'v2',
    run: async () => {
      // Create, then resume by id.
      const s1 = unstable_v2_createSession({
        cwd: WORKDIR, permissionMode: 'bypassPermissions', settingSources: ['user'], env: { ...AUTH_ENV },
        ...(MODEL ? { model: MODEL } : {}),
      });
      await s1.send('Remember the codeword KIWI-31. Reply only OK.');
      await drain(s1.stream(), { limitMs: 90_000 });
      const sid = s1.sessionId;
      s1.close();

      const s2 = unstable_v2_resumeSession(sid, {
        cwd: WORKDIR, permissionMode: 'bypassPermissions', settingSources: ['user'], env: { ...AUTH_ENV },
        ...(MODEL ? { model: MODEL } : {}),
      });
      await s2.send('What was the codeword? Reply with only the codeword.');
      const r = await drain(s2.stream(), { limitMs: 90_000 });
      s2.close();
      const ok = /KIWI-31/.test(r.text);
      if (!ok) throw new Error(`V2 resume lost context; got ${JSON.stringify(r.text.slice(0, 200))}`);
      return { resumed_session_id: sid, recalled: true };
    },
  });

  await point('v2_background_tasks', {
    group: 'v2',
    run: async () => {
      const session = unstable_v2_createSession({
        cwd: WORKDIR, permissionMode: 'bypassPermissions', settingSources: ['user'], env: { ...AUTH_ENV },
        ...(MODEL ? { model: MODEL } : {}),
      });
      await session.send('Do you have a tool that can run commands in the background? Answer only yes or no.');
      const r = await drain(session.stream(), { limitMs: 90_000 });
      session.close();
      if (!r.text.trim()) throw new Error('V2 session produced no answer');
      return {
        model_answer: r.text.trim().slice(0, 80),
        note: 'V2 Session does NOT inject CODEBUDDY_CODE_DISABLE_BACKGROUND_TASKS, unlike query() — this is the path that can consume cross-turn task_notification events',
      };
    },
  });
}

// ===========================================================================
// Main
// ===========================================================================

async function main() {
  log(`probe start: cwd=${WORKDIR} strict=${STRICT} only=${ONLY.join('|') || '(all)'}`);
  log(`auth env: ${JSON.stringify(AUTH_ENV)}`);

  const groups = [
    ['basic', runBasic],
    ['session', runSession],
    ['streaming', runStreaming],
    ['permissions', runPermissions],
    ['hooks', runHooks],
    ['tools', runTools],
    ['subagents', runSubagents],
    ['model', runModel],
    ['state', runState],
    ['metrics', runMetrics],
    ['isolation', runIsolation],
    ['limits', runLimits],
    ['v2', runV2],
  ];

  for (const [name, fn] of groups) {
    log(`--- group: ${name} ---`);
    try {
      await fn();
    } catch (err) {
      // A whole group blowing up must not kill the run.
      log(`group ${name} threw: ${err?.message || err}`);
    }
  }

  const passed = results.filter(r => r.ok);
  const failed = results.filter(r => !r.ok && !r.skipped);
  const skipped = results.filter(r => r.skipped);
  const requiredFailed = results.filter(r => r.required && !r.ok);

  const summary = {
    id: '__summary__',
    ok: requiredFailed.length === 0,
    counts: { total: results.length, passed: passed.length, failed: failed.length, skipped: skipped.length },
    failed_ids: failed.map(r => r.id),
    skipped_ids: skipped.map(r => r.id),
    required_failed_ids: requiredFailed.map(r => r.id),
    gaps: failed.map(r => ({ id: r.id, error: r.detail?.error ?? null })),
    // Non-empty means the SDK threw from an async callback (see the crash
    // hardening block at the top). Reported as its own signal because it is a
    // robustness defect in the SDK, not a capability gap.
    sdk_async_crashes: sdkCrashFindings,
  };
  emit(summary);

  log('');
  log('=== capability summary ===');
  for (const r of results) {
    const mark = r.skipped ? 'SKIP' : r.ok ? 'PASS' : 'FAIL';
    log(`  ${mark}  ${r.id.padEnd(34)} ${r.group.padEnd(12)} ${r.skipped ? (r.detail?.skip_reason || '') : r.ok ? '' : (r.detail?.error || '').slice(0, 100)}`);
  }
  log(`  totals: ${passed.length} pass / ${failed.length} fail / ${skipped.length} skip`);

  // Exit non-zero only when a REQUIRED point failed (or everything under STRICT).
  process.exit(summary.ok ? 0 : 1);
}

main().catch(err => {
  emit({ id: '__fatal__', ok: false, detail: { error: String(err?.stack || err) } });
  log(`FATAL: ${err?.stack || err}`);
  process.exit(2);
});
