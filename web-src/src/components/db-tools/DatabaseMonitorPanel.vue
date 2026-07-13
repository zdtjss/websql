<template>
  <!-- 统一数据库监控面板：合并自 EnhancedMonitorPanel + ServerStatusPanel -->
  <!-- 拆分原则：主容器仅负责对话框骨架、Tab 状态编排与数据透传；
       每个 Tab 的状态、数据加载、自动刷新、AI 分析均下沉到对应子组件。 -->
  <el-dialog
    v-model="visible"
    title="数据库监控"
    width="960px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog db-monitor-dialog"
    aria-label="数据库监控对话框"
    @opened="onOpen"
  >
    <el-tabs v-model="activeTab" type="card">
      <!-- 1. 概览：关键指标卡片 + 服务器基础信息 -->
      <el-tab-pane label="概览" name="overview">
        <MonitorOverviewTab :conn-id="connId" :schema="schema" :active="activeTab === 'overview'" />
      </el-tab-pane>

      <!-- 2. 会话与进程：合并进程列表，支持搜索与 Kill -->
      <el-tab-pane label="会话与进程" name="sessions">
        <MonitorSessionsTab :conn-id="connId" :schema="schema" :active="activeTab === 'sessions'" />
      </el-tab-pane>

      <!-- 3. 性能趋势：实时采样与 ECharts 趋势展示 -->
      <el-tab-pane label="性能趋势" name="performance">
        <MonitorPerformanceTab :conn-id="connId" :schema="schema" :active="activeTab === 'performance'" />
      </el-tab-pane>

      <!-- 4. 服务器变量：全局 / 会话变量，可搜索 -->
      <el-tab-pane label="服务器变量" name="variables">
        <MonitorVariablesTab :conn-id="connId" :active="activeTab === 'variables'" />
      </el-tab-pane>

      <!-- 5. 状态指标：状态计数器，可重置 -->
      <el-tab-pane label="状态指标" name="status">
        <MonitorStatusTab :conn-id="connId" :schema="schema" :active="activeTab === 'status'" />
      </el-tab-pane>

      <!-- 6. InnoDB 引擎状态（仅 MySQL/MariaDB 支持） -->
      <el-tab-pane label="InnoDB状态" name="innodb">
        <MonitorInnodbTab :conn-id="connId" :active="activeTab === 'innodb'" />
      </el-tab-pane>

      <!-- 7. 锁与事务等待 -->
      <el-tab-pane label="锁与等待" name="locks">
        <MonitorLocksTab :conn-id="connId" :active="activeTab === 'locks'" />
      </el-tab-pane>

      <!-- 8. 慢查询分析 -->
      <el-tab-pane label="慢查询分析" name="slow">
        <MonitorSlowTab :conn-id="connId" :active="activeTab === 'slow'" />
      </el-tab-pane>

      <!-- 9. 表统计 TOP N -->
      <el-tab-pane label="表统计" name="topTables">
        <MonitorTopTablesTab :conn-id="connId" :schema="schema" :active="activeTab === 'topTables'" />
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import MonitorOverviewTab from './components/MonitorOverviewTab.vue'
import MonitorSessionsTab from './components/MonitorSessionsTab.vue'
import MonitorPerformanceTab from './components/MonitorPerformanceTab.vue'
import MonitorVariablesTab from './components/MonitorVariablesTab.vue'
import MonitorStatusTab from './components/MonitorStatusTab.vue'
import MonitorInnodbTab from './components/MonitorInnodbTab.vue'
import MonitorLocksTab from './components/MonitorLocksTab.vue'
import MonitorSlowTab from './components/MonitorSlowTab.vue'
import MonitorTopTablesTab from './components/MonitorTopTablesTab.vue'

// 双向绑定可见性
const visible = defineModel({ default: false })
const { connId, schema, initialTab } = defineProps({
  connId: String,
  schema: String,
  // 打开时聚焦的 Tab：overview / sessions / performance / variables / status
  initialTab: { type: String, default: 'overview' },
})

// ============ 通用状态 ============
const activeTab = ref('overview')

function onOpen() {
  // 应用初始 Tab（树节点的"服务器状态"/"实时监控"可指定聚焦 Tab）
  // destroy-on-close 保证每次打开时子组件重新挂载，无需手动重置数据
  activeTab.value = initialTab || 'overview'
}
</script>
