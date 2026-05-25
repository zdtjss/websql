<template>
  <div class="system-management-page">
    <!-- 主内容区 -->
    <div class="page-content">
      <el-tabs v-model="activeTab" type="border-card" class="system-tabs">
        <!-- 系统配置 -->
        <el-tab-pane label="系统配置" name="system">
          <SystemConfig />
        </el-tab-pane>

        <!-- 角色管理 -->
        <el-tab-pane label="角色管理" name="role" lazy>
          <RolePermission />
        </el-tab-pane>

        <!-- 提示词管理 -->
        <el-tab-pane label="提示词管理" name="prompt" lazy>
          <PromptManagement />
        </el-tab-pane>

        <!-- 用户管理 -->
        <el-tab-pane label="用户管理" name="user" lazy>
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

// 当前用户信息 - 从 sessionStorage 读取（App.vue 存储的）
// 在 setup 阶段立即读取，确保在子组件初始化前就有值
const storedUser = sessionStorage.getItem('systemManagement_user')
console.log('[SystemManagement.vue] setup 阶段从 sessionStorage 读取:', storedUser)

const currentUser = ref(
  storedUser ? JSON.parse(storedUser) : { id: "", name: "", isAdmin: false }
)
console.log('[SystemManagement.vue] currentUser 初始化为:', currentUser.value)

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
  padding: 20px;
  overflow: hidden;
}

.system-tabs {
  height: 100%;
  background: var(--bg-primary);
  border-radius: 4px;
}
</style>
