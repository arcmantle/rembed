import { defineConfig } from 'vite';

export default defineConfig({
  base: './',
  build: {
    outDir: '../webdist',
    emptyOutDir: true,
    assetsDir: 'assets',
	 chunkSizeWarningLimit: 999999,
    rollupOptions: {
      output: {
        inlineDynamicImports: true,
        entryFileNames: 'assets/app.js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name].[ext]'
      }
    }
  }
});
