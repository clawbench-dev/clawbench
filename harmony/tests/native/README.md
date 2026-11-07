# Native Tunnel Regression

These checks exercise an installed HarmonyOS application, the production
native tunnel library, and the upstream HTTP/2 handlers. They are separate
from the dependency-free policy tests. The fixture binds only loopback
addresses and uses fixed, public test credentials.

## Core Regression Without A Device

On Linux, install CMake 3.22 or later and Clang, then run:

```sh
export CC=clang
export CXX=clang++
cmake -S harmony/tests/native -B build/native-tunnel-tests \
  -DTUNNEL_BUILD_DEPENDENCIES=ON -DCMAKE_BUILD_TYPE=Debug
cmake --build build/native-tunnel-tests --parallel 2
ctest --test-dir build/native-tunnel-tests --output-on-failure
```

This builds the same dependency versions and checksums as the app through
`entry/src/main/cpp/ThirdParty.cmake`; it does not substitute a different
system Mbed TLS major version. Dependency downloads require network access
during configuration and building.

The test includes the production tunnel core and runs real nghttp2 client
and server sessions over local socket pairs. It covers queued-reset
object lifetime, reset cleanup under backpressure, claim retention at
local and global capacity limits, bounded-queue overflow, and libssh2
EAGAIN buffer and close retries. It needs no HarmonyOS SDK, application
credentials, TCP listeners, or external service at runtime. These core
checks complement the installed-app checks below.

## Start The Fixture

From a full upstream ClawBench checkout containing its root `go.mod`:

```sh
go run ./harmony/tests/native/upstream-fixture/main.go
```

The fixture is explicitly excluded from ordinary package discovery. Its
source is still checked with
`go vet ./harmony/tests/native/upstream-fixture/main.go`.

The fixture exposes HTTP/1.1 plus h2c on `127.0.0.1:20121`, SSH on port
`20122`, and a TCP echo server on port `20124`. Its HTTP/2 routes call the
real `internal/handler` implementation. The SSH fixture uses Go's SSH
library and only permits the loopback echo and HTTP targets.

For a HarmonyOS emulator reachable by HDC, replace `DEVICE` with its target
identifier and create these mappings:

```sh
hdc -t DEVICE rport tcp:20121 tcp:20121
hdc -t DEVICE rport tcp:20122 tcp:20122
hdc -t DEVICE rport tcp:17583 tcp:20124
hdc -t DEVICE fport tcp:17582 tcp:17582
```

Add `http://127.0.0.1:20121` in the app and use the fixture password
`fixture-tunnel-pass`. Choose **H2**, then **Connect**, **Forward**, and
**Reverse** in the fixture page. Forward maps app port `17582` to the host
echo server; reverse maps host port `17682` to the device-side echo
forwarding port. Check the actual assigned ports in **Status** if a port
was already occupied.

## Run The Checks

```sh
node harmony/tests/native/verify-tunnel.mjs 17582
node harmony/tests/native/verify-tunnel.mjs 17682
node harmony/tests/native/stress-tunnel.mjs 17582
node harmony/tests/native/stress-tunnel.mjs 17682
```

Each command emits one JSON result and exits nonzero on failure. The byte
test covers a 2 MiB transfer with a slow reader, parallel connections,
small payloads, and SHA-256 equality. The stress test aborts 40 writes and
then checks 55 successful round trips. Its final sequential burst exceeds
the 32-stream limit while the upstream server retains closed visitors
during its drain grace. A client must queue incoming claims until a slot
becomes available, rather than discard the claims and leave sockets idle.

Repeat all four commands after selecting **SSH** and **Connect**. Verify
the displayed SHA-256 host-key fingerprint against the fixture's local
`/api/ssh/info` response before trusting it. Restarting the fixture
generates a new host key and lets the changed-key rejection path be
checked without changing any production server.

**Browser forward** and **Open port** exercise the ordinary system-browser
bridge. The browser should show `Tunnel browser proof` while the app is
in the background. `/results` records UI actions and the browser request;
it does not expose a command-execution endpoint.

## Half-Close Boundary

The fixture echo server appends `|eof|` after reading TCP EOF. To verify
the endpoint directly:

```sh
node harmony/tests/native/verify-tunnel.mjs 20124 --half-close
```

Only use `--half-close` for a tunnel acceptance test when every intervening
forwarder preserves TCP half-close. HDC port forwarding did not preserve
FIN in the tested emulator setup: the direct endpoint passed while a
baseline path through HDC, without the native tunnel, failed. Passing
the default byte and stress tests must therefore not be reported as a
native-device half-close acceptance result.

## Cleanup

Choose **Forget all** in the app, stop the fixture, and remove only the
test mappings created above:

```sh
hdc -t DEVICE fport rm tcp:17582 tcp:17582
hdc -t DEVICE fport rm tcp:17583 tcp:20124
hdc -t DEVICE fport rm tcp:20122 tcp:20122
hdc -t DEVICE fport rm tcp:20121 tcp:20121
```

These checks use h2c and an emulator. They do not certify TLS trust on a
physical device, OS background survival, or screen capture.

## Notification Delivery With A Legacy Frontend

From the repository root, run `npm ci`, then start this isolated fixture:

```sh
node harmony/tests/native/notification-fixture.mjs
hdc -t DEVICE rport tcp:20126 tcp:20126
```

Use a separate test installation and add `http://127.0.0.1:20126` with the
public fixture password `fixture-notification-pass`. Allow system
notifications and connect. The fixture page does not call any native bridge,
so it reproduces a server frontend without HarmonyOS support.

Press Home, then inspect `http://127.0.0.1:20126/fixture/status` on the host.
The corrected native client reads the configuration after login and on the
background transition, then opens a WebSocket (`clients: 1`). Emit both kinds:

```sh
curl "http://127.0.0.1:20126/fixture/emit?kind=reply"
curl "http://127.0.0.1:20126/fixture/emit?kind=approval"
```

Verify both system notifications in the notification drawer. The 0.2.1
baseline instead has zero config reads and zero clients with this page,
even after system authorization.

Switch the fixture to `disabled` through
`/fixture/mode?value=disabled`, resume the app, and press Home again.
The client must reread the mode and leave no native WebSocket connected.
Switch back through `/fixture/mode?value=native`, repeat the foreground and
background transition, and verify delivery resumes. These routes modify only
the isolated fixture, never the shared test server.

Stop the fixture with Ctrl+C, uninstall only the temporary test bundle, and
remove the mapping with `hdc -t DEVICE fport rm tcp:20126 tcp:20126`.
This demonstrates delivery while the process runs; it does not certify
physical-device background survival or vendor push.
