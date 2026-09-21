import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],

  // shadcn components are copied into this repository and import each other
  // through @/, so the alias is part of the component contract rather than a
  // convenience. tsconfig.app.json has to agree with it.
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },

  // The Go server embeds this directory, so the build lands where
  // internal/web can find it rather than inside web/.
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },

  server: {
    // The logo lives in assets/ at the repository root, outside this
    // package. The build resolves it either way; the dev server refuses to
    // serve anything above its root unless told.
    fs: { allow: ['..'] },

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
