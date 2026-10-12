import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  build: {
    emptyOutDir: true,
    outDir: '../internal/control/webui/dist',
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
})
