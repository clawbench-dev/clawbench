#!/usr/bin/env node
// Install git hooks for local development.
// Run automatically via `npm run prepare` (triggered by `npm install`).

import { writeFileSync, chmodSync, existsSync } from 'node:fs';
import { join } from 'node:path';

const hooksDir = join(process.cwd(), '.git', 'hooks');

if (!existsSync(hooksDir)) {
    console.log('Not a git repo — skipping hook installation');
    process.exit(0);
}

// ── pre-commit ────────────────────────────────────────────────────────────
// Lints only the staged frontend files, so a commit stays fast.
//
// Two pathspec notes, both verified against git rather than assumed:
//   * `web/**/*.css` (not `web/css/**/*.css`) is what matches the plain
//     stylesheets. A `web/css/**/*.css` pathspec matches nothing at all, so a
//     CSS-only commit would silently skip linting.
//   * `.vue` files are listed because their `<style>` blocks are linted by
//     stylelint; ESLint covers their script/template half.
const preCommitPath = join(hooksDir, 'pre-commit');
const preCommitScript = `#!/bin/sh
# Pre-commit hook: lint staged frontend files (ESLint + stylelint)
# Installed by: npm run prepare (scripts/install-hooks.mjs)

# .vue/.ts -> ESLint; .vue/.css -> stylelint (the <style> block / stylesheet)
STAGED_JS=$(git diff --cached --name-only --diff-filter=ACM 'web/src/**/*.vue' 'web/src/**/*.ts' 2>/dev/null)
STAGED_CSS=$(git diff --cached --name-only --diff-filter=ACM 'web/src/**/*.vue' 'web/**/*.css' 2>/dev/null)

if [ -z "$STAGED_JS" ] && [ -z "$STAGED_CSS" ]; then
    exit 0
fi

# Strip the "web/" prefix since the tools run from inside web/
STATUS=0

if [ -n "$STAGED_JS" ]; then
    STAGED_JS_REL=$(echo "$STAGED_JS" | sed 's|^web/||')
    echo "Running ESLint on staged files..."
    (cd web && npx eslint $STAGED_JS_REL) || STATUS=1
fi

if [ -n "$STAGED_CSS" ]; then
    STAGED_CSS_REL=$(echo "$STAGED_CSS" | sed 's|^web/||')
    echo "Running stylelint on staged files..."
    (cd web && npx stylelint $STAGED_CSS_REL) || STATUS=1
fi

if [ $STATUS -ne 0 ]; then
    echo ""
    echo "Lint found errors. Fix them before committing."
    echo "   Run: npm run lint:fix && npm run lint:css:fix"
    exit 1
fi

exit 0
`;

writeFileSync(preCommitPath, preCommitScript);
chmodSync(preCommitPath, 0o755);
console.log('Installed pre-commit hook: ESLint + stylelint on staged files');
