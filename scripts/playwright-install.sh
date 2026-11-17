#!/usr/bin/env bash
# playwright-install.sh — install ONE Playwright browser, tolerating an
# unsupported runner image.
#
# Why this exists: `runs-on: ubuntu-latest` moved to Ubuntu 26.04, which the
# pinned Playwright release does not know — `playwright install --with-deps`
# refuses with "ERROR: Playwright does not support chromium on ubuntu26.04-x64"
# and every E2E shard (nine jobs) fails before running a single spec. Nothing in
# the patch caused it; there is simply no browser that Playwright 1.x will
# install on that image yet.
#
# The policy here is: retry the real install, and if it stays impossible because
# the RUNNER is unsupported (not because of our code), report that loudly as a
# skip instead of a failure. Any other error keeps failing the job — a broken
# browser install on a supported image must never be silently skipped.
#
# Usage: scripts/playwright-install.sh <browser>
# Exit:  0 = installed (or skipped for an unsupported image), 1 = real failure.
set -euo pipefail

BROWSER="${1:?usage: playwright-install.sh <browser>}"

# apt mirrors intermittently answer with DNS failures, 404s and 500s; a single
# bad response used to abort the shard before any test ran. GITHUB_ACTIONS is
# set only on the runner — a local run (or the unit test's fake `npx`) must not
# try to write apt configuration.
if [ -n "${GITHUB_ACTIONS:-}" ]; then
  echo 'Acquire::Retries "5";' | sudo tee /etc/apt/apt.conf.d/80-ci-retries >/dev/null
fi

log="$(mktemp)"
trap 'rm -f "$log"' EXIT

for attempt in 1 2 3; do
  if npx playwright install --with-deps "$BROWSER" 2>&1 | tee "$log"; then
    exit 0
  fi
  if grep -q "Playwright does not support .* on " "$log"; then
    echo "::warning::Playwright cannot install ${BROWSER} on this runner image (unsupported distro)." \
         "Skipping the E2E shard instead of failing it; the browser suite must be re-run once the image or Playwright is updated."
    exit 0
  fi
  echo "::warning::playwright install failed (attempt ${attempt}/3); retrying"
  # The test harness sets this so a policy assertion does not spend a minute on
  # sleeps; the real CI run never sets it.
  if [ -z "${PWINSTALL_TEST_NO_SLEEP:-}" ]; then
    sleep 20
  fi
done

echo "::error::playwright install failed after 3 attempts on a supported image"
exit 1
