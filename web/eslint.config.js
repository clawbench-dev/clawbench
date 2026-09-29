import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import vuePlugin from 'eslint-plugin-vue'
import vueParser from 'vue-eslint-parser'
import globals from 'globals'

export default tseslint.config(
    // Global ignores
    {
        ignores: [
            'dist/**',
            'node_modules/**',
            'coverage/**',
            '*.config.ts',
            '*.config.js',
            '**/*.test.ts',
            '**/*.test.tsx',
            '**/appLog.ts',
        ],
    },

    // Base JS recommended rules
    js.configs.recommended,

    // TypeScript recommended rules
    ...tseslint.configs.recommended,

    // Browser globals for all source files
    {
        languageOptions: {
            globals: {
                ...globals.browser,
            },
        },
    },

    // Vue files
    {
        files: ['**/*.vue'],
        plugins: {
            vue: vuePlugin,
        },
        languageOptions: {
            parser: vueParser,
            parserOptions: {
                parser: tseslint.parser,
                ecmaVersion: 'latest',
                sourceType: 'module',
            },
        },
        processor: vuePlugin.processors['.vue'],
        rules: {
            'vue/multi-word-component-names': 'off',
            'vue/no-v-html': 'off',
            // `isWideScreen || ...` in <script setup> is a constant-true expression:
            // a ref object is always truthy and script code does NOT auto-unwrap.
            // That silently collapsed the completion-notification guard to
            // "any event for the current session is suppressed" (see App.vue's
            // isChatPanelVisible and completionNotifyChatPanelGuard.test.ts).
            //
            // Caveat: this rule only recognises refs it can see being declared with
            // ref()/computed() in the same file. A ref destructured from a
            // composable (the App.vue case) carries no type info here, so the rule
            // does NOT catch it — vue-tsc does not either (TS permits truthiness on
            // objects). For those, pass the value explicitly into a typed helper so
            // the mistake becomes a type error.
            'vue/no-ref-as-operand': 'error',
            // typescript-eslint's `eslint-recommended` turns no-undef off on the
            // assumption that tsc will catch it — but only ~half of the .vue
            // files here declare `lang="ts"`, so vue-tsc silently skips the rest
            // (App.vue among them). Without this rule a renamed-but-not-updated
            // reference is a runtime ReferenceError, not a build failure.
            'no-undef': 'error',
        },
    },

    // TypeScript files
    {
        files: ['**/*.ts'],
        languageOptions: {
            parser: tseslint.parser,
            parserOptions: {
                ecmaVersion: 'latest',
                sourceType: 'module',
            },
        },
    },

    // Custom rule overrides
    {
        rules: {
            '@typescript-eslint/no-explicit-any': 'error',
            '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
            'no-console': 'off',
            'no-empty': ['error', { allowEmptyCatch: true }],
        },
    },

    // Ban raw console.* in source code (tests and appLog.ts are exempt via ignores)
    {
        files: ['**/*.{ts,vue}'],
        rules: {
            'no-restricted-syntax': [
                'error',
                {
                    selector: 'CallExpression[callee.object.name="console"]',
                    message: 'Use appLog.d/i/w/e() from @/utils/appLog instead of console.*(). See AGENTS.md.',
                },
            ],
        },
    },
)
