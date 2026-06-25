<template>
  <!-- 统一数据库监控面板：合并自 EnhancedMonitorPanel + ServerStatusPanel -->
  <el-dialog
    v-model="visible"
    title="数据库监控"
    width="960px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog db-monitor-dialog"
    aria-label="数据库监控对话框"
    @opened="onOpen"
    @close="onClose"
  >
    <el-tabs v-model="activeTab" type="card" @tab-change="onTabChange">
      <!-- 1. 概览：关键指标卡片 + 服务器基础信息 -->
      <el-tab-pane label="概览" name="overview">
        <div v-loading="overviewLoading" :aria-busy="overviewLoading" style="min-height: 200px;" role="region" aria-label="监控概览">
          <el-row :gutter="10" style="margin-bottom: 12px;">
            <el-col :span="8">
              <div class="stat-card" role="group" :aria-label="`连接数：${metrics?.connections ?? 0}`">
                <div class="stat-value">{{ metrics?.connections ?? 0 }}</div>
                <div class="stat-label">连接数（活跃 {{ metrics?.activeConnections ?? 0 }}）</div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="stat-card" role="group" :aria-label="`QPS：${(metrics?.qps ?? 0).toFixed(1)}`">
                <div class="stat-value">{{ (metrics?.qps ?? 0).toFixed(1) }}</div>
                <div class="stat-label">QPS</div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="stat-card" role="group" :aria-label="`TPS：${(metrics?.tps ?? 0).toFixed(1)}`">
                <div class="stat-value">{{ (metrics?.tps ?? 0).toFixed(1) }}</div>
                <div class="stat-label">TPS</div>
              </div>
            </el-col>
          </el-row>
          <el-row :gutter="10" style="margin-bottom: 12px;">
            <el-col :span="8">
              <div class="stat-card" role="group" :aria-label="`线程：${metrics?.threadsRunning ?? 0}`">
                <div class="stat-value">{{ metrics?.threadsRunning ?? 0 }} <span class="stat-sub">/ {{ metrics?.threadsConnected ?? 0 }}</span></div>
                <div class="stat-label">线程（运行 / 连接）</div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="stat-card" role="group" :aria-label="`慢查询：${metrics?.slowQueries ?? 0}`">
                <div class="stat-value" :style="{ color: (metrics?.slowQueries ?? 0) > 0 ? 'var(--db-danger)' : 'var(--db-success)' }">{{ metrics?.slowQueries ?? 0 }}</div>
                <div class="stat-label">慢查询</div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="stat-card" role="group" :aria-label="`锁等待：${metrics?.lockWaits ?? 0}`">
                <div class="stat-value" :style="{ color: (metrics?.lockWaits ?? 0) > 0 ? 'var(--db-danger)' : 'var(--db-success)' }">{{ metrics?.lockWaits ?? 0 }}</div>
                <div class="stat-label">锁等待</div>
              </div>
            </el-col>
          </el-row>

          <!-- Buffer Pool 命中率与使用情况 -->
          <div v-if="resources" class="buffer-section">
            <div class="buffer-row">
              <span class="buffer-label">Buffer Pool 命中率</span>
              <span class="buffer-value" :style="{ color: (resources.bufferPoolHitRate ?? 0) > 95 ? 'var(--db-success)' : 'var(--db-warning)' }" aria-live="polite">{{ (resources.bufferPoolHitRate ?? 0).toFixed(1) }}%</span>
            </div>
            <el-progress
              :percentage="resources.bufferPoolHitRate ?? 0"
              :stroke-width="10"
              :color="(resources.bufferPoolHitRate ?? 0) > 95 ? '#67c23a' : '#e6a23c'"
              role="progressbar"
              :aria-valuenow="Math.round(resources.bufferPoolHitRate ?? 0)"
              aria-valuemin="0"
              aria-valuemax="100"
              aria-label="Buffer Pool 命中率"
            />
            <div v-if="resources.bufferPoolSize" class="buffer-row" style="margin-top: 10px;">
              <span class="buffer-label">Buffer Pool 使用</span>
              <span class="buffer-value">{{ formatBytes(resources.bufferPoolUsed ?? 0) }} / {{ formatBytes(resources.bufferPoolSize ?? 0) }}</span>
            </div>
            <el-progress
              v-if="resources.bufferPoolSize"
              :percentage="resources.bufferPoolSize ? Math.round((resources.bufferPoolUsed ?? 0) / resources.bufferPoolSize * 100) : 0"
              :stroke-width="10"
              role="progressbar"
              :aria-valuenow="resources.bufferPoolSize ? Math.round((resources.bufferPoolUsed ?? 0) / resources.bufferPoolSize * 100) : 0"
              aria-valuemin="0"
              aria-valuemax="100"
              aria-label="Buffer Pool 使用率"
            />
          </div>

          <!-- 资源概览：数据/索引大小、表行数、InnoDB 行操作 -->
          <el-row v-if="resources" :gutter="10" style="margin: 12px 0;">
            <el-col :span="8">
              <div class="mini-stat">
                <div class="mini-label">数据大小</div>
                <div class="mini-value">{{ formatBytes(resources.dataSize) }}</div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="mini-stat">
                <div class="mini-label">索引大小</div>
                <div class="mini-value">{{ formatBytes(resources.indexSize) }}</div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="mini-stat">
                <div class="mini-label">表 / 行数</div>
                <div class="mini-value">{{ resources.tableCount ?? 0 }} <span class="stat-sub">/ {{ formatNum(resources.totalRows) }} 行</span></div>
              </div>
            </el-col>
          </el-row>
          <el-row v-if="resources" :gutter="10" style="margin-bottom: 12px;">
            <el-col :span="8">
              <div class="mini-stat">
                <div class="mini-label">InnoDB 读</div>
                <div class="mini-value">{{ formatNum(resources.innodbRowsRead ?? 0) }}</div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="mini-stat">
                <div class="mini-label">InnoDB 插入</div>
                <div class="mini-value">{{ formatNum(resources.innodbRowsInserted ?? 0) }}</div>
              </div>
            </el-col>
            <el-col :span="8">
              <div class="mini-stat">
                <div class="mini-label">InnoDB 更新</div>
                <div class="mini-value">{{ formatNum(resources.innodbRowsUpdated ?? 0) }}</div>
              </div>
            </el-col>
          </el-row>

          <!-- 服务器基础信息（来自 SHOW STATUS / VARIABLES） -->
          <el-descriptions v-if="Object.keys(serverInfo).length > 0" :column="2" border size="small" aria-label="服务器基础信息">
            <el-descriptions-item v-for="(val, key) in serverInfo" :key="key" :label="key">{{ val }}</el-descriptions-item>
          </el-descriptions>

          <div class="overview-toolbar">
            <el-button type="primary" size="small" @click="refreshOverview" :loading="overviewLoading" aria-label="刷新监控数据">刷新</el-button>
            <el-button size="small" :type="overviewAutoRefresh ? 'success' : ''" :aria-pressed="overviewAutoRefresh" :aria-label="overviewAutoRefresh ? '停止自动刷新' : '每 5 秒自动刷新'" @click="toggleOverviewAutoRefresh">
              {{ overviewAutoRefresh ? '停止自动' : '自动刷新' }}
            </el-button>
            <span v-if="metrics" class="update-time" aria-live="polite">更新于 {{ metrics.timestamp }}</span>
          </div>
        </div>
      </el-tab-pane>

      <!-- 2. 会话与进程：合并进程列表，支持搜索与 Kill -->
      <el-tab-pane label="会话与进程" name="sessions">
        <div v-loading="sessionLoading" :aria-busy="sessionLoading" style="min-height: 200px;" role="region" aria-label="会话与进程列表">
          <div class="session-toolbar">
            <el-input
              v-model="sessionFilter"
              placeholder="按 user / host / state / db 过滤"
              size="small"
              clearable
              style="width: 280px;"
              aria-label="过滤会话列表"
            />
            <span class="session-count" aria-live="polite">共 {{ filteredSessions.length }} / {{ sessionList.length }} 个连接</span>
            <div style="flex: 1;"></div>
            <el-select v-model="sessionInterval" size="small" style="width: 110px;" aria-label="自动刷新间隔" @change="onSessionIntervalChange">
              <el-option label="不自动" :value="0" />
              <el-option label="每 5 秒" :value="5000" />
              <el-option label="每 10 秒" :value="10000" />
              <el-option label="每 30 秒" :value="30000" />
            </el-select>
            <el-button size="small" @click="loadSessions" :loading="sessionLoading" aria-label="刷新会话列表">刷新</el-button>
          </div>
          <el-table :data="filteredSessions" max-height="420" size="small" stripe border aria-label="数据库会话与进程列表">
            <el-table-column prop="id" label="ID" width="70" resizable />
            <el-table-column prop="user" label="用户" width="110" resizable show-overflow-tooltip />
            <el-table-column prop="host" label="来源" width="180" resizable show-overflow-tooltip />
            <el-table-column prop="db" label="数据库" width="120" resizable show-overflow-tooltip>
              <template #default="scope">{{ scope.row.db || '-' }}</template>
            </el-table-column>
            <el-table-column prop="command" label="命令" width="90" resizable>
              <template #default="scope">
                <el-tag size="small" :type="scope.row.command === 'Sleep' ? 'info' : 'warning'">{{ scope.row.command }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="time" label="时间(s)" width="80" resizable />
            <el-table-column prop="state" label="状态" min-width="150" show-overflow-tooltip resizable />
            <el-table-column prop="info" label="SQL" min-width="180" show-overflow-tooltip resizable />
            <el-table-column label="操作" width="80" fixed="right" resizable>
              <template #default="scope">
                <el-button size="small" link type="danger" :aria-label="`终止连接 ${scope.row.id}`" @click="confirmKill(scope.row)">Kill</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!sessionLoading && filteredSessions.length === 0" description="没有符合条件的连接" :image-size="60" />
        </div>
      </el-tab-pane>

      <!-- 3. 性能趋势：实时采样与 ECharts 趋势展示 -->
      <el-tab-pane label="性能趋势" name="performance">
        <div style="min-height: 200px;" role="region" aria-label="性能趋势">
          <div class="perf-toolbar">
            <!-- 模式切换：实时 / 历史 -->
            <el-radio-group v-model="trendMode" size="small" @change="onTrendModeChange">
              <el-radio-button label="realtime">实时</el-radio-button>
              <el-radio-button label="history">历史趋势</el-radio-button>
            </el-radio-group>
            <div style="flex: 1;"></div>
            <!-- 实时模式控件 -->
            <template v-if="trendMode === 'realtime'">
              <span class="perf-tip">采样间隔 5 秒，保留最近 {{ TREND_MAX }} 个样本</span>
              <el-button size="small" :type="trendPaused ? 'success' : 'warning'" :aria-pressed="trendPaused" :aria-label="trendPaused ? '继续刷新' : '暂停刷新'" @click="toggleTrendPause">
                {{ trendPaused ? '继续' : '暂停' }}
              </el-button>
              <el-button size="small" @click="clearTrend" aria-label="清空趋势历史">清空</el-button>
            </template>
            <!-- 历史模式控件 -->
            <template v-else>
              <el-select v-model="historyMetric" size="small" style="width: 140px;" aria-label="选择指标" @change="loadHistory">
                <el-option v-for="m in HISTORY_METRICS" :key="m.key" :label="m.label" :value="m.key" />
              </el-select>
              <el-select v-model="historyRange" size="small" style="width: 150px;" aria-label="选择时间范围" @change="loadHistory">
                <el-option label="最近 1 小时" value="1h" />
                <el-option label="最近 24 小时" value="24h" />
                <el-option label="最近 7 天" value="7d" />
                <el-option label="最近 30 天" value="30d" />
              </el-select>
              <el-button size="small" @click="loadHistory" :loading="historyLoading" aria-label="刷新历史数据">刷新</el-button>
            </template>
          </div>

          <!-- 实时模式内容 -->
          <template v-if="trendMode === 'realtime'">
            <el-empty v-if="trendHistory.length === 0" description="等待采样数据..." :image-size="60" />
            <template v-else>
              <!-- 主趋势图：QPS / TPS / 连接数 / 缓冲池命中率 多线合并 -->
              <EChart :option="trendChartOption" height="320px" />
              <!-- 指标统计卡片：展示当前值与最值/均值 -->
              <el-row :gutter="12" style="margin-top: 12px;">
                <el-col v-for="metric in trendMetrics" :key="metric.key" :span="6" style="margin-bottom: 12px;">
                  <div class="trend-card">
                    <div class="trend-header">
                      <span class="trend-title">{{ metric.label }}</span>
                      <span class="trend-current" :style="{ color: metric.color }">{{ metric.display(metric.latest) }}</span>
                    </div>
                    <div class="trend-stats">
                      <span>最小 {{ metric.display(metric.min) }}</span>
                      <span>最大 {{ metric.display(metric.max) }}</span>
                      <span>平均 {{ metric.display(metric.avg) }}</span>
                    </div>
                  </div>
                </el-col>
              </el-row>
            </template>
          </template>

          <!-- 历史模式内容 -->
          <template v-else>
            <el-empty v-if="!historyLoading && historyPoints.length === 0" description="暂无历史数据" :image-size="60" />
            <EChart v-else :option="historyChartOption" height="320px" />
          </template>
        </div>
      </el-tab-pane>

      <!-- 4. 服务器变量：全局 / 会话变量，可搜索 -->
      <el-tab-pane label="服务器变量" name="variables">
        <div v-loading="varsLoading" :aria-busy="varsLoading" style="min-height: 200px;" role="region" aria-label="服务器变量">
          <div class="vars-toolbar">
            <el-radio-group v-model="varsScope" size="small" @change="onVarsScopeChange">
              <el-radio-button label="global">全局变量</el-radio-button>
              <el-radio-button label="session">会话变量</el-radio-button>
            </el-radio-group>
            <el-input v-model="varsFilter" placeholder="按变量名或值过滤" size="small" clearable style="width: 260px;" aria-label="过滤变量列表" />
            <el-button size="small" @click="loadVariables" :loading="varsLoading" aria-label="刷新变量列表">刷新</el-button>
          </div>
          <el-table :data="filteredVariables" max-height="440" size="small" stripe border aria-label="服务器变量列表">
            <el-table-column prop="name" label="变量名" min-width="220" resizable show-overflow-tooltip />
            <el-table-column prop="value" label="值" min-width="220" resizable show-overflow-tooltip />
          </el-table>
          <el-empty v-if="!varsLoading && filteredVariables.length === 0" description="没有符合条件的变量" :image-size="60" />
        </div>
      </el-tab-pane>

      <!-- 5. 状态指标：状态计数器，可重置 -->
      <el-tab-pane label="状态指标" name="status">
        <div v-loading="statusLoading" :aria-busy="statusLoading" style="min-height: 200px;" role="region" aria-label="状态指标">
          <div class="vars-toolbar">
            <el-input v-model="statusFilter" placeholder="按状态名或值过滤" size="small" clearable style="width: 260px;" aria-label="过滤状态列表" />
            <el-button size="small" @click="loadStatus" :loading="statusLoading" aria-label="刷新状态列表">刷新</el-button>
            <el-button size="small" type="warning" @click="confirmFlushStatus" aria-label="重置状态计数器">重置状态计数器</el-button>
          </div>
          <el-table :data="filteredStatus" max-height="440" size="small" stripe border aria-label="状态计数器列表">
            <el-table-column prop="name" label="状态名" min-width="240" resizable show-overflow-tooltip />
            <el-table-column prop="value" label="值" min-width="180" resizable show-overflow-tooltip />
          </el-table>
          <el-empty v-if="!statusLoading && filteredStatus.length === 0" description="没有符合条件的状态" :image-size="60" />
        </div>
      </el-tab-pane>

      <!-- 6. InnoDB 引擎状态（仅 MySQL/MariaDB 支持） -->
      <el-tab-pane label="InnoDB状态" name="innodb">
        <div v-loading="innodbLoading" :aria-busy="innodbLoading" style="min-height: 200px;" role="region" aria-label="InnoDB 引擎状态">
          <div class="vars-toolbar">
            <el-button size="small" @click="loadInnodb" :loading="innodbLoading" aria-label="刷新 InnoDB 状态">刷新</el-button>
          </div>
          <el-empty v-if="!innodbLoading && !innodbSupported" description="当前数据库不支持 InnoDB 状态查看" :image-size="60" />
          <pre v-else-if="innodbStatus" class="innodb-status-text">{{ innodbStatus }}</pre>
          <el-empty v-else description="暂无 InnoDB 状态数据（可能缺少 PROCESS 权限）" :image-size="60" />
        </div>
      </el-tab-pane>

      <!-- 7. 锁与事务等待 -->
      <el-tab-pane label="锁与等待" name="locks">
        <div v-loading="locksLoading" :aria-busy="locksLoading" style="min-height: 200px;" role="region" aria-label="锁与事务等待">
          <div class="vars-toolbar">
            <el-button size="small" @click="loadLocks" :loading="locksLoading" aria-label="刷新锁等待列表">刷新</el-button>
          </div>
          <el-table :data="locksList" max-height="440" size="small" stripe border aria-label="锁与事务等待列表">
            <el-table-column prop="waitingId" label="等待事务/会话" width="140" resizable show-overflow-tooltip />
            <el-table-column prop="blockingId" label="阻塞会话" width="120" resizable show-overflow-tooltip />
            <el-table-column prop="lockType" label="锁类型/事件" min-width="160" resizable show-overflow-tooltip />
            <el-table-column prop="waitSeconds" label="等待(秒)" width="100" resizable />
            <el-table-column prop="tableName" label="表名" width="140" resizable show-overflow-tooltip />
            <el-table-column prop="query" label="SQL" min-width="180" resizable show-overflow-tooltip />
          </el-table>
          <el-empty v-if="!locksLoading && locksList.length === 0" description="当前无锁等待" :image-size="60" />
        </div>
      </el-tab-pane>

      <!-- 8. 慢查询分析 -->
      <el-tab-pane label="慢查询分析" name="slow">
        <div v-loading="slowLoading" :aria-busy="slowLoading" style="min-height: 200px;" role="region" aria-label="慢查询分析">
          <div class="vars-toolbar">
            <el-button size="small" @click="loadSlow" :loading="slowLoading" aria-label="刷新慢查询列表">刷新</el-button>
          </div>
          <el-table :data="slowList" max-height="440" size="small" stripe border aria-label="慢查询列表">
            <el-table-column prop="digestText" label="SQL 摘要" min-width="280" resizable show-overflow-tooltip />
            <el-table-column prop="avgMs" label="平均耗时(ms)" width="130" resizable>
              <template #default="scope">{{ scope.row.avgMs != null ? scope.row.avgMs.toFixed(2) : '-' }}</template>
            </el-table-column>
            <el-table-column prop="execCount" label="执行次数" width="110" resizable />
            <el-table-column prop="rowsExamined" label="扫描行数" width="110" resizable />
            <el-table-column prop="lastSeen" label="最后出现" width="160" resizable show-overflow-tooltip />
          </el-table>
          <el-empty v-if="!slowLoading && slowList.length === 0" description="暂无慢查询数据（可能未启用 performance_schema）" :image-size="60" />
        </div>
      </el-tab-pane>

      <!-- 9. 表统计 TOP N -->
      <el-tab-pane label="表统计" name="topTables">
        <div v-loading="topTablesLoading" :aria-busy="topTablesLoading" style="min-height: 200px;" role="region" aria-label="表统计 TOP N">
          <div class="vars-toolbar">
            <el-button size="small" @click="loadTopTables" :loading="topTablesLoading" aria-label="刷新表统计">刷新</el-button>
          </div>
          <el-table :data="topTablesList" max-height="440" size="small" stripe border aria-label="表统计列表">
            <el-table-column prop="tableName" label="表名" min-width="180" resizable show-overflow-tooltip />
            <el-table-column prop="engine" label="引擎" width="90" resizable />
            <el-table-column prop="tableRows" label="行数" width="110" resizable>
              <template #default="scope">{{ formatNum(scope.row.tableRows) }}</template>
            </el-table-column>
            <el-table-column prop="dataSize" label="数据大小" width="110" resizable>
              <template #default="scope">{{ formatBytes(scope.row.dataSize) }}</template>
            </el-table-column>
            <el-table-column prop="indexSize" label="索引大小" width="110" resizable>
              <template #default="scope">{{ formatBytes(scope.row.indexSize) }}</template>
            </el-table-column>
            <el-table-column prop="dataFree" label="碎片空间" width="110" resizable>
              <template #default="scope">{{ formatBytes(scope.row.dataFree) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!topTablesLoading && topTablesList.length === 0" description="暂无表统计数据" :image-size="60" />
        </div>
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { execSQL } from '@/api/sql'
import { getMonitorMetrics, getMonitorResources, getMonitorHistory, getInnodbStatus, getLocks, getSlowQueries, getTopTables } from '@/api/conn'
import { handleError } from '@/utils/errorHandler'
import EChart from '@/components/common/EChart.vue'

// 双向绑定可见性
const visible = defineModel({ default: false })
const { connId, schema, initialTab } = defineProps({
  connId: String,
  schema: String,
  // 打开时聚焦的 Tab：overview / sessions / performance / variables / status
  initialTab: { type: String, default: 'overview' },
})

// 趋势样本上限
const TREND_MAX = 30

// ============ 通用状态 ============
const activeTab = ref('overview')

// 概览 Tab 数据
const metrics = ref(null)
const resources = ref(null)
const serverInfo = ref({})
const overviewLoading = ref(false)
const overviewAutoRefresh = ref(false)
let overviewTimer = null

// 会话 Tab 数据
const sessionList = ref([])
const sessionFilter = ref('')
const sessionLoading = ref(false)
const sessionInterval = ref(0) // 0 表示不自动
let sessionTimer = null

// 性能趋势 Tab 数据
const trendHistory = ref([]) // 每个元素：{ qps, tps, connections, bufferHitRate, ts }
const trendPaused = ref(false)
let trendTimer = null

// 性能趋势 - 历史模式数据
const trendMode = ref('realtime') // realtime（实时）/ history（历史趋势）
const historyMetric = ref('qps') // 当前查询的指标 key
const historyRange = ref('1h') // 时间范围：1h / 24h / 7d / 30d
const historyPoints = ref([]) // 历史数据点：[{ timestamp, value }]
const historyLoading = ref(false)

// 服务器变量 Tab 数据
const varsScope = ref('global')
const varsList = ref([])
const varsFilter = ref('')
const varsLoading = ref(false)

// 状态指标 Tab 数据
const statusList = ref([])
const statusFilter = ref('')
const statusLoading = ref(false)

// InnoDB 状态 Tab 数据
const innodbStatus = ref('')
const innodbLoading = ref(false)
const innodbSupported = ref(false)
const innodbLoaded = ref(false)

// 锁与等待 Tab 数据
const locksList = ref([])
const locksLoading = ref(false)
const locksLoaded = ref(false)

// 慢查询分析 Tab 数据
const slowList = ref([])
const slowLoading = ref(false)
const slowLoaded = ref(false)

// 表统计 Tab 数据
const topTablesList = ref([])
const topTablesLoading = ref(false)
const topTablesLoaded = ref(false)

// ============ 工具函数 ============
function formatBytes(val) {
  if (!val || val === 0) return '0 B'
  val = Number(val)
  if (val < 1024) return val + ' B'
  if (val < 1048576) return (val / 1024).toFixed(1) + ' KB'
  if (val < 1073741824) return (val / 1048576).toFixed(2) + ' MB'
  return (val / 1073741824).toFixed(2) + ' GB'
}

function formatNum(val) {
  if (!val) return '0'
  val = Number(val)
  if (val >= 1000000) return (val / 1000000).toFixed(1) + 'M'
  if (val >= 1000) return (val / 1000).toFixed(1) + 'K'
  return val.toString()
}

function formatUptime(seconds) {
  if (!seconds || seconds <= 0) return '-'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const parts = []
  if (d > 0) parts.push(d + '天')
  if (h > 0) parts.push(h + '时')
  parts.push(m + '分')
  return parts.join(' ')
}

// 通过 execSQL 执行查询，返回数据行数组
async function execQuery(sql) {
  const resp = await execSQL({ connId, schema, sql, maxLine: '500' })
  return resp.data.data?.data || []
}

// ============ 概览 Tab ============
async function loadMetrics() {
  if (!connId) return
  try {
    const res = await getMonitorMetrics(connId)
    // 后端 response.WriteOK 包成 {code,msg,data}，前端拦截器返回完整 response，
    // 故真实快照在 res.data.data；之前取 res.data 拿到外壳导致指标卡片为 0、"更新于"为空
    metrics.value = res.data?.data
  } catch (e) { handleError(e, '加载监控指标') }
}

async function loadResources() {
  if (!connId) return
  try {
    const res = await getMonitorResources(connId, schema)
    resources.value = res.data?.data?.dbResources
  } catch (e) { handleError(e, '加载资源监控') }
}

// 加载服务器基础信息（运行时间、版本、字符集等），来自 SHOW STATUS / VARIABLES
async function loadServerInfo() {
  if (!connId) return
  try {
    const [statusRows, varsRows] = await Promise.all([
      execQuery('SHOW STATUS'),
      execQuery('SHOW VARIABLES'),
    ])
    const statusMap = {}
    const varsMap = {}
    statusRows.forEach(r => { statusMap[r.Variable_name || r.variable_name] = r.Value || r.value })
    varsRows.forEach(r => { varsMap[r.Variable_name || r.variable_name] = r.Value || r.value })

    const uptime = parseInt(statusMap['Uptime'] || '0')
    serverInfo.value = {
      '运行时间': formatUptime(uptime),
      '数据库版本': varsMap['version'] || '-',
      '数据目录': varsMap['datadir'] || '-',
      '字符集': varsMap['character_set_server'] || '-',
      '连接数(活跃/上限)': (statusMap['Threads_connected'] || '?') + ' / ' + (varsMap['max_connections'] || '?'),
      '慢查询数': statusMap['Slow_queries'] || '0',
    }
  } catch (e) { handleError(e, '加载服务器信息') }
}

async function refreshOverview() {
  overviewLoading.value = true
  await Promise.all([loadMetrics(), loadResources(), loadServerInfo()])
  overviewLoading.value = false
}

function toggleOverviewAutoRefresh() {
  overviewAutoRefresh.value = !overviewAutoRefresh.value
}

function startOverviewAutoRefresh() {
  stopOverviewAutoRefresh()
  overviewTimer = setInterval(refreshOverview, 5000)
}

function stopOverviewAutoRefresh() {
  if (overviewTimer) { clearInterval(overviewTimer); overviewTimer = null }
}

watch(overviewAutoRefresh, (val) => {
  if (val) startOverviewAutoRefresh()
  else stopOverviewAutoRefresh()
})

// ============ 会话与进程 Tab ============
// 使用 SHOW FULL PROCESSLIST，获取包含 info 的完整字段
async function loadSessions() {
  if (!connId) return
  sessionLoading.value = true
  try {
    const rows = await execQuery('SHOW FULL PROCESSLIST')
    sessionList.value = rows.map(p => ({
      id: p.Id ?? p.id,
      user: p.User || p.user || '',
      host: p.Host || p.host || '',
      db: p.db || p.database || '',
      command: p.Command || p.command || '',
      time: p.Time ?? p.time ?? 0,
      state: p.State || p.state || '',
      info: p.Info || p.info || '',
    }))
  } catch (e) { handleError(e, '加载会话列表') } finally { sessionLoading.value = false }
}

const filteredSessions = computed(() => {
  const kw = sessionFilter.value.trim().toLowerCase()
  if (!kw) return sessionList.value
  return sessionList.value.filter(s =>
    String(s.user || '').toLowerCase().includes(kw) ||
    String(s.host || '').toLowerCase().includes(kw) ||
    String(s.state || '').toLowerCase().includes(kw) ||
    String(s.db || '').toLowerCase().includes(kw)
  )
})

function onSessionIntervalChange(val) {
  stopSessionAutoRefresh()
  if (val > 0) {
    sessionTimer = setInterval(loadSessions, val)
  }
}

function stopSessionAutoRefresh() {
  if (sessionTimer) { clearInterval(sessionTimer); sessionTimer = null }
}

// Kill 连接：弹确认对话框，确认后通过 execSQL 执行 KILL <id>
function confirmKill(row) {
  ElMessageBox.confirm(
    `确定要终止连接 ID ${row.id}（用户 ${row.user}）吗？该操作会强制中断对应会话。`,
    '终止连接确认',
    { type: 'warning', confirmButtonText: '终止', cancelButtonText: '取消' }
  ).then(() => doKill(row)).catch((e) => {
    if (e !== 'cancel' && e !== 'close') handleError(e, '终止连接')
  })
}

async function doKill(row) {
  try {
    await execSQL({ connId, schema, sql: `KILL ${row.id}`, maxLine: '1' })
    ElMessage.success(`已终止连接 ${row.id}`)
    await loadSessions()
  } catch (e) { handleError(e, '终止连接') }
}

// ============ 性能趋势 Tab ============
// 趋势指标定义：颜色 + 数值格式化函数
const TREND_METRIC_DEFS = [
  { key: 'qps', label: 'QPS', color: '#409eff', display: (v) => Number(v).toFixed(1) },
  { key: 'tps', label: 'TPS', color: '#67c23a', display: (v) => Number(v).toFixed(1) },
  { key: 'connections', label: '连接数', color: '#e6a23c', display: (v) => String(Math.round(v)) },
  { key: 'bufferHitRate', label: '缓冲池命中率', color: '#9b59b6', display: (v) => Number(v).toFixed(1) + '%' },
]

// 各指标当前值与最值/均值统计（不再构造 SVG 点，趋势由 ECharts 渲染）
const trendMetrics = computed(() => {
  const hist = trendHistory.value
  if (hist.length === 0) return []
  return TREND_METRIC_DEFS.map(def => {
    const values = hist.map(h => Number(h[def.key]) || 0)
    const latest = values[values.length - 1]
    const min = Math.min(...values)
    const max = Math.max(...values)
    const avg = values.reduce((a, b) => a + b, 0) / values.length
    return { ...def, latest, min, max, avg }
  })
})

// 格式化时间戳为 HH:mm:ss，用于 X 轴展示
function formatTrendTime(ts) {
  const d = new Date(ts)
  const pad = (n) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// ECharts 主趋势图配置：QPS / TPS / 连接数 共用左 Y 轴，缓冲池命中率使用右 Y 轴（百分比）
const trendChartOption = computed(() => {
  const hist = trendHistory.value
  const xData = hist.map(h => formatTrendTime(h.ts))
  const buildSeries = (def, yAxisIndex = 0) => ({
    name: def.label,
    type: 'line',
    yAxisIndex,
    smooth: true,
    showSymbol: false,
    areaStyle: { opacity: 0.15 },
    lineStyle: { width: 2 },
    itemStyle: { color: def.color },
    data: hist.map(h => Number(h[def.key]) || 0),
  })
  return {
    tooltip: {
      trigger: 'axis',
      // 自定义 tooltip：显示时间 + 各指标值（含单位）
      formatter: (params) => {
        if (!params || params.length === 0) return ''
        const time = params[0].axisValue
        const lines = params.map(p => {
          const def = TREND_METRIC_DEFS.find(d => d.label === p.seriesName)
          const val = def ? def.display(p.value) : p.value
          return `${p.marker}${p.seriesName}: ${val}`
        })
        return [time, ...lines].join('<br/>')
      },
    },
    legend: {
      data: TREND_METRIC_DEFS.map(d => d.label),
      top: 0,
    },
    grid: { left: 50, right: 60, top: 40, bottom: 30 },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: xData,
      axisLabel: { fontSize: 10 },
    },
    yAxis: [
      {
        type: 'value',
        name: 'QPS/TPS/连接',
        axisLabel: { fontSize: 10 },
        scale: true,
      },
      {
        type: 'value',
        name: '命中率(%)',
        min: 0,
        max: 100,
        axisLabel: { fontSize: 10, formatter: '{value}%' },
      },
    ],
    series: [
      buildSeries(TREND_METRIC_DEFS[0], 0), // QPS
      buildSeries(TREND_METRIC_DEFS[1], 0), // TPS
      buildSeries(TREND_METRIC_DEFS[2], 0), // 连接数
      buildSeries(TREND_METRIC_DEFS[3], 1), // 缓冲池命中率（右轴）
    ],
  }
})

async function sampleTrend() {
  if (!connId) return
  try {
    const res = await getMonitorMetrics(connId)
    // 真实快照在 res.data.data（见 loadMetrics 注释）
    const m = res.data?.data
    const hitRate = resources.value?.bufferPoolHitRate ?? 0
    const sample = {
      qps: m?.qps ?? 0,
      tps: m?.tps ?? 0,
      connections: m?.connections ?? 0,
      bufferHitRate: hitRate,
      ts: Date.now(),
    }
    trendHistory.value.push(sample)
    if (trendHistory.value.length > TREND_MAX) {
      trendHistory.value.shift()
    }
    // 同步更新 metrics，便于概览 Tab 复用
    metrics.value = m
  } catch (e) { handleError(e, '采样性能趋势') }
}

function startTrendAutoRefresh() {
  stopTrendAutoRefresh()
  trendTimer = setInterval(sampleTrend, 5000)
}

function stopTrendAutoRefresh() {
  if (trendTimer) { clearInterval(trendTimer); trendTimer = null }
}

function toggleTrendPause() {
  trendPaused.value = !trendPaused.value
  if (trendPaused.value) stopTrendAutoRefresh()
  else startTrendAutoRefresh()
}

function clearTrend() {
  trendHistory.value = []
}

// ============ 性能趋势 - 历史模式 ============
// 历史指标定义：key（前端）→ metric（后端 metric_name）+ 显示配置
const HISTORY_METRICS = [
  { key: 'qps', metric: 'qps', label: 'QPS', color: '#409eff', display: (v) => Number(v).toFixed(1) },
  { key: 'tps', metric: 'tps', label: 'TPS', color: '#67c23a', display: (v) => Number(v).toFixed(1) },
  { key: 'connections', metric: 'connections', label: '连接数', color: '#e6a23c', display: (v) => String(Math.round(v)) },
  { key: 'buffer_pool_hit_rate', metric: 'buffer_pool_hit_rate', label: '缓冲池命中率', color: '#9b59b6', display: (v) => Number(v).toFixed(1) + '%' },
  { key: 'slow_queries', metric: 'slow_queries', label: '慢查询', color: '#f56c6c', display: (v) => String(Math.round(v)) },
  { key: 'lock_waits', metric: 'lock_waits', label: '锁等待', color: '#e6a23c', display: (v) => String(Math.round(v)) },
]

// 时间范围配置：value → { interval, durationMs }
const HISTORY_RANGE_CONFIG = {
  '1h': { interval: 'raw', durationMs: 60 * 60 * 1000 },
  '24h': { interval: '5min', durationMs: 24 * 60 * 60 * 1000 },
  '7d': { interval: '1hour', durationMs: 7 * 24 * 60 * 60 * 1000 },
  '30d': { interval: '1hour', durationMs: 30 * 24 * 60 * 60 * 1000 },
}

// 格式化为后端可解析的时间字符串 "YYYY-MM-DD HH:mm:ss"
function formatDateTime(date) {
  const pad = (n) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

// 根据时间范围格式化 X 轴时间标签
function formatHistoryTime(ts, range) {
  const d = new Date(ts.replace(/-/g, '/'))
  const pad = (n) => String(n).padStart(2, '0')
  // 1h 显示 HH:mm:ss，24h 显示 HH:mm，7d/30d 显示 MM-DD HH:mm
  if (range === '1h') return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  if (range === '24h') return `${pad(d.getHours())}:${pad(d.getMinutes())}`
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 加载历史趋势数据：调用 /monitor/history API
async function loadHistory() {
  if (!connId) return
  const cfg = HISTORY_RANGE_CONFIG[historyRange.value]
  if (!cfg) return
  const metricDef = HISTORY_METRICS.find(m => m.key === historyMetric.value)
  if (!metricDef) return

  historyLoading.value = true
  try {
    const now = new Date()
    const from = new Date(now.getTime() - cfg.durationMs)
    const res = await getMonitorHistory(connId, metricDef.metric, formatDateTime(from), formatDateTime(now), cfg.interval)
    historyPoints.value = res.data?.data?.points || res.data?.points || []
  } catch (e) {
    handleError(e, '加载历史趋势')
    historyPoints.value = []
  } finally {
    historyLoading.value = false
  }
}

// 历史趋势 ECharts 配置：单指标折线图
const historyChartOption = computed(() => {
  const points = historyPoints.value
  const metricDef = HISTORY_METRICS.find(m => m.key === historyMetric.value)
  if (!metricDef || points.length === 0) return {}

  const xData = points.map(p => formatHistoryTime(p.timestamp, historyRange.value))
  const values = points.map(p => Number(p.value) || 0)
  // 计算最值/均值用于 tooltip 展示
  const min = Math.min(...values)
  const max = Math.max(...values)
  const avg = values.reduce((a, b) => a + b, 0) / values.length

  return {
    title: {
      text: `${metricDef.label}（最小 ${metricDef.display(min)} / 最大 ${metricDef.display(max)} / 平均 ${metricDef.display(avg)}）`,
      textStyle: { fontSize: 12, fontWeight: 'normal' },
      left: 'center',
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params) => {
        if (!params || params.length === 0) return ''
        return `${params[0].axisValue}<br/>${params[0].marker}${metricDef.label}: ${metricDef.display(params[0].value)}`
      },
    },
    grid: { left: 60, right: 30, top: 50, bottom: 40 },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: xData,
      axisLabel: { fontSize: 10 },
    },
    yAxis: {
      type: 'value',
      name: metricDef.label,
      axisLabel: { fontSize: 10 },
      scale: true,
    },
    series: [{
      name: metricDef.label,
      type: 'line',
      smooth: true,
      showSymbol: false,
      areaStyle: { opacity: 0.15 },
      lineStyle: { width: 2 },
      itemStyle: { color: metricDef.color },
      data: values,
    }],
  }
})

// 切换实时/历史模式
function onTrendModeChange(mode) {
  if (mode === 'realtime') {
    // 切回实时模式：恢复自动刷新
    stopTrendAutoRefresh()
    if (!trendPaused.value) startTrendAutoRefresh()
  } else {
    // 切到历史模式：停止实时采样并加载历史数据
    stopTrendAutoRefresh()
    loadHistory()
  }
}

// ============ 服务器变量 Tab ============
async function loadVariables() {
  if (!connId) return
  varsLoading.value = true
  try {
    const sql = varsScope.value === 'global' ? 'SHOW GLOBAL VARIABLES' : 'SHOW SESSION VARIABLES'
    const rows = await execQuery(sql)
    varsList.value = rows.map(r => ({
      name: r.Variable_name || r.variable_name || '',
      value: r.Value ?? r.value ?? '',
    }))
  } catch (e) { handleError(e, '加载服务器变量') } finally { varsLoading.value = false }
}

function onVarsScopeChange() {
  loadVariables()
}

const filteredVariables = computed(() => {
  const kw = varsFilter.value.trim().toLowerCase()
  if (!kw) return varsList.value
  return varsList.value.filter(v =>
    String(v.name).toLowerCase().includes(kw) ||
    String(v.value).toLowerCase().includes(kw)
  )
})

// ============ 状态指标 Tab ============
async function loadStatus() {
  if (!connId) return
  statusLoading.value = true
  try {
    const rows = await execQuery('SHOW STATUS')
    statusList.value = rows.map(r => ({
      name: r.Variable_name || r.variable_name || '',
      value: r.Value ?? r.value ?? '',
    }))
  } catch (e) { handleError(e, '加载状态指标') } finally { statusLoading.value = false }
}

const filteredStatus = computed(() => {
  const kw = statusFilter.value.trim().toLowerCase()
  if (!kw) return statusList.value
  return statusList.value.filter(s =>
    String(s.name).toLowerCase().includes(kw) ||
    String(s.value).toLowerCase().includes(kw)
  )
})

// 重置状态计数器：FLUSH STATUS，需用户确认
function confirmFlushStatus() {
  ElMessageBox.confirm(
    '确定要执行 FLUSH STATUS 重置大部分状态计数器吗？该操作会将会话级状态计数器清零。',
    '重置状态确认',
    { type: 'warning', confirmButtonText: '重置', cancelButtonText: '取消' }
  ).then(() => doFlushStatus()).catch((e) => {
    if (e !== 'cancel' && e !== 'close') handleError(e, '重置状态')
  })
}

async function doFlushStatus() {
  try {
    await execSQL({ connId, schema, sql: 'FLUSH STATUS', maxLine: '1' })
    ElMessage.success('状态计数器已重置')
    await loadStatus()
  } catch (e) { handleError(e, '重置状态') }
}

// ============ InnoDB 状态 / 锁 / 慢查询 / 表统计 Tab ============
// InnoDB 引擎状态（仅 MySQL/MariaDB 支持，其他库后端返回 supported:false）
async function loadInnodb() {
  if (!connId) return
  innodbLoading.value = true
  try {
    const res = await getInnodbStatus(connId)
    const data = res.data?.data || {}
    innodbSupported.value = !!data.supported
    innodbStatus.value = data.status || ''
  } catch (e) { handleError(e, 'InnoDB 状态') } finally { innodbLoading.value = false; innodbLoaded.value = true }
}

// 锁与事务等待
async function loadLocks() {
  if (!connId) return
  locksLoading.value = true
  try {
    const res = await getLocks(connId)
    // 后端返回 { locks: [...], count, supported }，取 locks 数组
    locksList.value = res.data?.data?.locks || []
  } catch (e) { handleError(e, '锁与等待') } finally { locksLoading.value = false; locksLoaded.value = true }
}

// 慢查询分析
async function loadSlow() {
  if (!connId) return
  slowLoading.value = true
  try {
    const res = await getSlowQueries(connId, 20)
    // 后端返回 { queries: [...], count, supported }，取 queries 数组
    slowList.value = res.data?.data?.queries || []
  } catch (e) { handleError(e, '慢查询分析') } finally { slowLoading.value = false; slowLoaded.value = true }
}

// 表统计 TOP N（依赖当前 schema）
async function loadTopTables() {
  if (!connId) return
  topTablesLoading.value = true
  try {
    const res = await getTopTables(connId, schema || '', 20)
    // 后端返回 { tables: [...], count, supported }，取 tables 数组
    topTablesList.value = res.data?.data?.tables || []
  } catch (e) { handleError(e, '表统计') } finally { topTablesLoading.value = false; topTablesLoaded.value = true }
}

// ============ Tab 切换与生命周期 ============
function onTabChange(name) {
  // 切换到对应 Tab 时按需加载数据
  if (name === 'overview' && !metrics.value) refreshOverview()
  else if (name === 'sessions' && sessionList.value.length === 0) loadSessions()
  else if (name === 'performance') {
    // 实时模式启动采样，历史模式加载数据
    if (trendMode.value === 'realtime') {
      if (!trendPaused.value && !trendTimer) startTrendAutoRefresh()
    } else {
      loadHistory()
    }
  }
  else if (name === 'variables' && varsList.value.length === 0) loadVariables()
  else if (name === 'status' && statusList.value.length === 0) loadStatus()
  else if (name === 'innodb' && !innodbLoaded.value) loadInnodb()
  else if (name === 'locks' && !locksLoaded.value) loadLocks()
  else if (name === 'slow' && !slowLoaded.value) loadSlow()
  else if (name === 'topTables' && !topTablesLoaded.value) loadTopTables()
}

function onOpen() {
  // 应用初始 Tab（树节点的"服务器状态"/"实时监控"可指定聚焦 Tab）
  activeTab.value = initialTab || 'overview'
  // 首次打开加载概览数据
  refreshOverview()
  // 按需加载初始 Tab 数据
  if (activeTab.value !== 'overview') onTabChange(activeTab.value)
}

function onClose() {
  // 关闭时停止所有自动刷新
  overviewAutoRefresh.value = false
  stopOverviewAutoRefresh()
  stopSessionAutoRefresh()
  sessionInterval.value = 0
  stopTrendAutoRefresh()
  trendPaused.value = false
  // 重置趋势模式为实时，清空历史数据
  trendMode.value = 'realtime'
  historyPoints.value = []
  // 清空新增 Tab 数据，避免串连数据污染
  innodbStatus.value = ''
  innodbSupported.value = false
  innodbLoaded.value = false
  locksList.value = []
  locksLoaded.value = false
  slowList.value = []
  slowLoaded.value = false
  topTablesList.value = []
  topTablesLoaded.value = false
}

onUnmounted(() => {
  stopOverviewAutoRefresh()
  stopSessionAutoRefresh()
  stopTrendAutoRefresh()
})
</script>

<style scoped>
/* 指标卡片：使用 db-tools.css 中的 CSS 变量，支持深色模式 */
.stat-card {
  background: var(--db-card-bg);
  border: 1px solid var(--db-card-border);
  border-radius: 8px;
  padding: 14px;
  text-align: center;
  box-shadow: var(--db-card-shadow);
}

.stat-value {
  font-size: 22px;
  font-weight: 700;
  color: var(--db-accent);
  font-family: 'JetBrains Mono', monospace;
  line-height: 1.2;
}

.stat-label {
  font-size: 12px;
  color: var(--db-text-tertiary);
  margin-top: 4px;
}

.stat-sub {
  font-size: 12px;
  font-weight: 400;
  color: var(--db-text-tertiary);
}

.buffer-section {
  margin: 8px 0 4px;
}

.buffer-row {
  display: flex;
  justify-content: space-between;
  margin-bottom: 4px;
  font-size: 12px;
}

.buffer-label {
  color: var(--db-text-tertiary);
}

.buffer-value {
  font-weight: 600;
  color: var(--db-text-primary);
}

.mini-stat {
  text-align: center;
  padding: 8px;
  background: var(--db-bg-secondary);
  border-radius: 6px;
}

.mini-label {
  font-size: 12px;
  color: var(--db-text-tertiary);
}

.mini-value {
  font-size: 15px;
  font-weight: 600;
  color: var(--db-text-primary);
}

.overview-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}

.update-time {
  color: var(--db-text-tertiary);
  font-size: 12px;
}

/* 会话 Tab 工具栏 */
.session-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.session-count {
  color: var(--db-text-tertiary);
  font-size: 12px;
}

/* 性能趋势 Tab */
.perf-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.perf-tip {
  color: var(--db-text-tertiary);
  font-size: 12px;
}

.trend-card {
  background: var(--db-card-bg);
  border: 1px solid var(--db-card-border);
  border-radius: 8px;
  padding: 12px;
  box-shadow: var(--db-card-shadow);
}

.trend-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 6px;
}

.trend-title {
  font-size: 13px;
  color: var(--db-text-secondary);
}

.trend-current {
  font-size: 18px;
  font-weight: 700;
  font-family: 'JetBrains Mono', monospace;
}

.trend-stats {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--db-text-tertiary);
  margin-top: 4px;
}

/* 服务器变量 / 状态指标 Tab 工具栏 */
.vars-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}
</style>
