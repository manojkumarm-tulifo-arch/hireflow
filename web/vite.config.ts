import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // Order matters — Vite walks the entries in declaration order and
      // takes the first prefix match. The candidate-bgv API is a sibling
      // service on :8081, so its routes must be listed BEFORE the
      // catch-all /api → :8080 (hireflow) entry.
      '/api/v1/bgv': 'http://localhost:8081',
      '/api': 'http://localhost:8080',
    },
  },
});
