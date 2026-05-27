import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import IconsResolver from 'unplugin-icons/resolver'
import vue from '@vitejs/plugin-vue'
import viteCompression from 'vite-plugin-compression'

export default defineConfig({
  server: {
    port: 5175,
    host: "0.0.0.0",
    proxy: {
      '/api/': {
        target: 'http://localhost:9081',
        secure: false,
        changeOrigin: true,
        timeout: 300000,
      },
      '/sysapi/': {
        target: 'http://localhost:8081',
        secure: false,
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/sysapi/, '/nway-system')
      }
    }
  },
  optimizeDeps: {
    include: ['vditor', 'mermaid'],
  },
  build: {
    commonjsOptions: {
      transformMixedEsModules: true,
    },
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/vue') || id.includes('node_modules/pinia') || id.includes('node_modules/vue-router')) {
            return 'vue-core';
          }
          if (id.includes('node_modules/element-plus')) {
            return 'element-plus';
          }
          if (id.includes('node_modules/mermaid')) {
            return 'mermaid';
          }
          if (id.includes('node_modules/markdown-it') || id.includes('node_modules/markdown-it-texmath')) {
            return 'markdown-it';
          }
          if (id.includes('node_modules/katex')) {
            return 'katex';
          }
          if (id.includes('node_modules/axios')) {
            return 'axios';
          }
          if (id.includes('node_modules/codemirror') || id.includes('node_modules/@codemirror')) {
            return 'codemirror';
          }
          if (id.includes('node_modules/xlsx')) {
            return 'excel';
          }
          if (id.includes('node_modules/highlight.js')) {
            return 'highlight';
          }
          if (id.includes('node_modules/@antv/x6') || id.includes('node_modules/@antv/layout')) {
            return 'antv';
          }
          if (id.includes('node_modules/vditor')) {
            return 'vditor';
          }
          if (id.includes('node_modules/sql-formatter')) {
            return 'sql-formatter';
          }
          if (id.includes('node_modules')) {
            return 'vendor';
          }
          if (id.includes('/src/views/')) {
            const match = id.match(/\/src\/views\/([^/]+)/);
            if (match) return `views/${match[1]}`;
          }
          if (id.includes('/src/components/')) {
            const match = id.match(/\/src\/components\/([^/]+)/);
            if (match) return `components/${match[1]}`;
          }
        }
      }
    },
    sourcemap: false,
    minify: 'terser',
    terserOptions: {
      compress: {
        drop_console: true,
        drop_debugger: true,
        pure_funcs: ['console.log', 'console.info'],
      },
      format: {
        comments: false,
      },
    },
  },
  plugins: [
    vue(),
    AutoImport({
      resolvers: [
        ElementPlusResolver({ importStyle: false }),
        IconsResolver({ prefix: 'Icon' }),
      ],
      dts: 'src/auto-imports.d.ts',
    }),
    Components({
      resolvers: [
        ElementPlusResolver({ importStyle: false }),
        IconsResolver({ enabledCollections: ['ep'] }),
      ],
      dts: 'src/components.d.ts',
    }),
    viteCompression({
      algorithm: 'gzip',
      threshold: 1024,
      deleteOriginFile: false,
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  }
})
