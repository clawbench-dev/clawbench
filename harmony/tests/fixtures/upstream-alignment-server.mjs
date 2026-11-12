import http from 'node:http';
import { readFileSync, existsSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';

// Local emulator acceptance only. No production accounts or remote writes.
// Requires Node >= 22.13: /guard.js is produced by stripping the TypeScript
// types off useSingleTab.ts with the (still experimental) node:module API.
const port = Number(process.env.CLAWBENCH_FIXTURE_PORT || 21713);
// Reference checkout first, then this repo's own copy. Resolved from the
// fixture's location, so it works from any working directory.
const guardCandidates = [
  new URL('../../../clawbench-source code/web/src/composables/useSingleTab.ts', import.meta.url),
  new URL('../../clawbench-source code/web/src/composables/useSingleTab.ts', import.meta.url),
  new URL('../../../web/src/composables/useSingleTab.ts', import.meta.url),
];
const guardPath = guardCandidates.find(existsSync);
if (!guardPath) console.warn('useSingleTab.ts not found — /guard.js will 404');
const events = [];
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aV1sAAAAASUVORK5CYII=', 'base64');
const page = `<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1">
<style>body{background:#17191d;color:#eee;font:16px sans-serif;padding:20px}button,input{display:block;margin:18px 0;padding:16px;max-width:100%}h1{font-size:22px}pre{font-size:12px;max-height:220px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere}</style>
<h1>HarmonyOS 0.2.12 acceptance</h1><pre id="status">Connecting</pre>
<button id="download">Download acceptance-image.png</button>
<button id="reload">Reload native workbench</button>
<button id="mail">Check invalid mail feedback</button>
<label>Offline draw.io archive<input id="archive" type="file" accept=".war,.zip"></label>
<script type="module">
import { createSingleTabGuard } from '/guard.js';
const guard = createSingleTabGuard();
const status=document.querySelector('#status');
function record(event){status.textContent+= '\\n'+JSON.stringify(event);fetch('/events',{method:'POST',body:JSON.stringify(event)})}
const acquired=await guard.acquire();
addEventListener('pagehide',()=>guard.dispose(),{once:true});
status.textContent='Single-tab ownership: '+acquired;
record({type:'ready',acquired,version:await window.ClawBenchHarmony?.getAppVersion(),host:window.ClawBenchHarmony?.getHostPlatform()});
addEventListener('clawbench-download-progress',e=>record({type:'progress',...e.detail}));
addEventListener('clawbench-native-error',e=>record({type:'native-error',...e.detail}));
document.querySelector('#download').onclick=()=>window.ClawBenchHarmony.downloadFileWithProgress('acceptance.png','acceptance-image.png',712);
document.querySelector('#reload').onclick=()=>window.ClawBenchHarmony.reloadApp();
document.querySelector('#mail').onclick=async()=>{try{await window.ClawBenchHarmony.openExternalUrl('mailto:?subject=%')}catch(e){record({type:'mail-rejected',message:e.message})}};
document.querySelector('#archive').onchange=e=>record({type:'archive',count:e.target.files.length});
</script>`;
const server=http.createServer(async(req,res)=>{
 const url=new URL(req.url,'http://localhost');
 if(url.pathname==='/events'){
  if(req.method==='POST'){let b='';for await(const c of req)b+=c;events.push(JSON.parse(b));res.end('ok');return}
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify(events));return;
 }
 if(url.pathname==='/api/health'){res.setHeader('Content-Type','application/json');res.end(JSON.stringify({app:'clawbench',version:'v0.139.0-fixture',protocol:1}));return}
 if(url.pathname==='/login'){res.setHeader('Content-Type','application/json');res.setHeader('Set-Cookie','clawbench_session=acceptance; Path=/; HttpOnly');res.end('{"ok":true}');return}
 if(url.pathname==='/api/config'){res.setHeader('Content-Type','application/json');res.end('{"push_mode":"none"}');return}
 if(url.pathname==='/api/ai/events/pending'){res.setHeader('Content-Type','application/json');res.end('{"events":[]}');return}
 if(url.pathname==='/api/fs/raw/acceptance.png'){res.setHeader('Content-Type','image/png');res.setHeader('Content-Length',png.length);res.end(png);return}
 if(url.pathname==='/guard.js'&&guardPath){res.setHeader('Content-Type','text/javascript');res.end(stripTypeScriptTypes(readFileSync(guardPath,'utf8')));return}
 if(url.pathname==='/'){res.setHeader('Content-Type','text/html;charset=utf-8');res.end(page);return}
 res.writeHead(404);res.end();
});
server.listen(port,'127.0.0.1',()=>console.log(`HarmonyOS acceptance fixture ready on http://127.0.0.1:${port}`));
