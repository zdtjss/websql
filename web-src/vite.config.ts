import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import IconsResolver from 'unplugin-icons/resolver'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config
export default defineConfig({
  build: {
    sourcemap: true,
    outDir: "../static",
    emptyOutDir: true
  },
  server: {
    proxy: {
      '/api/': {
        target: 'http://localhost', // 目标代理接口地址
        secure: false,
        changeOrigin: true, // 开启代理，在本地创建一个虚拟服务端
        rewrite: (path) => path.replace(/^\/api/, '')
      }
    }
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
    }),],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  }
})
