import assert from 'node:assert/strict';
import net from 'node:net';

const port = Number(process.argv[2]);
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error('Usage: node stress-tunnel.mjs <loopback-port>');
}
const result = { port, abortedWrites: 0, roundtripsAfterClose: 0, passed: false };

function abortedClient() {
  return new Promise(resolve => {
    const socket = net.connect(port, '127.0.0.1', () => {
      socket.write(Buffer.alloc(512 * 1024, 0xa5));
      setTimeout(() => socket.destroy(), 20);
    });
    socket.on('error', () => {});
    socket.on('close', resolve);
    socket.setTimeout(2000, () => socket.destroy());
  });
}

function roundtrip(label) {
  return new Promise((resolve, reject) => {
    const expected = Buffer.from(`native-close-stress-${Math.random()}`);
    const socket = net.connect(port, '127.0.0.1', () => socket.write(expected));
    const chunks = [];
    let received = 0;
    let finished = false;
    socket.on('data', bytes => {
      chunks.push(bytes);
      received += bytes.length;
      if (received < expected.length) return;
      try {
        assert.deepEqual(Buffer.concat(chunks), expected);
        finished = true;
        socket.destroy();
        resolve();
      } catch (error) {
        socket.destroy();
        reject(error);
      }
    });
    socket.on('error', reject);
    socket.on('close', () => {
      if (!finished) reject(new Error(`${label}: closed after ${received}/${expected.length} bytes`));
    });
    socket.setTimeout(10000, () =>
      socket.destroy(new Error(`${label}: recovery timeout after ${received}/${expected.length} bytes`)));
  });
}

try {
  for (let round = 0; round < 5; round++) {
    await Promise.all(Array.from({ length: 8 }, abortedClient));
    result.abortedWrites += 8;
    await new Promise(resolve => setTimeout(resolve, 100));
    await roundtrip(`aborted-write round ${round + 1}`);
    result.roundtripsAfterClose++;
  }
  // Exceed the 32 active-stream limit while the server drains closed visitors.
  for (let index = 0; index < 50; index++) {
    await roundtrip(`sequential round ${index + 1}`);
    result.roundtripsAfterClose++;
  }
  result.passed = true;
} catch (error) {
  result.error = String(error);
  process.exitCode = 1;
}
console.log(JSON.stringify(result));
