import path from "path"
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from "@tailwindcss/vite"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:23333',
      '/pypi': 'http://localhost:23333',
      '/apt': 'http://localhost:23333',
      '/health': 'http://localhost:23333',
    }
  }
})
