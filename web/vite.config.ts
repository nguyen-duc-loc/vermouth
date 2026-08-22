import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    // The browser talks to the gateway through this proxy in development, so
    // the app and the API share an origin. That keeps CORS out of the gateway
    // for a development only reason, and it matches feature 5, where the built
    // static files sit behind the same edge as the gateway.
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
      '/health': { target: 'http://localhost:8080', changeOrigin: true },
      '/ready': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
