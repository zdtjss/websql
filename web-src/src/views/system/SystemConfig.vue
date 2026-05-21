<template>
  <div class="system-config">
    <el-divider content-position="left">
      <el-icon>
        <Monitor />
      </el-icon>
      AI 模型管理
    </el-divider>
    <div class="model-list-section">
      <div class="model-list-header">
        <span>已配置的模型</span>
        <el-button type="primary" size="small" @click="showAddModelDialog">
          <el-icon><Plus /></el-icon>
          添加模型
        </el-button>
      </div>
      <el-empty v-if="aiModelList.length === 0" description="暂无配置模型，请添加" />
      <div v-else class="model-list">
        <div v-for="model in aiModelList" :key="model.id"
          :class="['model-item', { 'is-selected': systemConfig.selectedModelId === model.id }]">
          <div class="model-item-info">
            <span class="model-model">{{ model.model }}</span>
            <span class="model-provider">{{ model.provider }}</span>
          </div>
          <div class="model-item-actions">
            <el-tag v-if="systemConfig.selectedModelId === model.id" type="success" size="small">当前使用</el-tag>
            <el-button size="small" text @click="selectModel(model)">
              <el-icon><Check /></el-icon>
              设为默认
            </el-button>
            <el-button size="small" text type="primary" @click="showEditModelDialog(model)">
              <el-icon><Edit /></el-icon>
              编辑
            </el-button>
            <el-button size="small" text type="danger" @click="removeModel(model)">
              <el-icon><Delete /></el-icon>
              删除
            </el-button>
          </div>
        </div>
      </div>
    </div>

    <el-divider content-position="left">
      <el-icon>
        <Link />
      </el-icon>
      外部用户认证
    </el-divider>
    <el-form label-width="120px" :model="systemConfig">
      <el-form-item label="认证接口 URL">
        <el-input v-model="systemConfig.outterUser" placeholder="http://localhost:8081/api/login" />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" @click="testOutterUser" :loading="testingOutterUser">
          测试接口
        </el-button>
      </el-form-item>
    </el-form>

    <el-divider content-position="left">
      <el-icon>
        <Lock />
      </el-icon>
      IP 访问控制
    </el-divider>
    <el-form label-width="120px" :model="systemConfig">
      <el-form-item label="允许的 IP 列表">
        <el-input v-model="systemConfig.allowedIP" type="textarea" :rows="4" placeholder="请输入 IP 地址，每行一个" />
        <div style="font-size: 12px; color: #909399; margin-top: 4px;">
          💡 每行一个 IP 地址，例如：127.0.0.1 或 192.168.1.100
        </div>
      </el-form-item>
    </el-form>

    <el-divider content-position="left">
      <el-icon>
        <Coin />
      </el-icon>
      Redis 配置
    </el-divider>
    <el-form label-width="120px" :model="systemConfig">
      <el-form-item label="Redis 地址">
        <el-input v-model="systemConfig.redisAddr" placeholder="127.0.0.1:6379" />
      </el-form-item>
      <el-form-item label="Redis 密码">
        <el-input v-model="systemConfig.redisPassword" placeholder="可选" show-password />
      </el-form-item>
      <el-form-item label="Redis DB">
        <el-input-number v-model="systemConfig.redisDB" :min="0" :max="15" :step="1" />
        <div style="font-size: 12px; color: #909399; margin-top: 4px;">
          💡 Redis 数据库编号，范围 0-15
        </div>
      </el-form-item>
    </el-form>

    <el-divider content-position="left">
      <el-icon>
        <User />
      </el-icon>
      生物识别配置
    </el-divider>
    <div class="bio-section">
      <el-alert title="生物识别登录" type="info" :closable="false">
        <div>💡 使用指纹或面容识别快速登录系统，仅在当前设备支持生物识别时可用</div>
      </el-alert>
      <el-form label-width="120px">
        <el-form-item label="设备支持">
          <el-tag :type="bioSupported ? 'success' : 'danger'">
            {{ bioSupported ? '支持' : '不支持' }}
          </el-tag>
        </el-form-item>
        <el-form-item label="已注册">
          <el-tag :type="bioRegistered ? 'success' : 'info'">
            {{ bioRegistered ? '已注册' : '未注册' }}
          </el-tag>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="registerBio" :disabled="!bioSupported || bioRegistering"
            :loading="bioRegistering">
            <el-icon>
              <User />
            </el-icon>
            {{ bioRegistered ? '重新注册' : '注册生物识别' }}
          </el-button>
          <el-button v-if="bioRegistered" type="danger" @click="removeBio" :loading="bioRemoving">
            <el-icon>
              <Delete />
            </el-icon>
            删除生物识别
          </el-button>
        </el-form-item>
      </el-form>
    </div>

    <div class="config-actions">
      <el-button type="primary" @click="saveAllConfig" :loading="savingAll" size="large">
        <el-icon>
          <Check />
        </el-icon>
        保存所有配置
      </el-button>
    </div>

    <el-dialog v-model="modelDialogVisible" :title="editingModel ? '编辑模型' : '添加模型'" width="600px" append-to-body>
      <el-form :model="modelForm" label-width="120px">
        <el-form-item label="AI 提供商">
          <el-radio-group v-model="modelForm.provider">
            <el-radio value="openai">OpenAI</el-radio>
            <el-radio value="ollama">Ollama</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="Base URL">
          <el-input v-model="modelForm.baseUrl" placeholder="https://ollama.com" />
        </el-form-item>
        <el-form-item label="Model">
          <el-input v-model="modelForm.model" placeholder="qwen3.5:2b" />
        </el-form-item>
        <el-form-item label="API Key">
          <el-input v-model="modelForm.apiKey" placeholder="sk-...（OpenAI 必填，Ollama 可留空）" show-password />
        </el-form-item>
        <el-form-item label="Temperature">
          <el-slider v-model="modelForm.temperature" :min="0" :max="2" :step="0.1" show-input />
        </el-form-item>
        <el-form-item label="Max Tokens">
          <el-input-number v-model="modelForm.maxTokens" :min="0" :max="modelMaxTokensMax" :step="100" placeholder="0=不限" />
          <span v-if="modelMaxTokensMax !== Infinity" style="margin-left: 10px; font-size: 12px; color: #909399;">Ollama 最大 262100</span>
        </el-form-item>
        <el-form-item label="思考模式">
          <el-switch v-model="modelForm.enableThinking" />
          <span style="margin-left: 10px; font-size: 12px; color: #909399;">启用后模型会输出思考过程</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="modelDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="testModelConfig" :loading="modelTesting">
          <el-icon><Connection /></el-icon>
          测试
        </el-button>
        <el-button type="primary" @click="saveModel">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import http from '@/utils/httpProxy'
import { client, parsers, server } from '@passwordless-id/webauthn'
import { Check, Coin, Connection, Delete, Edit, Link, Lock, Monitor, Plus, User } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, inject, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

const emit = defineEmits(['config-saved'])
const router = useRouter()
const currentUser = inject('currentUser')

const bioLocalStorageKey = "nway_websql_bio_credential_id"
const bioSupported = ref(false)
const bioRegistered = ref(false)
const bioRegistering = ref(false)
const bioRemoving = ref(false)

const systemConfig = ref({
  outterUser: '',
  allowedIP: '127.0.0.1\n::1',
  aiModelList: [],
  selectedModelId: '',
  redisAddr: '',
  redisPassword: '',
  redisDB: 0
})

const aiModelList = ref([])
const modelDialogVisible = ref(false)
const editingModel = ref(null)
const modelTesting = ref(false)
const testingOutterUser = ref(false)
const savingAll = ref(false)

const modelForm = ref({
  id: '',
  provider: 'ollama',
  baseUrl: '',
  model: '',
  apiKey: '',
  temperature: 0.7,
  maxTokens: 0,
  enableThinking: false
})

const modelMaxTokensMax = computed(() => {
  return modelForm.value.baseUrl.toLowerCase().startsWith('https://ollama.com')
    || modelForm.value.baseUrl.toLowerCase().startsWith('http://ollama.com')
    ? 262100 : Infinity
})

watch(() => modelForm.value.baseUrl, () => {
  const isOllama = modelForm.value.baseUrl.toLowerCase().startsWith('https://ollama.com')
    || modelForm.value.baseUrl.toLowerCase().startsWith('http://ollama.com')
  if (isOllama && modelForm.value.maxTokens > 262100) {
    modelForm.value.maxTokens = 262100
  }
})

const loadSystemConfig = () => {
  http.get("/system/config/all/get").then((resp) => {
    if (resp.data && resp.data.data) {
      const data = resp.data.data
      systemConfig.value.outterUser = data.outterUser || ''
      systemConfig.value.selectedModelId = data.selectedModelId || ''
      systemConfig.value.redisAddr = data.redisAddr || ''
      systemConfig.value.redisPassword = data.redisPassword || ''
      systemConfig.value.redisDB = data.redisDB !== undefined ? data.redisDB : 0

      if (data.allowedIP && Array.isArray(data.allowedIP)) {
        systemConfig.value.allowedIP = data.allowedIP.join('\n')
      }

      if (data.aiModelList && Array.isArray(data.aiModelList)) {
        aiModelList.value = data.aiModelList
        systemConfig.value.aiModelList = data.aiModelList
      } else {
        aiModelList.value = []
        systemConfig.value.aiModelList = []
      }
    }
  })
}

const saveAllConfig = () => {
  savingAll.value = true
  const ips = systemConfig.value.allowedIP.split('\n').map(ip => ip.trim()).filter(ip => ip !== '')

  http.post("/system/config/all/save", {
    aiModelList: aiModelList.value,
    selectedModelId: systemConfig.value.selectedModelId,
    outterUser: systemConfig.value.outterUser,
    allowedIP: ips,
    redisAddr: systemConfig.value.redisAddr,
    redisPassword: systemConfig.value.redisPassword,
    redisDB: systemConfig.value.redisDB
  }).then(() => {
    ElMessage.success("保存成功")
    emit('config-saved', systemConfig.value)
  }).finally(() => {
    savingAll.value = false
  })
}

const showAddModelDialog = () => {
  editingModel.value = null
  modelForm.value = {
    id: '',
    provider: 'ollama',
    baseUrl: '',
    model: '',
    apiKey: '',
    temperature: 0.7,
    maxTokens: 0,
    enableThinking: false
  }
  modelDialogVisible.value = true
}

const showEditModelDialog = (model) => {
  editingModel.value = model
  modelForm.value = {
    id: model.id,
    provider: model.provider,
    baseUrl: model.baseUrl,
    model: model.model,
    apiKey: model.apiKey || '',
    temperature: model.temperature || 0.7,
    maxTokens: model.maxTokens || 0,
    enableThinking: model.enableThinking || false
  }
  modelDialogVisible.value = true
}

const saveModel = () => {
  if (!modelForm.value.baseUrl) {
    ElMessage.warning('请输入 Base URL')
    return
  }
  if (!modelForm.value.model) {
    ElMessage.warning('请输入 Model 名称')
    return
  }

  if (editingModel.value) {
    const idx = aiModelList.value.findIndex(m => m.id === editingModel.value.id)
    if (idx !== -1) {
      aiModelList.value[idx] = { ...modelForm.value }
    }
  } else {
    const newModel = {
      ...modelForm.value,
      id: 'model_' + Date.now() + '_' + Math.random().toString(36).substring(2, 8)
    }
    aiModelList.value.push(newModel)
  }
  modelDialogVisible.value = false
  ElMessage.success(editingModel.value ? '模型已更新' : '模型已添加')
}

const removeModel = (model) => {
  ElMessageBox.confirm(
    `确定要删除模型 "${model.model}" 吗？`,
    '确认删除',
    { confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning' }
  ).then(() => {
    aiModelList.value = aiModelList.value.filter(m => m.id !== model.id)
    if (systemConfig.value.selectedModelId === model.id) {
      systemConfig.value.selectedModelId = aiModelList.value.length > 0 ? aiModelList.value[0].id : ''
    }
    ElMessage.success('模型已删除')
  }).catch(() => {})
}

const selectModel = (model) => {
  systemConfig.value.selectedModelId = model.id
  ElMessage.success(`已选择模型：${model.model}`)
}

const testModelConfig = () => {
  modelTesting.value = true
  http.post("/ai/config/test", {
    provider: modelForm.value.provider,
    baseUrl: modelForm.value.baseUrl,
    model: modelForm.value.model,
    apiKey: modelForm.value.apiKey
  }).then(() => {
    ElMessage.success("连接成功")
  }).finally(() => {
    modelTesting.value = false
  })
}

const testOutterUser = () => {
  testingOutterUser.value = true
  http.post("/system/config/outterUser/test", { url: systemConfig.value.outterUser })
    .then((resp) => {
      if (resp.data.code === 200) {
        ElMessage.success("测试成功：" + JSON.stringify(resp.data.data))
      } else {
        console.error('[SystemConfig] 外部用户接口测试失败 - msg:', resp.data.msg)
        ElMessage.error("测试失败，请检查接口配置")
      }
    })
    .catch((err) => {
      console.error('[SystemConfig] 外部用户接口测试异常:', err)
      ElMessage.error("测试失败：接口无响应")
    })
    .finally(() => {
      testingOutterUser.value = false
    })
}

const checkBioSupport = () => {
  bioSupported.value = client.isAvailable()
}

const checkBioRegistered = () => {
  const credential = localStorage.getItem(bioLocalStorageKey)
  bioRegistered.value = !!credential
}

const registerBio = async () => {
  console.log('[SystemConfig.vue] 点击注册生物识别按钮')
  console.log('[SystemConfig.vue] 当前 currentUser:', currentUser.value)

  if (!bioSupported.value) {
    ElMessage({
      showClose: true,
      message: '您的设备不支持生物识别',
      type: 'error',
    })
    return
  }

  if (!currentUser.value || !currentUser.value.id) {
    console.error('[SystemConfig.vue] 用户未登录，无法注册生物识别')
    ElMessage.error("请先登录")
    return
  }

  bioRegistering.value = true
  let registration = await client.register({
    challenge: server.randomChallenge(),
    user: { id: currentUser.value.id, name: currentUser.value.name }
  })

  const parsed = parsers.parseRegistration(registration)
  console.log(JSON.stringify(parsed))

  localStorage.setItem(bioLocalStorageKey, JSON.stringify({ id: parsed.credential.id, transports: parsed.credential.transports }))

  const params = new URLSearchParams();
  params.append("bioKey", parsed.credential.id);
  http.post("/saveUserBio", params).then((resp) => {
    if (resp.data.code == 200) {
      ElMessage("注册成功")
      checkBioRegistered()
    } else {
      console.error('[SystemConfig] 生物识别注册失败 - code:', resp.data.code)
      ElMessage("注册失败")
    }
  }).catch((error) => {
    console.error('[SystemConfig] 生物识别注册异常:', error)
    ElMessage.error("注册失败")
  }).finally(() => {
    bioRegistering.value = false
  })
}

const removeBio = () => {
  localStorage.removeItem(bioLocalStorageKey)
  ElMessage.success("删除成功")
  checkBioRegistered()
}

onMounted(() => {
  loadSystemConfig()
  checkBioSupport()
  checkBioRegistered()
  console.log('[SystemConfig.vue] onMounted, inject 的 currentUser:', currentUser.value)
})
</script>

<style scoped>
.system-config {
  padding: 20px;
  max-height: calc(100vh - 150px);
  overflow-y: auto;
}

.model-list-section {
  margin-bottom: 20px;
}

.model-list-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  font-weight: 600;
  font-size: 14px;
}

.model-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.model-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  background: #fafafa;
  transition: all 0.2s ease;
}

.model-item:hover {
  border-color: #409eff;
  background: #f5f7fa;
}

.model-item.is-selected {
  border-color: #67c23a;
  background: #f0f9ff;
}

.model-item-info {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
}

.model-model {
  font-weight: 600;
  color: #303133;
  font-size: 14px;
  font-family: monospace;
}

.model-provider {
  font-size: 12px;
  color: #909399;
  background: #f0f0f0;
  padding: 2px 8px;
  border-radius: 4px;
}

.model-item-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.config-actions {
  margin-top: 20px;
  text-align: center;
  padding-top: 20px;
  border-top: 1px solid #e4e7ed;
}

.bio-section {
  margin-bottom: 20px;
}

:deep(.el-form-item) {
  margin-bottom: 20px;
}

:deep(.el-divider) {
  margin: 20px 0;
}
</style>
