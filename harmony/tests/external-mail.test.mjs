import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import { externalAppUrl } from '../entry/src/main/ets/models/NativeActions.ts';
import { NativeFailure, nativeMessage } from '../entry/src/main/ets/models/NativeMessages.ts';

const source = readFileSync(new URL('../entry/src/main/ets/pages/Index.ets', import.meta.url), 'utf8');
const start = source.indexOf('  private async openExternal(');
const end = source.indexOf('  private async showSelectionMenu(', start);
assert.ok(start >= 0 && end > start);
const code = stripTypeScriptTypes('class Host {\n' + source.slice(start, end) + '\n}', { mode: 'strip' }) + '\nHost;';

function host({ failLink = false, failAbility = false } = {}) {
  const Host = runInNewContext(code, { externalAppUrl, NativeFailure });
  const instance = new Host();
  const calls = [];
  instance.store = { language: 'en' };
  instance.context = () => ({
    async openLink(uri) {
      calls.push({ api: 'openLink', uri });
      if (failLink) throw new Error('no app link');
    },
    async startAbility(want) {
      calls.push({ api: 'startAbility', ...want });
      if (failAbility) throw new Error('no ability');
    }
  });
  return { instance, calls };
}

test('mail opens a compose intent with the exact encoded draft and no web fallback', async () => {
  const h = host();
  const uri = 'mailto:?subject=' + encodeURIComponent('分享会议') + '&body=' + encodeURIComponent('https://example.com/share/x\nnotes');
  await h.instance.openExternal(uri);
  assert.deepEqual(h.calls, [{ api: 'startAbility', action: 'ohos.want.action.sendToData', uri }]);
});

test('an unavailable mail application returns a localized actionable failure without opening a browser', async () => {
  const h = host({ failAbility: true });
  await assert.rejects(h.instance.openExternal('mailto:?subject=share&body=link'), error => {
    assert.equal(error.code, 'mail_app_unavailable');
    assert.equal(error.message, nativeMessage('mail_app_unavailable', 'en'));
    return true;
  });
  assert.equal(h.calls.length, 1);
});

test('HTTP links retain openLink and viewData fallback routing', async () => {
  for (const failLink of [false, true]) {
    const h = host({ failLink });
    await h.instance.openExternal('https://EXAMPLE.COM/page');
    assert.equal(h.calls[0].api, 'openLink');
    assert.equal(h.calls[0].uri, 'https://example.com/page');
    assert.equal(h.calls.length, failLink ? 2 : 1);
    if (failLink) assert.equal(h.calls[1].action, 'ohos.want.action.viewData');
  }
});

test('invalid external URLs never invoke the operating system', async () => {
  const h = host();
  for (const uri of ['file:///private', 'intent://mail', 'mailto:?body=%00']) {
    await assert.rejects(h.instance.openExternal(uri));
  }
  assert.deepEqual(h.calls, []);
});
