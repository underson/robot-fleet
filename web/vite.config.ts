import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// In development the API runs separately; proxying /v1 keeps the app on one
// origin, as it is in production behind nginx, so there is no CORS to configure.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: { '/v1': process.env.API_URL ?? 'http://localhost:8080' },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test-setup.ts'],
  },
})
