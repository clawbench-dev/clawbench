/**
 * Client/server compatibility number for the desktop shell.
 *
 * This is a MIRROR of `internal/version/client_protocol.txt`, which is the one
 * source of truth: the Go server embeds that file and reports it from
 * `/api/desktop/latest`, and the Android build bakes it into
 * `BuildConfig.CLIENT_PROTOCOL`. `protocol.test.ts` fails if this constant and
 * the file disagree, so the three cannot drift.
 *
 * The desktop gate compares this number with the server's instead of comparing
 * version strings. That is what lets a release which did not change the
 * shell<->server contract keep shipping the previous desktop package: the
 * versions differ, the protocol does not, and nobody is asked to re-download
 * ~150MB for an identical build.
 *
 * Bump it ONLY for a breaking change to the contract a shell depends on (the
 * native bridge surface, the startup/upgrade endpoints, the login handshake).
 * The web UI and WebSocket event schema are NOT part of it — they are served by
 * the server and are therefore always current.
 */
export const CLIENT_PROTOCOL = 1
