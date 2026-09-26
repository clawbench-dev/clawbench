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

// The API-28 WebView is Chrome 69; chromedriver 2.44 is the newest driver that
// supports it. Appium 3.x is W3C-only while 2.44 is JSONWP-first, so we point it
// at a shim that reconciles the two (see scripts/chromedriver-shim.py).
const CHROMEDRIVER_SHIM =
  process.env.E2E_CHROMEDRIVER || '/home/androidusr/chromedriver/chromedriver-shim.py';

export const config = {
  runner: 'local',
  hostname: APPIUM_HOST,
  port: APPIUM_PORT,
  path: '/',
  specs: ['./tests/**/*.mjs'],
  maxInstances: 1,
  capabilities: [{
    platformName: 'Android',
    'appium:automationName': 'UiAutomator2',
    'appium:deviceName': 'emulator-5554',
    'appium:app': APK_PATH,
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
    timeout: 300000,
  },

  /** Screenshot + page source on failure, so a red run is diagnosable. */
  afterTest: async function (test, _context, { error, passed }) {
    if (passed) return;
    const fs = await import('node:fs/promises');
    await fs.mkdir(ARTIFACTS_DIR, { recursive: true });
    const safe = test.title.replace(/[^a-z0-9]+/gi, '-').toLowerCase();
    try {
      await browser.saveScreenshot(`${ARTIFACTS_DIR}/FAIL-${safe}.png`);
    } catch (e) {
      console.error(`[artifacts] screenshot failed: ${e.message}`);
    }
    try {
      const src = await browser.getPageSource();
      await fs.writeFile(`${ARTIFACTS_DIR}/FAIL-${safe}.xml`, src);
    } catch (e) {
      console.error(`[artifacts] page source failed: ${e.message}`);
    }
    try {
      const ctxs = await browser.getContexts();
      await fs.writeFile(`${ARTIFACTS_DIR}/FAIL-${safe}.contexts.json`,
        JSON.stringify(ctxs, null, 2));
    } catch (e) {
      console.error(`[artifacts] contexts failed: ${e.message}`);
    }
    if (error) console.error(`[artifacts] failure: ${error.message}`);
  },
};
