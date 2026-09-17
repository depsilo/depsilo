import path from 'node:path'
import { defineConfig } from 'vitest/config'

// Keep the same `@/` alias the app and the Vite build use, so a unit test can
// import an application component without a relative path reaching into src.
export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    environment: 'node',
    include: ['unit/**/*.test.ts'],
  },
})
