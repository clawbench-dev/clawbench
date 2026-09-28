// @ts-check
/**
 * Pi Coding Agent SDK — capability probe harness.
 *
 * Purpose: empirically answer "can ClawBench add an SDK-based transport for Pi
 * (in addition to its existing CLI line-parsing transport)?" by exercising every
 * SDK capability ClawBench's ACP transport relies on and reporting, per
 * capability point, what actually works.
 *
 * Output contract (consumed by internal/ai/pi_sdk_integration_test.go):
 *   - one NDJSON line per test point on stdout:
 *       {"id":"basic_query","group":"basic","ok":true,"ms":1234,"detail":{...}}
 *   - a final line: {"id":"__summary__","ok":<all required passed>,"counts":{...}}
 *   - human-readable diagnostics go to stderr only (never stdout).
 *
 * A failing point never aborts the run: each is independently try/caught, so one
 * broken capability cannot mask the rest.
 *
 * Env:
 *   PI_SDK_PROBE_STRICT=1   treat every point as required (default: only REQUIRED_POINTS)
 *   PI_SDK_PROBE_ONLY=a,b   run only these point ids
 *   PI_SDK_PROBE_MODEL=...  override the model used by every point
 *   PI_SDK_PROBE_CWD=...    working directory for every point (created by the Go test)
 *
 * Auth: inherited from the parent process. The SDK reads credentials from
 * ~/.pi/agent/auth.json (or the agentDir override) — no env vars are required on
 * this machine, unlike the CodeBuddy SDK probe.
 *
 * Integration note: the SDK embeds the agent in-process. `createAgentSession()`
 * builds a ModelRuntime + SettingsManager + SessionManager + ResourceLoader and
 * returns an AgentSession; events arrive via `session.subscribe()`.
 */

import {
  createAgentSession,
  SessionManager,
  ModelRuntime,
  DefaultResourceLoader,
  defineTool,
  getAgentDir,
  loadSkillsFromDir,
} from '@earendil-works/pi-coding-agent';
import { Type } from 'typebox';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

// ---------------------------------------------------------------------------
// Crash hardening
// ---------------------------------------------------------------------------
//
// Mirrors the CodeBuddy probe: an async throw must not take the whole run with
// it, or a single defect would hide every remaining capability point.
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

const STRICT = process.env.PI_SDK_PROBE_STRICT === '1';
const ONLY = (process.env.PI_SDK_PROBE_ONLY || '').split(',').map((s) => s.trim()).filter(Boolean);
const MODEL = process.env.PI_SDK_PROBE_MODEL || undefined;

/**
 * Points that MUST pass for the SDK to be a viable ClawBench transport at all.
 * Everything else is recorded as an observation. Under PI_SDK_PROBE_STRICT=1
 * every point is required, which turns this probe into a regression gate.
 */
const REQUIRED_POINTS = new Set([
  'basic_query',
  'text_delta_streaming',
  'session_id_capture',
  'cwd_honored',
  'resume_continue_recent',
  'resume_by_id',
  'builtin_tool_use',
  'toolcall_end_events',
  'usage_tokens',
  'abort_mid_run',
]);

/** Working directory for every point. Created by the Go test. */
const WORKDIR = process.env.PI_SDK_PROBE_CWD || process.cwd();

/** Scratch root for per-point temp dirs (session files, skills, exports). */
const SCRATCH = fs.mkdtempSync(path.join(os.tmpdir(), 'pi-sdk-probe-'));

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
 * @param {{run?:(ctx:any)=>Promise<any>,group?:string,timeoutMs?:number,skipReason?:string}} spec
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

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

// ---------------------------------------------------------------------------
// Session helpers
// ---------------------------------------------------------------------------

/** mkdtemp under the scratch root. */
function scratchDir(prefix) {
  return fs.mkdtempSync(path.join(SCRATCH, prefix + '-'));
}

/**
 * Create a session, run `fn`, always dispose. Keeps each point isolated.
 * @param {object} opts  createAgentSession options
 * @param {(session:any, ctx:any)=>Promise<any>} fn
 */
async function withSession(opts, fn) {
  const created = await createAgentSession(opts);
  try {
    return await fn(created.session, created);
  } finally {
    try { created.session.dispose(); } catch { /* dispose is best-effort */ }
  }
}

/**
 * Subscribe and collect a structured event summary while `fn` runs.
 * Returns the summary; the subscription is always removed.
 */
async function observe(session, fn, { collectText = false } = {}) {
  const types = {};
  const assistantTypes = {};
  const toolCalls = [];
  const toolExecutions = [];
  const usageSamples = [];
  const stopReasons = [];
  let text = '';
  let thinking = '';
  let settled = 0;
  let agentEnd = 0;

  const unsub = session.subscribe((event) => {
    types[event.type] = (types[event.type] || 0) + 1;
    if (event.type === 'message_update') {
      const a = event.assistantMessageEvent;
      assistantTypes[a.type] = (assistantTypes[a.type] || 0) + 1;
      if (a.type === 'text_delta') text += a.delta || '';
      if (a.type === 'thinking_delta') thinking += a.delta || '';
      if (a.type === 'toolcall_end') toolCalls.push(a.toolCall);
    }
    if (event.type === 'tool_execution_end') {
      const texts = (event.result?.content || [])
        .filter((c) => c && c.type === 'text' && typeof c.text === 'string')
        .map((c) => c.text);
      toolExecutions.push({ name: event.toolName, isError: !!event.isError, text: texts.join('\n') });
    }
    if (event.type === 'message_end' && event.message?.role === 'assistant') {
      if (event.message.usage) usageSamples.push(event.message.usage);
      if (event.message.stopReason) stopReasons.push(event.message.stopReason);
    }
    if (event.type === 'agent_end') agentEnd++;
    if (event.type === 'agent_settled') settled++;
  });

  try {
    await fn();
  } finally {
    unsub();
  }

  return {
    types,
    assistantTypes,
    toolCalls,
    toolExecutions,
    usageSamples,
    stopReasons,
    text,
    thinking,
    agentEnd,
    settled,
    // Convenience: last cumulative usage, mirroring PiStreamParser's metadata.
    lastUsage: usageSamples.length ? usageSamples[usageSamples.length - 1] : null,
  };
}

// ---------------------------------------------------------------------------
// Groups
// ---------------------------------------------------------------------------

async function runBasic() {
  currentGroup = 'basic';

  await point('basic_query', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        await session.prompt('Reply with exactly the word PONG and nothing else.');
        const last = session.getLastAssistantText();
        assert(typeof last === 'string' && last.length > 0, `expected non-empty assistant text, got ${JSON.stringify(last)}`);
        return { lastText: last };
      }),
  });

  await point('text_delta_streaming', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const obs = await observe(session, () => session.prompt('Say the word STREAMING.'));
        assert(obs.assistantTypes.text_delta > 0, 'no text_delta events observed');
        assert(obs.types.message_update > 0, 'no message_update events observed');
        assert(obs.assistantTypes.text_end > 0, 'no text_end event observed');
        return { assistantEventTypes: obs.assistantTypes, streamedChars: obs.text.length };
      }),
  });

  await point('session_id_capture', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        await session.prompt('Say OK.');
        const id = session.sessionId;
        assert(typeof id === 'string' && id.length > 0, `expected a session id, got ${JSON.stringify(id)}`);
        return { sessionId: id };
      }),
  });

  await point('cwd_honored', {
    run: async () => {
      const dir = scratchDir('cwd');
      fs.writeFileSync(path.join(dir, 'cwd-marker.txt'), 'MARKER_CONTENT_9137\n');
      return withSession({ cwd: dir, sessionManager: SessionManager.inMemory() }, async (session) => {
        const obs = await observe(session, () =>
          session.prompt('Use the bash tool to run: cat cwd-marker.txt'),
        );
        assert(obs.toolCalls.length > 0, 'model did not call a tool');
        const bash = obs.toolExecutions.find((t) => t.name === 'bash');
        assert(bash, `bash never executed: ${JSON.stringify(obs.toolExecutions)}`);
        assert(!bash.isError, `bash errored: ${bash.text}`);
        // The tool ran in `dir`, so the marker written there must come back.
        assert(/MARKER_CONTENT_9137/.test(bash.text),
          `bash did not run in the requested cwd; output=${JSON.stringify(bash.text.slice(0, 120))}`);
        return { cwd: dir, bashOutput: bash.text.trim().slice(0, 80) };
      });
    },
  });

  await point('thinking_events', {
    run: async () =>
      withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), thinkingLevel: 'low' },
        async (session) => {
          const obs = await observe(session, () =>
            session.prompt('Think step by step: what is 17 * 23? Reply with just the number.'),
          );
          const hasThinking = obs.assistantTypes.thinking_delta > 0;
          return {
            assistantEventTypes: obs.assistantTypes,
            thinkingChars: obs.thinking.length,
            supportsThinking: session.supportsThinking(),
            availableLevels: session.getAvailableThinkingLevels(),
            // Not asserted: whether the model actually emits thinking is model-dependent.
            emittedThinking: hasThinking,
          };
        },
      ),
  });

  await point('model_reported', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const m = session.model;
        assert(m && m.provider && m.id, `expected a resolved model, got ${JSON.stringify(m)}`);
        return { model: `${m.provider}/${m.id}` };
      }),
  });
}

async function runSession() {
  currentGroup = 'session';

  await point('resume_continue_recent', {
    run: async () => {
      const cwd = scratchDir('resume-cwd');
      const dir = scratchDir('resume-sessions');
      const first = await createAgentSession({ cwd, sessionManager: SessionManager.create(cwd, dir) });
      let file;
      try {
        await first.session.prompt('Remember the secret number 4242. Reply OK.');
        file = first.session.sessionFile;
        assert(file && fs.existsSync(file), `expected a persisted session file, got ${JSON.stringify(file)}`);
      } finally {
        first.session.dispose();
      }
      const second = await createAgentSession({ cwd, sessionManager: SessionManager.continueRecent(cwd, dir) });
      try {
        await second.session.prompt('What was the secret number? Reply with just the number.');
        const answer = second.session.getLastAssistantText();
        assert(/\b4242\b/.test(answer || ''), `context not preserved across continueRecent; answer=${JSON.stringify(answer)}`);
        return { sessionFile: file, answer };
      } finally {
        second.session.dispose();
      }
    },
  });

  await point('resume_by_id', {
    run: async () => {
      const cwd = scratchDir('resumeid-cwd');
      const dir = scratchDir('resumeid-sessions');
      const first = await createAgentSession({ cwd, sessionManager: SessionManager.create(cwd, dir) });
      let id;
      try {
        await first.session.prompt('Remember the secret number 555. Reply OK.');
        id = first.session.sessionId;
      } finally {
        first.session.dispose();
      }
      const file = SessionManager.findById(cwd, id, dir);
      assert(file, `SessionManager.findById could not locate session ${id}`);
      const second = await createAgentSession({ cwd, sessionManager: SessionManager.open(file, dir) });
      try {
        await second.session.prompt('What was the secret number? Reply with just the number.');
        const answer = second.session.getLastAssistantText();
        assert(/\b555\b/.test(answer || ''), `context not preserved across resume-by-id; answer=${JSON.stringify(answer)}`);
        return { sessionId: id, resumedSameId: second.session.sessionId === id, answer };
      } finally {
        second.session.dispose();
      }
    },
  });

  await point('session_list', {
    run: async () => {
      const cwd = scratchDir('list-cwd');
      const dir = scratchDir('list-sessions');
      for (const n of [1, 2]) {
        const s = await createAgentSession({ cwd, sessionManager: SessionManager.create(cwd, dir) });
        try { await s.session.prompt(`Say the number ${n}.`); } finally { s.session.dispose(); }
      }
      const list = await SessionManager.list(cwd, dir);
      assert(list.length >= 2, `expected >=2 listed sessions, got ${list.length}`);
      return { count: list.length, sample: list.slice(0, 2).map((s) => ({ id: s.id, firstMessage: String(s.firstMessage || '').slice(0, 40) })) };
    },
  });

  await point('fork_session', {
    run: async () => {
      const cwd = scratchDir('fork-cwd');
      const dir = scratchDir('fork-sessions');
      const a = await createAgentSession({ cwd, sessionManager: SessionManager.create(cwd, dir) });
      let src;
      try {
        await a.session.prompt('Remember the secret number 777. Reply OK.');
        src = a.session.sessionFile;
      } finally {
        a.session.dispose();
      }
      assert(src, 'source session file missing');
      const b = await createAgentSession({ cwd, sessionManager: SessionManager.forkFrom(src, cwd, dir) });
      try {
        await b.session.prompt('What was the secret number? Reply with just the number.');
        const answer = b.session.getLastAssistantText();
        assert(/\b777\b/.test(answer || ''), `fork did not inherit context; answer=${JSON.stringify(answer)}`);
        return { source: src, forkedFile: b.session.sessionFile, answer };
      } finally {
        b.session.dispose();
      }
    },
  });

  await point('in_memory_no_persistence', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        await session.prompt('Say OK.');
        return { sessionFile: session.sessionFile ?? null, persisted: !!session.sessionFile };
      }),
  });
}

async function runStreaming() {
  currentGroup = 'streaming';

  await point('toolcall_end_events', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const obs = await observe(session, () =>
          session.prompt('Call the bash tool exactly once with command: echo TOOLCALL_PROBE'),
        );
        assert(obs.toolCalls.length > 0, 'no toolcall_end events observed');
        const tc = obs.toolCalls[0];
        assert(tc && tc.name, `toolcall_end carried no tool name: ${JSON.stringify(tc)}`);
        return {
          toolCalls: obs.toolCalls.map((t) => ({ name: t.name, id: t.id, args: t.arguments })),
          assistantEventTypes: obs.assistantTypes,
        };
      }),
  });

  await point('tool_execution_events', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const obs = await observe(session, () =>
          session.prompt('Call the bash tool exactly once with command: echo EXEC_PROBE'),
        );
        assert(obs.toolExecutions.length > 0, 'no tool_execution_end events observed');
        assert(obs.types.tool_execution_start > 0, 'no tool_execution_start events observed');
        return {
          executions: obs.toolExecutions,
          eventCounts: {
            start: obs.types.tool_execution_start || 0,
            update: obs.types.tool_execution_update || 0,
            end: obs.types.tool_execution_end || 0,
          },
        };
      }),
  });

  await point('usage_tokens', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const obs = await observe(session, () => session.prompt('Say OK.'));
        const u = obs.lastUsage;
        assert(u, 'no usage on the completed assistant message');
        assert(u.input > 0 || u.output > 0, `usage carried no tokens: ${JSON.stringify(u)}`);
        return { usage: u, stopReasons: obs.stopReasons };
      }),
  });

  await point('cost_usd', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const obs = await observe(session, () => session.prompt('Say OK.'));
        const u = obs.lastUsage;
        assert(u, 'no usage on the completed assistant message');
        const total = u.cost?.total ?? null;
        return { costTotal: total, cost: u.cost ?? null, hasCostField: u.cost != null };
      }),
  });

  await point('stop_reason', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const obs = await observe(session, () => session.prompt('Say OK.'));
        assert(obs.stopReasons.length > 0, 'no stopReason on completed assistant messages');
        return { stopReasons: obs.stopReasons };
      }),
  });

  await point('agent_lifecycle_events', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const obs = await observe(session, () => session.prompt('Say OK.'));
        assert(obs.agentEnd > 0, 'no agent_end event observed');
        assert(obs.settled > 0, 'no agent_settled event observed');
        assert(obs.types.turn_start > 0, 'no turn_start event observed');
        assert(obs.types.turn_end > 0, 'no turn_end event observed');
        return { agentEnd: obs.agentEnd, settled: obs.settled, turnStart: obs.types.turn_start, turnEnd: obs.types.turn_end };
      }),
  });

  await point('abort_mid_run', {
    run: async () => {
      const session = (await createAgentSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() })).session;
      try {
        const pending = session.prompt('Count slowly from 1 to 500, one number per line.');
        await new Promise((r) => setTimeout(r, 900));
        const t0 = Date.now();
        await session.abort();
        const abortMs = Date.now() - t0;
        // abort() must settle the run rather than reject it (observed behaviour).
        let outcome = 'resolved';
        try { await pending; } catch (e) { outcome = 'rejected:' + String(e?.message || e); }
        await session.waitForIdle();
        return { outcome, abortMs, partialText: (session.getLastAssistantText() || '').slice(0, 60) };
      } finally {
        session.dispose();
      }
    },
  });

  await point('steer_mid_run', {
    run: async () => {
      const session = (await createAgentSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() })).session;
      try {
        const pending = session.prompt('Count slowly from 1 to 500, one number per line.');
        await new Promise((r) => setTimeout(r, 900));
        session.steer('Stop counting and reply with exactly the word STEERED.');
        await pending;
        const last = session.getLastAssistantText() || '';
        return { lastText: last.slice(-80), steered: /STEERED/i.test(last) };
      } finally {
        session.dispose();
      }
    },
  });

  await point('follow_up_queue', {
    run: async () => {
      const session = (await createAgentSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() })).session;
      try {
        const pending = session.prompt('Write one short sentence about the ocean.');
        await new Promise((r) => setTimeout(r, 700));
        session.followUp('After that, also reply with exactly the word FOLLOWED.');
        await pending;
        const last = session.getLastAssistantText() || '';
        return { lastText: last.slice(-80), followed: /FOLLOWED/i.test(last) };
      } finally {
        session.dispose();
      }
    },
  });
}

async function runTools() {
  currentGroup = 'tools';

  await point('builtin_tool_use', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const obs = await observe(session, () =>
          session.prompt('Call the bash tool exactly once with command: echo BUILTIN_PROBE'),
        );
        assert(obs.toolExecutions.length > 0, 'builtin bash tool never executed');
        assert(obs.toolExecutions.every((t) => !t.isError), `builtin tool errored: ${JSON.stringify(obs.toolExecutions)}`);
        return { executions: obs.toolExecutions, lastText: (session.getLastAssistantText() || '').slice(0, 80) };
      }),
  });

  await point('builtin_tool_names', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => ({
        active: session.getActiveToolNames(),
        all: session.getAllTools().map((t) => t.name),
      })),
  });

  await point('tool_whitelist', {
    run: async () =>
      withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), tools: ['read', 'grep'] },
        async (session) => {
          const active = session.getActiveToolNames();
          assert(active.length === 2 && active.includes('read') && active.includes('grep'),
            `whitelist not applied: ${JSON.stringify(active)}`);
          return { active };
        },
      ),
  });

  await point('tool_excludelist', {
    run: async () =>
      withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), excludeTools: ['bash', 'write'] },
        async (session) => {
          const active = session.getActiveToolNames();
          assert(!active.includes('bash') && !active.includes('write'),
            `denylist not applied: ${JSON.stringify(active)}`);
          return { active };
        },
      ),
  });

  await point('custom_tool', {
    run: async () => {
      const echo = defineTool({
        name: 'probe_echo',
        label: 'Probe Echo',
        description: 'Echoes the supplied text back with an ECHO: prefix.',
        parameters: Type.Object({ text: Type.String({ description: 'Text to echo' }) }),
        execute: async (_id, params) => ({
          content: [{ type: 'text', text: `ECHO:${params.text}` }],
          details: {},
        }),
      });
      return withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), customTools: [echo], tools: ['read', 'probe_echo'] },
        async (session) => {
          const obs = await observe(session, () =>
            session.prompt('Call the probe_echo tool with text=hello. Then reply DONE.'),
          );
          const called = obs.toolCalls.some((t) => t.name === 'probe_echo');
          assert(called, `custom tool never called; toolCalls=${JSON.stringify(obs.toolCalls.map((t) => t.name))}`);
          return { toolCalls: obs.toolCalls.map((t) => t.name), lastText: (session.getLastAssistantText() || '').slice(0, 60) };
        },
      );
    },
  });

  await point('extension_tool_call_hook', {
    run: async () => {
      const calls = [];
      const loader = new DefaultResourceLoader({
        cwd: WORKDIR,
        agentDir: getAgentDir(),
        extensionFactories: [
          (pi) => {
            pi.on('tool_call', async (e) => {
              calls.push(e.toolName);
              return undefined;
            });
          },
        ],
      });
      await loader.reload();
      return withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), resourceLoader: loader },
        async (session) => {
          await session.prompt('Call the bash tool exactly once with command: echo HOOK_PROBE');
          assert(calls.length > 0, 'extension tool_call handler never fired');
          return { hookedTools: calls };
        },
      );
    },
  });

  await point('extension_block_tool', {
    run: async () => {
      let blocked = 0;
      const loader = new DefaultResourceLoader({
        cwd: WORKDIR,
        agentDir: getAgentDir(),
        extensionFactories: [
          (pi) => {
            pi.on('tool_call', async (e) => {
              if (e.toolName === 'bash') {
                blocked++;
                return { block: true, reason: 'blocked by probe' };
              }
              return undefined;
            });
          },
        ],
      });
      await loader.reload();
      return withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), resourceLoader: loader },
        async (session) => {
          const obs = await observe(session, () =>
            session.prompt('You are testing tool plumbing. Call the bash tool exactly once with command: echo BLOCKED_TEST'),
          );
          assert(blocked > 0, 'extension block handler never fired for bash');
          const bashErrored = obs.toolExecutions.some((t) => t.name === 'bash' && t.isError);
          assert(bashErrored, `blocked tool did not surface as an error: ${JSON.stringify(obs.toolExecutions)}`);
          return { blocked, executions: obs.toolExecutions };
        },
      );
    },
  });

  await point('extension_register_command', {
    run: async () => {
      let handlerRan = 0;
      const loader = new DefaultResourceLoader({
        cwd: WORKDIR,
        agentDir: getAgentDir(),
        extensionFactories: [
          (pi) => {
            pi.registerCommand('probe-command', {
              description: 'Probe command',
              handler: async () => {
                handlerRan++;
              },
            });
          },
        ],
      });
      await loader.reload();
      return withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), resourceLoader: loader },
        async (session) => {
          // Extension commands are dispatched by prompt() without reaching the model.
          await session.prompt('/probe-command');
          assert(handlerRan === 1, `extension command handler ran ${handlerRan} time(s), expected exactly 1`);
          return { handlerRan, hasExtensionHandlers: session.hasExtensionHandlers('tool_call') };
        },
      );
    },
  });
}

async function runModelConfig() {
  currentGroup = 'model';

  await point('thinking_levels', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const levels = session.getAvailableThinkingLevels();
        assert(Array.isArray(levels) && levels.length > 0, `no thinking levels reported: ${JSON.stringify(levels)}`);
        return { levels, supportsThinking: session.supportsThinking(), current: session.thinkingLevel };
      }),
  });

  await point('set_thinking_level', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const before = session.thinkingLevel;
        session.setThinkingLevel('low');
        const after = session.thinkingLevel;
        assert(after === 'low', `setThinkingLevel('low') did not stick: ${after}`);
        return { before, after };
      }),
  });

  await point('available_models', {
    run: async () => {
      const rt = await ModelRuntime.create();
      const available = await rt.getAvailable();
      assert(available.length > 0, 'ModelRuntime.getAvailable() returned no models');
      return {
        count: available.length,
        sample: available.slice(0, 5).map((m) => `${m.provider}/${m.id}`),
      };
    },
  });

  await point('set_model_live', {
    run: async () => {
      const rt = await ModelRuntime.create();
      const available = await rt.getAvailable();
      return withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory(), modelRuntime: rt }, async (session) => {
        const current = session.model;
        const alt = available.find((m) => !(m.provider === current.provider && m.id === current.id));
        if (!alt) return { skipped: true, reason: 'only one model available' };
        await session.setModel(alt);
        const now = session.model;
        assert(now.provider === alt.provider && now.id === alt.id,
          `setModel did not apply: ${now.provider}/${now.id} != ${alt.provider}/${alt.id}`);
        return { before: `${current.provider}/${current.id}`, after: `${now.provider}/${now.id}` };
      });
    },
  });

  await point('system_prompt_append', {
    run: async () => {
      const loader = new DefaultResourceLoader({
        cwd: WORKDIR,
        agentDir: getAgentDir(),
        appendSystemPromptOverride: (base) => [...base, 'Always end every response with the token ZZZ_APPENDED.'],
      });
      await loader.reload();
      return withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), resourceLoader: loader },
        async (session) => {
          await session.prompt('Say hello.');
          const last = session.getLastAssistantText() || '';
          const sys = session.systemPrompt || '';
          return { honored: /ZZZ_APPENDED/.test(last), systemPromptHasToken: /ZZZ_APPENDED/.test(sys), lastText: last.slice(-60) };
        },
      );
    },
  });

  await point('system_prompt_override', {
    run: async () => {
      const loader = new DefaultResourceLoader({
        cwd: WORKDIR,
        agentDir: getAgentDir(),
        systemPromptOverride: () => 'You are a pirate. Always end responses with Arrr!',
        appendSystemPromptOverride: () => [],
      });
      await loader.reload();
      return withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), resourceLoader: loader },
        async (session) => {
          const sys = session.systemPrompt || '';
          assert(/pirate/i.test(sys), `systemPromptOverride not reflected in session.systemPrompt: ${sys.slice(0, 120)}`);
          return { systemPromptHead: sys.slice(0, 120) };
        },
      );
    },
  });

  await point('context_files_override', {
    run: async () => {
      const loader = new DefaultResourceLoader({
        cwd: WORKDIR,
        agentDir: getAgentDir(),
        agentsFilesOverride: (current) => ({
          agentsFiles: [...current.agentsFiles, { path: '/virtual/PROBE_AGENTS.md', content: '# Probe\n- marker: PROBE_CONTEXT_4242' }],
        }),
      });
      await loader.reload();
      return withSession(
        { cwd: WORKDIR, sessionManager: SessionManager.inMemory(), resourceLoader: loader },
        async (session) => {
          const files = loader.getAgentsFiles().agentsFiles;
          const sys = session.systemPrompt || '';
          return {
            contextFileCount: files.length,
            markerInSystemPrompt: /PROBE_CONTEXT_4242/.test(sys),
            paths: files.map((f) => f.path),
          };
        },
      );
    },
  });
}

async function runResources() {
  currentGroup = 'resources';

  await point('skills_discovery', {
    run: async () => {
      const dir = scratchDir('skills');
      fs.mkdirSync(path.join(dir, 'demo-skill'));
      fs.writeFileSync(
        path.join(dir, 'demo-skill', 'SKILL.md'),
        '---\nname: demo-skill\ndescription: A probe skill\n---\n\nDo demo things.\n',
      );
      const res = loadSkillsFromDir({ dir });
      const names = (res.skills || []).map((s) => s.name);
      assert(names.includes('demo-skill'), `skill not discovered: ${JSON.stringify(names)}`);
      return { skills: names };
    },
  });

  await point('prompt_templates', {
    run: async () => {
      const loader = new DefaultResourceLoader({
        cwd: WORKDIR,
        agentDir: getAgentDir(),
        promptsOverride: (current) => ({
          prompts: [
            ...current.prompts,
            {
              name: 'probe-template',
              description: 'Probe template',
              filePath: '/virtual/prompts/probe.md',
              sourceInfo: { source: 'sdk', path: '/virtual/prompts/probe.md' },
              content: '# Probe Template\nDo the probe thing.',
            },
          ],
          diagnostics: current.diagnostics,
        }),
      });
      await loader.reload();
      const prompts = loader.getPrompts().prompts;
      const names = prompts.map((p) => p.name);
      assert(names.includes('probe-template'), `prompt template not registered: ${JSON.stringify(names)}`);
      return { prompts: names };
    },
  });

  await point('session_stats', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        await session.prompt('Call the bash tool exactly once with command: echo STATS_PROBE');
        const stats = session.getSessionStats();
        assert(stats.userMessages > 0, `stats reported no user messages: ${JSON.stringify(stats)}`);
        assert(stats.tokens.total > 0, `stats reported zero tokens: ${JSON.stringify(stats.tokens)}`);
        return stats;
      }),
  });

  await point('context_usage', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        await session.prompt('Say OK.');
        const usage = session.getContextUsage();
        assert(usage && usage.contextWindow > 0, `context usage unavailable: ${JSON.stringify(usage)}`);
        return usage;
      }),
  });

  await point('export_jsonl', {
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        await session.prompt('Say OK.');
        const out = path.join(scratchDir('export'), 'session.jsonl');
        const returned = session.exportToJsonl(out);
        const exists = fs.existsSync(out);
        const size = exists ? fs.statSync(out).size : 0;
        assert(exists && size > 0, `exportToJsonl produced no file (returned=${JSON.stringify(returned)}, size=${size})`);
        return { path: out, size, returnedType: typeof returned };
      }),
  });

  await point('export_html', {
    run: async () => {
      const cwd = scratchDir('html-cwd');
      const dir = scratchDir('html-sessions');
      const session = (await createAgentSession({ cwd, sessionManager: SessionManager.create(cwd, dir) })).session;
      try {
        await session.prompt('Say OK.');
        const out = path.join(scratchDir('html-out'), 'session.html');
        session.exportToHtml(out);
        const exists = fs.existsSync(out);
        const size = exists ? fs.statSync(out).size : 0;
        return { path: out, exists, size, note: exists ? '' : 'exportToHtml produced no file' };
      } finally {
        session.dispose();
      }
    },
  });

  await point('compaction', {
    // Compaction refuses trivially small sessions ("Nothing to compact"). The cut
    // point search walks backwards accumulating message sizes until it reaches
    // keepRecentTokens (20000 by default), so the session must hold more than
    // that much message content — a few short turns are not enough.
    timeoutMs: 300_000,
    run: async () =>
      withSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() }, async (session) => {
        const filler = 'The quick brown fox jumps over the lazy dog and then runs away quickly. '.repeat(200);
        for (let i = 0; i < 6; i++) {
          await session.prompt(
            `Repeat the following text verbatim, nothing else. Begin your reply with PART${i}:\n\n${filler}`,
          );
        }
        const result = await session.compact();
        assert(result && typeof result.summary === 'string' && result.summary.length > 0,
          `compact returned no summary: ${JSON.stringify(result)}`);
        return { summaryChars: result.summary.length, tokensBefore: result.tokensBefore, estimatedTokensAfter: result.estimatedTokensAfter };
      }),
  });
}

async function runRobustness() {
  currentGroup = 'robustness';

  await point('multiple_sessions_isolated', {
    run: async () => {
      const a = (await createAgentSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() })).session;
      const b = (await createAgentSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() })).session;
      try {
        await a.prompt('Remember the secret number 111. Reply OK.');
        await b.prompt('What is the secret number? If you do not know it, reply exactly UNKNOWN.');
        const bAnswer = b.getLastAssistantText() || '';
        assert(a.sessionId !== b.sessionId, 'two sessions shared an id');
        return { aId: a.sessionId, bId: b.sessionId, bAnswer: bAnswer.slice(0, 60), isolated: !/\b111\b/.test(bAnswer) };
      } finally {
        a.dispose();
        b.dispose();
      }
    },
  });

  await point('dispose_idempotent', {
    run: async () => {
      const session = (await createAgentSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() })).session;
      await session.prompt('Say OK.');
      session.dispose();
      let secondDispose = 'ok';
      try { session.dispose(); } catch (e) { secondDispose = 'threw:' + String(e?.message || e); }
      return { secondDispose };
    },
  });

  await point('concurrent_prompts_rejected_or_queued', {
    run: async () => {
      const session = (await createAgentSession({ cwd: WORKDIR, sessionManager: SessionManager.inMemory() })).session;
      try {
        const pending = session.prompt('Write a paragraph about mountains.');
        await new Promise((r) => setTimeout(r, 400));
        let outcome;
        try {
          await session.prompt('Say SECOND.');
          outcome = 'accepted';
        } catch (e) {
          outcome = 'rejected:' + String(e?.message || e);
        }
        await pending;
        return { outcome, note: 'SDK docs: a second prompt while streaming must choose steer or followUp' };
      } finally {
        session.dispose();
      }
    },
  });
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  const groups = [
    ['basic', runBasic],
    ['session', runSession],
    ['streaming', runStreaming],
    ['tools', runTools],
    ['model', runModelConfig],
    ['resources', runResources],
    ['robustness', runRobustness],
  ];

  for (const [name, fn] of groups) {
    try {
      await fn();
    } catch (err) {
      // A group-level throw must not lose the points that already reported.
      log(`[harness] group ${name} threw: ${err?.message || err}`);
      emit({
        id: `__group_failure__:${name}`,
        group: name,
        ok: false,
        skipped: false,
        ms: 0,
        required: false,
        detail: { error: String(err?.message || err) },
      });
    }
  }

  const failed = results.filter((r) => !r.skipped && !r.ok).map((r) => r.id);
  const skipped = results.filter((r) => r.skipped).map((r) => r.id);
  const requiredFailed = results.filter((r) => r.required && !r.skipped && !r.ok).map((r) => r.id);
  const passed = results.filter((r) => !r.skipped && r.ok).length;

  emit({
    id: '__summary__',
    ok: requiredFailed.length === 0,
    counts: {
      total: results.length,
      passed,
      failed: failed.length,
      skipped: skipped.length,
    },
    failed_ids: failed.sort(),
    skipped_ids: skipped.sort(),
    required_failed_ids: requiredFailed.sort(),
    sdk_async_crashes: sdkCrashFindings,
  });

  // Best-effort scratch cleanup; never fail the run over it.
  try { fs.rmSync(SCRATCH, { recursive: true, force: true }); } catch { /* ignore */ }

  process.exit(requiredFailed.length === 0 ? 0 : 1);
}

main().catch((err) => {
  emit({ id: '__fatal__', ok: false, detail: { error: String(err?.stack || err) } });
  process.exit(1);
});
