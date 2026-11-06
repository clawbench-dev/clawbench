import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import net from 'node:net';

const port = Number(process.argv[2]);
const halfClose = process.argv.includes('--half-close');
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error('Usage: node verify-tunnel.mjs <loopback-port> [--half-close]');
}
const result = { port, checks: [], halfClose, sha256: true, passed: false };

async function exchange(size, slow = false) {
  const payload = crypto.randomBytes(size);
  const chunks = [];
  let received = 0;
  let finished = false;
  const socket = net.createConnection({ host: '127.0.0.1', port, allowHalfOpen: true });
  await new Promise((resolve, reject) => {
    const finish = () => { finished = true; socket.destroy(); resolve(); };
    socket.setTimeout(20000, () => socket.destroy(new Error('Tunnel exchange timed out')));
    socket.on('error', reject);
    socket.on('connect', () => halfClose ? socket.end(payload) : socket.write(payload));
    socket.on('data', bytes => {
      chunks.push(bytes);
      received += bytes.length;
      if (!halfClose && received >= payload.length) {
        finish();
      } else if (slow) {
        socket.pause();
        setTimeout(() => socket.resume(), 10);
      }
    });
    socket.on('end', finish);
    socket.on('close', () => {
      if (!finished) reject(new Error(`Tunnel closed after ${received} bytes`));
    });
  });
  const response = Buffer.concat(chunks);
  assert.equal(response.length, payload.length + (halfClose ? 5 : 0));
  if (halfClose) assert.equal(response.subarray(-5).toString(), '|eof|');
  const body = halfClose ? response.subarray(0, -5) : response;
  assert.equal(
    crypto.createHash('sha256').update(body).digest('hex'),
    crypto.createHash('sha256').update(payload).digest('hex'),
  );
  result.checks.push({ size, slow, passed: true });
}

try {
  await exchange(127);
  await exchange(2 * 1024 * 1024, true);
  await Promise.all([halfClose ? 0 : 17, 1, 65535, 262145].map(size => exchange(size)));
  result.passed = true;
} catch (error) {
  result.error = String(error);
  process.exitCode = 1;
}
console.log(JSON.stringify(result));
