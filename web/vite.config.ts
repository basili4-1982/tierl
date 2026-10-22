/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  build: {
    // Go embeds this directory (internal/web/static, go:embed all:dist).
    outDir: '../internal/web/static/dist',
    emptyOutDir: true,
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/api/schema.d.ts', 'src/main.tsx', 'src/test/**', 'src/**/*.test.*'],
      thresholds: { lines: 80, statements: 80, functions: 80, branches: 80 },
    },
  },
});
