import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],

  // The Go server embeds this directory, so the build lands where
  // internal/web can find it rather than inside web/.
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },

  server: {
    // `npm run dev` proxies the API to a locally running control plane, so
    // the dashboard can be developed without rebuilding the Go binary.
    proxy: {
      '/api': {
        target: process.env.RUNNERLY_SERVER_URL ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})
