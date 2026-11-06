import http from 'node:http';
import { WebSocketServer } from 'ws';

// Manual native-device fixture. The page deliberately never calls the Harmony bridge.
// Forward device port 20127 to this host, then use notification-fixture-only to log in.
const port = Number(process.env.CLAWBENCH_FIXTURE_PORT || 20127);
const instance = Date.now();
let sequence = 0;
let mode = 'native';
let configReads = 0;
let connections = 0;
const pending = [];
const pendingReads = [];
const nextId = () => `evt_${instance}_${++sequence}`;
const page = `<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Notification fixture</title></head><body><h1>Notification fixture</h1>
<p>Legacy frontend fixture. No native bridge calls.</p></body></html>`;

async function body(request) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > 16384) throw new Error('Fixture request too large');
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString() || '{}');
}

function json(response, value, status = 200) {
  response.writeHead(status, { 'content-type': 'application/json' });
  response.end(JSON.stringify(value));
}

const server = http.createServer(async (request, response) => {
  const url = new URL(request.url, `http://127.0.0.1:${port}`);
  try {
    if (url.pathname === '/api/health') {
      return json(response, { app: 'clawbench', protocol: 1, version: 'notification-fixture' });
    }
    if (url.pathname === '/login' && request.method === 'POST') {
      if ((await body(request)).password !== 'notification-fixture-only') {
        return json(response, { error: 'incorrect password' }, 401);
      }
      response.setHeader('Set-Cookie', `cb${port}_clawbench_session=fixture; Path=/; HttpOnly; SameSite=Lax`);
      return json(response, { ok: true });
    }
    if (url.pathname === '/_test/status') {
      return json(response, { mode, configReads, connections, clients: sockets.clients.size,
        pending: pending.length, pendingReads });
    }
    if (url.pathname === '/_test/mode' && request.method === 'POST') {
      mode = (await body(request)).mode === 'disabled' ? 'disabled' : 'native';
      return json(response, { mode });
    }
    if (url.pathname === '/_test/disconnect' && request.method === 'POST') {
      for (const client of sockets.clients) client.close();
      return json(response, { ok: true });
    }
    if (url.pathname === '/_test/expire' && request.method === 'POST') {
      pending.length = 0;
      return json(response, { ok: true });
    }
    if (url.pathname === '/_test/event' && request.method === 'POST') {
      const data = await body(request);
      const event = {
        type: 'event', id: nextId(), event: 'session_update',
        data: {
          status: data.status || 'completed', session_id: data.session || 'fixture-session',
          session_title: data.title || 'Notification fixture',
          response_preview_plain: data.text || 'Fixture reply delivered',
          message_id: Number(data.messageId || sequence), project_path: '/fixture'
        }
      };
      const delivery = data.delivery || 'both';
      if (delivery !== 'live') {
        pending.push({ event_id: event.id, payload: JSON.stringify(event),
          suppress_notification: data.suppressed === true });
      }
      let liveId = '';
      if (delivery !== 'pending') {
        // Real completion writes A to pending, then broadcasts a separate WSOnly B.
        liveId = nextId();
        for (const client of sockets.clients) client.send(JSON.stringify({ ...event, id: liveId }));
      }
      return json(response, { pendingId: delivery === 'live' ? '' : event.id, liveId,
        clients: sockets.clients.size });
    }
    if (url.pathname.startsWith('/api/')) {
      if (!(request.headers.cookie || '').includes(`cb${port}_clawbench_session=fixture`)) {
        return json(response, { error: 'authentication required' }, 401);
      }
      if (url.pathname === '/api/config') {
        configReads++;
        return json(response, { push_mode: mode });
      }
      if (url.pathname === '/api/ai/events/pending') {
        const after = url.searchParams.get('after') || '';
        pendingReads.push(after);
        if (pendingReads.length > 100) pendingReads.shift();
        const index = pending.findIndex(event => event.event_id === after);
        // The upstream endpoint returns [] for both expired and caught-up cursors.
        return json(response, { events: after ? (index < 0 ? [] : pending.slice(index + 1)) : pending });
      }
      return json(response, {});
    }
    response.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
    response.end(page);
  } catch (error) {
    json(response, { error: error.message }, 400);
  }
});

const sockets = new WebSocketServer({ server, path: '/api/ai/events/ws' });
sockets.on('connection', (client, request) => {
  if (!(request.headers.cookie || '').includes(`cb${port}_clawbench_session=fixture`)) {
    client.close(1008, 'authentication required');
    return;
  }
  connections++;
  client.send('{"type":"connected"}');
});
server.listen(port, '127.0.0.1', () => console.log(`Notification fixture listening on 127.0.0.1:${port}`));
