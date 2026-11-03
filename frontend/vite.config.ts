import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// In dev the SPA talks to the Go API through this proxy, so the browser only
// ever sees one origin (no CORS in day-to-day work).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
      '/healthz': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
