import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'

function envPort(value: string | undefined): number {
  const port = Number(value ?? 5173)
  return Number.isInteger(port) && port > 0 && port <= 65535 ? port : 5173
}

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    host: process.env.HOST ?? '127.0.0.1',
    port: envPort(process.env.PORT),
    strictPort: true,
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY_TARGET ?? 'http://127.0.0.1:8080',
        // Preserve the authority checked by the management HTTP boundary.
        changeOrigin: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            { name: 'react-runtime', test: /node_modules\/(?:react|react-dom|scheduler)\// },
            { name: 'api-validation', test: /node_modules\/zod\/|api\/generated\/zod\.gen/ },
          ],
        },
      },
    },
  },
})
