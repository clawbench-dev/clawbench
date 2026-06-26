#!/usr/bin/env node
// Install git hooks for local development.
// Run automatically via `npm run prepare` (triggered by `npm install`).

import { writeFileSync, chmodSync, existsSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';

const hooksDir = join(process.cwd(), '.git', 'hooks');

if (!existsSync(hooksDir)) {
    console.log('Not a git repo — skipping hook installation');
    process.exit(0);
}

const preCommitPath = join(hooksDir, 'pre-commit');
const preCommitScript = `#!/bin/sh
# Pre-commit hook: run ESLint on staged frontend files
# Installed by: npm run prepare (scripts/install-hooks.mjs)

STAGED=$(git diff --cached --name-only --diff-filter=ACM 'web/src/**/*.vue' 'web/src/**/*.ts' 2>/dev/null)

if [ -z "$STAGED" ]; then
    exit 0
fi

# Strip the "web/" prefix since eslint runs from inside web/
STAGED_REL=$(echo "$STAGED" | sed 's|^web/||')

echo "Running ESLint on staged files..."
cd web && npx eslint $STAGED_REL 2>&1

if [ $? -ne 0 ]; then
    echo ""
    echo "ESLint found errors. Fix them before committing."
    echo "   Run: npm run lint:fix"
    exit 1
fi

exit 0
`;

writeFileSync(preCommitPath, preCommitScript);
chmodSync(preCommitPath, 0o755);
console.log('Installed pre-commit hook: ESLint check on staged files');
