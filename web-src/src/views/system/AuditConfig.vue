<template>
  <div class="audit-config-container">
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px;">
      <span style="font-size: 16px; font-weight: 600; color: #303133;">审计配置</span>
      <el-button size="small" @click="loadConfig" :loading="loading">刷新</el-button>
    </div>

    <el-form label-width="220px" :model="config" v-loading="loading">
      <el-form-item label="启用审计日志">
        <el-switch v-model="config.enabled" active-text="开启" inactive-text="关闭" />
        <span class="desc">全局审计日志开关</span>
      </el-form-item>

      <el-divider content-position="left">来源控制</el-divider>

      <el-form-item label="Agent 工具调用">
        <el-switch v-model="config.recordAgentTools" :disabled="!config.enabled" active-text="记录" inactive-text="不记录" />
        <span class="desc">控制是否记录 AI Agent 的 SQL 操作（query_data / exec_sql / import_data）</span>
      </el-form-item>

      <el-form-item label="SQL 编辑器执行">
        <el-switch v-model="config.recordSQLEditor" :disabled="!config.enabled" active-text="记录" inactive-text="不记录" />
        <span class="desc">控制是否记录用户在 SQL 编辑器中直接执行的 SQL</span>
      </el-form-item>

      <el-divider content-position="left">操作类型控制</el-divider>

      <el-form-item label="只读查询 (SELECT/SHOW)">
        <el-switch v-model="config.recordQuery" :disabled="!config.enabled" active-text="记录" inactive-text="不记录" />
        <span class="desc">查询类语句默认不记录以避免日志量过大</span>
      </el-form-item>

      <el-form-item label="写操作 (INSERT/UPDATE/DELETE)">
        <el-switch v-model="config.recordWrite" :disabled="!config.enabled" active-text="记录" inactive-text="不记录" />
        <span class="desc">包括 INSERT、UPDATE、DELETE、IMPORT 等写操作</span>
      </el-form-item>

      <el-form-item label="高风险操作 (DROP/ALTER)">
        <el-switch v-model="config.recordDangerous" :disabled="!config.enabled" active-text="记录" inactive-text="不记录" />
        <span class="desc">包括 DROP、TRUNCATE、ALTER、CREATE 等结构变更操作</span>
      </el-form-item>

      <el-divider content-position="left">风险等级控制</el-divider>

      <el-form-item label="最低记录风险等级">
        <el-radio-group v-model="config.minRiskLevel" :disabled="!config.enabled">
          <el-radio value="low">低（全量记录）</el-radio>
          <el-radio value="medium">中（仅记录中高）</el-radio>
          <el-radio value="high">高（仅记录高风险）</el-radio>
        </el-radio-group>
        <div class="desc">低于此等级的操作将不会被记录</div>
      </el-form-item>

      <el-divider content-position="left">存储控制</el-divider>

      <el-form-item label="日志保留天数">
        <el-input-number v-model="config.retentionDays" :min="1" :max="365" :step="1" />
        <span class="desc">超过保留天数的日志将被自动清理</span>
      </el-form-item>

      <el-form-item style="margin-top: 24px;">
        <el-button type="primary" @click="saveConfig" :loading="saving">保存配置</el-button>
        <el-button @click="resetConfig">恢复默认</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import http from '@/utils/httpProxy.js'
import { ElMessage } from 'element-plus'
import { reactive, ref, onMounted } from 'vue'

const defaultConfig = {
  enabled: true,
  recordQuery: false,
  recordWrite: true,
  recordDangerous: true,
  recordAgentTools: true,
  recordSQLEditor: true,
  retentionDays: 90,
  minRiskLevel: 'low'
}

const config = reactive({ ...defaultConfig })
const loading = ref(false)
const saving = ref(false)

async function loadConfig() {
  loading.value = true
  try {
    const resp = await http.get('/audit/config/get')
    Object.assign(config, resp.data.data || defaultConfig)
  } catch (e) {
    console.error('[AuditConfig] 加载配置失败:', e)
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  try {
    await http.post('/audit/config/save', config)
    ElMessage.success('审计配置已保存')
  } catch (e) {
    console.error('[AuditConfig] 保存配置失败:', e)
    ElMessage.error('保存配置失败')
  } finally {
    saving.value = false
  }
}

function resetConfig() {
  Object.assign(config, defaultConfig)
}

onMounted(() => {
  loadConfig()
})
</script>

<style scoped>
.audit-config-container {
  padding: 16px;
  max-width: 800px;
}

.desc {
  margin-left: 12px;
  font-size: 12px;
  color: #909399;
}
</style>