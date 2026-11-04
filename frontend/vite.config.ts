import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

// In dev the SPA talks to the Go API through this proxy, so the browser only
// ever sees one origin (no CORS in day-to-day work).
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '..', '')
  const apiTarget = env.VITE_API_TARGET || `http://localhost:${env.PORT || '8080'}`

  return {
    plugins: [react()],
    server: {
      port: Number(env.WEB_DEV_PORT) || 5173,
      proxy: {
        '/api': { target: apiTarget, changeOrigin: true },
        '/auth': { target: apiTarget, changeOrigin: true },
        '/healthz': { target: apiTarget, changeOrigin: true },
      },
    },
  }
})
