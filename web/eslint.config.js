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
