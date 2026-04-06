<template>
  <div class="system-config">
    <el-divider content-position="left">
      <el-icon>
        <Monitor />
      </el-icon>
      AI 服务配置
    </el-divider>
    <el-form label-width="120px" :model="systemConfig">
      <el-form-item label="AI 提供商">
        <el-radio-group v-model="systemConfig.aiProvider">
          <el-radio value="ollama">Ollama</el-radio>
          <el-radio value="openai">OpenAI</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="Base URL">
        <el-input v-model="systemConfig.aiBaseUrl" placeholder="http://localhost:11434" />
      </el-form-item>
      <el-form-item label="Model">
        <el-input v-model="systemConfig.aiModel" placeholder="e.g. qwen2.5:14b" />
      </el-form-item>
      <el-form-item label="API Key">
        <el-input v-model="systemConfig.aiApiKey" type="password" show-password placeholder="sk-..." />
      </el-form-item>
      <el-form-item label="Temperature">
        <el-slider v-model="systemConfig.aiTemperature" :min="0" :max="2" :step="0.1" show-input />
      </el-form-item>
      <el-form-item label="Max Tokens">
        <el-input-number v-model="systemConfig.aiMaxTokens" :min="0" :max="128000" :step="1024" placeholder="0=不限" />
      </el-form-item>
      <el-form-item label="思考模式">
        <el-switch v-model="systemConfig.aiEnableThinking" />
        <span style="margin-left: 10px; font-size: 12px; color: #909399;">启用后模型会输出思考过程</span>
      </el-form-item>
      <el-form-item>
        <el-button type="primary" @click="testAiConfig" :loading="aiTesting">
          <el-icon>
            <Connection />
          </el-icon>
          测试 AI 配置
        </el-button>
      </el-form-item>
    </el-form>

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
  </div>
</template>

<script setup>
import { ref, onMounted, defineEmits, inject } from 'vue'
import { useRouter } from 'vue-router'
import http from '@/js/utils/httpProxy'
import { ElMessage } from 'element-plus'
import { client, parsers, server } from '@passwordless-id/webauthn'

const emit = defineEmits(['config-saved'])
const router = useRouter()
const currentUser = inject('currentUser')

const bioLocalStorageKey = "nway_websql_bio_credential_id"
const bioSupported = ref(false)
const bioRegistered = ref(false)
const bioRegistering = ref(false)
const bioRemoving = ref(false)

const systemConfig = ref({
  aiProvider: 'ollama',
  aiBaseUrl: '',
  aiModel: '',
  aiApiKey: '',
  aiTemperature: 0.7,
  aiMaxTokens: 0,
  aiEnableThinking: false,
  outterUser: '',
  allowedIP: '127.0.0.1\n::1'
})

const aiTesting = ref(false)
const testingOutterUser = ref(false)
const savingAll = ref(false)

const loadSystemConfig = () => {
  http.get("/system/config/all/get").then((resp) => {
    if (resp.data && resp.data.data) {
      const data = resp.data.data
      systemConfig.value.aiProvider = data.aiProvider || 'ollama'
      systemConfig.value.aiBaseUrl = data.aiBaseUrl || ''
      systemConfig.value.aiModel = data.aiModel || ''
      systemConfig.value.aiApiKey = data.aiApiKey || ''
      systemConfig.value.aiTemperature = parseFloat(data.aiTemperature) || 0.7
      systemConfig.value.aiMaxTokens = parseInt(data.aiMaxTokens) || 0
      systemConfig.value.aiEnableThinking = data.aiEnableThinking === 'true'
      systemConfig.value.outterUser = data.outterUser || ''

      if (data.allowedIP && Array.isArray(data.allowedIP)) {
        systemConfig.value.allowedIP = data.allowedIP.join('\n')
      }
    }
  })
}

const saveAllConfig = () => {
  savingAll.value = true
  const ips = systemConfig.value.allowedIP.split('\n').map(ip => ip.trim()).filter(ip => ip !== '')

  http.post("/system/config/all/save", {
    aiProvider: systemConfig.value.aiProvider,
    aiBaseUrl: systemConfig.value.aiBaseUrl,
    aiModel: systemConfig.value.aiModel,
    aiApiKey: systemConfig.value.aiApiKey,
    aiTemperature: String(systemConfig.value.aiTemperature),
    aiMaxTokens: String(systemConfig.value.aiMaxTokens),
    aiEnableThinking: String(systemConfig.value.aiEnableThinking),
    outterUser: systemConfig.value.outterUser,
    allowedIP: ips
  }).then(() => {
    ElMessage.success("保存成功")
    emit('config-saved', systemConfig.value)
  }).finally(() => {
    savingAll.value = false
  })
}

const testAiConfig = () => {
  aiTesting.value = true
  http.post("/ai/config/test", {
    provider: systemConfig.value.aiProvider,
    baseUrl: systemConfig.value.aiBaseUrl,
    model: systemConfig.value.aiModel,
    apiKey: systemConfig.value.aiApiKey
  }).then(() => {
    ElMessage.success("连接成功")
  }).finally(() => {
    aiTesting.value = false
  })
}

const testOutterUser = () => {
  testingOutterUser.value = true
  http.post("/system/config/outterUser/test", { url: systemConfig.value.outterUser })
    .then((resp) => {
      if (resp.data.code === 200) {
        ElMessage.success("测试成功：" + JSON.stringify(resp.data.data))
      } else {
        ElMessage.error("测试失败：" + resp.data.msg)
      }
    })
    .catch(() => {
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
      ElMessage(data.msg)
    }
  }).catch((error) => {
    ElMessage.error(error.message || "注册失败")
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
  max-width: 800px;
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
