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
        // Preserve the trusted LAN-facing authority. Setup completion derives
        // its management URL from this validated Host rather than the proxy target.
        changeOrigin: false,
      },
    },
  },
  build: {
    outDir: 'dist',
  },
})
