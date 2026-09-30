#!/usr/bin/env python3
"""Compatibility shim: run an old chromedriver under a modern Appium.

Problem
-------
The Android 9 (API 28) system image ships WebView/Chrome 69. The only
chromedriver that can automate Chrome 69 is chromedriver 2.42, whose `GET
/status` reply is `{"build":{"version":"alpha"},"os":{...}}` — it predates the
`ready` field. Appium 3.x's `appium-chromedriver` (commands/process.js,
`waitForOnline`) hard-requires `status.ready` and aborts with:

    The response to the /status API is not valid: {...}

The rest of the W3C wire protocol is compatible, so this shim starts the real
chromedriver on an internal port and proxies to it, injecting `"ready": true`
into the `/status` body. Appium is pointed at this script via
`appium:chromedriverExecutable`; Appium spawns it with `--port=<n>` exactly as
it would the real binary.

Usage (spawned by Appium, not by hand):
    chromedriver-shim.py --port=9515 [other chromedriver args...]

Set CHROMEDRIVER_REAL to the real binary path. The default prefers
chromedriver 2.44 (`chromedriver-244`) because it is the newest driver that
still supports Chrome 69 AND speaks W3C sessions; Appium 3.x always requests a
W3C session, so 2.42/2.43 (JSONWP-only) fail with "Missing or invalid
capabilities".
"""
import http.client
import json
import os
import socket
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

_DEFAULT_REAL = "/home/androidusr/chromedriver/chromedriver-244"
if not os.path.exists(_DEFAULT_REAL):
    _DEFAULT_REAL = "/home/androidusr/chromedriver/chromedriver"
REAL = os.environ.get("CHROMEDRIVER_REAL", _DEFAULT_REAL)

# chromedriver 2.44 replies with build.version "alpha" (no number), so Appium
# cannot detect the protocol and defaults to W3C. Appium's session builder then
# sends `{"capabilities":{"alwaysMatch":{"goog:chromeOptions":...}}}`, which
# 2.44 rejects ("Missing or invalid capabilities") because its W3C support is
# partial: it only understands JSONWP `desiredCapabilities`.
#
# appium-chromedriver's `syncProtocol` (commands/session.js) switches to
# JSONWP whenever `semver.coerce(build.version).major < 75`. Reporting a
# synthetic-but-plausible version here makes Appium negotiate JSONWP, which
# 2.44 accepts, while the actual Chrome automation stays on the real 2.44
# binary. The version is reported only in /status; it is never used to pick
# behaviour downstream.
STATUS_VERSION = os.environ.get("CHROMEDRIVER_STATUS_VERSION", "2.44")

def parse_port(argv):
    for i, a in enumerate(argv):
        if a.startswith("--port="):
            return int(a.split("=", 1)[1]), i
        if a == "--port":
            return int(argv[i + 1]), i
    return None, None


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def main():
    listen_port, port_idx = parse_port(sys.argv[1:])
    if listen_port is None:
        print("[shim] FATAL: no --port argument", file=sys.stderr)
        return 2

    internal_port = free_port()
    # Forward every arg except --port, then append our own internal port.
    args = [a for i, a in enumerate(sys.argv[1:]) if i != port_idx]
    if port_idx is not None and sys.argv[port_idx + 1] == str(listen_port) and sys.argv[port_idx] == "--port":
        # `--port N` form: drop the value too.
        args = [a for i, a in enumerate(sys.argv[1:]) if i not in (port_idx, port_idx + 1)]
    proc = subprocess.Popen([REAL, *args, f"--port={internal_port}"])
    print(f"[shim] real chromedriver pid={proc.pid} on 127.0.0.1:{internal_port}, "
          f"serving on :{listen_port}", flush=True)

    # Wait for the real driver to accept connections.
    for _ in range(100):
        try:
            with socket.create_connection(("127.0.0.1", internal_port), timeout=1):
                break
        except OSError:
            time.sleep(0.1)

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *a):  # keep Appium's logs clean
            pass

        def _proxy(self):
            length = int(self.headers.get("Content-Length") or 0)
            body = self.rfile.read(length) if length else None
            conn = http.client.HTTPConnection("127.0.0.1", internal_port, timeout=300)
            headers = {k: v for k, v in self.headers.items()
                       if k.lower() not in ("host", "connection")}
            try:
                conn.request(self.command, self.path, body=body, headers=headers)
                resp = conn.getresponse()
                data = resp.read()
            finally:
                conn.close()

            # Rewrite the /status payload so Appium 3.x accepts the old driver:
            #   * chromedriver 2.42/2.44 speak JSONWP, so the payload is wrapped
            #     in `{"status":0,"value":{...}}`; Appium unwraps `value` before
            #     checking `.ready`, so the flag must go INSIDE `value`.
            #   * report a synthetic build.version so `syncProtocol` picks JSONWP
            #     (see STATUS_VERSION above).
            if self.path.split("?")[0].rstrip("/") in ("/status", "") and resp.status == 200:
                try:
                    payload = json.loads(data)
                    if isinstance(payload, dict):
                        target = payload.get("value") if isinstance(payload.get("value"), dict) else payload
                        target.setdefault("ready", True)
                        target.setdefault("message", "shim: ready")
                        build = target.get("build")
                        if not isinstance(build, dict):
                            build = {}
                            target["build"] = build
                        build["version"] = STATUS_VERSION
                        data = json.dumps(payload).encode()
                except (ValueError, TypeError):
                    pass

            self.send_response(resp.status)
            for k, v in resp.getheaders():
                if k.lower() in ("transfer-encoding", "content-length", "connection"):
                    continue
                self.send_header(k, v)
            self.send_header("Content-Length", str(len(data)))
            self.send_header("Connection", "keep-alive")
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(data)

        do_GET = do_POST = do_DELETE = do_PUT = _proxy

    server = ThreadingHTTPServer(("127.0.0.1", listen_port), Handler)
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()
    try:
        proc.wait()
    except KeyboardInterrupt:
        pass
    finally:
        proc.terminate()
        server.shutdown()
    return proc.returncode


if __name__ == "__main__":
    sys.exit(main() or 0)
