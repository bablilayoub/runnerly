import path from "path"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  server: {
    // The docs pages are the repository's own docs/*.md, read at build time
    // rather than copied. One copy, so the site cannot drift from what ships
    // with the source.
    fs: { allow: [path.resolve(__dirname, "..")] },
  },
})
