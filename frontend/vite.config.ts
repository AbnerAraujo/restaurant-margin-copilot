import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'
import { VitePWA } from 'vite-plugin-pwa'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      // autoUpdate, not prompt: a stale cached bundle silently answering
      // margin questions with old logic is a worse failure than a
      // mid-session reload. The service worker only ever precaches the
      // build's own hashed JS/CSS/icon assets (Workbox's default
      // generateSW behavior) — it never touches /api/*, so every
      // reconciliation figure and chat answer always comes from a live
      // network request, never a cached response.
      registerType: 'autoUpdate',
      includeAssets: ['favicon.svg', 'icons.svg'],
      manifest: {
        name: 'My Business Steward',
        short_name: 'Steward',
        description:
          'Daily margin reconciliation and Q&A copilot for an independent restaurant.',
        start_url: '/',
        display: 'standalone',
        background_color: '#ffffff',
        theme_color: '#0e6e52',
        icons: [
          {
            src: '/pwa-icons/icon-192.png',
            sizes: '192x192',
            type: 'image/png',
          },
          {
            src: '/pwa-icons/icon-512.png',
            sizes: '512x512',
            type: 'image/png',
          },
          {
            src: '/pwa-icons/icon-512-maskable.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'maskable',
          },
        ],
      },
      workbox: {
        // The SPA fallback must never intercept an API call — only a
        // client-side ROUTE (e.g. a hard refresh on /close) with no
        // matching precached file falls back to index.html.
        navigateFallbackDenylist: [/^\/api\//],
      },
    }),
  ],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
  },
})
