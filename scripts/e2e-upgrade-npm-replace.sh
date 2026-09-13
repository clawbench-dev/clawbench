#!/usr/bin/env bash
# e2e-upgrade-npm-replace.sh — End-to-end test for the "npm replaced the
# package while the service was running" upgrade failure.
#
# Reproduces the real incident faithfully:
#   1. The service runs from a binary inside an npm-style package layout.
#   2. A package manager replaces that package: the old package directory is
#      renamed (npm "retire") and then deleted, while the service keeps running.
#      The service's os.Executable() now points at a deleted inode.
#   3. An upgrade is requested through the HTTP API (what the UI calls).
#
# Asserts the fix: the upgrade succeeds (no "Failed to backup binary" ENOENT),
# because the path recorded at startup still resolves.
#
# Isolation: runs on its own port and data dir under a network namespace with
# only loopback, so it can never touch the live service on :20000 and never
# reaches a real npm registry (a local mock registry is authoritative).
#
# Usage: ./scripts/e2e-upgrade-npm-replace.sh
# Exit:  0 = pass, 1 = fail

set -euo pipefail

# Network isolation: the service queries a registry on startup and during an
# upgrade. Without isolation the real npmjs/npmmirror would answer first and
# the mock registry (which serves the version this test upgrades to) would
# never be consulted. Re-exec inside a network namespace with only loopback,
# so the mock registry is authoritative and no real network is touched.
if [ "${E2E_IN_NETNS:-}" != "1" ]; then
  if command -v unshare >/dev/null 2>&1 && unshare -rn true 2>/dev/null; then
    echo "=== re-executing inside an isolated network namespace ==="
    exec env E2E_IN_NETNS=1 unshare -rn "$0" "$@"
  fi
  echo "ERROR: network isolation unavailable (need unshare)." >&2
  echo "       Refusing to run against a real registry." >&2
  exit 1
fi

# A fresh network namespace starts with loopback DOWN; the mock registry and
# the service both need it.
ip link set lo up 2>/dev/null || true

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

PORT="${E2E_UPGRADE_PORT:-20987}"
REG_PORT="${E2E_REGISTRY_PORT:-20988}"
ROOT="${E2E_UPGRADE_DIR:-/tmp/cb-e2e-upgrade}"
OLD_VER="v0.1.0"
NEW_VER="v0.2.0"

PASS=0
FAIL=0
check() { # check <desc> <condition-result>
  if [ "$2" = "0" ]; then
    echo "  PASS: $1"; PASS=$((PASS+1))
  else
    echo "  FAIL: $1"; FAIL=$((FAIL+1))
  fi
}

cleanup() {
  local rc=$?
  [ -n "${SVC_PID:-}" ] && kill -TERM "$SVC_PID" 2>/dev/null || true
  [ -n "${REG_PID:-}" ] && kill -TERM "$REG_PID" 2>/dev/null || true
  sleep 1
  [ -n "${SVC_PID:-}" ] && kill -KILL "$SVC_PID" 2>/dev/null || true
  [ -n "${REG_PID:-}" ] && kill -KILL "$REG_PID" 2>/dev/null || true
  return $rc
}
trap cleanup EXIT

echo "=== E2E: upgrade after npm replaces the running package ==="
echo "port=$PORT registry=$REG_PORT root=$ROOT"
echo

# ---------------------------------------------------------------- setup
rm -rf "$ROOT"
PKG="$ROOT/pkg"
BIN_DIR="$PKG/node_modules/@xulongzhe/clawbench-linux-x64/bin"
DATA="$ROOT/data"
HOME_DIR="$ROOT/home"
mkdir -p "$BIN_DIR" "$DATA" "$HOME_DIR" "$ROOT/registry"

echo "[1/7] building binaries (old=$OLD_VER new=$NEW_VER)"
go build -ldflags "-X clawbench/internal/version.Version=$OLD_VER" -o "$BIN_DIR/clawbench" ./cmd/server
go build -ldflags "-X clawbench/internal/version.Version=$NEW_VER" -o "$ROOT/registry/clawbench-new" ./cmd/server
"$BIN_DIR/clawbench" --version | grep -q "$OLD_VER" && check "old binary reports $OLD_VER" 0 || check "old binary reports $OLD_VER" 1

# ------------------------------------------------------- mock registry
echo "[2/7] starting mock npm registry on :$REG_PORT"
# Build the release tarball (package/bin/clawbench) up front: packing the ~90MB
# binary takes seconds, and the registry must be serving before the service
# queries it.
python3 - "$ROOT/registry" "$NEW_VER" <<'PY'
import base64, gzip, hashlib, io, json, os, sys, tarfile

regdir, newver = sys.argv[1], sys.argv[2]
src = os.path.join(regdir, "clawbench-new")
data = open(src, "rb").read()

buf = io.BytesIO()
with gzip.GzipFile(fileobj=buf, mode="wb") as gz:
    with tarfile.open(fileobj=gz, mode="w") as tar:
        ti = tarfile.TarInfo("package/bin/clawbench")
        ti.size = len(data); ti.mode = 0o755
        tar.addfile(ti, io.BytesIO(data))
tarball = buf.getvalue()
open(os.path.join(regdir, "pkg.tgz"), "wb").write(tarball)
json.dump(
    {"version": newver.lstrip("v"),
     "integrity": "sha512-" + base64.b64encode(hashlib.sha512(tarball).digest()).decode()},
    open(os.path.join(regdir, "meta.json"), "w"),
)
PY

python3 - "$REG_PORT" "$ROOT/registry" <<'PY' &
import http.server, json, os, socketserver, sys

port, regdir = int(sys.argv[1]), sys.argv[2]
pkg = "clawbench-linux-x64"
tarball = open(os.path.join(regdir, "pkg.tgz"), "rb").read()
meta = json.load(open(os.path.join(regdir, "meta.json")))

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path.endswith("/latest"):
            body = json.dumps({
                "version": meta["version"],
                "dist": {"tarball": f"http://127.0.0.1:{port}/{pkg}/-/{pkg}-{meta['version']}.tgz",
                         "integrity": meta["integrity"]},
            }).encode()
            self.send_response(200); self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body))); self.end_headers()
            self.wfile.write(body)
        elif self.path.endswith(".tgz"):
            self.send_response(200); self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Content-Length", str(len(tarball))); self.end_headers()
            self.wfile.write(tarball)
        else:
            self.send_response(404); self.end_headers()

socketserver.TCPServer.allow_reuse_address = True
with socketserver.TCPServer(("127.0.0.1", port), H) as httpd:
    httpd.serve_forever()
PY
REG_PID=$!

# Wait for readiness rather than guessing a sleep. The registry reports the
# version without the "v" prefix, so compare against the stripped form.
NEW_VER_NUM="${NEW_VER#v}"
REG_OK=1
for i in $(seq 1 40); do
  if curl -s --max-time 2 "http://127.0.0.1:$REG_PORT/clawbench-linux-x64/latest" 2>/dev/null | grep -q "\"$NEW_VER_NUM\""; then
    REG_OK=0; break
  fi
  sleep 0.5
done
check "mock registry serves metadata" "$REG_OK"
[ "$REG_OK" -ne 0 ] && { echo "registry failed to start"; exit 1; }

# ------------------------------------------------------------ start svc
echo "[3/7] starting service from npm-style package layout"
# NPM_CONFIG_REGISTRY points the upgrade check at the mock registry. The
# default base is tried first, but it is unreachable inside the network
# namespace, so the mock becomes the effective source.
HOME="$HOME_DIR" NPM_CONFIG_REGISTRY="http://127.0.0.1:$REG_PORT" \
  setsid "$BIN_DIR/clawbench" --port "$PORT" --data-dir "$DATA" \
  > "$ROOT/service.log" 2>&1 < /dev/null &
SVC_PID=$!
for i in $(seq 1 30); do
  ss -ltn 2>/dev/null | grep -q ":$PORT " && break
  sleep 0.5
done
ss -ltn 2>/dev/null | grep -q ":$PORT " && check "service listening on :$PORT" 0 || check "service listening on :$PORT" 1

echo "      service pid=$SVC_PID"
echo "      recorded self-path: $(cat "$DATA/self-path" 2>/dev/null || echo '(missing)')"
[ -f "$DATA/self-path" ] && check "startup recorded self-path" 0 || check "startup recorded self-path" 1

# --------------------------------------------- simulate npm replacement
echo "[4/7] simulating npm replace: retire old package dir, install new, delete retired"
RETIRE="$ROOT/.clawbench-dS2lu9p5"
mv "$PKG" "$RETIRE"                                  # (1) retire (rename)
mkdir -p "$BIN_DIR"                                  # (2) install new
cp "$ROOT/registry/clawbench-new" "$BIN_DIR/clawbench"
rm -rf "$RETIRE"                                     # (3) delete retired dir

EXE_LINK="$(readlink /proc/$SVC_PID/exe 2>/dev/null || echo '?')"
echo "      running exe now: $EXE_LINK"
case "$EXE_LINK" in
  *"(deleted)"*) check "running exe is now a deleted inode (scenario reproduced)" 0 ;;
  *)             check "running exe is now a deleted inode (scenario reproduced)" 1 ;;
esac
[ ! -e "${EXE_LINK% (deleted)}" ] && check "old executable path no longer exists" 0 || check "old executable path no longer exists" 1
kill -0 "$SVC_PID" 2>/dev/null && check "service still alive after replacement" 0 || check "service still alive after replacement" 1

# ------------------------------------------------------- trigger upgrade
echo "[5/7] requesting upgrade via API (what the UI calls)"
curl -s --max-time 10 -X POST "http://127.0.0.1:$PORT/api/upgrade/start" > "$ROOT/start.json" || true
head -c 200 "$ROOT/start.json" 2>/dev/null; echo

# Poll status until terminal. The service may restart mid-poll (that is the
# expected outcome of the short-circuit), so a failed request is not fatal —
# only the final recorded state matters.
STATUS=""
PHASE=""
for i in $(seq 1 60); do
  STATUS="$(curl -s --max-time 5 "http://127.0.0.1:$PORT/api/upgrade/status" 2>/dev/null || true)"
  PHASE="$(printf '%s' "$STATUS" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("phase",""))' 2>/dev/null || echo '')"
  case "$PHASE" in
    completed|restarting|failed) break ;;
  esac
  sleep 1
done
echo "      final status: ${STATUS:-<no response>}"

# ------------------------------------------------------------- assert
echo "[6/7] asserting outcome"
ERR="$(printf '%s' "$STATUS" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("error",""))' 2>/dev/null || echo '')"
CODE="$(printf '%s' "$STATUS" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("error_code",""))' 2>/dev/null || echo '')"

printf '%s' "$STATUS" | grep -q "Failed to backup binary" \
  && check "no 'Failed to backup binary' error" 1 || check "no 'Failed to backup binary' error" 0
[ "$CODE" != "self_path_unresolved" ] \
  && check "no self_path_unresolved error" 0 || check "no self_path_unresolved error" 1
[ "$CODE" != "install_dir_not_writable" ] \
  && check "no install_dir_not_writable error" 0 || check "no install_dir_not_writable error" 1

# Short-circuit: disk was already at the target version, so no download.
if grep -q "disk binary already at target version" "$ROOT/service.log"; then
  check "version short-circuit engaged (no redundant download)" 0
else
  check "version short-circuit engaged (no redundant download)" 1
fi

# --------------------------------------------------------------- restart
echo "[7/7] verifying restart brings the service back"
# The short-circuit calls the restart function; on this unsupervised deploy it
# spawns a sentinel that re-execs the recorded path. Give it time to come back.
sleep 8
NEW_PID="$(ss -ltnp 2>/dev/null | grep ":$PORT " | grep -oP 'pid=\K[0-9]+' | head -1 || true)"
if [ -n "$NEW_PID" ] && [ "$NEW_PID" != "$SVC_PID" ]; then
  check "service restarted (new pid=$NEW_PID)" 0
  NEW_EXE="$(readlink /proc/$NEW_PID/exe 2>/dev/null || echo '?')"
  echo "      new exe: $NEW_EXE"
  case "$NEW_EXE" in
    *"(deleted)"*) check "restarted process has a live executable" 1 ;;
    *)             check "restarted process has a live executable" 0 ;;
  esac
  "$NEW_EXE" --version 2>/dev/null | grep -q "$NEW_VER" \
    && check "restarted process runs $NEW_VER" 0 || check "restarted process runs $NEW_VER" 1
else
  check "service restarted" 1
fi

echo
echo "=== result: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] || { echo "--- service.log tail ---"; tail -30 "$ROOT/service.log"; }
exit $([ "$FAIL" -eq 0 ] && echo 0 || echo 1)
