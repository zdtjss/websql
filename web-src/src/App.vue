<template>
  <el-config-provider :locale="zhCn">
    <router-view />
    <MouseGestureOverlay />
  </el-config-provider>
</template>

<script setup>
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import MouseGestureOverlay from '@/components/common/MouseGestureOverlay.vue'
</script>
