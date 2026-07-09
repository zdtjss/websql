<template>
  <div class="system-management-page">
    <!-- 浮动返回按钮：便捷美观，桌面/网页模式通用 -->
    <el-tooltip content="返回上一页" placement="right" :show-after="300">
      <button class="back-fab" @click="goBack" aria-label="返回上一页">
        <el-icon :size="16"><ArrowLeft /></el-icon>
      </button>
    </el-tooltip>
    <!-- 主内容区 -->
    <div class="page-content">
      <el-tabs v-model="activeTab" type="border-card" class="system-tabs">
        <!-- 系统配置 -->
        <el-tab-pane label="系统配置" name="system">
          <SystemConfig />
        </el-tab-pane>

        <!-- 角色管理（本地/桌面模式隐藏） -->
        <el-tab-pane v-if="isRemote" label="角色管理" name="role" lazy>
          <RolePermission />
        </el-tab-pane>

        <!-- 提示词管理 -->
        <el-tab-pane label="提示词管理" name="prompt" lazy>
          <PromptManagement />
        </el-tab-pane>

        <!-- 用户管理（本地/桌面模式隐藏） -->
        <el-tab-pane v-if="isRemote" label="用户管理" name="user" lazy>
          <UserManagement />
        </el-tab-pane>

        <!-- 连接管理 -->
        <el-tab-pane label="连接管理" name="conn" lazy>
          <ConnManagement />
        </el-tab-pane>

        <!-- 目录管理 -->
        <el-tab-pane label="目录管理" name="dir" lazy>
          <DirManagement />
        </el-tab-pane>

        <!-- SQL 审计日志 -->
        <el-tab-pane label="SQL 审计" name="audit" lazy>
          <SQLAuditLog />
        </el-tab-pane>

        <!-- 审计配置 -->
        <el-tab-pane label="审计配置" name="auditConfig" lazy>
          <AuditConfig />
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<script setup>
import { ref, provide, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowLeft } from '@element-plus/icons-vue'
import RolePermission from './RolePermission.vue'
import PromptManagement from './PromptManagement.vue'
import UserManagement from './UserManagement.vue'
import ConnManagement from './ConnManagement.vue'
import DirManagement from './DirManagement.vue'
import SystemConfig from './SystemConfig.vue'
import SQLAuditLog from './SQLAuditLog.vue'
import AuditConfig from './AuditConfig.vue'

const router = useRouter()
const activeTab = ref('system')
const isRemote = ref(sessionStorage.getItem("isRemote") === "true")

// 返回上一页：有历史则 back，无历史（直接进入/刷新）则回首页，首页路由守卫会跳到默认主页
function goBack() {
  if (window.history.length > 1) {
    router.back()
  } else {
    router.push('/')
  }
}

// 当前用户信息 - 从 sessionStorage 读取（App.vue 存储的）
// 在 setup 阶段立即读取，确保在子组件初始化前就有值
const storedUser = sessionStorage.getItem('systemManagement_user')

const currentUser = ref(
  storedUser ? JSON.parse(storedUser) : { id: "", name: "", isAdmin: false }
)

// 提供给子组件 SystemConfig.vue 使用
provide('currentUser', currentUser)

// 页面卸载时清理 sessionStorage
onMounted(() => {
  // 可选：在页面卸载时清理，或者保留以便刷新页面后仍能使用
})
</script>

<style scoped>
.system-management-page {
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: var(--bg-tertiary);
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 24px;
  background: var(--bg-primary);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
  z-index: 100;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 16px;
}

.page-title {
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.page-content {
  flex: 1;
  padding: 10px;
  overflow: hidden;
}

.system-tabs {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
  border-radius: 4px;
}

/* 为浮动返回按钮让出空间，避免遮挡首个 tab 标签 */
.system-tabs :deep(.el-tabs__header) {
  padding-left: 52px;
}

.system-tabs :deep(.el-tabs__content) {
  flex: 1;
  overflow: hidden;
}

.system-tabs :deep(.el-tab-pane) {
  height: 100%;
  overflow: hidden;
}

/* 浮动返回按钮：便捷美观，适配深浅主题 */
.back-fab {
  position: fixed;
  top: 14px;
  left: 14px;
  z-index: 2000;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--text-primary);
  background: var(--bg-primary);
  border: 1px solid var(--border-primary);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease, color 0.18s ease;
  padding: 0;
}

.back-fab:hover {
  transform: scale(1.08);
  border-color: var(--accent-color, #409eff);
  color: var(--accent-color, #409eff);
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.18);
}

.back-fab:active {
  transform: scale(0.96);
}
</style>
