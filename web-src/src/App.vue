<template>
  <el-config-provider :locale="zhCn">
    <router-view />
    <MouseGestureOverlay />
    <PageFindBar v-if="isDesktop" />
  </el-config-provider>
</template>

<script setup>
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import MouseGestureOverlay from '@/components/common/MouseGestureOverlay.vue'
import PageFindBar from '@/components/common/PageFindBar.vue'

// 桌面模式才挂载页面内查找栏（Ctrl/Cmd+F 触发）
const isDesktop = sessionStorage.getItem('isDesktop') === 'true'
</script>
