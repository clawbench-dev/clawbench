import http from 'node:http';
import { WebSocketServer } from 'ws';

const port = Number(process.argv[2] || 20126);
const cookie = 'clawbench_session=notification-fixture';
let mode = 'native';
let configReads = 0;
let nextEvent = 0;
let connections = 0;
const events = [];
const page = `<!doctype html><html><head>
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Notification delivery fixture</title></head>
<body style="font:18px sans-serif;background:white;color:black;padding:24px">
<h1>Notification delivery fixture</h1>
<p>Legacy frontend</p>
<p id="result">Ready</p>
</body></html>`;

const server = http.createServer(async (request, response) => {
  const url = new URL(request.url, `http://127.0.0.1:${port}`);
  const json = value => {
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify(value));
  };
  if (url.pathname === '/api/health') {
    return json({ app: 'clawbench', protocol: 1, version: 'notification-fixture' });
  }
  if (url.pathname === '/login' && request.method === 'POST') {
    let body = '';
    for await (const chunk of request) body += chunk;
    if (JSON.parse(body).password !== 'fixture-notification-pass') {
      response.writeHead(401);
      return json({ error: 'Invalid fixture password' });
    }
    response.setHeader('Set-Cookie', cookie + '; Path=/; HttpOnly');
    return json({ ok: true });
  }
  if (url.pathname.startsWith('/api/') && !request.headers.cookie?.includes(cookie)) {
    response.writeHead(401);
    return json({ error: 'Fixture authentication required' });
  }
  if (url.pathname === '/api/config') {
    configReads++;
    return json({ push_mode: mode });
  }
  if (url.pathname === '/api/ai/events/pending') {
    return json({ events: [] });
  }
  if (url.pathname === '/fixture/status') {
    return json({ mode, configReads, connections, clients: sockets.clients.size, events });
  }
  if (url.pathname === '/fixture/mode') {
    const value = url.searchParams.get('value');
    if (!['native', 'disabled', 'dingtalk', 'feishu', 'wechat'].includes(value)) {
      response.writeHead(400);
      return json({ error: 'Invalid fixture mode' });
    }
    mode = value;
    return json({ mode });
  }
  if (url.pathname === '/fixture/emit') {
    const approval = url.searchParams.get('kind') === 'approval';
    const event = {
      type: 'event', event: 'session_update', id: `evt_901_${++nextEvent}`,
      data: {
        status: approval ? 'permission_pending' : 'completed',
        session_id: approval ? 'fixture-approval' : 'fixture-reply',
        session_title: 'Notification fixture', project_path: '/fixture',
        response_preview_plain: 'Background reply delivered',
        tool_name: 'Confirm notification fixture'
      }
    };
    events.push({ id: event.id, status: event.data.status, receivers: sockets.clients.size });
    for (const client of sockets.clients) client.send(JSON.stringify(event));
    return json({ receivers: sockets.clients.size, event: event.id });
  }
  response.setHeader('Content-Type', 'text/html; charset=utf-8');
  response.end(page);
});

const sockets = new WebSocketServer({ server, path: '/api/ai/events/ws' });
sockets.on('connection', (client, request) => {
  if (!request.headers.cookie?.includes(cookie)) {
    client.close(1008);
    return;
  }
  connections++;
  client.send(JSON.stringify({ type: 'connected' }));
});
server.listen(port, '127.0.0.1', () => console.log(`Notification fixture listening on ${port}`));

function shutdown() {
  for (const client of sockets.clients) client.terminate();
  sockets.close();
  server.close();
}
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
