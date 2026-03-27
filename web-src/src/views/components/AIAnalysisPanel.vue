<template>
  <div v-if="visible" class="ai-analysis-panel" style="border-top:1px solid #e4e7ed;display:flex;flex-direction:column;height:320px;">
    <!-- Chat messages -->
    <div ref="msgContainer" style="flex:1;overflow-y:auto;padding:12px;display:flex;flex-direction:column;gap:8px;">
      <div v-for="(msg, idx) in messages" :key="idx"
        :style="{
          alignSelf: msg.role === 'user' ? 'flex-end' : 'flex-start',
          maxWidth: '80%',
          background: msg.role === 'user' ? '#409eff' : '#f5f7fa',
          color: msg.role === 'user' ? '#fff' : '#303133',
          borderRadius: '8px',
          padding: '8px 12px',
          fontSize: '13px',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }"
      >{{ msg.content }}</div>
      <div v-if="loading" style="align-self:flex-start;color:#909399;font-size:13px;">AI 正在思考...</div>
    </div>

    <!-- Input area -->
    <div style="padding:8px;border-top:1px solid #e4e7ed;display:flex;gap:8px;align-items:flex-end;">
      <el-input
        v-model="inputText"
        type="textarea"
        :rows="2"
        placeholder="输入问题..."
        :disabled="loading"
        @keydown.enter.exact.prevent="sendMessage"
        style="flex:1;"
      />
      <el-button type="primary" size="small" :loading="loading" :disabled="!inputText.trim()" @click="sendMessage">
        发送
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import http from '../../js/utils/httpProxy.js'

const props = defineProps({
  visible: Boolean,
  connId: String,
  schema: String,
  tableName: String,
  dataSample: { type: Array, default: () => [] },
})

const messages = ref([])
const inputText = ref('')
const loading = ref(false)
const msgContainer = ref(null)

async function sendMessage() {
  const text = inputText.value.trim()
  if (!text || loading.value) return

  messages.value.push({ role: 'user', content: text })
  inputText.value = ''
  loading.value = true
  scrollToBottom()

  try {
    const resp = await http.post('/ai/chat', {
      connId: props.connId,
      schema: props.schema,
      tableName: props.tableName,
      messages: messages.value.slice(),
      dataSample: props.dataSample.slice(0, 20),
    })
    const reply = resp.data?.data?.reply || ''
    if (reply) {
      messages.value.push({ role: 'assistant', content: reply })
    } else {
      ElMessage({ message: 'AI 未返回回复', type: 'warning' })
    }
  } catch (e) {
    // httpProxy interceptor shows error
  } finally {
    loading.value = false
    scrollToBottom()
  }
}

function scrollToBottom() {
  nextTick(() => {
    if (msgContainer.value) {
      msgContainer.value.scrollTop = msgContainer.value.scrollHeight
    }
  })
}

watch(() => props.visible, (v) => {
  if (v) scrollToBottom()
})
</script>
