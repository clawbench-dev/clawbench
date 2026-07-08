# OpenCode server connection error
_Exported on 7/8/2026 at 02:39:59 GMT+8 from Cursor (3.10.17)_

---

**User**

Debug: when using third party app to connect the OpenCode server, it reports: 
AI 后端异常退出: connection: acp: initialize: {"code":-32603,"message":"Internal error","data":{"error":"peer disconnected before response"}}3.4s

---

**Cursor**

Investigating the OpenCode server configuration and ACP initialization path to find why the peer disconnects before a response.


Checking server status and ACP connection handling.


Reproduced: `opencode acp` crashes with `ServeError` before responding to initialize. Upgrading OpenCode and applying fixes.


ACP works when the web server is stopped — checking whether a port conflict with the running server is the cause.


Root cause: `OPENCODE_SERVER_PASSWORD` breaks ACP. Scoping that env var to only the web server process in `start-server.ps1`.


Root cause: `OPENCODE_SERVER_PASSWORD` in the environment breaks ACP initialize. Updating `start-server.ps1` to scope auth env vars to the web server process only.


## Diagnosis

The error `connection: acp: initialize: peer disconnected before response` means the ACP backend (`opencode acp`) **exits before it can reply** to the `initialize` handshake. I reproduced this on your machine.

### Root causes found

**1. `OPENCODE_SERVER_PASSWORD` breaks ACP (primary cause)**

When `OPENCODE_SERVER_PASSWORD` is set in the environment, `opencode acp` crashes with `ServeError` during initialize — exactly matching your error. This is a [known OpenCode bug](https://github.com/anomalyco/opencode/issues/17285).

Your `start-server.ps1` was setting this globally for the whole PowerShell session.

**2. Port conflict with `opencode web` on 4096**

When the web server is already running on port 4096, default `opencode acp` (stdio mode) also fails. Using an explicit port works:

```powershell
opencode acp --port 18100   # ✅ works while web is on 4096
opencode acp                # ❌ ServeError when web is running
```

**3. Remote ACP is not supported**

OpenCode **1.17.15 does not expose ACP over the network**. Hitting `/acp` on the server returns the web UI HTML, not an ACP endpoint. Third-party apps connecting to `https://opencode.telepass.app` must use the **REST/web API with basic auth**, not ACP.

**4. Stale Cursor proxy port (also fixed)**

The live server pointed at port `2444` while the actual proxy was on `11773`. Synced to the current port.

---

## Fixes applied

1. **Updated `start-server.ps1`** — auth env vars are now scoped only to the `opencode web` child process, so they won't leak into ACP clients.
2. **Upgraded OpenCode** `1.17.13` → `1.17.15`
3. **Synced Cursor proxy URL** to the current bootstrap port
4. **Restarted the server** — running healthy on `http://127.0.0.1:4096`

---

## What to configure in the third-party app

| Connection type | Correct setup |
|---|---|
| **Remote server** (iOS, browser, etc.) | URL: `https://opencode.telepass.app`, user: `opencode`, password: from `server-password.txt`. **Do not use ACP.** |
| **Local ACP client** (Zed, JetBrains, etc.) | Command: `opencode`, args: `["acp", "--port", "18100"]`. Ensure `OPENCODE_SERVER_PASSWORD` is **not** set in the app's environment. |

For local ACP, verify with:

```powershell
# Must NOT have OPENCODE_SERVER_PASSWORD set
Remove-Item Env:OPENCODE_SERVER_PASSWORD -ErrorAction SilentlyContinue
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientInfo":{"name":"test","version":"1.0"}}}' | opencode acp --port 18100
```

You should get a JSON response with `"protocolVersion":1`.

---

Which third-party app are you using (e.g. Zed, Cherry Studio, iOS OpenCode app)? That determines whether the fix is switching from ACP to REST, or adjusting the local ACP command/args.

---

**User**

<system_notification>
The following task has finished. If you were already aware, ignore this notification and do not restate prior responses.

<task>
kind: shell
status: success
task_id: 142121
title: Test ACP from project dir with --pure and no server password
tool_call_id: tool_7654ccc4-967a-413e-8385-c7c4fc02c7f
output_path: C:\Users\admin\.cursor\projects\c-Users-admin-config-opencode\terminals\142121.txt
</task>
</system_notification>
<user_query>Briefly inform the user about the task result and perform any follow-up actions (if needed). If there's no follow-ups needed, don't explicitly say that.</user_query>

---

**Cursor**

The background ACP test confirmed the failure: with `--pure` and no server password, `opencode acp` still exited with `ServeError` before returning an initialize response — matching the third-party app's `peer disconnected before response` error.

Follow-up from that test is already done:

- **`start-server.ps1` updated** so `OPENCODE_SERVER_PASSWORD` is only passed to the web server process (that env var breaks ACP)
- **OpenCode upgraded** to 1.17.15 and the server restarted
- **Cursor proxy port synced** (was pointing at a dead port)

For the third-party app: if it's connecting **remotely** to `https://opencode.telepass.app`, use basic auth — not ACP. If it's a **local ACP client** on this machine, configure it as `opencode acp --port 18100` and make sure `OPENCODE_SERVER_PASSWORD` is not in the app's environment.

