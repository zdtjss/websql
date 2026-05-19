<template>
  <el-drawer
    v-model="visible"
    title="SQL 收藏夹"
    size="420px"
    @close="visible = false"
  >
    <div class="snippet-actions">
      <el-button size="small" type="primary" @click="showAddSnippet()">+ 添加当前 SQL</el-button>
    </div>

    <el-input
      v-model="searchText"
      placeholder="搜索代码片段..."
      clearable
      size="small"
      style="margin: 12px 0;"
    />

    <el-scrollbar style="height: calc(100vh - 230px);">
      <div v-if="filteredSnippets.length === 0" class="snippet-empty">
        暂无收藏的 SQL 片段
      </div>
      <div
        v-for="snippet in filteredSnippets"
        :key="snippet.id"
        class="snippet-card"
        @click="applySnippet(snippet)"
      >
        <div class="snippet-header">
          <span class="snippet-name">{{ snippet.name || '未命名' }}</span>
          <el-icon class="snippet-delete" :size="14" @click.stop="deleteSnippet(snippet.id)">
            <Delete />
          </el-icon>
        </div>
        <pre class="snippet-body">{{ snippet.sql }}</pre>
      </div>
    </el-scrollbar>

    <el-dialog
      v-model="addDialogVisible"
      title="添加收藏"
      width="500px"
      :append-to-body="true"
      destroy-on-close
    >
      <el-form ref="formRef" :model="formData" label-width="60px" size="small" :rules="rules">
        <el-form-item label="名称" prop="name">
          <el-input v-model="formData.name" placeholder="片段名称" />
        </el-form-item>
        <el-form-item label="SQL" prop="sql">
          <el-input v-model="formData.sql" type="textarea" :rows="6" placeholder="SQL 语句" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" @click="addDialogVisible = false">取消</el-button>
        <el-button size="small" type="primary" @click="saveSnippet">保存</el-button>
      </template>
    </el-dialog>
  </el-drawer>
</template>

<script setup>
import { Delete } from '@element-plus/icons-vue'
import { computed, onMounted, ref, useTemplateRef } from 'vue'

const visible = defineModel({ default: false })
const { currentSql } = defineProps({
  currentSql: { type: String, default: '' },
})
const emit = defineEmits(['apply'])

const STORAGE_KEY = 'websql_snippets'

const snippets = ref([])
const searchText = ref('')
const addDialogVisible = ref(false)
const formRef = useTemplateRef('formRef')

const formData = ref({ name: '', sql: '' })

const rules = {
  name: [{ required: true, message: '请输入片段名称', trigger: 'blur' }],
  sql: [{ required: true, message: '请输入 SQL 语句', trigger: 'blur' }],
}

const filteredSnippets = computed(() => {
  const kw = searchText.value.trim().toLowerCase()
  if (!kw) return snippets.value
  return snippets.value.filter(
    s => (s.name || '').toLowerCase().includes(kw) || (s.sql || '').toLowerCase().includes(kw)
  )
})

function loadSnippets() {
  try {
    snippets.value = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]')
  } catch {
    snippets.value = []
  }
}

function saveAll() {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(snippets.value))
}

function showAddSnippet() {
  formData.value.name = ''
  formData.value.sql = currentSql || ''
  addDialogVisible.value = true
}

function saveSnippet() {
  formRef.value.validate((valid) => {
    if (!valid) return
    snippets.value.push({
      id: Date.now().toString(36),
      name: formData.value.name.trim(),
      sql: formData.value.sql.trim(),
    })
    saveAll()
    addDialogVisible.value = false
    formRef.value.resetFields()
  })
}

function deleteSnippet(id) {
  snippets.value = snippets.value.filter(s => s.id !== id)
  saveAll()
}

function applySnippet(snippet) {
  emit('apply', snippet.sql)
  visible.value = false
}

onMounted(() => {
  loadSnippets()
})
</script>

<style scoped>
.snippet-actions {
  display: flex;
  gap: 8px;
}

.snippet-empty {
  text-align: center;
  color: #c0c4cc;
  padding: 40px 0;
  font-size: 14px;
}

.snippet-card {
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 8px;
  cursor: pointer;
  transition: all 0.15s;
}

.snippet-card:hover {
  border-color: #409eff;
  background: #ecf5ff;
}

.snippet-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}

.snippet-name {
  font-size: 13px;
  font-weight: 600;
  color: #303133;
}

.snippet-delete {
  color: #909399;
  cursor: pointer;
}

.snippet-delete:hover {
  color: #f56c6c;
}

.snippet-body {
  background: #f5f7fa;
  border-radius: 4px;
  padding: 8px;
  font-size: 12px;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 80px;
  overflow: hidden;
  color: #606266;
}
</style>