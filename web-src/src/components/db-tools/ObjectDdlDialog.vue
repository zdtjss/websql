<template>
  <el-dialog
    v-model="visible"
    :title="name"
    width="800px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
    aria-label="对象定义详情对话框"
    @opened="onDetailOpened"
  >
    <!-- 图标按钮：仅图标无文字，需补充 aria-label -->
    <el-icon style="position:absolute;right:18px;cursor:pointer;z-index:9999;" size="16" role="button" tabindex="0"
      aria-label="复制对象定义到剪贴板" title="复制"
      @click="copyDetail" @keyup.enter="copyDetail">
      <CopyDocument />
    </el-icon>
    <div style="max-height: 500px;overflow-y:auto;">
      <pre v-loading="loading"><code class="language-sql" v-html="highlightedCode"></code></pre>
    </div>
    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { CopyDocument } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { nextTick, ref, watch } from 'vue'
import { highlightSql } from '@/utils/lazyDeps'
import { getObjectDDL } from '@/api/sql'
import { isValidIdentifier } from '@/utils/identifierValidator'

const visible = defineModel({ default: false })
const props = defineProps({
  connId: String,
  schema: String,
  objType: String,
  name: String,
})

const code = ref('')
const highlightedCode = ref('')
const loading = ref(false)

// DDL 文本变化时异步生成高亮 HTML
watch(code, async (val) => {
  if (!val) { highlightedCode.value = ''; return }
  highlightedCode.value = await highlightSql(val)
}, { immediate: true })

// 对话框打开或对象变化时加载 DDL
watch(
  () => [visible.value, props.name, props.objType, props.schema, props.connId],
  ([open]) => {
    if (!open) return
    loadDdl()
  }
)

function loadDdl() {
  code.value = ''
  highlightedCode.value = ''
  if (!props.name || !isValidIdentifier(props.name)) {
    code.value = '-- 非法的对象名，无法获取定义'
    return
  }
  loading.value = true
  getObjectDDL({ connId: props.connId, schema: props.schema, type: props.objType, name: props.name })
    .then(resp => {
      code.value = resp.data?.data || '-- 没有可用的定义'
    })
    .catch(() => {
      code.value = '-- 获取定义失败'
    })
    .finally(() => { loading.value = false })
}

// 对话框打开后将焦点移到复制按钮，便于键盘操作
function onDetailOpened() {
  nextTick(() => {
    document.querySelector('.classical-dialog [role="button"][aria-label^="复制"]')?.focus()
  })
}

function copyDetail() {
  navigator.clipboard.writeText(code.value).then(() => {
    ElMessage({ message: '已复制到剪贴板', type: 'success' })
  })
}
</script>
