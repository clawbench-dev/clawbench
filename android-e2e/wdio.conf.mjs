/**
 * WebdriverIO + Appium config for the ClawBench Android E2E harness.
 *
 * Runs from the `runner` compose service, which shares the compose network with
 * the `emulator` service — so Appium is reached by service name
 * (`http://emulator:4723`) and no host-published port is involved.
 *
 * The APK is baked into the emulator image at E2E_APK_PATH, because Appium
 * resolves `appium:app` on the filesystem of the machine it runs on (the
 * emulator container), not the runner's.
 */
const APK_PATH = process.env.E2E_APK_PATH || '/apk/clawbench-android-debug.apk';
const APP_PACKAGE = process.env.E2E_APP_PACKAGE || 'com.clawbench.app.debug';
const APP_ACTIVITY = process.env.E2E_APP_ACTIVITY || 'com.clawbench.app.MainActivity';
const APPIUM_HOST = process.env.E2E_APPIUM_HOST || 'emulator';
const APPIUM_PORT = Number(process.env.E2E_APPIUM_PORT || 4723);
const ARTIFACTS_DIR = process.env.E2E_ARTIFACTS_DIR || '/e2e/artifacts';

/**
 * Which specs to run, selected by run.sh via E2E_TIER.
 *
 * `all` is the union, ordered Tier 1 first: the login smoke proves the
 * shell/login path every Tier 2 spec depends on, so a Tier 2 failure with a
 * green Tier 1 is unambiguously a tunnel problem.
 */
const TIER = process.env.E2E_TIER || '1';
const TIER2_SPECS = [
  './tests/tunnel.server.mjs',
  './tests/tunnel.forward.mjs',
  './tests/tunnel.reverse.mjs',
];
const SPECS_BY_TIER = {
  '1': ['./tests/smoke.login.mjs'],
  '2': TIER2_SPECS,
  all: ['./tests/smoke.login.mjs', ...TIER2_SPECS],
};
const SPECS = SPECS_BY_TIER[TIER];
if (!SPECS) {
  throw new Error(`unknown E2E_TIER=${TIER} (expected 1, 2 or all)`);
}

// The API-28 WebView is Chrome 69; chromedriver 2.44 is the newest driver that
// supports it. Appium 3.x is W3C-only while 2.44 is JSONWP-first, so we point it
// at a shim that reconciles the two (see scripts/chromedriver-shim.py).
const CHROMEDRIVER_SHIM =
  process.env.E2E_CHROMEDRIVER || '/home/androidusr/chromedriver/chromedriver-shim.py';

// Hard cap for each artifact capture in `afterTest`. When a test fails because the
// renderer is wedged, the very commands we reach for to diagnose it (screenshot,
// page source) hang on that same wedged socket. Left uncapped this cost ~6 minutes
// per failure *on top of* the mocha timeout. A few seconds is ample for a healthy
// renderer, and aborting cleanly keeps a red run diagnosable and prompt.
const ARTIFACT_TIMEOUT_MS = Number(process.env.E2E_ARTIFACT_TIMEOUT_MS || 8000);

/** Reject if `promise` has not settled within `ms`; the underlying command is
 *  abandoned (its eventual rejection is swallowed) rather than awaited. */
function withTimeout(promise, ms, label) {
  promise.catch(() => {}); // never let the abandoned promise become unhandled
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      timer = setTimeout(
        () => reject(new Error(`${label} exceeded ${ms}ms`)),
        ms,
      );
      // Do not keep the event loop alive just for this timer.
      if (typeof timer.unref === 'function') timer.unref();
    }),
  ]).finally(() => clearTimeout(timer));
}

export const config = {
  runner: 'local',
  hostname: APPIUM_HOST,
  port: APPIUM_PORT,
  path: '/',
  specs: SPECS,
  // NOTE: this top-level `maxInstances` is NOT what limits concurrency when
  // `capabilities` is an array — WebdriverIO reads the per-capability value, and
  // with the top-level one alone it happily started one worker PER SPEC FILE.
  // Measured: 3 spec files -> 3 workers ("Execution of 3 workers"), all driving
  // the same emulator, each with `fullReset: true`. Every worker's reinstall
  // wiped the others' app data, so a worker's login cookie vanished underneath
  // it and its tunnel streams 401'd (`has_cookie=false` server-side). The
  // capability below carries the real limit; this one is kept for clarity.
  maxInstances: 1,
  capabilities: [{
    platformName: 'Android',
    'appium:automationName': 'UiAutomator2',
    'appium:deviceName': 'emulator-5554',
    'appium:app': APK_PATH,
    // THE effective concurrency limit. There is exactly one device, so only one
    // session may exist at a time; anything higher makes concurrent workers
    // fight over it (see the note above).
    maxInstances: 1,
    // State isolation: the app persists the server URL in SharedPreferences, so a
    // second run would skip the login page entirely. fullReset uninstalls and
    // reinstalls between runs, guaranteeing first-launch state.
    'appium:fullReset': true,
    'appium:noReset': false,
    'appium:chromedriverExecutable': CHROMEDRIVER_SHIM,
    // WebView context switching needs headroom: chromedriver attaches over the
    // devtools socket, which is slow on this emulator.
    'appium:newCommandTimeout': 600,
    'appium:uiautomator2ServerLaunchTimeout': 180000,
    'appium:adbExecTimeout': 180000,
    // Tier 2 drives `mobile: shell` to run device-side `toybox nc`, which is how
    // it proves real bytes traverse the tunnel from inside the emulator. That
    // command is gated by the Appium SERVER's --allow-insecure flag, not by a
    // capability: Dockerfile.emulator sets
    //   APPIUM_ADDITIONAL_ARGS="--allow-insecure *:adb_shell"
    // (the `*:` destination prefix is required). No capability is set here on
    // purpose — a bare `adb_shell` capability would be a misleading no-op.
  }],
  logLevel: process.env.E2E_LOG_LEVEL || 'info',
  bail: 0,
  waitforTimeout: 30000,
  connectionRetryTimeout: 240000,
  connectionRetryCount: 3,
  framework: 'mocha',
  reporters: ['spec'],
  mochaOpts: {
    ui: 'bdd',
    // Bounds how long a single test may hang. The longest legitimate in-test wait
    // budget is the connect test's own deadlines (60s health + 60s login + 90s
    // home marker = 210s), so this stays above that to let the test's specific
    // failure message fire first, while capping a wedged-renderer hang well below
    // mocha's 300s default. Combined with the artifact cap below, a failure now
    // returns in ~4 min worst case instead of ~13.
    timeout: 240000,
  },

  /** Screenshot + page source on failure, so a red run is diagnosable.
   *
   *  Every capture is hard-capped: a failure caused by a wedged renderer wedges
   *  these commands too, and an uncapped screenshot was observed to hang ~6
   *  minutes *after* the test had already timed out. Each artifact is attempted
   *  independently so one hang does not skip the others. */
  afterTest: async function (test, _context, { error, passed }) {
    if (passed) return;
    const fs = await import('node:fs/promises');
    await fs.mkdir(ARTIFACTS_DIR, { recursive: true });
    const safe = test.title.replace(/[^a-z0-9]+/gi, '-').toLowerCase();
    const cap = (p, label) => withTimeout(p, ARTIFACT_TIMEOUT_MS, label);

    try {
      await cap(
        browser.saveScreenshot(`${ARTIFACTS_DIR}/FAIL-${safe}.png`),
        'screenshot',
      );
    } catch (e) {
      console.error(`[artifacts] screenshot failed: ${e.message}`);
    }
    try {
      const src = await cap(browser.getPageSource(), 'page source');
      await fs.writeFile(`${ARTIFACTS_DIR}/FAIL-${safe}.xml`, src);
    } catch (e) {
      console.error(`[artifacts] page source failed: ${e.message}`);
    }
    try {
      const ctxs = await cap(browser.getContexts(), 'contexts');
      await fs.writeFile(`${ARTIFACTS_DIR}/FAIL-${safe}.contexts.json`,
        JSON.stringify(ctxs, null, 2));
    } catch (e) {
      console.error(`[artifacts] contexts failed: ${e.message}`);
    }
    if (error) console.error(`[artifacts] failure: ${error.message}`);
  },
};
