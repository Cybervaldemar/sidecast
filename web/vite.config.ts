import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';
import { writeFileSync } from 'node:fs';
export default defineConfig({
  root: fileURLToPath(new URL('.', import.meta.url)),
  plugins: [
    react(),
    {
      name: 'preserve-go-embed-placeholder',
      closeBundle() {
        writeFileSync(fileURLToPath(new URL('dist/.gitkeep', import.meta.url)), '');
      },
    },
  ],
  build: { outDir: 'dist', emptyOutDir: true },
  server: { host: '127.0.0.1', port: 5173, proxy: { '/api': 'http://127.0.0.1:8080' } },
});
