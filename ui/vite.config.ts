import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  base: './',
  build: {
    outDir: '../internal/webui/dist/app',
    emptyOutDir: true,
  },
  server: {
    // In sviluppo l'API la fornisce il backend Go su :8080.
    proxy: {
      '/api': 'http://localhost:8080',
      '/icons': 'http://localhost:8080',
    },
  },
})
