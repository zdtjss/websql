<template>
  <el-dialog v-model="visible" title="数据同步与结构同步" width="1200px" :close-on-click-modal="false" @opened="onOpen"
    :draggable="!isFullscreen" :fullscreen="isFullscreen" :show-close="false"
    aria-label="数据同步与结构同步对话框">
    <template #header="{ close }">
      <div style="display:flex;justify-content:space-between;align-items:center">
        <span style="font-weight:bold;font-size:16px">数据同步与结构同步</span>
        <div style="display:flex;gap:4px">
          <!-- 图标按钮：仅图标无文字，需补充 aria-label（复用 title 的值） -->
          <el-button text @click="isFullscreen = !isFullscreen" :title="isFullscreen ? '还原' : '全屏'" :aria-label="isFullscreen ? '还原窗口大小' : '全屏显示'">
            <el-icon><FullScreen /></el-icon>
          </el-button>
          <el-button text @click="close" title="关闭" aria-label="关闭对话框" aria-keyshortcuts="Escape">
            <el-icon><Close /></el-icon>
          </el-button>
        </div>
      </div>
    </template>
    <el-steps :active="activeStep" align-center style="margin-bottom:20px" aria-label="同步步骤进度">
      <el-step title="选择源和目标" />
      <el-step title="比较中" />
      <el-step title="预览差异" />
      <el-step title="执行同步" />
    </el-steps>

    <div v-show="activeStep === 0">
      <el-row :gutter="20">
        <el-col :span="11">
          <el-card shadow="never">
            <template #header><span style="color:#409EFF;font-weight:bold">源数据库</span></template>
            <el-form label-position="top">
              <el-form-item label="连接">
                <el-select v-model="sourceConnId" placeholder="选择源连接" style="width:100%" :disabled="!!connId" aria-label="源数据库连接" @change="onSourceConnChange">
                  <el-option v-for="c in connections" :key="c.id" :label="c.name" :value="c.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="Schema">
                <el-select v-model="sourceSchema" placeholder="选择源Schema" style="width:100%" aria-label="源数据库 Schema">
                  <el-option v-for="s in sourceSchemas" :key="s" :label="s" :value="s" />
                </el-select>
              </el-form-item>
            </el-form>
          </el-card>
        </el-col>
        <el-col :span="2" style="text-align:center;padding-top:60px">
          <el-icon :size="28" color="#409EFF" aria-hidden="true"><Right /></el-icon>
        </el-col>
        <el-col :span="11">
          <el-card shadow="never">
            <template #header><span style="color:#67c23a;font-weight:bold">目标数据库</span></template>
            <el-form label-position="top">
              <el-form-item label="连接">
                <el-select v-model="targetConnId" filterable placeholder="选择目标连接" style="width:100%" aria-label="目标数据库连接" @change="onTargetConnChange">
                  <el-option v-for="c in connections" :key="c.id" :label="c.name" :value="c.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="Schema">
                <el-select v-model="targetSchema" placeholder="选择目标Schema" style="width:100%" aria-label="目标数据库 Schema">
                  <el-option v-for="s in targetSchemas" :key="s" :label="s" :value="s" />
                </el-select>
              </el-form-item>
            </el-form>
          </el-card>
        </el-col>
      </el-row>

      <div style="text-align:center;margin:15px 0">
        <el-radio-group v-model="syncMode" size="large" aria-label="同步模式">
          <el-radio-button value="structure">结构同步</el-radio-button>
          <el-radio-button value="data">数据同步</el-radio-button>
        </el-radio-group>
      </div>

      <div v-if="syncMode === 'data'" style="margin:0 auto;max-width:500px">
        <el-form label-position="top">
          <el-form-item label="选择要同步的表">
            <el-select v-model="syncTable" placeholder="选择表" style="width:100%" filterable aria-label="选择要同步的表">
              <el-option v-for="t in sourceTables" :key="t" :label="t" :value="t" />
            </el-select>
          </el-form-item>
          <el-form-item label="同步方向">
            <el-radio-group v-model="syncDirection" aria-label="同步方向">
              <el-radio value="source_to_target">源 → 目标</el-radio>
              <el-radio value="target_to_source">目标 → 源</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="分块大小">
            <el-input-number v-model="chunkSize" :min="1000" :max="50000" :step="1000" style="width:100%" aria-label="分块大小" />
            <div style="font-size:12px;color:#909399;margin-top:4px">大表建议5000，小表可增大到20000以提升速度</div>
          </el-form-item>
          <el-form-item label="冲突处理策略">
            <el-select v-model="conflictStrategy" style="width:100%" aria-label="冲突处理策略" placeholder="选择主键冲突时的处理方式">
              <el-option v-for="opt in conflictStrategyOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
            </el-select>
            <div style="font-size:12px;color:#909399;margin-top:4px">{{ conflictStrategyDesc }}</div>
          </el-form-item>
        </el-form>
      </div>

      <div v-if="syncMode === 'structure'" style="margin:0 auto;max-width:500px">
        <el-form label-position="top">
          <el-form-item label="过滤表（留空则比较全部）">
            <el-select v-model="tableFilter" multiple filterable allow-create default-first-option placeholder="输入表名" style="width:100%" aria-label="过滤表名">
              <el-option v-for="t in sourceTables" :key="t" :label="t" :value="t" />
            </el-select>
          </el-form-item>
        </el-form>
      </div>

      <div style="text-align:center;margin-top:20px">
        <el-button type="primary" size="large" @click="startCompare" :loading="comparing" :disabled="!canCompare" aria-keyshortcuts="Alt+Enter">
          开始比较
        </el-button>
      </div>
    </div>

    <!-- 比较中加载态：role="status" + aria-live 通知屏幕阅读器 -->
    <div v-show="activeStep === 1" style="text-align:center;padding:40px" role="status" aria-live="polite" :aria-busy="comparing">
      <el-icon class="is-loading" :size="48" color="#409EFF" aria-hidden="true"><Loading /></el-icon>
      <p style="margin-top:15px;color:#606266;font-size:15px">{{compareStatus}}</p>
      <el-progress v-if="compareProgress > 0" :percentage="compareProgress" style="margin-top:15px;max-width:400px;margin-left:auto;margin-right:auto"
        role="progressbar" :aria-valuenow="compareProgress" aria-valuemin="0" aria-valuemax="100" aria-label="比较进度" />
      <div v-if="chunkInfo.totalChunks > 1" style="margin-top:10px;color:#909399;font-size:13px">
        分块 {{chunkInfo.currentChunk}} / {{chunkInfo.totalChunks}}，每块 {{chunkSize}} 行
      </div>
      <div v-if="chunkInfo.accumulatedStats.add || chunkInfo.accumulatedStats.modify || chunkInfo.accumulatedStats.delete" style="margin-top:10px;font-size:13px">
        <el-tag type="success" size="small" style="margin-right:6px">新增 {{chunkInfo.accumulatedStats.add}}</el-tag>
        <el-tag type="warning" size="small" style="margin-right:6px">修改 {{chunkInfo.accumulatedStats.modify}}</el-tag>
        <el-tag type="danger" size="small">删除 {{chunkInfo.accumulatedStats.delete}}</el-tag>
      </div>
      <el-button v-if="comparing" type="danger" size="small" style="margin-top:15px" @click="cancelOperation">取消比较</el-button>
    </div>

    <div v-show="activeStep === 2">
      <div v-if="syncMode === 'structure'">
        <el-alert :title="`发现 ${schemaDiffs.length} 个差异 (新增:${addCount}, 修改:${modifyCount}, 删除:${dropCount})`" type="warning" :closable="false" style="margin-bottom:15px" role="alert" />

        <el-table :data="schemaDiffs" max-height="400" stripe highlight-current-row aria-label="结构差异列表" @row-click="selectSchemaDiff">
          <el-table-column prop="tableName" label="表名" width="180" />
          <el-table-column prop="diffType" label="差异类型" width="100">
            <template #default="{row}">
              <el-tag v-if="row.diffType==='ADD'" type="success" size="small">新增</el-tag>
              <el-tag v-else-if="row.diffType==='DROP'" type="danger" size="small">删除</el-tag>
              <el-tag v-else type="warning" size="small">修改</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="列变更" min-width="250">
            <template #default="{row}">
              <template v-if="row.columnDiffs && row.columnDiffs.length">
                <div v-for="cd in row.columnDiffs" :key="cd.columnName" style="margin:2px 0">
                  <el-tag v-if="cd.diffType==='ADD'" type="success" size="small">+{{cd.columnName}}</el-tag>
                  <el-tag v-else-if="cd.diffType==='DROP'" type="danger" size="small">-{{cd.columnName}}</el-tag>
                  <el-tag v-else type="warning" size="small">~{{cd.columnName}}</el-tag>
                  <span style="font-size:12px;color:#909399;margin-left:4px">{{cd.sourceDef}}</span>
                </div>
              </template>
              <span v-else style="color:#909399;font-size:12px">{{ row.diffType === 'ADD' ? '整表新增' : row.diffType === 'DROP' ? '整表删除' : '索引变更' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="索引变更" width="200">
            <template #default="{row}">
              <template v-if="row.indexDiffs && row.indexDiffs.length">
                <div v-for="id in row.indexDiffs" :key="id.indexName" style="margin:2px 0">
                  <el-tag :type="id.diffType==='ADD'?'success':id.diffType==='DROP'?'danger':'warning'" size="small">
                    {{id.diffType==='ADD'?'+':id.diffType==='DROP'?'-':'~'}}idx:{{id.indexName}}
                  </el-tag>
                </div>
              </template>
            </template>
          </el-table-column>
        </el-table>

        <div v-if="selectedSchemaDiff" style="margin-top:15px">
          <el-card shadow="never">
            <template #header>
              <div style="display:flex;justify-content:space-between;align-items:center">
                <span style="font-weight:bold">SQL变更脚本</span>
                <el-button type="primary" size="small" @click="copySQL(generatedSQL)" :aria-label="`复制 ${selectedSchemaDiff.tableName} 的 SQL 变更脚本`">复制SQL</el-button>
              </div>
            </template>
            <pre style="background:#1e1e1e;color:#d4d4d4;padding:15px;border-radius:6px;max-height:300px;overflow:auto;font-size:13px;line-height:1.5"><code>{{generatedSQL}}</code></pre>
          </el-card>
        </div>
      </div>

      <div v-else>
        <el-alert :title="`表 ${syncTable}: 源 ${dataDiff.totalSource||0} 行, 目标 ${dataDiff.totalTarget||0} 行, 新增 ${dataDiff.addCount||0}, 修改 ${dataDiff.modifyCount||0}, 删除 ${dataDiff.deleteCount||0}`" type="warning" :closable="false" style="margin-bottom:15px" role="alert" />

        <div v-if="dataDiff.addedRows && dataDiff.addedRows.length" style="margin-bottom:15px">
          <div style="display:flex;align-items:center;margin-bottom:8px">
            <el-tag type="success" size="small">新增行</el-tag>
            <span style="margin-left:8px;color:#909399;font-size:12px">共 {{dataDiff.addCount}} 行，显示前 {{dataDiff.addedRows.length}} 行</span>
          </div>
          <el-table :data="dataDiff.addedRows" max-height="200" stripe size="small" border aria-label="新增行列表">
            <el-table-column v-for="col in dataDiff.columns" :key="col.name" :prop="col.name" :label="col.name" width="120" :show-overflow-tooltip="true" />
          </el-table>
        </div>

        <div v-if="dataDiff.modifiedRows && dataDiff.modifiedRows.length" style="margin-bottom:15px">
          <div style="display:flex;align-items:center;margin-bottom:8px">
            <el-tag type="warning" size="small">修改行</el-tag>
            <span style="margin-left:8px;color:#909399;font-size:12px">共 {{dataDiff.modifyCount}} 行，显示前 {{dataDiff.modifiedRows.length}} 行</span>
          </div>
          <el-table :data="dataDiff.modifiedRows" max-height="200" stripe size="small" border aria-label="修改行列表">
            <el-table-column label="键值" width="180">
              <template #default="{row}">
                <span v-for="(v,k) in row.key" :key="k" style="margin-right:6px;font-size:12px"><strong>{{k}}</strong>={{v}}</span>
              </template>
            </el-table-column>
            <el-table-column label="变更字段">
              <template #default="{row}">
                <div v-for="ch in row.changes" :key="ch.columnName" style="margin:2px 0;font-size:12px">
                  <strong>{{ch.columnName}}</strong>:
                  <span style="color:#f56c6c;text-decoration:line-through">{{ch.oldValue}}</span>
                  <span style="margin:0 4px">&rarr;</span>
                  <span style="color:#67c23a">{{ch.newValue}}</span>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div v-if="dataDiff.deletedRows && dataDiff.deletedRows.length" style="margin-bottom:15px">
          <div style="display:flex;align-items:center;margin-bottom:8px">
            <el-tag type="danger" size="small">删除行</el-tag>
            <span style="margin-left:8px;color:#909399;font-size:12px">共 {{dataDiff.deleteCount}} 行，显示前 {{dataDiff.deletedRows.length}} 行</span>
          </div>
          <el-table :data="dataDiff.deletedRows" max-height="200" stripe size="small" border aria-label="删除行列表">
            <el-table-column v-for="col in dataDiff.columns" :key="col.name" :prop="col.name" :label="col.name" width="120" :show-overflow-tooltip="true" />
          </el-table>
        </div>

        <el-card shadow="never" style="margin-top:15px">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span style="font-weight:bold">同步SQL预览</span>
              <div style="display:flex;gap:8px;align-items:center">
                <span v-if="accumulatedSQL && !syncSQL" style="font-size:12px;color:#909399" aria-live="polite">已生成 {{sqlStatementCount}} 条SQL</span>
                <el-button v-if="!accumulatedSQL && !syncSQL" type="primary" size="small" @click="loadSyncSQL" :loading="loadingSQL">生成SQL</el-button>
                <el-button type="primary" size="small" @click="copySQL(syncSQL || accumulatedSQL || '')" aria-label="复制同步 SQL">复制SQL</el-button>
              </div>
            </div>
          </template>
          <pre v-if="syncSQL || accumulatedSQL" style="background:#1e1e1e;color:#d4d4d4;padding:15px;border-radius:6px;max-height:250px;overflow:auto;font-size:13px;line-height:1.5"><code>{{syncSQL || accumulatedSQL}}</code></pre>
          <div v-else style="text-align:center;color:#909399;padding:20px">点击"生成SQL"预览同步语句</div>
        </el-card>

        <!-- Dry-Run 试运行结果：展示预估影响行数与示例 SQL，不执行写操作 -->
        <el-card v-if="dryRunResult" shadow="never" style="margin-top:15px">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span style="font-weight:bold">Dry-Run 试运行结果</span>
              <el-tag v-if="dryRunResult.operationCounts" type="info" size="small">
                源 {{dryRunResult.totalSource}} 行 / 目标 {{dryRunResult.totalTarget}} 行
              </el-tag>
            </div>
          </template>
          <el-alert type="info" :closable="false" style="margin-bottom:12px" role="alert">
            <template #title>
              预估：新增 {{dryRunResult.operationCounts?.INSERT || 0}}，
              更新 {{dryRunResult.operationCounts?.UPDATE || 0}}，
              删除 {{dryRunResult.operationCounts?.DELETE || 0}}，
              潜在冲突 {{dryRunResult.operationCounts?.CONFLICT || 0}}
            </template>
          </el-alert>
          <el-table :data="dryRunResult.samples" stripe size="small" border aria-label="Dry-Run 预估明细">
            <el-table-column label="操作类型" width="110">
              <template #default="{row}">
                <el-tag :type="row.operation==='INSERT'?'success':row.operation==='UPDATE'?'warning':'danger'" size="small">{{row.operation}}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="estimate" label="预估行数" width="100" />
            <el-table-column label="SQL 预览（前 5 条）">
              <template #default="{row}">
                <pre style="background:#1e1e1e;color:#d4d4d4;padding:8px;border-radius:4px;max-height:140px;overflow:auto;font-size:12px;line-height:1.4;margin:0"><code>{{row.sqlPreview}}</code></pre>
              </template>
            </el-table-column>
          </el-table>
          <div v-if="dryRunResult.conflicts && dryRunResult.conflicts.length" style="margin-top:12px">
            <div style="font-size:12px;color:#909399;margin-bottom:6px">潜在冲突（目标已存在且字段不同，显示前 {{dryRunResult.conflicts.length}} 条）</div>
            <el-table :data="dryRunResult.conflicts" max-height="160" stripe size="small" border aria-label="潜在冲突列表">
              <el-table-column label="键值" width="180">
                <template #default="{row}">
                  <span v-for="(v,k) in row.key" :key="k" style="margin-right:6px;font-size:12px"><strong>{{k}}</strong>={{v}}</span>
                </template>
              </el-table-column>
              <el-table-column label="变更字段">
                <template #default="{row}">
                  <div v-for="ch in row.changes" :key="ch.columnName" style="margin:2px 0;font-size:12px">
                    <strong>{{ch.columnName}}</strong>:
                    <span style="color:#f56c6c;text-decoration:line-through">{{ch.oldValue}}</span>
                    <span style="margin:0 4px">&rarr;</span>
                    <span style="color:#67c23a">{{ch.newValue}}</span>
                  </div>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-card>
      </div>
    </div>

    <div v-show="activeStep === 3">
      <div v-if="syncing">
        <!-- 同步中加载态：role="status" + aria-live 通知屏幕阅读器 -->
        <div style="text-align:center;padding:40px" role="status" aria-live="polite" :aria-busy="syncing">
          <el-icon class="is-loading" :size="48" color="#409EFF" aria-hidden="true"><Loading /></el-icon>
          <p style="margin-top:15px;color:#606266;font-size:15px">{{syncStatusText}}</p>
          <el-progress :percentage="syncProgress" style="margin-top:15px;max-width:400px;margin-left:auto;margin-right:auto"
            role="progressbar" :aria-valuenow="syncProgress" aria-valuemin="0" aria-valuemax="100" aria-label="同步进度" />
          <div style="margin-top:10px;font-size:13px;color:#909399">
            批次 {{syncBatchInfo.current}} / {{syncBatchInfo.total}}
            <span v-if="syncTotalStatements"> · 共 {{syncTotalStatements}} 条语句</span>
          </div>
          <div v-if="syncBatchInfo.insertCount || syncBatchInfo.updateCount || syncBatchInfo.deleteCount" style="margin-top:10px;font-size:13px">
            <el-tag type="success" size="small" style="margin-right:6px">插入 {{syncBatchInfo.insertCount}}</el-tag>
            <el-tag type="warning" size="small" style="margin-right:6px">更新 {{syncBatchInfo.updateCount}}</el-tag>
            <el-tag type="danger" size="small">删除 {{syncBatchInfo.deleteCount}}</el-tag>
            <span style="margin-left:8px;color:#606266">已影响 {{syncAffectedRows}} 行</span>
          </div>
          <div v-if="syncElapsedSec > 0" style="margin-top:8px;font-size:12px;color:#909399">
            已用 {{formatSyncDuration(syncElapsedSec)}}<span v-if="syncEta"> · 预计剩余 {{syncEta}}</span>
          </div>
          <el-button type="danger" size="small" style="margin-top:15px" @click="cancelOperation">取消同步</el-button>
        </div>
      </div>
      <div v-else>
        <el-result :icon="syncSuccess ? 'success' : 'warning'" :title="syncSuccess ? '同步完成' : '同步完成（部分错误）'" :sub-title="syncResult" role="status">
          <template #extra>
            <el-button @click="resetDialog">重新同步</el-button>
            <!-- 回滚按钮：仅数据同步且生成了会话 ID 时可用（回滚已执行则隐藏） -->
            <el-button v-if="canRollback" type="warning" :loading="rollbackLoading" @click="onRollback">
              回滚最近同步
            </el-button>
            <!-- 导出报告：HTML（含 Mermaid 流程图）/ CSV -->
            <el-button v-if="syncMode === 'data'" :loading="exportLoading === 'html'" @click="onExportReport('html')">导出 HTML 报告</el-button>
            <el-button v-if="syncMode === 'data'" :loading="exportLoading === 'csv'" @click="onExportReport('csv')">导出 CSV 报告</el-button>
            <el-button type="primary" @click="visible=false">关闭</el-button>
          </template>
        </el-result>
      </div>
    </div>

    <template #footer>
      <el-button @click="visible=false">关闭</el-button>
      <el-button v-if="activeStep===2" @click="activeStep=0">上一步</el-button>
      <!-- Dry-Run 试运行：仅数据同步模式，不执行写操作 -->
      <el-button v-if="activeStep===2 && syncMode === 'data'" :loading="dryRunLoading" @click="runDryRun">
        Dry-Run 试运行
      </el-button>
      <el-button v-if="activeStep===2" type="primary" @click="executeSync" :loading="syncing" aria-keyshortcuts="Alt+Enter">
        {{syncMode === 'structure' ? '执行结构同步' : '执行数据同步'}}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, reactive, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Right, FullScreen, Close, Loading } from '@element-plus/icons-vue'
import { handleError } from '@/utils/errorHandler'
import {
  generateSyncSQL,
  compareSyncSchema,
  compareDataChunked,
  applySchemaDiff,
  applyDataSync,
  dryRunSync,
  getRollbackLog,
  rollbackSync,
  exportSyncReport,
} from '@/api/conn'
import {
  loadConnections,
  loadSyncTargets,
  unwrapResponse,
  countDiffsByType,
  buildSyncFormData,
  buildSchemaDiffSQL,
} from '@/utils/dbMetadata'

const visible = defineModel({ default: false })

const { connId, schema } = defineProps({
  connId: String,
  schema: String
})

const activeStep = ref(0)
const isFullscreen = ref(false)
const syncMode = ref('structure')
const sourceConnId = ref('')
const targetConnId = ref('')
const sourceSchema = ref('')
const targetSchema = ref('')
const connections = ref([])
const sourceSchemas = ref([])
const targetSchemas = ref([])
const sourceTables = ref([])
const comparing = ref(false)
const syncing = ref(false)
const compareProgress = ref(0)
const compareStatus = ref('')
const schemaDiffs = ref([])
const dataDiff = ref({})
const syncTable = ref('')
const syncDirection = ref('source_to_target')
const tableFilter = ref([])
const selectedSchemaDiff = ref(null)
const syncResult = ref('')
const syncSuccess = ref(true)
const syncSQL = ref('')
const loadingSQL = ref(false)
const chunkSize = ref(5000)
const accumulatedSQL = ref('')
const sqlStatementCount = ref(0)
const cancelled = ref(false)
let abortController = null

// ===== 数据同步增强：冲突策略 / Dry-Run / 回滚 / 报告 =====
// 冲突处理策略（默认 update）。后端按此策略生成 INSERT 语句。
const conflictStrategy = ref('update')
// Dry-Run 试运行结果
const dryRunResult = ref(null)
const dryRunLoading = ref(false)
// 同步会话 ID（前端生成），用于关联回滚日志
const syncSessionId = ref('')
// 累计错误详情，供报告导出使用
const syncErrors = ref([])
// 回滚状态
const rollbackLoading = ref(false)
const rollbackDone = ref(false)
// 报告导出加载态：'' | 'html' | 'csv'
const exportLoading = ref('')
// 最近一次同步的元信息，供报告导出使用
const lastSyncMeta = reactive({
  startedAt: '',
  durationMs: 0,
  insert: 0,
  update: 0,
  delete: 0,
  dryRun: false,
})

const chunkInfo = reactive({
  totalChunks: 0,
  currentChunk: 0,
  accumulatedStats: { add: 0, modify: 0, delete: 0 }
})

const syncProgress = ref(0)
const syncStatusText = ref('')
const syncBatchInfo = reactive({
  current: 0,
  total: 0,
  insertCount: 0,
  updateCount: 0,
  deleteCount: 0
})

// 同步耗时与预计剩余时间相关状态
let syncStartTime = 0                  // 同步开始时间戳（毫秒）
const syncTotalStatements = ref(0)     // 同步语句总数（用于估算）
const syncElapsedSec = ref(0)          // 已用时间（秒）
let syncTickTimer = null               // 已用时间刷新定时器

// 已影响行数（插入+更新+删除）
const syncAffectedRows = computed(() =>
  syncBatchInfo.insertCount + syncBatchInfo.updateCount + syncBatchInfo.deleteCount
)

// 预计剩余时间：基于已用时间和批次进度估算
const syncEta = computed(() => {
  if (syncBatchInfo.current < 1 || syncBatchInfo.total <= 0) return ''
  if (syncBatchInfo.current >= syncBatchInfo.total) return ''
  const elapsed = syncElapsedSec.value
  if (elapsed < 1) return ''
  const secPerBatch = elapsed / syncBatchInfo.current
  const remainingSec = Math.round((syncBatchInfo.total - syncBatchInfo.current) * secPerBatch)
  return formatSyncDuration(remainingSec)
})

function formatSyncDuration(sec) {
  if (sec < 1) return '<1秒'
  if (sec < 60) return `${sec}秒`
  const min = Math.floor(sec / 60)
  const remSec = sec % 60
  if (min < 60) return `${min}分${remSec}秒`
  const hr = Math.floor(min / 60)
  return `${hr}小时${min % 60}分`
}

const addCount = computed(() => countDiffsByType(schemaDiffs.value, 'ADD'))
const dropCount = computed(() => countDiffsByType(schemaDiffs.value, 'DROP'))
const modifyCount = computed(() => countDiffsByType(schemaDiffs.value, 'MODIFY'))

const canCompare = computed(() => {
  if (!sourceConnId.value || !targetConnId.value || !sourceSchema.value || !targetSchema.value) return false
  if (syncMode.value === 'data' && !syncTable.value) return false
  return true
})

// 目标数据库类型（用于过滤可用的冲突策略）
const targetDbType = computed(() => {
  const c = connections.value.find(x => x.id === targetConnId.value)
  return c?.dbType || ''
})

// 不同数据库类型支持的冲突策略
const conflictStrategyOptions = computed(() => {
  const t = targetDbType.value
  // Oracle 没有 INSERT IGNORE / REPLACE 语法
  if (t === 'oracle') {
    return [
      { value: 'update', label: 'update - 更新冲突记录（默认）' },
      { value: 'skip', label: 'skip - 跳过冲突记录' },
      { value: 'fail', label: 'fail - 遇冲突即停止' },
    ]
  }
  return [
    { value: 'update', label: 'update - 更新冲突记录（默认）' },
    { value: 'skip', label: 'skip - 跳过冲突记录' },
    { value: 'insert_ignore', label: 'insert_ignore - INSERT IGNORE（MySQL）' },
    { value: 'replace', label: 'replace - REPLACE INTO（MySQL）' },
    { value: 'fail', label: 'fail - 遇冲突即停止' },
  ]
})

const conflictStrategyDesc = computed(() => {
  switch (conflictStrategy.value) {
    case 'skip': return '目标已存在的行不处理（INSERT 用 IGNORE，UPDATE 跳过）'
    case 'insert_ignore': return 'MySQL：INSERT IGNORE INTO，遇主键冲突跳过该行'
    case 'replace': return 'MySQL：REPLACE INTO，遇主键冲突先删后插'
    case 'fail': return '普通 INSERT，遇主键冲突立即报错停止'
    default: return 'update：MySQL 用 INSERT...ON DUPLICATE KEY UPDATE，冲突转为更新'
  }
})

// 可回滚：数据同步模式、有会话 ID、未取消、尚未执行过回滚
const canRollback = computed(() =>
  syncMode.value === 'data' && !!syncSessionId.value && !rollbackDone.value && !cancelled.value
)

const generatedSQL = computed(() => {
  if (!selectedSchemaDiff.value) return ''
  let sql = ''
  if (selectedSchemaDiff.value.diffType === 'ADD') {
    sql = selectedSchemaDiff.value.sourceDDL ? `CREATE TABLE ... (源表DDL);\n` : ''
  } else if (selectedSchemaDiff.value.diffType === 'DROP') {
    sql = `-- 目标端存在但源端不存在，如需删除: DROP TABLE \`${selectedSchemaDiff.value.tableName}\`;\n`
  }
  // 列/索引变更 SQL 统一由公共函数拼接，避免与 executeStructureSync 重复维护
  sql += buildSchemaDiffSQL([selectedSchemaDiff.value])
  return sql || '-- 无变更'
})

watch(syncMode, () => {
  syncTable.value = ''
  syncSQL.value = ''
  accumulatedSQL.value = ''
})

watch(sourceConnId, () => { sourceSchema.value = '' })
watch(targetConnId, () => { targetSchema.value = '' })

async function onOpen() {
  activeStep.value = 0
  isFullscreen.value = false
  schemaDiffs.value = []
  dataDiff.value = {}
  syncSQL.value = ''
  accumulatedSQL.value = ''
  sqlStatementCount.value = 0
  selectedSchemaDiff.value = null
  sourceConnId.value = ''
  targetConnId.value = ''
  sourceSchema.value = ''
  targetSchema.value = ''
  sourceSchemas.value = []
  targetSchemas.value = []
  sourceTables.value = []
  chunkInfo.totalChunks = 0
  chunkInfo.currentChunk = 0
  chunkInfo.accumulatedStats = { add: 0, modify: 0, delete: 0 }
  syncProgress.value = 0
  syncBatchInfo.current = 0
  syncBatchInfo.total = 0
  syncBatchInfo.insertCount = 0
  syncBatchInfo.updateCount = 0
  syncBatchInfo.deleteCount = 0
  syncStartTime = 0
  syncElapsedSec.value = 0
  syncTotalStatements.value = 0
  // 重置数据同步增强相关状态：冲突策略/Dry-Run/回滚/报告
  dryRunResult.value = null
  syncSessionId.value = ''
  syncErrors.value = []
  rollbackDone.value = false
  rollbackLoading.value = false
  exportLoading.value = ''
  conflictStrategy.value = 'update'
  lastSyncMeta.startedAt = ''
  lastSyncMeta.durationMs = 0
  lastSyncMeta.insert = 0
  lastSyncMeta.update = 0
  lastSyncMeta.delete = 0
  lastSyncMeta.dryRun = false
  stopSyncTickTimer()
  connections.value = await loadConnections()
  if (connId) {
    sourceConnId.value = connId
    onSourceConnChange()
  }
  if (schema) sourceSchema.value = schema
}

async function onSourceConnChange() {
  if (!sourceConnId.value) return
  const targets = await loadSyncTargets(sourceConnId.value, '加载源库信息')
  sourceSchemas.value = targets.schemas
  sourceTables.value = targets.tables
}

async function onTargetConnChange() {
  if (!targetConnId.value) return
  const targets = await loadSyncTargets(targetConnId.value, '加载目标库信息')
  targetSchemas.value = targets.schemas
}

function selectSchemaDiff(row) { selectedSchemaDiff.value = row }

function copySQL(sql) {
  if (!sql) { ElMessage.warning('没有可复制的SQL'); return }
  navigator.clipboard.writeText(sql).then(() => ElMessage.success('已复制到剪贴板'))
}

function cancelOperation() {
  cancelled.value = true
  if (abortController) {
    abortController.abort()
    abortController = null
  }
}

async function loadSyncSQL() {
  loadingSQL.value = true
  try {
    const formData = buildSyncFormData({
      sourceConnId: sourceConnId.value,
      sourceSchema: sourceSchema.value,
      targetConnId: targetConnId.value,
      targetSchema: targetSchema.value,
      table: syncTable.value,
      direction: syncDirection.value,
      conflictStrategy: conflictStrategy.value,
    })
    const res = await generateSyncSQL(formData)
    const result = unwrapResponse(res) || {}
    syncSQL.value = result.sql || '-- 无需同步'
  } catch (e) {
    handleError(e, '生成SQL')
  } finally {
    loadingSQL.value = false
  }
}

// Dry-Run 试运行：不执行写操作，仅返回预估影响行数与示例 SQL
async function runDryRun() {
  if (!syncTable.value) {
    ElMessage.warning('请先选择要同步的表')
    return
  }
  dryRunLoading.value = true
  try {
    const formData = buildSyncFormData({
      sourceConnId: sourceConnId.value,
      sourceSchema: sourceSchema.value,
      targetConnId: targetConnId.value,
      targetSchema: targetSchema.value,
      table: syncTable.value,
      direction: syncDirection.value,
      conflictStrategy: conflictStrategy.value,
    })
    const res = await dryRunSync(formData)
    const result = unwrapResponse(res) || {}
    if (result.error) {
      ElMessage.error(result.error)
      return
    }
    dryRunResult.value = result
    ElMessage.success('Dry-Run 完成，未执行任何写操作')
  } catch (e) {
    handleError(e, 'Dry-Run 试运行')
  } finally {
    dryRunLoading.value = false
  }
}

async function startCompare() {
  if (!canCompare.value) {
    ElMessage.warning('请选择源和目标数据库')
    return
  }
  comparing.value = true
  cancelled.value = false
  activeStep.value = 1
  compareProgress.value = 10
  compareStatus.value = '正在比较...'
  chunkInfo.accumulatedStats = { add: 0, modify: 0, delete: 0 }
  accumulatedSQL.value = ''
  sqlStatementCount.value = 0

  try {
    if (syncMode.value === 'structure') {
      await startStructureCompare()
    } else {
      await startDataCompareChunked()
    }
  } catch (e) {
    if (cancelled.value) {
      ElMessage.info('比较已取消')
    } else {
      handleError(e, '比较')
    }
    if (chunkInfo.accumulatedStats.add || chunkInfo.accumulatedStats.modify || chunkInfo.accumulatedStats.delete) {
      activeStep.value = 2
    } else {
      activeStep.value = 0
    }
  } finally {
    comparing.value = false
  }
}

async function startStructureCompare() {
  compareStatus.value = '正在比较表结构...'
  compareProgress.value = 50
  const formData = buildSyncFormData({
    sourceConnId: sourceConnId.value,
    targetConnId: targetConnId.value,
    sourceSchema: sourceSchema.value,
    targetSchema: targetSchema.value,
    tables: tableFilter.value.length ? tableFilter.value.join(',') : '',
  })
  const res = await compareSyncSchema(formData)
  const result = unwrapResponse(res) || {}
  if (result.error) {
    ElMessage.error(result.error)
    activeStep.value = 0
    return
  }
  schemaDiffs.value = result.diffs || []
  compareProgress.value = 100
  compareStatus.value = '比较完成'
  if (schemaDiffs.value.length) selectedSchemaDiff.value = schemaDiffs.value[0]
  setTimeout(() => activeStep.value = 2, 500)
}

async function startDataCompareChunked() {
  abortController = new AbortController()
  compareStatus.value = `正在比较表 ${syncTable.value} 的数据...`

  const allAddedRows = []
  const allDeletedRows = []
  const allModifiedRows = []
  let totalSource = 0
  let totalTarget = 0
  let columns = []
  let chunkIndex = 0
  let hasMore = true

  while (hasMore && !cancelled.value) {
    chunkInfo.currentChunk = chunkIndex + 1
    compareStatus.value = `正在比较表 ${syncTable.value} 的数据 (分块 ${chunkIndex + 1})...`

    const formData = buildSyncFormData({
      sourceConnId: sourceConnId.value,
      targetConnId: targetConnId.value,
      sourceSchema: sourceSchema.value,
      targetSchema: targetSchema.value,
      table: syncTable.value,
      chunkSize: String(chunkSize.value),
      chunkIndex: String(chunkIndex),
      direction: syncDirection.value,
      generateSQL: 'true',
      conflictStrategy: conflictStrategy.value,
    })

    const res = await compareDataChunked(formData, abortController.signal)
    const result = unwrapResponse(res) || {}

    if (result.error) {
      ElMessage.error(result.error)
      if (allAddedRows.length || allDeletedRows.length || allModifiedRows.length) break
      activeStep.value = 0
      return
    }

    totalSource = result.totalSource || 0
    totalTarget = result.totalTarget || 0
    columns = result.columns || columns
    chunkInfo.totalChunks = result.totalChunks || 1

    if (result.addedRows) allAddedRows.push(...result.addedRows)
    if (result.deletedRows) allDeletedRows.push(...result.deletedRows)
    if (result.modifiedRows) allModifiedRows.push(...result.modifiedRows)

    chunkInfo.accumulatedStats.add += result.addCount || 0
    chunkInfo.accumulatedStats.modify += result.modifyCount || 0
    chunkInfo.accumulatedStats.delete += result.deleteCount || 0

    if (result.sql) {
      accumulatedSQL.value += result.sql
      sqlStatementCount.value += (result.sql.match(/;\n/g) || []).length
    }

    hasMore = result.hasMore || false
    chunkIndex++

    compareProgress.value = Math.min(90, Math.round((chunkIndex / (chunkInfo.totalChunks || 1)) * 90))
  }

  const previewLimit = 100
  dataDiff.value = {
    tableName: syncTable.value,
    totalSource,
    totalTarget,
    addedRows: allAddedRows.slice(0, previewLimit),
    deletedRows: allDeletedRows.slice(0, previewLimit),
    modifiedRows: allModifiedRows.slice(0, previewLimit),
    addCount: chunkInfo.accumulatedStats.add,
    deleteCount: chunkInfo.accumulatedStats.delete,
    modifyCount: chunkInfo.accumulatedStats.modify,
    columns
  }

  compareProgress.value = 100
  compareStatus.value = '比较完成'
  setTimeout(() => activeStep.value = 2, 500)
}

async function executeSync() {
  try {
    let sqlCount = 0
    let actionDesc = ''
    if (syncMode.value === 'structure') {
      const sqlToApply = buildSchemaDiffSQL(schemaDiffs.value)
      if (!sqlToApply.trim()) { ElMessage.info('没有需要执行的SQL'); return }
      sqlCount = sqlToApply.split(';').filter(s => s.trim()).length
      actionDesc = `即将对目标库执行 ${sqlCount} 条结构变更语句（ALTER/CREATE INDEX/DROP INDEX），是否确认？`
    } else {
      const sqlToExecute = syncSQL.value || accumulatedSQL.value
      if (!sqlToExecute || sqlToExecute === '-- 无需同步') { ElMessage.info('没有需要同步的数据'); return }
      sqlCount = (sqlToExecute.match(/;\n/g) || []).length
      actionDesc = `即将对目标库执行 ${sqlCount} 条数据同步语句（INSERT/UPDATE/DELETE），将分批执行，是否确认？`
    }

    await ElMessageBox.confirm(actionDesc, '同步确认', {
      confirmButtonText: '确认执行',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch { return }

  syncing.value = true
  cancelled.value = false
  activeStep.value = 3
  syncProgress.value = 0
  syncBatchInfo.current = 0
  syncBatchInfo.total = 0
  syncBatchInfo.insertCount = 0
  syncBatchInfo.updateCount = 0
  syncBatchInfo.deleteCount = 0
  // 初始化耗时与 ETA 跟踪
  syncStartTime = Date.now()
  syncElapsedSec.value = 0
  syncTotalStatements.value = 0
  startSyncTickTimer()
  // 数据同步增强：生成会话 ID（用于回滚）、重置错误/回滚状态、记录报告元信息
  syncErrors.value = []
  rollbackDone.value = false
  if (syncMode.value === 'data') {
    syncSessionId.value = (crypto?.randomUUID?.() || `sync-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`)
  } else {
    syncSessionId.value = ''
  }
  lastSyncMeta.startedAt = new Date(syncStartTime).toLocaleString()
  lastSyncMeta.durationMs = 0
  lastSyncMeta.insert = 0
  lastSyncMeta.update = 0
  lastSyncMeta.delete = 0
  lastSyncMeta.dryRun = false

  try {
    if (syncMode.value === 'structure') {
      await executeStructureSync()
    } else {
      await executeDataSyncBatched()
    }
  } catch (e) {
    if (cancelled.value) {
      syncSuccess.value = false
      syncResult.value = '同步已取消'
    } else {
      syncSuccess.value = false
      syncResult.value = '同步失败: ' + (e.message || '未知错误')
    }
  } finally {
    syncing.value = false
    abortController = null
    stopSyncTickTimer()
  }
}

// 启动已用时间刷新定时器（每秒更新一次，驱动 ETA 计算）
function startSyncTickTimer() {
  stopSyncTickTimer()
  syncTickTimer = setInterval(() => {
    if (syncStartTime > 0) {
      syncElapsedSec.value = Math.floor((Date.now() - syncStartTime) / 1000)
    }
  }, 1000)
}

function stopSyncTickTimer() {
  if (syncTickTimer) {
    clearInterval(syncTickTimer)
    syncTickTimer = null
  }
}

async function executeStructureSync() {
  const sqlToApply = buildSchemaDiffSQL(schemaDiffs.value)
  if (!sqlToApply.trim()) { ElMessage.info('没有需要执行的SQL'); return }

  syncStatusText.value = '正在执行结构同步...'
  syncProgress.value = 50

  // 注意：applySchemaDiff 接口字段为 connId/schema/sql，与 sync 系列接口不同，
  // 因此不复用 buildSyncFormData，保持与后端契约一致
  const formData = new FormData()
  formData.append('connId', targetConnId.value)
  formData.append('schema', targetSchema.value)
  formData.append('sql', sqlToApply)
  const res = await applySchemaDiff(formData)
  const result = unwrapResponse(res) || {}
  syncSuccess.value = result.success
  syncResult.value = `成功执行 ${result.executedCount || 0} 条语句` + (result.errors && result.errors.length ? `，${result.errors.length} 条失败` : '')
  syncProgress.value = 100
}

async function executeDataSyncBatched() {
  const sqlToExecute = syncSQL.value || accumulatedSQL.value
  if (!sqlToExecute || sqlToExecute === '-- 无需同步') { ElMessage.info('没有需要同步的数据'); return }

  const statements = splitSQLStatements(sqlToExecute)
  if (statements.length === 0) { ElMessage.info('没有需要同步的数据'); return }

  const batchSize = 200
  const batches = []
  for (let i = 0; i < statements.length; i += batchSize) {
    batches.push(statements.slice(i, i + batchSize).join(';\n') + ';\n')
  }

  syncBatchInfo.total = batches.length
  syncTotalStatements.value = statements.length
  abortController = new AbortController()

  const targetConn = syncDirection.value === 'source_to_target' ? targetConnId.value : sourceConnId.value
  const targetSch = syncDirection.value === 'source_to_target' ? targetSchema.value : sourceSchema.value

  let totalInsert = 0
  let totalUpdate = 0
  let totalDelete = 0
  let totalErrors = 0
  const allErrors = []
  let executedBatches = 0

  for (let i = 0; i < batches.length; i++) {
    if (cancelled.value) break

    syncBatchInfo.current = i + 1
    const affectedSoFar = totalInsert + totalUpdate + totalDelete
    syncStatusText.value = `正在执行数据同步 (批次 ${i + 1}/${batches.length})，已影响 ${affectedSoFar} 行`
    syncProgress.value = Math.round(((i) / batches.length) * 100)

    const formData = new FormData()
    formData.append('connId', targetConn)
    formData.append('schema', targetSch)
    formData.append('sql', batches[i])
    // 传入同步会话 ID，后端据此记录撤销 SQL（供回滚使用）
    formData.append('syncSessionId', syncSessionId.value)

    try {
      const res = await applyDataSync(formData, abortController.signal)
      const result = unwrapResponse(res) || {}
      totalInsert += result.insertCount || 0
      totalUpdate += result.updateCount || 0
      totalDelete += result.deleteCount || 0
      if (result.errors && result.errors.length) {
        totalErrors += result.errors.length
        allErrors.push(...result.errors)
      }
      executedBatches++
    } catch (e) {
      if (cancelled.value) break
      totalErrors++
      allErrors.push(`批次 ${i + 1} 执行失败: ${e.message || '未知错误'}`)
    }

    syncBatchInfo.insertCount = totalInsert
    syncBatchInfo.updateCount = totalUpdate
    syncBatchInfo.deleteCount = totalDelete
  }

  // 取消时保持当前进度，不设为 100%
  if (cancelled.value) {
    syncProgress.value = Math.round((executedBatches / batches.length) * 100)
    syncSuccess.value = false
    syncResult.value = `同步已取消：已执行 ${executedBatches}/${batches.length} 批次，插入${totalInsert}行, 更新${totalUpdate}行, 删除${totalDelete}行`
      + (totalErrors > 0 ? `，${totalErrors} 条失败` : '')
  } else {
    syncProgress.value = 100
    syncSuccess.value = totalErrors === 0
    syncResult.value = `插入${totalInsert}行, 更新${totalUpdate}行, 删除${totalDelete}行`
      + (totalErrors > 0 ? `，${totalErrors} 条失败` : '')
  }

  // 暴露错误明细供报告导出使用
  syncErrors.value = allErrors
  // 记录报告元信息（耗时与三类操作行数）
  lastSyncMeta.insert = totalInsert
  lastSyncMeta.update = totalUpdate
  lastSyncMeta.delete = totalDelete
  lastSyncMeta.durationMs = syncStartTime > 0 ? Date.now() - syncStartTime : 0
}

/**
 * 感知引号的 SQL 语句分割
 * 正确处理单引号字符串内的分号、转义单引号（''）、行注释（--）和块注释
 */
function splitSQLStatements(sqlText) {
  const statements = []
  let current = ''
  let inSingleQuote = false
  let inLineComment = false
  let inBlockComment = false
  let i = 0

  while (i < sqlText.length) {
    const ch = sqlText[i]
    const next = sqlText[i + 1]

    // 行注释
    if (!inSingleQuote && !inBlockComment && ch === '-' && next === '-') {
      inLineComment = true
      current += ch
      i++
      continue
    }
    if (inLineComment) {
      current += ch
      if (ch === '\n') inLineComment = false
      i++
      continue
    }

    // 块注释
    if (!inSingleQuote && !inLineComment && ch === '/' && next === '*') {
      inBlockComment = true
      current += ch + next
      i += 2
      continue
    }
    if (inBlockComment) {
      current += ch
      if (ch === '*' && next === '/') {
        current += next
        i += 2
        inBlockComment = false
        continue
      }
      i++
      continue
    }

    // 单引号字符串
    if (ch === "'" && !inLineComment && !inBlockComment) {
      if (inSingleQuote && next === "'") {
        // 转义单引号
        current += "''"
        i += 2
        continue
      }
      inSingleQuote = !inSingleQuote
      current += ch
      i++
      continue
    }

    // 分号分割（不在字符串/注释中）
    if (ch === ';' && !inSingleQuote && !inLineComment && !inBlockComment) {
      const trimmed = current.trim()
      if (trimmed) statements.push(trimmed)
      current = ''
      i++
      continue
    }

    current += ch
    i++
  }

  const trimmed = current.trim()
  if (trimmed) statements.push(trimmed)
  return statements
}

// 回滚最近一次同步：先查询撤销日志，确认后执行回滚
async function onRollback() {
  if (!syncSessionId.value) {
    ElMessage.warning('没有可回滚的同步会话')
    return
  }
  rollbackLoading.value = true
  let log
  try {
    const res = await getRollbackLog(syncSessionId.value)
    const result = unwrapResponse(res) || {}
    if (result.error) {
      ElMessage.error(result.error)
      return
    }
    log = result
  } catch (e) {
    handleError(e, '获取回滚日志')
    return
  } finally {
    rollbackLoading.value = false
  }

  // 撤销 SQL 预览（最多展示 8 条，避免对话框过长）
  const undoCount = log.undoCount || 0
  const previewSQLs = (log.undoSQLs || []).slice(0, 8).join('\n')
  const moreHint = undoCount > 8 ? `\n... 其余 ${undoCount - 8} 条省略` : ''
  const expiresIn = log.expiresIn || 0
  const msg = `即将回滚最近一次同步，共 ${undoCount} 条撤销 SQL（按逆序执行）。\n` +
    `日志将在 ${Math.max(0, Math.floor(expiresIn / 60))} 分钟后过期。\n\n` +
    `撤销 SQL 预览：\n${previewSQLs}${moreHint}\n\n` +
    `⚠️ 回滚将修改目标数据，请确认。`

  try {
    await ElMessageBox.confirm(msg, '回滚确认', {
      confirmButtonText: '确认回滚',
      cancelButtonText: '取消',
      type: 'warning',
      dangerouslyUseHTMLString: false,
      customClass: 'rollback-confirm-box',
    })
  } catch {
    return
  }

  rollbackLoading.value = true
  try {
    const res = await rollbackSync(syncSessionId.value)
    const result = unwrapResponse(res) || {}
    if (result.success === false) {
      ElMessage.warning(result.message || '回滚完成但存在错误')
      if (result.errors && result.errors.length) {
        syncErrors.value = [...syncErrors.value, ...result.errors.map(e => `[回滚] ${e}`)]
      }
      return
    }
    rollbackDone.value = true
    ElMessage.success(result.message || `回滚成功，执行 ${result.executed} 条撤销语句`)
  } catch (e) {
    handleError(e, '执行回滚')
  } finally {
    rollbackLoading.value = false
  }
}

// 导出同步报告：HTML（含 Mermaid 流程图）或 CSV
async function onExportReport(format) {
  if (!lastSyncMeta.startedAt && !dryRunResult.value) {
    ElMessage.warning('暂无可导出的同步结果')
    return
  }
  exportLoading.value = format
  try {
    // 源/目标端点信息（用于报告头部与 Mermaid 流程图）
    const isS2T = syncDirection.value === 'source_to_target'
    const srcConnObj = connections.value.find(c => c.id === sourceConnId.value) || {}
    const tgtConnObj = connections.value.find(c => c.id === targetConnId.value) || {}
    const payload = {
      format,
      syncMode: syncMode.value,
      direction: syncDirection.value,
      conflictStrategy: conflictStrategy.value,
      source: {
        connId: isS2T ? sourceConnId.value : targetConnId.value,
        connName: isS2T ? (srcConnObj.name || sourceConnId.value) : (tgtConnObj.name || targetConnId.value),
        schema: isS2T ? sourceSchema.value : targetSchema.value,
      },
      target: {
        connId: isS2T ? targetConnId.value : sourceConnId.value,
        connName: isS2T ? (tgtConnObj.name || targetConnId.value) : (srcConnObj.name || sourceConnId.value),
        schema: isS2T ? targetSchema.value : sourceSchema.value,
      },
      table: syncTable.value,
      results: [{
        tableName: syncTable.value,
        insert: lastSyncMeta.insert,
        update: lastSyncMeta.update,
        delete: lastSyncMeta.delete,
        failed: syncErrors.value.length,
      }],
      errors: syncErrors.value.slice(),
      startedAt: lastSyncMeta.startedAt,
      durationMs: lastSyncMeta.durationMs,
      dryRun: lastSyncMeta.dryRun,
    }
    const res = await exportSyncReport(payload)
    const result = unwrapResponse(res) || {}
    if (result.error) {
      ElMessage.error(result.error)
      return
    }
    if (result.url) {
      // 在新标签页打开下载链接（/exports/<filename>）
      window.open(result.url, '_blank')
      ElMessage.success(`已生成 ${format.toUpperCase()} 报告`)
    }
  } catch (e) {
    handleError(e, '导出报告')
  } finally {
    exportLoading.value = ''
  }
}

function resetDialog() {
  activeStep.value = 0
  schemaDiffs.value = []
  dataDiff.value = {}
  syncSQL.value = ''
  accumulatedSQL.value = ''
  sqlStatementCount.value = 0
  selectedSchemaDiff.value = null
  chunkInfo.totalChunks = 0
  chunkInfo.currentChunk = 0
  chunkInfo.accumulatedStats = { add: 0, modify: 0, delete: 0 }
  syncProgress.value = 0
  syncBatchInfo.current = 0
  syncBatchInfo.total = 0
  syncBatchInfo.insertCount = 0
  syncBatchInfo.updateCount = 0
  syncBatchInfo.deleteCount = 0
  syncStartTime = 0
  syncElapsedSec.value = 0
  syncTotalStatements.value = 0
  // 重置数据同步增强相关状态：冲突策略/Dry-Run/回滚/报告
  dryRunResult.value = null
  syncSessionId.value = ''
  syncErrors.value = []
  rollbackDone.value = false
  rollbackLoading.value = false
  exportLoading.value = ''
  lastSyncMeta.startedAt = ''
  lastSyncMeta.durationMs = 0
  lastSyncMeta.insert = 0
  lastSyncMeta.update = 0
  lastSyncMeta.delete = 0
  lastSyncMeta.dryRun = false
}

// 组件卸载时清理定时器，避免内存泄漏
onBeforeUnmount(() => {
  stopSyncTickTimer()
})
</script>
