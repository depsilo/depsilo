import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      'react-refresh/only-export-components': ['error', {
        allowConstantExport: true,
        extraHOCs: ['lazyRoute'],
      }],
    },
  },
  {
    // The route factory intentionally keeps its private fallback components
    // beside the exported HOC so loading and failure policy stay local.
    files: ['src/routing/lazyRoute.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
  {
    // `src/components/ui` holds shadcn-generated component source. Upstream
    // co-locates a component with its `cva` variant factory (and other
    // shadcn components import those factories), so the refresh rule cannot
    // hold here. Everything else in the repo still enforces it, and
    // `src/components/app` owns all Depsilo-specific composition.
    files: ['src/components/ui/**/*.{ts,tsx}'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
])
