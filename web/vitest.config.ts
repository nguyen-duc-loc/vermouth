import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// biome-ignore lint/style/noDefaultExport: Vitest requires the config as a default export.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    clearMocks: true,
    restoreMocks: true,
  },
})
