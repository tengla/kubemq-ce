import { copyFileSync, mkdirSync, rmSync } from 'node:fs'
import { resolve } from 'node:path'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The Go server embeds ../assets and serves it as static files. The new UI
// owns / and /dashboard; the legacy Nuxt pages (/queues, /pubsub, /cqrs) keep
// living next to it in assets/ until they are rebuilt here.
const assetsDir = resolve(import.meta.dirname, '../assets')
const legacyServer = 'http://localhost:8080'

function legacyLayout() {
  return {
    name: 'legacy-layout',
    apply: 'build',
    buildStart() {
      rmSync(resolve(assetsDir, 'app'), { recursive: true, force: true })
    },
    closeBundle() {
      mkdirSync(resolve(assetsDir, 'dashboard'), { recursive: true })
      copyFileSync(resolve(assetsDir, 'index.html'), resolve(assetsDir, 'dashboard/index.html'))
    },
  }
}

export default defineConfig({
  plugins: [vue(), legacyLayout()],
  build: {
    outDir: assetsDir,
    emptyOutDir: false,
    assetsDir: 'app',
  },
  server: {
    proxy: {
      '/api': legacyServer,
      '/queues': legacyServer,
      '/pubsub': legacyServer,
      '/cqrs': legacyServer,
      '/_nuxt': legacyServer,
    },
  },
})
