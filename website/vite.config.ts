import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import { watch } from 'node:fs'
import { syncBrand } from './scripts/sync-brand.mjs'

const appIcon = fileURLToPath(new URL('../internal/brand/assets/readyrig-app-icon.png', import.meta.url))

export default defineConfig({
  plugins: [
    react(),
    {
      name: 'readyrig-brand-sync',
      configureServer(server) {
        // The icon task shares this repository. Refresh the site when its final
        // source artwork changes, without asking it to edit website files.
        let updating = false
        let queued = false
        const update = async () => {
          if (updating) { queued = true; return }
          updating = true
          try {
            if (await syncBrand()) server.ws.send({ type: 'full-reload' })
          } catch (error) {
            server.config.logger.warn(`Brand sync: ${String(error)}`)
          } finally {
            updating = false
            if (queued) { queued = false; void update() }
          }
        }
        try {
          const watcher = watch(appIcon, () => { void update() })
          server.httpServer?.once('close', () => watcher.close())
        } catch {
          server.config.logger.info('Using bundled website artwork.')
        }
      },
    },
  ],
  server: { port: 5173, strictPort: true },
  preview: { port: 4173, strictPort: true },
})
