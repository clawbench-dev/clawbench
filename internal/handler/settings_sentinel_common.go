package handler

// sentinelWaitSeconds is how long the restart sentinel gives the outgoing
// process to finish shutting down before forcing it down.
//
// Both halves of this number matter:
//
//   - It must be long enough that a normal drain never reaches it. The budget
//     is the whole shutdown, not just the terminals: stream finalize (5s) +
//     HTTP drain (10s) + ACP exit grace (3s) + second finalize (3s) + the
//     terminal close (~4s, concurrent since 2026-11-11) ≈ 25s worst case. An
//     observed restart drained in 8s.
//   - It must be finite, and expiring must end in SIGKILL. Waiting without a
//     bound looks harmless (the target is already shutting down), but a drain
//     that never finishes — a session close stuck on a socket write, a wedged
//     finalize — would leave the sentinel waiting for good, and the service
//     down with nobody left to start it. That is the 2026-11-11 outcome with a
//     different trigger.
//
// Killing is safe because shutdown was requested BEFORE the sentinel was
// launched (makeRestartFunc), so the target is never a healthy server.
//
// Same value as build.sh's STOP_GRACE_SECONDS, so "how long we give a
// shutdown" has one answer across both restart paths. It lives outside the
// per-OS files because the two sentinels must not drift apart.
const sentinelWaitSeconds = 30
