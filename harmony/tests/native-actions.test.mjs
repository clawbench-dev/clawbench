import { test } from 'node:test';
import assert from 'node:assert/strict';
import { externalHttpUrl, externalAppUrl, portBrowserUrl, selectionText, redactLog } from '../entry/src/main/ets/models/NativeActions.ts';

test('external and port browsers validate schemes, authority and port independently', () => {
  assert.equal(externalHttpUrl('HTTPS://EXAMPLE.COM:443/path?q=ok#x'), 'https://example.com/path?q=ok#x');
  assert.equal(portBrowserUrl(8080, 'http', '', '/hello?q=1'), 'http://127.0.0.1:8080/hello?q=1');
  for (const value of ['javascript:alert(1)', 'file:///private', 'https://user:password@example.com/',
    'https://example.com\\@evil.example', 'https://example.com/\n', 'intent://example']) {
    assert.throws(() => externalHttpUrl(value));
  }
  for (const args of [[0, 'http', '', '/'], [65536, 'https', '', '/'], [80, 'file', '', '/'],
    [8080, 'http', 'user:password@evil.example', '/'], [8080, 'http', '', '//evil.example/']]) {
    assert.throws(() => portBrowserUrl(...args));
  }
});

test('system mail accepts the share center blank recipient, Unicode and encoded multiline body', () => {
  const url = 'mailto:?subject=' + encodeURIComponent('会议 & 文件 #1') +
    '&body=' + encodeURIComponent('第一行\nhttps://example.com/share?a=1&b=2#page');
  assert.equal(externalAppUrl(url), url);
  assert.equal(externalAppUrl('MAILTO:one@example.com,two@example.com?cc=copy%40example.com'),
    'mailto:one@example.com,two@example.com?cc=copy%40example.com');
  assert.equal(externalAppUrl('https://EXAMPLE.COM/'), 'https://example.com/');
  assert.throws(() => externalHttpUrl(url), 'port-browser validation stays HTTP-only');
});

test('system mail rejects other schemes, malformed encoding, authority and injected headers', () => {
  for (const url of ['javascript:alert(1)', 'file:///private', 'intent://mail',
    'mailto://example.com', 'mailto:%2F%2Fexample.com', 'mailto:bad%00@example.com',
    'mailto:?subject=%', 'mailto:?subject=%0D%0ABcc%3Asecret%40example.com',
    'mailto:?body=%00', 'mailto:?attachment=file%3A%2F%2Fprivate',
    'mailto:?subject=a&subject=b', 'mailto:?body=raw space', 'mailto:?body=a#fragment',
    'mailto:' + 'a'.repeat(8192)]) {
    assert.throws(() => externalAppUrl(url), url);
  }
});

test('selection is bounded without altering non-ASCII text', () => {
  assert.equal(selectionText('\u4e2d\u6587\nline'), '\u4e2d\u6587\nline');
  assert.throws(() => selectionText(''));
  assert.throws(() => selectionText('x'.repeat(65537)));
});

test('diagnostic logs redact credentials and URLs before truncating and flattening', () => {
  const log = redactLog('GET https://example.com/?token=hidden\nCookie: session=secret\n' +
    'password="two words" api_key=xyz token: abc\nfixture-pass\u0000done', ['fixture-pass']);
  for (const privatePart of ['example.com', 'hidden', 'session=secret', 'two words', 'xyz', 'abc', 'fixture-pass']) {
    assert.equal(log.includes(privatePart), false, privatePart);
  }
  assert.doesNotMatch(log, /[\n\r\u0000]/);
  assert.equal(redactLog('x'.repeat(30000)).length, 1024);
  assert.equal(redactLog('connected status=200'), 'connected status=200');
});
