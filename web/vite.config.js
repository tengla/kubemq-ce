import { resolve } from 'node:path'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The Go server embeds ../assets and serves it, falling back to index.html
// for client-side routes (services/api spaHandler). The build owns the whole
// directory, so it is emptied on every build.
const assetsDir = resolve(import.meta.dirname, '../assets')
// `npm run dev` proxies API calls to a running broker; BROKER_URL overrides it.
const broker = process.env.BROKER_URL ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: assetsDir,
    emptyOutDir: true,
    assetsDir: 'app',
  },
  server: {
    proxy: {
      '/api': { target: broker, ws: true },
      '/metrics': broker,
    },
  },
})
