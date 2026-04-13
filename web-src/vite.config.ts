import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import IconsResolver from 'unplugin-icons/resolver'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config
export default defineConfig({
  server: {
    port: 5175,
    host: "0.0.0.0",
    proxy: {
      '/api/': {
        target: 'http://localhost:9081', // 目标代理接口地址
        secure: false,
        changeOrigin: true, // 开启代理，在本地创建一个虚拟服务端
        // rewrite: (path) => path.replace(/^\/api/, '')
      },
      '/sysapi/': {
        target: 'http://localhost:8081', // 目标代理接口地址
        secure: false,
        changeOrigin: true, // 开启代理，在本地创建一个虚拟服务端
        rewrite: (path) => path.replace(/^\/sysapi/, '/nway-system')
      }
    }
  },
  build: {
    chunkSizeWarningLimit: 2000,
    rollupOptions: {
      output: {
        manualChunks(id) {
          // 框架核心
          if (id.includes('node_modules/vue') || id.includes('node_modules/pinia') || id.includes('node_modules/vue-router')) {
            return 'vue-core';
          }
          // UI 库单独分包
          if (id.includes('node_modules/element-plus')) {
            return 'element-plus';
          }
          // 大型依赖单独分包
          if (id.includes('node_modules/mermaid')) {
            return 'mermaid';
          }
          if (id.includes('node_modules/markdown-it')) {
            return 'markdown-it';
          }
          if (id.includes('node_modules/axios')) {
            return 'axios';
          }
          if (id.includes('node_modules/codemirror')) {
            return 'codemirror';
          }
          if (id.includes('node_modules/exceljs') || id.includes('node_modules/xlsx')) {
            return 'excel';
          }
          if (id.includes('node_modules/highlight.js')) {
            return 'highlight';
          }
          // 其他 node_modules 归为 vendor
          if (id.includes('node_modules')) {
            return 'vendor';
          }
          // 应用代码按功能模块分割
          if (id.includes('/src/views/')) {
            const match = id.match(/\/src\/views\/([^/]+)/);
            if (match) return `views/${match[1]}`;
          }
          if (id.includes('/src/components/')) {
            return 'components';
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
      resolvers: [ElementPlusResolver(), IconsResolver({
        prefix: 'Icon',
      }),],
    }),
    Components({
      resolvers: [ElementPlusResolver(),
      IconsResolver({
        enabledCollections: ['ep'],
      }),],
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  }
})
