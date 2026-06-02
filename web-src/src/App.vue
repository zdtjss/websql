<template>
  <el-config-provider :locale="zhCn">
  <div>
    <router-view v-if="systemRoutes.includes(route.path)" />
    <div v-else>

    <div class="ai-sql-panel-container">
      <div class="container">
        <!-- 会话历史消息 -->
        <div ref="msgContainer" class="chat-messages">
          <div v-if="hiddenMsgCount > 0" class="load-more-msgs" @click="showAllHistory = true">
            ↑ 点击加载更早的 {{ hiddenMsgCount }} 条消息
          </div>
          <!-- 思考过程（历史中的，可折叠） -->
          <div v-for="(msg, vIdx) in visibleChatHistory.msgs" :key="'h' + (visibleChatHistory.offset + vIdx)">
            <div v-if="msg.role === 'thinking'" class="thinking-block">
              <div class="thinking-label" style="cursor:pointer;" @click="toggleThinking(msg)">
                💭 思考过程 <span style="font-size:11px;">{{ msg.collapsed ? '▶ 展开' : '▼ 折叠' }}</span>
              </div>
              <div v-show="!msg.collapsed" class="thinking-content markdown-body" v-html="getCachedHtml(msg)"></div>
            </div>
            <div v-else-if="msg.role === 'user'" :class="['chat-bubble', 'user']">
              <div class="bubble-label">你</div>
              <div class="bubble-content markdown-body" v-html="getCachedHtml(msg)"></div>
            </div>
            <div v-else-if="msg.role === 'assistant'" :class="['chat-bubble', 'assistant']">
              <div class="bubble-label">AI</div>
              <div v-if="msg.hasSql" class="bubble-content">
                <pre class="sql-pre"><code v-html="highlightSql(msg.content)" /></pre>
              </div>
              <div v-else class="bubble-content markdown-body" v-html="getCachedHtml(msg)"></div>
            </div>
            <div v-else-if="msg.role === 'tool_call'" class="tool-call-block">
              <span>🔧 {{ msg.content }}</span>
            </div>
          </div>

          <!-- 实时思考过程（流式中） -->
          <div v-if="thinkingText && loading" class="thinking-block">
            <div class="thinking-label">💭 思考中...</div>
            <div class="thinking-content markdown-body" v-html="thinkingHtml"></div>
          </div>

          <!-- 流式输出中 -->
          <div v-if="streamingContent" class="chat-bubble assistant">
            <div class="bubble-label">AI</div>
            <div class="bubble-content markdown-body" v-html="streamingHtml"></div>
          </div>

          <!-- 危险SQL确认后的流式输出 -->
          <div v-if="streamingExecContent" class="chat-bubble assistant">
            <div class="bubble-label">AI</div>
            <div class="bubble-content markdown-body" v-html="streamingExecHtml"></div>
          </div>

          <div v-if="loading" style="color:#909399;font-size:13px;padding:4px 0;">AI 正在处理...</div>
        </div>

        <!-- 内联 SQL 确认区域 -->
        <SQLConfirmInline v-model="confirmVisible" :sql="confirmSQL" :operation-type="confirmOperationType"
          :risk-level="confirmRiskLevel" :description="confirmDescription" :table-name="confirmTableName"
          @confirm="handleConfirmExec" @cancel="handleConfirmCancel" />

        <!-- 多条 SQL 批量确认区域 -->
        <div v-if="pendingSQLList.length > 0" class="multi-sql-confirm">
          <div style="font-weight:600;margin-bottom:8px;font-size:14px;">
            检测到 {{ pendingSQLList.length }} 条需要确认的 SQL：
          </div>
          <div style="margin-bottom:8px;">
            <el-checkbox v-model="selectAllChecked" @change="handleSelectAllChange">
              全选
            </el-checkbox>
            <span style="font-size:12px;color:#909399;margin-left:8px;">
              已选择 {{ pendingSQLList.filter(i => i.selected).length }} 条
            </span>
          </div>
          <div v-for="(item, idx) in pendingSQLList" :key="idx" class="sql-confirm-item">
            <div class="sql-confirm-header">
              <el-checkbox v-model="item.selected" style="margin-right:8px;">
                <el-tag :type="item.riskLevel === 'high' ? 'danger' : 'warning'" size="small">
                  {{ item.riskLevel === 'high' ? '高危' : '中危' }} - {{ item.type }}
                </el-tag>
              </el-checkbox>
              <span v-if="item.tableName" style="font-size:12px;color:#909399;">表：{{ item.tableName }}</span>
            </div>
            <pre class="sql-pre"><code v-html="highlightSql(item.sql)" /></pre>
          </div>
          <div style="display:flex;gap:8px;margin-top:12px;justify-content:flex-end;">
            <el-button size="small" @click="handleCancelAllSQL">全部取消</el-button>
            <el-button size="small" type="danger" @click="handleConfirmSelectedSQL" :disabled="selectedSQLCount === 0">
              确认执行选中 ({{ selectedSQLCount }} 条)
            </el-button>
          </div>
        </div>

        <!-- 重试确认区域 -->
        <div v-if="showRetryConfirm" class="retry-confirm-block">
          <div style="color:#e6a23c;font-weight:600;margin-bottom:8px;">⚠️ 工具调用多次失败</div>
          <div style="font-size:13px;color:#606266;margin-bottom:12px;">{{ retryMessage }}</div>
          <div style="display:flex;gap:8px;">
            <el-button size="small" type="primary" @click="handleRetryContinue">继续尝试</el-button>
            <el-button size="small" @click="showRetryConfirm = false">放弃</el-button>
          </div>
        </div>

        <!-- 输入区域 -->
        <div class="input-area">
          <div class="input-label">
            <span>描述你的需求（数据查询 / 数据分析 / SQL 生成 / 数据导出 / 数据导入）</span>
            <div style="display: flex; gap: 0px;">
              <!-- 已上传的 Excel 文件信息 -->
            <div v-if="uploadedExcel" class="uploaded-file-info" style="margin-left: 12px;">
              <span>{{ uploadedExcel.name }} ({{ uploadedExcel.rows }} 行, {{ uploadedExcel.columns.length }} 列)</span>
              <el-button size="small" text type="danger" @click="clearUploadedExcel">✕</el-button>
            </div>
               <el-upload ref="excelUploadRef" :auto-upload="false" :show-file-list="false" style="margin-left: 12px;" accept=".xlsx,.xls"
                :on-change="handleExcelUpload" :disabled="excelUploading">
                <el-button class="toolbar-btn" size="small" title="上传 Excel 导入数据" :loading="excelUploading">
                  <el-icon v-if="!excelUploading"><Upload /></el-icon>
                </el-button>
              </el-upload>
              <el-popover placement="top" :width="380" trigger="click" v-model:visible="promptPopoverVisible"
                @show="loadPrompts()">
                <div class="prompt-popover-body">
                  <el-tabs v-model="activeTab" class="prompt-tabs">
                    <el-tab-pane name="mine">
                      <template #label>
                        <span style="display: inline-flex; align-items: center; gap: 6px; width: 100%;">
                          我的
                          <el-icon v-if="myPromptLoading" class="is-loading" size="14"><Loading /></el-icon>
                          <el-icon :size="10" style="top: -10px;" @click="handlePromptAdd"><Plus /></el-icon>
                        </span>
                      </template>
                      <div class="prompt-search-box">
                        <el-input
                          v-model="promptSearchKeyword"
                          size="small"
                          placeholder="按标题搜索"
                          clearable
                          v-if="myPromptsTotal > promptPageSize"
                          @input="handlePromptSearchInput"
                        >
                          <template #prefix>
                            <el-icon><Search /></el-icon>
                          </template>
                        </el-input>
                      </div>
                      <div class="prompt-list">
                        <div v-if="myPromptLoading && myPrompts.length === 0" style="text-align: center; padding: 10px;">
                          <el-icon class="is-loading"><Loading /></el-icon>
                        </div>
                        <div v-else-if="myPrompts.length === 0" class="prompt-empty">
                          {{ promptSearchKeyword ? '没有匹配的提示词' : '暂无提示词' }}
                        </div>
                        <div v-for="prompt in myPrompts" :key="prompt.id" class="prompt-item" @click.stop="handlePromptSendToAI(prompt.content, { connSchemas: prompt.connSchemas, tables: prompt.tables })">
                          <div class="prompt-item-info">
                            <div class="prompt-item-title">{{ prompt.title }}</div>
                            <div v-if="prompt.connSchemas && prompt.connSchemas.length > 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>
                              {{ prompt.connSchemas.length }} 个 Schema
                            </div>
                            <div v-else-if="prompt.connSchemas && prompt.connSchemas.length === 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>
                              {{ prompt.connSchemas[0].schema }}
                            </div>
                            <div v-if="prompt.isShared" class="prompt-item-sub">
                              <el-icon size="12"><Share /></el-icon>
                              {{ prompt.sharedByName || '他人分享' }}
                            </div>
                          </div>
                          <div class="prompt-item-actions">
                            <el-button text type="primary">
                              <el-icon v-if="!prompt.isShared" @click.stop="handlePromptEdit(prompt.id)" title="编辑"><Edit /></el-icon>
                            </el-button>
                            <el-popconfirm v-if="!prompt.isShared" title="确定要删除？" @confirm="handleDeletePrompt(prompt)">
                              <template #reference>
                                <el-button style="margin-left: -10px;" text type="danger" title="删除" @click.stop>
                                  <el-icon><Delete /></el-icon>
                                </el-button>
                              </template>
                            </el-popconfirm>
                          </div>
                        </div>
                      </div>
                      <div v-if="myPromptsTotal > promptPageSize" class="prompt-pagination">
                        <el-pagination
                          v-model:current-page="myPromptCurrentPage"
                          :page-size="promptPageSize"
                          :total="myPromptsTotal"
                          layout="prev, pager, next"
                          small
                          @current-change="handleMyPromptPageChange"
                        />
                      </div>
                    </el-tab-pane>
                    <el-tab-pane label="系统" name="system">
                      <div class="prompt-search-box">
                        <el-input
                          v-model="promptSearchKeyword"
                          size="small"
                          placeholder="按标题搜索"
                          clearable
                          v-if="systemPromptsTotal > promptPageSize"
                          @input="handlePromptSearchInput"
                        >
                          <template #prefix>
                            <el-icon><Search /></el-icon>
                          </template>
                        </el-input>
                      </div>
                      <div class="prompt-list">
                        <div v-if="systemPromptLoading && systemPrompts.length === 0" style="text-align: center; padding: 10px;">
                          <el-icon class="is-loading"><Loading /></el-icon>
                        </div>
                        <div v-else-if="systemPrompts.length === 0" class="prompt-empty">
                          {{ promptSearchKeyword ? '没有匹配的系统提示词' : '暂无系统提示词' }}
                        </div>
                        <div v-for="prompt in systemPrompts" title="点击发送给大模型" :key="prompt.id" class="prompt-item" @click.stop="handlePromptSendToAI(prompt.content, { connSchemas: prompt.connSchemas, tables: prompt.tables })">
                          <div class="prompt-item-info">
                            <div class="prompt-item-title">{{ prompt.title }}</div>
                            <div v-if="prompt.connSchemas && prompt.connSchemas.length > 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>
                              {{ prompt.connSchemas.length }} 个 Schema
                            </div>
                            <div v-else-if="prompt.connSchemas && prompt.connSchemas.length === 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>
                              {{ prompt.connSchemas[0].schema }}
                            </div>
                          </div>
                          <div class="prompt-item-actions">
                            <el-button size="small" text type="info" @click.stop="handleViewPromptDetail(prompt)" title="查看">
                              <el-icon><View /></el-icon>
                            </el-button>
                          </div>
                        </div>
                      </div>
                      <div v-if="systemPromptsTotal > promptPageSize" class="prompt-pagination">
                        <el-pagination
                          v-model:current-page="systemPromptCurrentPage"
                          :page-size="promptPageSize"
                          :total="systemPromptsTotal"
                          layout="prev, pager, next"
                          small
                          @current-change="handleSystemPromptPageChange"
                        />
                      </div>
                    </el-tab-pane>
                  </el-tabs>
                </div>
                <PromptEditDialog v-model="promptEditDialogVisible" :prompt-id="editingPromptId" :role-id="editingRoleId" @saved="handlePromptSaved" @send-to-AI="handleSendToAIFromDialog" />
                <el-dialog v-model="promptDetailVisible" :title="promptDetail?.title || '提示词详情'" width="800px" append-to-body>
                  <div v-if="promptDetail">
                    <div v-if="promptDetail.connSchemas && promptDetail.connSchemas.length" class="prompt-detail-meta">
                      <div class="prompt-detail-meta-label">关联 Schema</div>
                      <div class="prompt-detail-meta-tags">
                        <el-tag v-for="cs in promptDetail.connSchemas" :key="cs.connId + cs.schema" size="small" type="info">
                          <el-icon style="margin-right: 4px;"><Coin /></el-icon>{{ cs.schema }}
                        </el-tag>
                      </div>
                    </div>
                    <div v-if="promptDetail.tables && promptDetail.tables.length" class="prompt-detail-meta">
                      <div class="prompt-detail-meta-label">关联表</div>
                      <div class="prompt-detail-meta-tags">
                        <el-tooltip v-for="t in promptDetail.tables" :key="typeof t === 'string' ? t : t.name" :content="getTableComment(typeof t === 'string' ? t : t.name) || (typeof t === 'object' ? t.comment : '') || ''" :disabled="!(getTableComment(typeof t === 'string' ? t : t.name) || (typeof t === 'object' && t.comment))" placement="top">
                          <el-tag size="small">{{ typeof t === 'string' ? t : t.name }}</el-tag>
                        </el-tooltip>
                      </div>
                    </div>
                    <el-divider v-if="promptDetail.connSchemas?.length || promptDetail.tables?.length" style="margin: 12px 0;" />
                    <div class="prompt-detail-content markdown-body" v-html="renderMarkdown(promptDetail.content)"></div>
                  </div>
                  <template #footer>
                    <el-button @click="promptDetailVisible = false">关闭</el-button>
                    <el-button type="primary" @click="handleSendFromDetail">
                      <el-icon><Promotion /></el-icon>
                      发送给大模型
                    </el-button>
                  </template>
                </el-dialog>
                <template #reference>
                  <el-button class="toolbar-btn" circle size="small" title="常用提示词" style="margin-left: 12px;">
                    <el-icon><ChatLineRound /></el-icon>
                  </el-button>
                </template>
              </el-popover>
              <el-popover placement="top" :width="380" trigger="click" v-model:visible="sessionHistoryVisible" 
                @show="loadSessionList()">
                <div class="session-history-body">
                  <div class="session-search-box">
                    <el-input
                      v-model="sessionSearchKeyword"
                      size="small"
                      placeholder="搜索会话标题"
                      clearable
                      @input="handleSessionSearchInput"
                    >
                      <template #prefix>
                        <el-icon><Search /></el-icon>
                      </template>
                    </el-input>
                  </div>
                  <div class="session-list-scroll">
                    <el-empty v-if="loadingSessions && sessionList.length === 0" :description="'加载中...'" />
                    <el-empty v-else-if="sessionList.length === 0 && sessionSearchKeyword" description="没有匹配的会话" />
                    <el-empty v-else-if="sessionList.length === 0" description="暂无历史会话" />
                    <el-skeleton v-else-if="loadingSessions" :rows="4" animated />
                    <div v-else style="display: flex; flex-direction: column; gap: 8px;">
                      <div v-for="sess in sessionList" :key="sess.id" class="session-item">
                        <div class="session-content" @click="handleClickSession(sess.id)">
                          <div class="session-title">{{ sess.title || '未命名会话' }}</div>
                          <div class="session-time">
                            <el-icon>
                              <Clock />
                            </el-icon>
                            {{ formatDate(sess.createdAt) }}
                          </div>
                        </div>
                        <div class="session-actions">
                          <el-popconfirm title="确定要删除这个会话吗？" @confirm="deleteSession(sess.id)">
                            <template #reference>
                              <el-button type="danger" size="small" text @click.stop>
                                <el-icon>
                                  <Delete />
                                </el-icon>
                              </el-button>
                            </template>
                          </el-popconfirm>
                        </div>
                      </div>
                    </div>
                  </div>
                  <div v-if="sessionListTotal > sessionPageSize" class="session-pagination">
                    <el-pagination
                      v-model:current-page="sessionCurrentPage"
                      v-model:page-size="sessionPageSize"
                      :page-sizes="[5, 10, 20, 50]"
                      :total="sessionListTotal"
                      layout="prev, pager, next"
                      small
                      @current-change="handleSessionPageChange"
                      @size-change="handleSessionSizeChange"
                    />
                  </div>
                </div>
                <template #reference>
                  <el-button class="toolbar-btn" size="small" title="历史会话" style="margin-left: 12px;">
                    <el-icon>
                      <Document />
                    </el-icon>
                  </el-button>
                </template>
              </el-popover>
              <el-button class="toolbar-btn" :type="isRecording ? 'danger' : 'primary'" size="small"
                @click="toggleRecording" :title="isRecording ? '停止录音' : '开始录音'">
                <el-icon style="vertical-align: middle;">
                  <component :is="isRecording ? VideoPause : Microphone" />
                </el-icon>
              </el-button>
              <el-button class="toolbar-btn" size="small" @click="clearSession" title="清空并新建会话">
                <el-icon>
                  <Delete />
                </el-icon>
              </el-button>
            </div>
          </div>
          <div class="table-selector-row">
            <div class="table-selector-container" v-if="shouldShowSchemaSelector">
              <div class="selector-header">
                <label class="table-selector-label">数据库 / Schema</label>
                <span v-if="schemasLoading" class="selector-badge loading">加载中...</span>
                <span v-else-if="selectedSchemas.length" class="selector-badge">{{ selectedSchemas.length }} 已选</span>
              </div>
              <div v-if="schemasLoading" class="selector-skeleton">
                <div class="skeleton-shimmer"></div>
              </div>
              <el-tree-select v-else
                v-model="selectedSchemas"
                :data="processedConnList"
                :props="{ label: 'label', value: 'value', children: 'children', disabled: 'disabled' }"
                placeholder="搜索数据库 / Schema..."
                class="modern-tree-select"
                @change="handleSchemaChange"
                filterable
                multiple
                :check-on-click-node="true"
                collapse-tags
                collapse-tags-tooltip
              />
            </div>
            <div class="table-selector-container" :class="{ 'full-width': !shouldShowSchemaSelector }">
              <div class="selector-header">
                <label class="table-selector-label" style="padding-top: 2px;">相关表</label>
                <span v-if="tablesLoading" class="selector-badge loading">加载中...</span>
                <span v-else-if="selectedTables.length" class="selector-badge">{{ selectedTables.length }} 已选</span>
                <span v-else-if="tableList.length" class="selector-badge ready">{{ tableList.length }} 可用</span>
              </div>
              <div v-if="tablesLoading" class="selector-skeleton">
                <div class="skeleton-shimmer"></div>
              </div>
              <el-select v-else v-model="selectedTables" multiple filterable placeholder="搜索表名..." class="modern-select">
                <template #tag="{ data, deleteTag, selectDisabled }">
                  <el-tag
                    v-for="item in data.slice(0, 2)"
                    :key="item.value"
                    :closable="!selectDisabled && !item.isDisabled"
                    @close="deleteTag($event, item)"
                    size="small"
                    disable-transitions
                  >
                    <el-tooltip :content="getTableComment(item.currentLabel)" :disabled="!getTableComment(item.currentLabel)" placement="top">
                      <span>{{ item.currentLabel }}</span>
                    </el-tooltip>
                  </el-tag>
                  <el-tooltip v-if="data.length > 2" placement="bottom">
                    <template #content>
                      <div v-for="item in data.slice(2)" :key="'c-' + item.value" style="line-height: 2;">
                        {{ item.currentLabel }}<span v-if="getTableComment(item.currentLabel)" style="color: var(--el-text-color-secondary); margin-left: 6px;">{{ getTableComment(item.currentLabel) }}</span>
                      </div>
                    </template>
                    <el-tag size="small" type="info" disable-transitions>+ {{ data.length - 2 }}</el-tag>
                  </el-tooltip>
                </template>
                <el-option v-for="table in tableList" :key="table.name + (table.schema || '')"
                  :label="table.label || table.name"
                  :value="table.label || table.name">
                  <div class="table-option-content">
                    <span class="table-option-name">{{ table.name }}</span>
                    <span v-if="table.comment" class="table-option-comment">{{ table.comment }}</span>
                    <span v-if="table.schema && selectedSchemas.length > 1" class="table-option-schema">{{ table.schema }}</span>
                  </div>
                </el-option>
              </el-select>
            </div>
            <div class="table-selector-container model-selector-container">
              <div class="selector-header">
                <label class="table-selector-label" style="padding-top: 2px;">AI 模型</label>
                <span v-if="selectedModel" class="selector-badge ready">{{ selectedModelName }}</span>
              </div>
              <el-select v-model="selectedModel" filterable placeholder="选择模型..." class="modern-select">
                <el-option v-for="model in aiModelList" :key="model.id"
                  :label="model.model"
                  :value="model.id">
                  <div class="model-option-content">
                    <span class="model-option-name">{{ model.model }}</span>
                    <span v-if="model.provider" class="model-option-provider">{{ model.provider }}</span>
                  </div>
                </el-option>
              </el-select>
            </div>
          </div>

          <div class="input-action-row">
            <div style="flex:1;display:flex;flex-direction:column;gap:6px;">
              <div v-if="extractedSchemaHints.length > 0" class="schema-hints">
                <span class="schema-hints-label">检测到跨库引用：</span>
                <template v-for="hint in extractedSchemaHints" :key="hint.schema">
                  <el-tag :type="hint.validated ? 'success' : 'danger'" size="small" effect="plain">
                    {{ hint.schema }}.{{ hint.tables.join(', ') }}
                  </el-tag>
                </template>
                <span v-if="extractedSchemaHints.some(h => !h.validated)" class="schema-hints-warn">
                  {{ extractedSchemaHints.filter(h => !h.validated).map(h => h.schema).join(', ') }} 不在授权中
                </span>
              </div>
              <el-input v-model="question" type="textarea" :rows="5" placeholder="描述你想查询的内容，或使用语音录入... (Ctrl+Enter 发送)"
                :disabled="loading" @keydown.ctrl.enter="sendMessage" class="question-input" />
            </div>
            <div class="action-buttons">
              <el-button v-if="loading" type="danger" @click="stopGeneration"
                class="stop-btn" size="default">
                <el-icon>
                  <VideoPause />
                </el-icon>
              </el-button>
              <el-button v-else type="primary" :disabled="!question.trim() && !uploadedExcel" @click="sendMessage"
                class="send-btn" size="default">
                <el-icon>
                  <Promotion />
                </el-icon>
              </el-button>
              <el-button v-if="lastSql" type="success" @click="insertToEditor" title="将最后生成的 SQL 加入编辑器"
                class="insert-btn" size="default">
                <el-icon>
                  <DocumentAdd />
                </el-icon>
              </el-button>
              <router-link v-if="canUseClassicView" to="/classical" class="switch-view-link" title="经典视图">
                <el-icon>
                  <Switch />
                </el-icon>
                <span>经典视图</span>
              </router-link>
            </div>
          </div>
        </div>
      </div>
    </div>
    <div class="login-button-container">
        <div style="display: flex; flex-direction: column; gap: 8px; align-items: center;">
          <el-button circle size="small" @click="toggleTheme" :title="currentTheme === 'light' ? '切换到夜色模式' : '切换到日间模式'">
            <el-icon>
              <component :is="currentTheme === 'light' ? Moon : Sunny" />
            </el-icon>
          </el-button>
          <el-button v-if="(currentUser.isAdmin || !isRemote) && loginSucc" circle size="small" @click="openSystemManagement" title="系统管理">
            <el-icon>
              <Setting />
            </el-icon>
          </el-button>
          <el-button v-if="!loginSucc && isRemote" circle size="small" @click="showLoginDialog" title="登录">
            <el-icon>
              <User />
            </el-icon>
          </el-button>
          <el-button v-else circle size="small" @click="logout" title="退出登录">
            <el-icon>
              <SwitchButton />
            </el-icon>
          </el-button>
        </div>
    </div>
    </div>
    <LoginDialog ref="loginDialogRef" v-model="loginDialogVisible" @login-success="handleLoginSuccess" />
  </div>
  </el-config-provider>
</template>

<script setup>
import SQLConfirmInline from '@/components/ai/SQLConfirmInline.vue'
import PromptEditDialog from '@/components/ai/PromptEditDialog.vue'
import { preloadVditor } from '@/utils/vditorLoader'
import { usePromptEditDialog } from '@/components/ai/usePromptEditDialog'
import LoginDialog from '@/components/auth/LoginDialog.vue'
import http from '@/utils/httpProxy.js'
import { sanitizeError } from '@/utils/errorHandler.js'
import { analyzeSQL } from '@/utils/sqlRiskAssessment'
import { useTheme } from '@/utils/useTheme'
import { ChatLineRound, Clock, Coin, Delete, Document, DocumentAdd, Loading, Microphone, Moon, Plus, Promotion, Search, Setting, Share, Sunny, SwitchButton, Upload, User, VideoPause, View } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getMarkdownRenderer, getMermaid, getNextMermaidId, getHljs, preloadHeavyDeps, switchMermaidTheme, getMermaidSvgCache, clearMermaidSvgCache } from '@/utils/lazyDeps'
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, useTemplateRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

const route = useRoute()
const systemRoutes = ['/system-management', '/role-permission', '/classical']

const { currentTheme, toggleTheme } = useTheme()

watch(currentTheme, async (theme) => {
  await switchMermaidTheme(theme === 'dark' ? 'dark' : 'default')
  clearMermaidSvgCache()
  chatHistory.value.forEach(msg => {
    msg._renderedHtml = null
    msg._lastContent = null
  })
  nextTick(() => doRenderMermaidBlocks(false))
})

const apiBase = import.meta.env.VITE_API_URL || ''

let md = null

async function ensureMd() {
  if (md) return md
  md = await getMarkdownRenderer(apiBase)

  const defaultFenceRender = md.renderer.rules.fence || function (tokens, idx, options, env, self) {
    return self.renderToken(tokens, idx, options)
  }
  md.renderer.rules.fence = function (tokens, idx, options, env, self) {
    const token = tokens[idx]
    const info = token.info ? token.info.trim().toLowerCase() : ''
    if (info === 'mermaid') {
      const source = token.content.trim()
      const svgCache = getMermaidSvgCache()
      const id = getNextMermaidId()
      if (svgCache.has(source)) {
        // 缓存命中：用缓存的内部 HTML，但始终生成带 data-mermaid-id 的外层容器
        return `<div class="mermaid-container" data-mermaid-id="${id}" data-mermaid-processed="true">${svgCache.get(source)}</div>`
      }
      const escaped = token.content
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
      return `<div class="mermaid-container" data-mermaid-id="${id}" data-mermaid-source="${escaped}" data-mermaid-processed="false"><pre class="mermaid-source-preview"><code>📊 Mermaid\n${escaped}</code></pre></div>`
    }
    const lang = info || ''
    const rawCode = token.content
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
    let encodedContent = ''
    try {
      encodedContent = btoa(encodeURIComponent(rawCode))
    } catch (_) {}
    const defaultHtml = defaultFenceRender(tokens, idx, options, env, self)
    return `<div class="code-block-wrapper">` +
      `<div class="code-block-header">` +
        `<span class="code-block-lang">${lang}</span>` +
        `<button class="code-copy-btn" data-code="${encodedContent}" title="复制代码">复制</button>` +
      `</div>` +
      defaultHtml +
    `</div>`
  }

  return md
}

const props = defineProps({})

const emit = defineEmits([])

const question = ref('')
const selectedTables = ref([])
const loading = ref(false)
const abortController = ref(null)
const isRecording = ref(false)
const thinkingText = ref('')
const streamingContent = ref('')
const streamingExecContent = ref('')
const chatHistory = ref([])
const sessionId = ref('')
const lastSql = ref('')
const msgContainer = useTemplateRef('msgContainer')
let speechRecognition = null

const VISIBLE_MSG_LIMIT = 30
const showAllHistory = ref(false)
const visibleChatHistory = computed(() => {
  if (showAllHistory.value || chatHistory.value.length <= VISIBLE_MSG_LIMIT) {
    return { msgs: chatHistory.value, offset: 0 }
  }
  const offset = chatHistory.value.length - VISIBLE_MSG_LIMIT
  return { msgs: chatHistory.value.slice(offset), offset }
})
const hiddenMsgCount = computed(() => visibleChatHistory.value.offset)

const isRemote = ref(sessionStorage.getItem("isRemote") === "true")
const canUseClassicView = ref(false)

const { visible: promptEditDialogVisible, promptId: editingPromptId, roleId: editingRoleId, openDialog: openPromptEditDialog, closeDialog: closePromptEditDialog, triggerSaved: triggerPromptSaved, setSendToAIHandler, handleSendToAI: handleSendToAIFromDialog } = usePromptEditDialog()

const promptPopoverVisible = ref(false)

const myPrompts = ref([])
const systemPrompts = ref([])
const myPromptsTotal = ref(0)
const systemPromptsTotal = ref(0)
const myPromptLoading = ref(false)
const systemPromptLoading = ref(false)
const activeTab = ref('mine')
const promptDetailVisible = ref(false)
const promptDetail = ref(null)

const promptSearchKeyword = ref('')
const myPromptCurrentPage = ref(1)
const systemPromptCurrentPage = ref(1)
const promptPageSize = ref(10)
let promptSearchDebounceTimer = null

async function fetchPrompts(tab) {
  const params = {
    tab,
    page: tab === 'mine' ? myPromptCurrentPage.value : systemPromptCurrentPage.value,
    pageSize: promptPageSize.value,
  }
  const keyword = promptSearchKeyword.value.trim()
  if (keyword) {
    params.keyword = keyword
  }
  const targetLoading = tab === 'mine' ? myPromptLoading : systemPromptLoading
  targetLoading.value = true
  try {
    const resp = await http.get('/promptList', { params })
    const data = resp.data.data || {}
    const items = (data.items || []).map(p => ({
      ...p,
      isShared: p.createdBy !== p.currentUserId && !p.isRolePrompt,
    }))
    if (tab === 'mine') {
      myPrompts.value = items
      myPromptsTotal.value = data.total || 0
    } else {
      systemPrompts.value = items
      systemPromptsTotal.value = data.total || 0
    }
  } catch (e) {
    console.error(`加载${tab === 'mine' ? '我的' : '系统'}提示词失败:`, e)
    if (tab === 'mine') {
      myPrompts.value = []
      myPromptsTotal.value = 0
    } else {
      systemPrompts.value = []
      systemPromptsTotal.value = 0
    }
  } finally {
    targetLoading.value = false
  }
}

function loadPrompts() {
  promptSearchKeyword.value = ''
  myPromptCurrentPage.value = 1
  systemPromptCurrentPage.value = 1
  return Promise.all([fetchPrompts('mine'), fetchPrompts('system')])
}

function refetchPrompts() {
  return Promise.all([fetchPrompts('mine'), fetchPrompts('system')])
}

function handlePromptSearchInput() {
  myPromptCurrentPage.value = 1
  systemPromptCurrentPage.value = 1
  if (promptSearchDebounceTimer) {
    clearTimeout(promptSearchDebounceTimer)
  }
  promptSearchDebounceTimer = setTimeout(() => {
    promptSearchDebounceTimer = null
    refetchPrompts()
  }, 300)
}

function handleMyPromptPageChange(page) {
  myPromptCurrentPage.value = page
  fetchPrompts('mine')
}

function handleSystemPromptPageChange(page) {
  systemPromptCurrentPage.value = page
  fetchPrompts('system')
}

const showLoginBtn = ref(true)
const loginDialogVisible = ref(false)
const loginForm = ref({ name: "", password: "" })
const loginName = ref()
const loginFormRef = useTemplateRef('loginFormRef')
function parseCurrentUser() {
  try {
    const stored = sessionStorage.getItem("currentUser")
    return stored ? JSON.parse(stored) : { id: "", name: "", isAdmin: false }
  } catch {
    return { id: "", name: "", isAdmin: false }
  }
}

const currentUser = ref(parseCurrentUser())
const loginSucc = ref(!!sessionStorage.getItem("authentication"))
const logining = ref(false)

const router = useRouter()

const loginRules = reactive({
  name: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
  ],
})

// 数据库连接配置 - 多 schema 选择模式
const selectedSchemas = ref([])
const tableList = ref([])
const connList = ref([])
const connSchemaList = ref([])
const schemasLoading = ref(false)
const tablesLoading = ref(false)

// AI 模型配置
const aiModelList = ref([])
const selectedModel = ref('')
const modelLoading = ref(false)

// 计算属性：处理后的 schema 级树结构
const processedConnList = computed(() => {
  return buildSchemaTree(connSchemaList.value)
})

// 计算属性：是否显示 schema 选择器（单连接单 schema 时隐藏）
const shouldShowSchemaSelector = computed(() => {
  // 加载中时显示
  if (schemasLoading.value) return true
  // 多个连接时显示
  if (connSchemaList.value.length > 1) return true
  // 单个连接但有多个 schema 时显示
  if (connSchemaList.value.length === 1) {
    const conn = connSchemaList.value[0]
    const schemaCount = (conn.schemas || []).length
    if (schemaCount > 1) return true
  }
  return false
})

// 将 [{connId, name, dbSchema, dirName, schemas: [{name}]}] 转为 el-tree-select 支持的 schema 级树
function buildSchemaTree(rawList) {
  const dirMap = new Map()
  const noDir = []

  for (const item of rawList) {
    const schemas = item.schemas || []
    const dbType = item.dbType || ''
    // 连接不可用时，创建禁用的叶子节点
    if (item.available === false) {
      const node = {
        label: item.name,
        value: item.connId + '::',
        connId: item.connId,
        schemaName: '',
        disabled: true,
        isSchemaLeaf: false,
        dbType,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(node)
      } else {
        noDir.push(node)
      }
      continue
    }
    // 如果连接只有一个 schema，折叠到连接名下
    if (schemas.length <= 1) {
      const singleSchema = schemas.length === 1 ? schemas[0].name : (item.dbSchema || '')
      const node = {
        label: item.name,
        value: item.connId + '::' + singleSchema,
        connId: item.connId,
        schemaName: singleSchema,
        disabled: false,
        isSchemaLeaf: true,
        dbType,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(node)
      } else {
        noDir.push(node)
      }
    } else {
      // 多个 schema：连接作为目录，schema 作为可选子节点
      const schemaChildren = schemas.map(s => ({
        label: s.name,
        value: item.connId + '::' + s.name,
        connId: item.connId,
        schemaName: s.name,
        disabled: false,
        isSchemaLeaf: true,
        dbType,
      }))
      const connNode = {
        label: item.name,
        value: '__conn__' + item.connId,
        disabled: true,
        children: schemaChildren,
        dbType,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(connNode)
      } else {
        noDir.push(connNode)
      }
    }
  }

  const tree = []
  for (const [dirName, children] of dirMap) {
    tree.push({ label: dirName, value: '__dir__' + dirName, disabled: true, children })
  }
  tree.push(...noDir)
  return tree
}

async function loadConnList() {
  schemasLoading.value = true
  const auth = sessionStorage.getItem('authentication') || ''
  const apiBase = import.meta.env.VITE_API_URL || ''

  try {
    const resp = await fetch(apiBase + '/listUserConnSchemasStream', {
      headers: { 'Authorization': auth }
    })

    if (!resp.ok) {
      if (resp.status === 401) {
        handleSessionExpired()
        return
      }
      throw new Error('HTTP ' + resp.status)
    }

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    const rawList = []

    while (true) {
      const { done, value } = await reader.read()
      if (value && value.length > 0) {
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop()

        for (const line of lines) {
          if (!line.startsWith('data:')) continue
          const data = line.slice(5).trim()
          if (!data) continue
          if (data === '"ok"' || data === '"empty"') continue

          try {
            const item = JSON.parse(data)
            if (item.connId) {
              rawList.push(item)
              connSchemaList.value = [...rawList]
              connList.value = buildSchemaTree(connSchemaList.value)
              if (schemasLoading.value) {
                schemasLoading.value = false
              }
            }
          } catch (_) {}
        }
      }
      if (done) break
    }

    if (connList.value.length > 0 && selectedSchemas.value.length === 0) {
      const allSchemaValues = connList.value.flatMap(node => {
        if (node.children && node.children.length > 0) {
          return node.children.filter(c => !c.disabled).map(c => c.value)
        }
        if (!node.disabled && node.isSchemaLeaf) {
          return [node.value]
        }
        return []
      })
      if (allSchemaValues.length === 1) {
        selectedSchemas.value = allSchemaValues
        loadTableListForSchemas()
      }
    }
  } catch (e) {
    if (e.response && e.response.status === 401) {
      handleSessionExpired()
      return
    }
    console.error('加载连接列表失败:', e)
  } finally {
    schemasLoading.value = false
  }
}

function parseSchemaValue(value) {
  const idx = value.indexOf('::')
  if (idx === -1) return null
  return { connId: value.substring(0, idx), schema: value.substring(idx + 2) }
}

function getTableComment(value) {
  const found = tableList.value.find(t => (t.label || t.name) === value)
  return found?.comment || ''
}

async function loadTableListForSchemas() {
  tablesLoading.value = true
  if (selectedSchemas.value.length === 0) {
    tableList.value = []
    tablesLoading.value = false
    return
  }
  try {
    const schemaRefs = selectedSchemas.value
      .map(v => parseSchemaValue(v))
      .filter(Boolean)

    if (schemaRefs.length === 0) {
      tableList.value = []
      tablesLoading.value = false
      return
    }

    const resp = await http.get('/listTableNames', {
      params: { schemas: JSON.stringify(schemaRefs) }
    })
    const tables = resp.data.data || []
    const allTables = tables.map(t => {
      const hasSchema = t.schema && selectedSchemas.value.length > 1
      return {
        name: t.name,
        comment: t.comment || '',
        schema: t.schema || '',
        label: hasSchema ? t.schema + '.' + t.name : t.name,
      }
    })
    tableList.value = allTables

    if (selectedTables.value.length > 0) {
      const newValues = allTables.map(t => t.label || t.name)
      selectedTables.value = selectedTables.value.filter(name => newValues.includes(name))
    }
  } catch (e) {
    tableList.value = []
    if (e.response && e.response.status === 401) {
      handleSessionExpired()
    }
  } finally {
    tablesLoading.value = false
  }
}

async function loadTableList(connId, schema) {
  tablesLoading.value = true
  try {
    if (!connId) {
      tableList.value = []
      tablesLoading.value = false
      return
    }
    const resp = await http.get('/listTableNames', {
      params: { connId, schema: schema || '' }
    })
    const newTableList = resp.data.data || []

    if (selectedTables.value.length > 0) {
      const newNames = newTableList.map(t => typeof t === 'string' ? t : t.name)
      selectedTables.value = selectedTables.value.filter(name => newNames.includes(name))
    }

    tableList.value = newTableList.map(t => typeof t === 'string' ? { name: t, comment: '', schema: '' } : t)
  } catch (e) {
    console.error('加载表列表失败:', e)
    tableList.value = []
  } finally {
    tablesLoading.value = false
  }
}

function handleSchemaChange() {
  loadTableListForSchemas()
}

// 从 selectedSchemas 获取主连接的 connId（向后兼容）
function getPrimaryConnId() {
  if (selectedSchemas.value.length > 0) {
    const parsed = parseSchemaValue(selectedSchemas.value[0])
    if (parsed) return parsed.connId
  }
  return ''
}

// 构建发送给后端的 schema 引用数组
function buildRequestSchemas() {
  return selectedSchemas.value
    .map(v => parseSchemaValue(v))
    .filter(p => p && p.schema !== '')
    .map(p => ({ connId: p.connId, schema: p.schema }))
}

// AI 模型相关
function loadModelList() {
  modelLoading.value = true
  http.get("/system/config/ai/models").then((resp) => {
    if (resp.data && resp.data.data) {
      const data = resp.data.data
      if (data.aiModelList && Array.isArray(data.aiModelList) && data.aiModelList.length > 0) {
        aiModelList.value = data.aiModelList
        selectedModel.value = data.selectedModelId || data.aiModelList[0].id
      } else {
        aiModelList.value = []
        selectedModel.value = ''
      }
    }
  }).catch(() => {
    aiModelList.value = []
    selectedModel.value = ''
  }).finally(() => {
    modelLoading.value = false
  })
}

const selectedModelName = computed(() => {
  if (!selectedModel.value || aiModelList.value.length === 0) return ''
  const model = aiModelList.value.find(m => m.id === selectedModel.value)
  return model ? model.model : ''
})

// 从用户输入中自动提取 schema.表名 引用（数据权限验证辅助）
const extractedSchemaHints = computed(() => {
  const text = question.value || ''
  const hints = []
  // 匹配 schema.table 模式，排除 URL（http/https）、文件路径、版本号
  const regex = /(?:^|[^/\w])([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)(?=[^a-zA-Z0-9_]|$)/g
  let match
  while ((match = regex.exec(text)) !== null) {
    const schemaName = match[1]
    const tableName = match[2]
    // 排除常见非数据关键词
    const excluded = ['www', 'http', 'https', 'ftp', 'com', 'org', 'net', 'io', 'cn', 'js', 'ts', 'vue', 'py', 'go', 'java']
    if (excluded.includes(schemaName.toLowerCase()) || excluded.includes(tableName.toLowerCase())) {
      continue
    }
    const existing = hints.find(h => h.schema === schemaName)
    if (existing) {
      if (!existing.tables.includes(tableName)) {
        existing.tables.push(tableName)
      }
    } else {
      // 验证 schema 是否在已授权连接中存在
      const schemaExists = connSchemaList.value.some(c =>
        (c.schemas || []).some(s => s.name === schemaName) ||
        (c.dbSchema === schemaName)
      )
      hints.push({ schema: schemaName, tables: [tableName], validated: schemaExists })
    }
  }
  return hints
})

function validateExtractedSchemas() {
  const hints = extractedSchemaHints.value
  const unvalidated = hints.filter(h => !h.validated)
  if (unvalidated.length > 0) {
    const names = unvalidated.map(h => h.schema).join(', ')
    ElMessage.warning(`Schema [${names}] 不在您授权的连接中，将无法访问相关表`)
    return false
  }
  return true
}

// 历史会话相关
const sessionHistoryVisible = ref(false)
const sessionList = ref([])
const sessionListTotal = ref(0)
const loadingSessions = ref(false)
const sessionSearchKeyword = ref('')
const sessionCurrentPage = ref(1)
const sessionPageSize = ref(10)

// 用于记录已经渲染过的链接，避免重复处理
const processedLinks = new Set()

// SQL 确认相关
const confirmVisible = ref(false)
const confirmSQL = ref('')
const confirmInterruptIds = ref([])
const confirmCheckPointId = ref('')
const confirmOperationType = ref('SELECT')
const confirmRiskLevel = ref('low')
const confirmDescription = ref('')
const confirmTableName = ref('')
let hasShownConfirm = false  // 防止重复弹出

// 多条 SQL 批量确认
const pendingSQLList = ref([])
const selectAllChecked = ref(false)

// 计算选中的SQL数量
const selectedSQLCount = computed(() => {
  return pendingSQLList.value.filter(item => item.selected).length
})

// 全选/取消全选
function handleSelectAllChange(val) {
  pendingSQLList.value.forEach(item => {
    item.selected = val
  })
}

// 重试确认
const showRetryConfirm = ref(false)
const retryMessage = ref('')
const lastQuestion = ref('')

// Excel 上传
const uploadedExcel = ref(null) // { fileId, name, columns, rows, preview }
const excelUploadRef = useTemplateRef('excelUploadRef')
const excelUploading = ref(false)

const mdReady = ref(false)
const hljsReady = ref(false)
let hljsLib = null

// 当 markdown-it 加载完成后，强制清除所有消息的渲染缓存并重新渲染
// 这确保了在 md 加载前收到的消息能正确渲染 mermaid 图表
watch(mdReady, (ready) => {
  if (ready && chatHistory.value.length > 0) {
    // 清除缓存，强制下次渲染时使用 markdown-it
    chatHistory.value.forEach(msg => {
      if (msg._renderedWithMd === false) {
        msg._renderedHtml = null
        msg._lastContent = null
      }
    })
    // 触发 Vue 重新渲染后检查 mermaid
    nextTick(() => {
      doRenderMermaidBlocks(false)
    })
  }
})

async function initHeavyDeps() {
  const [, hljsResult] = await Promise.all([
    ensureMd().then(() => { mdReady.value = true }),
    getHljs().then(h => { hljsLib = h; hljsReady.value = true }),
  ])
}

function highlightSql(text) {
  if (!text) return ''
  void hljsReady.value
  if (hljsLib) {
    try {
      return hljsLib.highlight(text, { language: 'sql' }).value
    } catch {
      return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    }
  }
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// 自动检测未被 code fence 包裹的 mermaid 代码并包裹为 ```mermaid ... ```
// 这解决了 AI 有时不用 code fence 直接输出 mermaid 代码的问题
function autoWrapMermaidCode(text) {
  // 如果已经有 ```mermaid 包裹，不处理
  if (/```mermaid/i.test(text)) return text
  
  // mermaid 图表类型关键字（必须在行首或段落首）
  const mermaidKeywords = /^(graph\s+(TD|TB|BT|RL|LR)|flowchart\s+(TD|TB|BT|RL|LR)|sequenceDiagram|classDiagram|stateDiagram|erDiagram|gantt|pie|gitGraph|journey|mindmap|timeline|quadrantChart|sankey|xychart|block-beta)/m
  
  if (!mermaidKeywords.test(text)) return text
  
  // 检测 mermaid 代码块的范围
  const match = text.match(mermaidKeywords)
  if (!match) return text
  
  const startIdx = match.index
  const before = text.substring(0, startIdx).trimEnd()
  const afterStart = text.substring(startIdx)
  
  // 尝试找到 mermaid 代码的结束位置：
  // mermaid 代码通常以连续的缩进行组成，遇到空行后的非缩进非 mermaid 语法行即结束
  const lines = afterStart.split('\n')
  let endLineIdx = lines.length
  let foundEmptyLine = false
  
  for (let i = 1; i < lines.length; i++) {
    const line = lines[i]
    const trimmedLine = line.trim()
    
    if (trimmedLine === '') {
      foundEmptyLine = true
      continue
    }
    
    // 在空行之后，如果遇到明显不是 mermaid 语法的行，认为 mermaid 结束
    if (foundEmptyLine) {
      // mermaid 语法行通常以这些开头：缩进、style、classDef、click、linkStyle、subgraph、end、%%
      const isMermaidLine = /^\s+/.test(line) || 
        /^(style|classDef|click|linkStyle|subgraph|end|%%|class\s)/.test(trimmedLine)
      if (!isMermaidLine) {
        endLineIdx = i
        break
      }
      foundEmptyLine = false
    }
  }
  
  const mermaidContent = lines.slice(0, endLineIdx).join('\n').trimEnd()
  const after = lines.slice(endLineIdx).join('\n').trimStart()
  
  let result = ''
  if (before) {
    result = before + '\n\n'
  }
  result += '```mermaid\n' + mermaidContent + '\n```'
  if (after) {
    result += '\n\n' + after
  }
  return result
}

function preprocessMarkdown(text) {
  if (!text) return ''
  
  text = autoWrapMermaidCode(text)
  
  const codeBlocks = []
  let processed = text.replace(/(```[\s\S]*?```|`[^`\n]+`)/g, (match) => {
    const placeholder = `\x00CB${codeBlocks.length}\x00`
    codeBlocks.push(match)
    return placeholder
  })
  processed = processed.replace(/\$\\(?:text|textbf|textit)\{([^}]+)\}\$/g, (match, inner) => {
    return inner
  })
  processed = processed.replace(/\$\\(?:bm|mathit|mathrm|mathsf|mathtt)\{([^}]+)\}\$/g, (match, inner) => {
    return inner
  })
  processed = processed.replace(/\*\*\[([^\]]+)\]\(([^)]+)\)\*\*/g, '[$1]($2)')
  processed = processed.replace(/`((\/|\.\/)[^`\s]+\.(xlsx|csv|pdf|txt|zip|json|md))`/g, (match, path) => {
    const filename = path.substring(path.lastIndexOf('/') + 1)
    return `[${filename}](${path})`
  })
  processed = processed.replace(/\[([^\]]+)\]\(([^)]*)\)/g, (match, linkText, url) => {
    if (!url || url.length === 0) {
      return match
    }
    let fullUrl = url
    if (url.startsWith('/') && !url.startsWith('//')) {
      fullUrl = apiBase + url
    }
    let exportAttr = ''
    if (fullUrl && fullUrl.includes('/exports/')) {
      exportAttr = ' data-export-link="true"'
    }
    return `<a href="${fullUrl}" target="_blank" rel="noopener noreferrer"${exportAttr}>${linkText}</a>`
  })
  processed = processed.replace(/\x00CB(\d+)\x00/g, (_, i) => {
    const block = codeBlocks[parseInt(i)]
    if (block.startsWith('```')) return block
    const inner = block.slice(1, -1)
    if (!inner.includes('$')) return block
    const mathRegex = /\$([^$\s](?:[^$]*[^$\s])?)\$/g
    const segments = []
    let lastIndex = 0
    let m
    while ((m = mathRegex.exec(inner)) !== null) {
      const before = inner.substring(lastIndex, m.index)
      if (before.trim()) segments.push('`' + before.trim() + '`')
      segments.push(m[0])
      lastIndex = m.index + m[0].length
    }
    const after = inner.substring(lastIndex)
    if (after.trim()) segments.push('`' + after.trim() + '`')
    if (segments.length <= 1) return block
    const hasIdentifier = segments.some(s => s.startsWith('`') && /[a-zA-Z\u4e00-\u9fff]/.test(s))
    if (!hasIdentifier) return block
    return segments.join(' ')
  })
  return processed
}

function renderMarkdown(text) {
  if (!text) return ''
  void mdReady.value
  if (!md) {
    return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\n/g, '<br>')
  }
  try {
    const processed = preprocessMarkdown(text)
    return md.render(processed)
  } catch (e) {
    console.error('Markdown parse error:', e)
    return text
  }
}

// 缓存版本：用于历史消息，避免重复调用 renderMarkdown 导致 mermaid ID 变化
function getCachedHtml(msg) {
  if (!msg._renderedHtml || msg._lastContent !== msg.content || (mdReady.value && !msg._renderedWithMd)) {
    msg._renderedHtml = renderMarkdown(msg.content)
    msg._lastContent = msg.content
    msg._renderedWithMd = mdReady.value
  }
  return msg._renderedHtml
}

function buildMermaidInnerHtml(svg, source) {
  const escapedSource = source.replace(/</g, '&lt;').replace(/>/g, '&gt;')
  return `<div class="mermaid-content-wrapper">` +
    `<div class="mermaid-svg-wrap" data-scale="1" data-translate-x="0" data-translate-y="0">${svg}</div>` +
    `<pre class="mermaid-source-preview" style="display:none;"><code>${escapedSource}</code></pre>` +
  `</div>` +
  `<div class="mermaid-resize-handle" title="拖拽调整高度">` +
    `<span class="mermaid-resize-dots">⋯</span>` +
  `</div>` +
  `<div class="mermaid-toolbar">` +
    `<button class="mermaid-tb-btn" data-action="zoom-out" title="缩小">` +
      `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="8" y1="11" x2="14" y2="11"/></svg>` +
    `</button>` +
    `<button class="mermaid-tb-btn" data-action="zoom-reset" title="重置">100%</button>` +
    `<button class="mermaid-tb-btn" data-action="zoom-in" title="放大">` +
      `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="11" y1="8" x2="11" y2="14"/><line x1="8" y1="11" x2="14" y2="11"/></svg>` +
    `</button>` +
    `<button class="mermaid-tb-btn" data-action="toggle-source" title="源码/图表">` +
      `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>` +
    `</button>` +
    `<button class="mermaid-tb-btn" data-action="copy-source" title="复制源码">` +
      `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>` +
    `</button>` +
     `<button class="mermaid-tb-btn" data-action="fullscreen" title="全屏">` +
      `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/></svg>` +
    `</button>` +
  `</div>`
}

async function doRenderMermaidBlocks(scrollAfter = true) {
  await nextTick()
  // 查找所有未处理的 mermaid 容器（不限于可见的，避免因 timing 问题遗漏）
  const containers = document.querySelectorAll('.mermaid-container[data-mermaid-processed="false"]')
  if (containers.length === 0) return

  const CONCURRENCY = 3
  const toRender = [...containers]
  const batches = []
  for (let i = 0; i < toRender.length; i += CONCURRENCY) {
    batches.push(toRender.slice(i, i + CONCURRENCY))
  }

  for (const batch of batches) {
    await Promise.allSettled(batch.map(el => renderSingleMermaid(el)))
  }

  if (scrollAfter && toRender.length > 0) {
    await nextTick()
    if (msgContainer.value) {
      msgContainer.value.scrollTop = msgContainer.value.scrollHeight
    }
  }
}

async function renderSingleMermaid(el) {
  const id = el.getAttribute('data-mermaid-id')
  const source = el.getAttribute('data-mermaid-source')
    ?.replace(/&quot;/g, '"')
    .replace(/&gt;/g, '>')
    .replace(/&lt;/g, '<')
    .replace(/&amp;/g, '&')
  if (!id || !source) return
  const trimmed = source.trim()
  if (!trimmed || trimmed.length < 5) return

  // 标记为已处理，防止重复渲染
  el.setAttribute('data-mermaid-processed', 'true')
  
  // 带重试的渲染（mermaid 内部动态 import 可能因缓存过期失败）
  const MAX_RETRIES = 2
  let lastError = null
  
  for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
    try {
      const mermaidLib = await getMermaid()
      const renderId = 'mermaid-r-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 6)
      const { svg } = await mermaidLib.render(renderId, trimmed)
      const innerHtml = buildMermaidInnerHtml(svg, source)
      el.innerHTML = innerHtml
      const svgCache = getMermaidSvgCache()
      svgCache.set(trimmed, innerHtml)
      // 关键修复：SVG 渲染成功后，清除包含此 mermaid 源码的消息的 HTML 缓存
      invalidateMermaidMsgCache(trimmed)
      return // 成功，退出
    } catch (e) {
      lastError = e
      // 如果是动态 import 失败（网络/缓存问题），等待后重试
      const isImportError = e && (
        String(e.message || '').includes('Failed to fetch dynamically imported module') ||
        String(e.message || '').includes('Importing a module script failed') ||
        String(e.message || '').includes('error loading dynamically imported module')
      )
      if (isImportError && attempt < MAX_RETRIES) {
        console.warn(`Mermaid dynamic import failed (attempt ${attempt + 1}), retrying...`)
        await new Promise(r => setTimeout(r, 500 * (attempt + 1)))
        continue
      }
      break
    }
  }
  
  // 所有重试都失败
  console.warn('Mermaid render error for source:', trimmed.substring(0, 100), lastError)
  const escapedSource = source.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  el.innerHTML = `<pre class="mermaid-error"><code>${escapedSource}</code></pre><div class="mermaid-error-hint">⚠️ 图表渲染失败，请刷新页面重试</div>`
}

// 清除包含指定 mermaid 源码的消息的渲染缓存
function invalidateMermaidMsgCache(mermaidSource) {
  for (const msg of chatHistory.value) {
    if (msg.role === 'assistant' && msg.content && msg.content.includes('mermaid')) {
      // 清除缓存，下次渲染时 getCachedHtml 会重新调用 renderMarkdown
      // renderMarkdown 的 fence renderer 会命中 SVG 缓存
      msg._renderedHtml = null
      msg._lastContent = null
    }
  }
}

// ── MutationObserver：自动检测新插入的 mermaid 容器并渲染 ──
// 这是最可靠的方式，彻底消除所有 timing 问题
let mermaidObserver = null
let mermaidObserverTimer = null

function setupMermaidObserver() {
  if (mermaidObserver) return
  mermaidObserver = new MutationObserver((mutations) => {
    // 检查是否有新的未处理 mermaid 容器被插入
    let hasNew = false
    for (const mutation of mutations) {
      if (mutation.type === 'childList') {
        for (const node of mutation.addedNodes) {
          if (node.nodeType !== 1) continue
          if (node.matches && node.matches('.mermaid-container[data-mermaid-processed="false"]')) {
            hasNew = true
            break
          }
          if (node.querySelector && node.querySelector('.mermaid-container[data-mermaid-processed="false"]')) {
            hasNew = true
            break
          }
        }
      }
      // 也检查 innerHTML 变化（v-html 更新时触发）
      if (mutation.type === 'childList' && mutation.target.querySelector) {
        if (mutation.target.querySelector('.mermaid-container[data-mermaid-processed="false"]')) {
          hasNew = true
        }
      }
      if (hasNew) break
    }
    if (hasNew) {
      // 防抖：短时间内多次 DOM 变化只触发一次渲染
      if (mermaidObserverTimer) clearTimeout(mermaidObserverTimer)
      mermaidObserverTimer = setTimeout(() => {
        doRenderMermaidBlocks(true)
        mermaidObserverTimer = null
      }, 100)
    }
    // 每次 DOM 变化后重新应用自定义高度（Vue 重新渲染会重置 inline style）
    if (mermaidCustomHeights.size > 0) {
      reapplyAllMermaidCustomHeights()
    }
  })
  // 观察整个 document.body，确保无论 mermaid 容器出现在哪里都能被检测到
  mermaidObserver.observe(document.body, { childList: true, subtree: true })
}

function teardownMermaidObserver() {
  if (mermaidObserver) {
    mermaidObserver.disconnect()
    mermaidObserver = null
  }
  if (mermaidObserverTimer) {
    clearTimeout(mermaidObserverTimer)
    mermaidObserverTimer = null
  }
}

function scrollToBottom() {
  nextTick(() => {
    if (msgContainer.value) {
      msgContainer.value.scrollTop = msgContainer.value.scrollHeight
    }
  })
}

function countClosedMermaidBlocks(content) {
  let closedCount = 0
  let searchFrom = 0
  while (true) {
    const startIdx = content.indexOf('```mermaid', searchFrom)
    if (startIdx === -1) break
    const afterStart = startIdx + '```mermaid'.length
    const lineEnd = content.indexOf('\n', afterStart)
    if (lineEnd === -1) break
    const closeIdx = content.indexOf('\n```', lineEnd)
    if (closeIdx === -1) break
    closedCount++
    searchFrom = closeIdx + 4
  }
  return closedCount
}

const streamingHtml = ref('')
const streamingExecHtml = ref('')
const thinkingHtml = ref('')
let streamingRenderTimer = null
let streamingExecRenderTimer = null
let thinkingRenderTimer = null
let lastStreamingMermaidCount = 0
let lastStreamingExecMermaidCount = 0
let lastThinkingMermaidCount = 0

watch(streamingContent, () => {
  if (streamingRenderTimer) clearTimeout(streamingRenderTimer)
  streamingRenderTimer = setTimeout(() => {
    streamingHtml.value = renderMarkdown(streamingContent.value)
    nextTick(() => {
      const content = streamingContent.value
      if (content && content.includes('```mermaid')) {
        const closedCount = countClosedMermaidBlocks(content)
        if (closedCount > lastStreamingMermaidCount) {
          lastStreamingMermaidCount = closedCount
          doRenderMermaidBlocks(true)
        }
      }
      // 即使 countClosedMermaidBlocks 未检测到新块，也检查 DOM 中是否有未渲染的容器
      // 这处理了 autoWrapMermaidCode 自动包裹的情况
      const unprocessed = document.querySelectorAll('.mermaid-container[data-mermaid-processed="false"]')
      if (unprocessed.length > 0) {
        doRenderMermaidBlocks(true)
      }
    })
    streamingRenderTimer = null
  }, 50)
}, { immediate: true })

watch(streamingExecContent, () => {
  if (streamingExecRenderTimer) clearTimeout(streamingExecRenderTimer)
  streamingExecRenderTimer = setTimeout(() => {
    streamingExecHtml.value = renderMarkdown(streamingExecContent.value)
    nextTick(() => {
      const content = streamingExecContent.value
      if (content && content.includes('```mermaid')) {
        const closedCount = countClosedMermaidBlocks(content)
        if (closedCount > lastStreamingExecMermaidCount) {
          lastStreamingExecMermaidCount = closedCount
          doRenderMermaidBlocks(true)
        }
      }
    })
    streamingExecRenderTimer = null
  }, 50)
}, { immediate: true })

watch(thinkingText, () => {
  if (thinkingRenderTimer) clearTimeout(thinkingRenderTimer)
  thinkingRenderTimer = setTimeout(() => {
    thinkingHtml.value = renderMarkdown(thinkingText.value)
    thinkingRenderTimer = null
  }, 50)
}, { immediate: true })

// 流式输出中检测已完成的 mermaid 代码块并立即渲染
let lastRenderedMermaidCount = 0
let mermaidRenderTimer = null

function tryRenderStreamingMermaid() {
  const content = streamingContent.value
  if (!content.includes('```mermaid')) return

  const closedCount = countClosedMermaidBlocks(content)

  if (closedCount > lastRenderedMermaidCount) {
    lastRenderedMermaidCount = closedCount
    lastStreamingMermaidCount = closedCount
    if (mermaidRenderTimer) clearTimeout(mermaidRenderTimer)
    mermaidRenderTimer = setTimeout(() => {
      doRenderMermaidBlocks(true)
      mermaidRenderTimer = null
    }, 100)
  }
}

function toggleThinking(msg) {
  msg.collapsed = !msg.collapsed
  if (!msg.collapsed) {
    nextTick(() => doRenderMermaidBlocks(false))
  }
}

const mermaidDragState = {
  isDragging: false,
  startX: 0,
  startY: 0,
  startTx: 0,
  startTy: 0,
  activeContainer: null,
}

function updateMermaidWrapTransform(wrap) {
  const s = parseFloat(wrap.dataset.scale || 1)
  const tx = parseFloat(wrap.dataset.translateX || 0)
  const ty = parseFloat(wrap.dataset.translateY || 0)
  wrap.style.transform = `translate(${tx}px, ${ty}px) scale(${s})`
}

function handleMermaidWheel(e) {
  if (!e.ctrlKey) return
  const container = e.target.closest('.mermaid-container')
  if (!container) return
  e.preventDefault()
  e.stopPropagation()

  const wrap = container.querySelector('.mermaid-svg-wrap')
  if (!wrap) return

  const oldScale = parseFloat(wrap.dataset.scale || 1)
  const delta = e.deltaY > 0 ? -0.1 : 0.1
  const newScale = Math.min(5, Math.max(0.25, +(oldScale + delta).toFixed(2)))
  if (newScale === oldScale) return

  const cw = container.querySelector('.mermaid-content-wrapper')
  const rect = cw ? cw.getBoundingClientRect() : container.getBoundingClientRect()
  const mx = rect.width / 2
  const my = rect.height / 2

  const oldTx = parseFloat(wrap.dataset.translateX || 0)
  const oldTy = parseFloat(wrap.dataset.translateY || 0)

  const ratio = newScale / oldScale
  const newTx = mx - (mx - oldTx) * ratio
  const newTy = my - (my - oldTy) * ratio

  wrap.dataset.scale = newScale
  wrap.dataset.translateX = +newTx.toFixed(1)
  wrap.dataset.translateY = +newTy.toFixed(1)
  updateMermaidWrapTransform(wrap)
}

function handleMermaidMouseDown(e) {
  if (e.button !== 0) return
  const container = e.target.closest('.mermaid-container')
  if (!container) return
  if (e.target.closest('.mermaid-toolbar')) return
  if (e.target.closest('.mermaid-resize-handle')) return

  const btn = e.target.closest('.mermaid-tb-btn')
  if (btn) return

  const wrap = container.querySelector('.mermaid-svg-wrap')
  if (!wrap) return

  wrap.classList.remove('smooth-transition')

  e.preventDefault()
  mermaidDragState.isDragging = true
  mermaidDragState.startX = e.clientX
  mermaidDragState.startY = e.clientY
  mermaidDragState.startTx = parseFloat(wrap.dataset.translateX || 0)
  mermaidDragState.startTy = parseFloat(wrap.dataset.translateY || 0)
  mermaidDragState.activeContainer = container

  document.body.classList.add('mermaid-dragging')
}

// 工具栏按钮事件委托
function handleMermaidToolbarClick(e) {
  const btn = e.target.closest('.mermaid-tb-btn[data-action]')
  if (!btn) return
  const container = btn.closest('.mermaid-container')
  if (!container) return
  const wrap = container.querySelector('.mermaid-svg-wrap')
  const action = btn.dataset.action

  // 阻止事件冒泡，防止触发拖拽等其他行为
  e.stopPropagation()

  switch (action) {
    case 'zoom-in': {
      if (!wrap) break
      wrap.classList.add('smooth-transition')
      const oldScale = parseFloat(wrap.dataset.scale || 1)
      const s = Math.min(5, +(oldScale + 0.25).toFixed(2))
      if (s !== oldScale) {
        const cw = container.querySelector('.mermaid-content-wrapper')
        const rect = cw ? cw.getBoundingClientRect() : container.getBoundingClientRect()
        const mx = rect.width / 2
        const my = rect.height / 2
        const oldTx = parseFloat(wrap.dataset.translateX || 0)
        const oldTy = parseFloat(wrap.dataset.translateY || 0)
        const ratio = s / oldScale
        wrap.dataset.scale = s
        wrap.dataset.translateX = +(mx - (mx - oldTx) * ratio).toFixed(1)
        wrap.dataset.translateY = +(my - (my - oldTy) * ratio).toFixed(1)
      }
      updateMermaidWrapTransform(wrap)
      setTimeout(() => wrap.classList.remove('smooth-transition'), 200)
      break
    }
    case 'zoom-out': {
      if (!wrap) break
      wrap.classList.add('smooth-transition')
      const oldScale = parseFloat(wrap.dataset.scale || 1)
      const s = Math.max(0.25, +(oldScale - 0.25).toFixed(2))
      if (s !== oldScale) {
        const cw = container.querySelector('.mermaid-content-wrapper')
        const rect = cw ? cw.getBoundingClientRect() : container.getBoundingClientRect()
        const mx = rect.width / 2
        const my = rect.height / 2
        const oldTx = parseFloat(wrap.dataset.translateX || 0)
        const oldTy = parseFloat(wrap.dataset.translateY || 0)
        const ratio = s / oldScale
        wrap.dataset.scale = s
        wrap.dataset.translateX = +(mx - (mx - oldTx) * ratio).toFixed(1)
        wrap.dataset.translateY = +(my - (my - oldTy) * ratio).toFixed(1)
      }
      updateMermaidWrapTransform(wrap)
      setTimeout(() => wrap.classList.remove('smooth-transition'), 200)
      break
    }
    case 'zoom-reset': {
      if (!wrap) break
      wrap.classList.add('smooth-transition')
      wrap.dataset.scale = 1
      wrap.dataset.translateX = 0
      wrap.dataset.translateY = 0
      updateMermaidWrapTransform(wrap)
      setTimeout(() => wrap.classList.remove('smooth-transition'), 200)
      break
    }
    case 'fullscreen': {
      handleMermaidFullscreen(container)
      break
    }
    case 'toggle-source': {
      const src = container.querySelector('.mermaid-source-preview')
      if (!src || !wrap) break
      const showing = src.style.display !== 'none'
      src.style.display = showing ? 'none' : 'block'
      wrap.style.display = showing ? 'block' : 'none'
      break
    }
    case 'copy-source': {
      const code = container.querySelector('.mermaid-source-preview code')
      if (!code) break
      navigator.clipboard.writeText(code.textContent).then(() => {
        const origHtml = btn.innerHTML
        btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#52c41a" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>'
        setTimeout(() => { btn.innerHTML = origHtml }, 1200)
      }).catch(() => {})
      break
    }
    case 'exit-fullscreen': {
      exitMermaidFullscreen()
      break
    }
  }
}

// ── Mermaid 全屏功能 ──
function handleMermaidFullscreen(container) {
  // 获取 mermaid 源码
  const sourceEl = container.querySelector('.mermaid-source-preview code')
  const svgWrap = container.querySelector('.mermaid-svg-wrap')
  if (!svgWrap && !sourceEl) return

  const svgHtml = svgWrap ? svgWrap.innerHTML : ''
  const source = sourceEl ? sourceEl.textContent : ''

  // 创建全屏遮罩
  const overlay = document.createElement('div')
  overlay.className = 'mermaid-fullscreen-overlay'
  overlay.innerHTML = `
    <div class="mermaid-fullscreen-container" data-mermaid-fullscreen="true">
      <div class="mermaid-fullscreen-content">
        <div class="mermaid-svg-wrap" data-scale="2" data-translate-x="0" data-translate-y="0">${svgHtml}</div>
      </div>
      <div class="mermaid-fullscreen-toolbar">
        <button class="mermaid-tb-btn" data-action="zoom-out" title="缩小">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="8" y1="11" x2="14" y2="11"/></svg>
        </button>
        <button class="mermaid-tb-btn" data-action="zoom-reset" title="重置">100%</button>
        <button class="mermaid-tb-btn" data-action="zoom-in" title="放大">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="11" y1="8" x2="11" y2="14"/><line x1="8" y1="11" x2="14" y2="11"/></svg>
        </button>
        <button class="mermaid-tb-btn" data-action="copy-source" title="复制源码">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
        </button>
        <button class="mermaid-tb-btn mermaid-tb-btn-exit" data-action="exit-fullscreen" title="退出全屏 (Esc)">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="14" y1="10" x2="21" y2="3"/><line x1="3" y1="21" x2="10" y2="14"/></svg>
        </button>
      </div>
    </div>
  `

  // 保存源码到 overlay 上，供复制使用
  overlay._mermaidSource = source

  document.body.appendChild(overlay)
  document.body.classList.add('mermaid-fullscreen-active')

  // 绑定全屏内的事件
  const fsContainer = overlay.querySelector('.mermaid-fullscreen-container')
  const fsContent = overlay.querySelector('.mermaid-fullscreen-content')

  // 全屏专用 transform 更新（transform-origin: center center 模式）
  // 只用 translate 做平移，scale 自动以元素中心为基准
  function updateFsTransform(fsWrap) {
    const s = parseFloat(fsWrap.dataset.scale || 1)
    const tx = parseFloat(fsWrap.dataset.translateX || 0)
    const ty = parseFloat(fsWrap.dataset.translateY || 0)
    fsWrap.style.transform = `translate(${tx}px, ${ty}px) scale(${s})`
  }

  const initialFsWrap = overlay.querySelector('.mermaid-svg-wrap')
  if (initialFsWrap) updateFsTransform(initialFsWrap)

  // 全屏内工具栏点击
  overlay.addEventListener('click', (e) => {
    const fsBtn = e.target.closest('.mermaid-tb-btn[data-action]')
    if (!fsBtn) return
    e.stopPropagation()
    const fsAction = fsBtn.dataset.action
    const fsWrap = overlay.querySelector('.mermaid-svg-wrap')

    switch (fsAction) {
      case 'zoom-in': {
        if (!fsWrap) break
        fsWrap.classList.add('smooth-transition')
        const s = Math.min(5, +(parseFloat(fsWrap.dataset.scale || 1) + 0.25).toFixed(2))
        fsWrap.dataset.scale = s
        updateFsTransform(fsWrap)
        setTimeout(() => fsWrap.classList.remove('smooth-transition'), 200)
        break
      }
      case 'zoom-out': {
        if (!fsWrap) break
        fsWrap.classList.add('smooth-transition')
        const s = Math.max(0.25, +(parseFloat(fsWrap.dataset.scale || 1) - 0.25).toFixed(2))
        fsWrap.dataset.scale = s
        updateFsTransform(fsWrap)
        setTimeout(() => fsWrap.classList.remove('smooth-transition'), 200)
        break
      }
      case 'zoom-reset': {
        if (!fsWrap) break
        fsWrap.classList.add('smooth-transition')
        fsWrap.dataset.scale = 1
        fsWrap.dataset.translateX = 0
        fsWrap.dataset.translateY = 0
        updateFsTransform(fsWrap)
        setTimeout(() => fsWrap.classList.remove('smooth-transition'), 200)
        break
      }
      case 'copy-source': {
        const src = overlay._mermaidSource || ''
        if (!src) break
        navigator.clipboard.writeText(src).then(() => {
          const origHtml = fsBtn.innerHTML
          fsBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#52c41a" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>'
          setTimeout(() => { fsBtn.innerHTML = origHtml }, 1200)
        }).catch(() => {})
        break
      }
      case 'exit-fullscreen': {
        exitMermaidFullscreen()
        break
      }
    }
  })

  // 全屏内拖拽
  let fsDrag = { isDragging: false, startX: 0, startY: 0, startTx: 0, startTy: 0 }

  fsContent.addEventListener('mousedown', (e) => {
    if (e.button !== 0) return
    if (e.target.closest('.mermaid-fullscreen-toolbar')) return
    const fsWrap = overlay.querySelector('.mermaid-svg-wrap')
    if (!fsWrap) return
    fsWrap.classList.remove('smooth-transition')
    e.preventDefault()
    fsDrag.isDragging = true
    fsDrag.startX = e.clientX
    fsDrag.startY = e.clientY
    fsDrag.startTx = parseFloat(fsWrap.dataset.translateX || 0)
    fsDrag.startTy = parseFloat(fsWrap.dataset.translateY || 0)
    fsContent.style.cursor = 'grabbing'
  })

  const fsMouseMove = (e) => {
    if (!fsDrag.isDragging) return
    const fsWrap = overlay.querySelector('.mermaid-svg-wrap')
    if (!fsWrap) return
    const dx = e.clientX - fsDrag.startX
    const dy = e.clientY - fsDrag.startY
    fsWrap.dataset.translateX = +(fsDrag.startTx + dx).toFixed(1)
    fsWrap.dataset.translateY = +(fsDrag.startTy + dy).toFixed(1)
    updateFsTransform(fsWrap)
  }

  const fsMouseUp = () => {
    if (!fsDrag.isDragging) return
    fsDrag.isDragging = false
    fsContent.style.cursor = 'grab'
  }

  document.addEventListener('mousemove', fsMouseMove)
  document.addEventListener('mouseup', fsMouseUp)

  // 全屏内滚轮缩放（Ctrl+滚轮）— 原地缩放
  // transform-origin: center center 意味着 scale 自动以元素中心为基准
  // 只需改 scale，不需要调整 translate
  fsContent.addEventListener('wheel', (e) => {
    if (!e.ctrlKey) return
    e.preventDefault()
    const fsWrap = overlay.querySelector('.mermaid-svg-wrap')
    if (!fsWrap) return
    fsWrap.classList.add('smooth-transition')
    const oldScale = parseFloat(fsWrap.dataset.scale || 1)
    const delta = e.deltaY > 0 ? -0.1 : 0.1
    const newScale = Math.min(5, Math.max(0.25, +(oldScale + delta).toFixed(2)))
    if (newScale === oldScale) return

    fsWrap.dataset.scale = newScale
    updateFsTransform(fsWrap)
    setTimeout(() => fsWrap.classList.remove('smooth-transition'), 200)
  }, { passive: false })

  // 保存清理函数
  overlay._cleanup = () => {
    document.removeEventListener('mousemove', fsMouseMove)
    document.removeEventListener('mouseup', fsMouseUp)
  }

  // 点击遮罩背景退出
  overlay.addEventListener('mousedown', (e) => {
    if (e.target === overlay) {
      exitMermaidFullscreen()
    }
  })
}

function exitMermaidFullscreen() {
  const overlay = document.querySelector('.mermaid-fullscreen-overlay')
  if (!overlay) return
  if (overlay._cleanup) overlay._cleanup()
  overlay.remove()
  document.body.classList.remove('mermaid-fullscreen-active')
}

function handleMermaidMouseMove(e) {
  if (!mermaidDragState.isDragging) return
  const container = mermaidDragState.activeContainer
  if (!container) return

  const wrap = container.querySelector('.mermaid-svg-wrap')
  if (!wrap) return

  const dx = e.clientX - mermaidDragState.startX
  const dy = e.clientY - mermaidDragState.startY

  wrap.dataset.translateX = +(mermaidDragState.startTx + dx).toFixed(1)
  wrap.dataset.translateY = +(mermaidDragState.startTy + dy).toFixed(1)
  updateMermaidWrapTransform(wrap)
}

function handleMermaidMouseUp() {
  if (!mermaidDragState.isDragging) return
  mermaidDragState.isDragging = false
  mermaidDragState.activeContainer = null
  document.body.classList.remove('mermaid-dragging')
}

function handleMermaidKeyDown(e) {
  if (e.key === 'Control' && !e.repeat) {
    document.body.classList.add('mermaid-ctrl-held')
  }
}

function handleMermaidKeyUp(e) {
  if (e.key === 'Control') {
    document.body.classList.remove('mermaid-ctrl-held')
  }
}

// ── Mermaid 容器高度拖拽调整 ──
// 存储用户自定义的高度（key: data-mermaid-id, value: height in px）
const mermaidCustomHeights = new Map()

const mermaidResizeState = {
  isResizing: false,
  startY: 0,
  startHeight: 0,
  activeMermaidId: null,
}

function handleMermaidResizeDown(e) {
  if (e.button !== 0) return
  const handle = e.target.closest('.mermaid-resize-handle')
  if (!handle) return
  const container = handle.closest('.mermaid-container')
  if (!container) return
  const wrapper = container.querySelector('.mermaid-content-wrapper')
  if (!wrapper) return
  const mermaidId = container.getAttribute('data-mermaid-id')
  if (!mermaidId) return

  e.preventDefault()
  e.stopPropagation()

  mermaidResizeState.isResizing = true
  mermaidResizeState.startY = e.clientY
  mermaidResizeState.startHeight = wrapper.offsetHeight
  mermaidResizeState.activeMermaidId = mermaidId

  document.body.classList.add('mermaid-resizing')
}

function handleMermaidResizeMove(e) {
  if (!mermaidResizeState.isResizing) return
  const mermaidId = mermaidResizeState.activeMermaidId
  if (!mermaidId) return

  const dy = e.clientY - mermaidResizeState.startY
  const newHeight = Math.max(100, mermaidResizeState.startHeight + dy)
  
  // 存储自定义高度
  mermaidCustomHeights.set(mermaidId, newHeight)
  
  // 应用到当前 DOM
  applyMermaidCustomHeight(mermaidId, newHeight)
}

function handleMermaidResizeUp() {
  if (!mermaidResizeState.isResizing) return
  mermaidResizeState.isResizing = false
  mermaidResizeState.activeMermaidId = null
  document.body.classList.remove('mermaid-resizing')
}

// 将自定义高度应用到指定 mermaid 容器
function applyMermaidCustomHeight(mermaidId, height) {
  const container = document.querySelector(`.mermaid-container[data-mermaid-id="${mermaidId}"]`)
  if (!container) return
  const wrapper = container.querySelector('.mermaid-content-wrapper')
  if (wrapper) {
    wrapper.style.height = height + 'px'
    wrapper.style.maxHeight = 'none'
  }
  container.style.maxHeight = 'none'
}

// 重新应用所有自定义高度（Vue 重新渲染后调用）
function reapplyAllMermaidCustomHeights() {
  for (const [mermaidId, height] of mermaidCustomHeights) {
    applyMermaidCustomHeight(mermaidId, height)
  }
}

function stopGeneration() {
  if (abortController.value) {
    abortController.value.abort()
    abortController.value = null
  }
}

async function sendMessage() {
  const text = question.value.trim()
  if (!text && !uploadedExcel.value) return
  if (loading.value) return

  // 重置状态
  resetDetectFlag()
  pendingSQLList.value = []
  showRetryConfirm.value = false
  lastQuestion.value = text

  // 构建消息内容
  let messageContent = text
  let excelContext = null

  if (uploadedExcel.value) {
    const excel = uploadedExcel.value
    excelContext = {
      fileId: excel.fileId,
      columns: excel.columns,
      totalRows: excel.rows,
    }
    if (!text) {
      messageContent = `请将上传的 Excel 数据导入数据库。文件ID：${excel.fileId}，Excel 列名：${excel.columns.join(', ')}，共 ${excel.rows} 行数据。`
    } else {
      messageContent += `\n\n[Excel 文件上下文] 文件ID：${excel.fileId}，列名：${excel.columns.join(', ')}，共 ${excel.rows} 行数据。`
    }
  }

  chatHistory.value.push({ role: 'user', content: text || '导入 Excel 数据' })
  question.value = ''
  loading.value = true
  thinkingText.value = ''
  streamingContent.value = ''
  lastRenderedMermaidCount = 0
  lastStreamingMermaidCount = 0
  lastStreamingExecMermaidCount = 0
  lastThinkingMermaidCount = 0
  showAllHistory.value = false
  scrollToBottom()

  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/chatStream'
  const auth = sessionStorage.getItem('authentication') || ''

  const controller = new AbortController()
  abortController.value = controller

  const schemas = buildRequestSchemas()
  if (schemas.length === 0) {
    ElMessage.warning('请先选择至少一个数据库 schema')
    loading.value = false
    chatHistory.value.pop()
    return
  }

  if (!validateExtractedSchemas()) {
    // 不阻止发送，但已给出警告
  }

  try {
    const primaryConnId = getPrimaryConnId()
    const primarySchema = schemas.length > 0 ? schemas[0].schema : ''
    const body = {
      sessionId: sessionId.value,
      connId: primaryConnId,
      schema: primarySchema,
      schemas,
      question: messageContent,
      tableContext: selectedTables.value,
      modelId: selectedModel.value,
    }
    if (excelContext) {
      body.excelData = excelContext
    }

    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': auth },
      body: JSON.stringify(body),
      signal: controller.signal,
    })

    if (resp.status === 401) {
      const errorData = await resp.json().catch(() => ({}))
      if (errorData.code === 401) {
        ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
        handleSessionExpired()
        return
      }
    }

    if (!resp.ok) {
      ElMessage({ message: `请求失败: ${resp.status}`, type: 'error' })
      return
    }

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    const collectedDangerSQLs = []

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })

      const lines = buf.split('\n')
      buf = lines.pop()

      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        const data = line.slice(6).trim()
        if (!data) continue
        try {
          const chunk = JSON.parse(data)
          switch (chunk.type) {
            case 'session':
              sessionId.value = chunk.content
              break
            case 'thinking':
              thinkingText.value += chunk.content
              scrollToBottom()
              break
            case 'content':
              streamingContent.value += chunk.content
              // 检测是否有新的完整 mermaid 代码块可以渲染
              tryRenderStreamingMermaid()
              scrollToBottom()
              break
            case 'danger_confirm':
              collectedDangerSQLs.push({ sql: chunk.sql || chunk.content, interruptId: chunk.interruptId || '', checkPointId: chunk.checkPointId || '' })
              break
            case 'retry_limit':
              retryMessage.value = chunk.content
              showRetryConfirm.value = true
              break
            case 'error':
              // 先将已有的思考过程和内容加入历史
              if (thinkingText.value) {
                chatHistory.value.push({ role: 'thinking', content: thinkingText.value, collapsed: true })
                thinkingText.value = ''
              }
              if (streamingContent.value) {
                const content = streamingContent.value
                const isSql = /^\s*(SELECT|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|DESCRIBE|EXPLAIN|WITH)\s/i.test(content.trim())
                chatHistory.value.push({ role: 'assistant', content, hasSql: isSql })
                if (isSql) lastSql.value = content
                streamingContent.value = ''
              }
              // 然后再添加错误消息，确保显示在结果区域下方
              chatHistory.value.push({ role: 'assistant', content: '❌ ' + (sanitizeError(chunk.content) || 'AI 服务错误') })
              scrollToBottom()
              break
            case 'done':
              break
          }
        } catch (_) { }
      }
      scrollToBottom()
    }

    // 流结束，将思考过程和内容加入历史
    if (thinkingText.value) {
      chatHistory.value.push({ role: 'thinking', content: thinkingText.value, collapsed: true })
      thinkingText.value = ''
    }
    if (streamingContent.value) {
      const content = streamingContent.value
      const isSql = /^\s*(SELECT|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|DESCRIBE|EXPLAIN|WITH)\s/i.test(content.trim())
      chatHistory.value.push({ role: 'assistant', content, hasSql: isSql })
      if (isSql) lastSql.value = content
      streamingContent.value = ''
    }

    // 处理收集到的危险 SQL
    if (collectedDangerSQLs.length > 0) {
      // 所有 danger_confirm 来自同一个 checkpoint，收集所有 interruptId
      const checkPointId = collectedDangerSQLs[0].checkPointId
      const allInterruptIds = collectedDangerSQLs.map(d => d.interruptId)

      if (collectedDangerSQLs.length === 1) {
        showConfirmDialog(collectedDangerSQLs[0].sql, allInterruptIds, checkPointId)
      } else {
        // 多条 SQL：合并显示，支持逐条选择和批量确认
        pendingSQLList.value = collectedDangerSQLs.map(item => {
          const analysis = analyzeSQL(item.sql)
          return { sql: item.sql, ...analysis, selected: true }
        })
        selectAllChecked.value = true
        // 保存 interruptIds 和 checkPointId 供批量确认使用
        pendingSQLList.interruptIds = allInterruptIds
        pendingSQLList.checkPointId = checkPointId
      }
    }

    // 清除已上传的 Excel
    if (uploadedExcel.value) {
      uploadedExcel.value = null
    }
  } catch (e) {
    if (e.name === 'AbortError') {
      if (thinkingText.value) {
        chatHistory.value.push({ role: 'thinking', content: thinkingText.value, collapsed: true })
        thinkingText.value = ''
      }
      if (streamingContent.value) {
        chatHistory.value.push({ role: 'assistant', content: streamingContent.value })
        streamingContent.value = ''
      }
      chatHistory.value.push({ role: 'assistant', content: '⏹ 对话已被手动终止' })
      scrollToBottom()
    } else {
      ElMessage({ message: sanitizeError(e) || '请求失败', type: 'error' })
    }
  } finally {
    loading.value = false
    abortController.value = null
    // 流结束后渲染 mermaid 图表
    scrollToBottom()
    doRenderMermaidBlocks()
    // 延迟再次检查：处理 md 异步加载完成后 getCachedHtml 重新渲染的情况
    setTimeout(() => doRenderMermaidBlocks(false), 500)
    setTimeout(() => doRenderMermaidBlocks(false), 1500)
  }
}

// 显示确认区域
function showConfirmDialog(sql, interruptIds, checkPointId) {
  const analysis = analyzeSQL(sql)

  confirmSQL.value = sql
  confirmInterruptIds.value = Array.isArray(interruptIds) ? interruptIds : [interruptIds]
  confirmCheckPointId.value = checkPointId || ''
  confirmOperationType.value = analysis.type
  confirmRiskLevel.value = analysis.riskLevel
  confirmDescription.value = analysis.description
  confirmTableName.value = analysis.tableName || ''
  confirmVisible.value = true
}

// 重置检测标记
function resetDetectFlag() {
  hasShownConfirm = false
}

// 处理确认执行
async function handleConfirmExec(confirmedSql) {
  loading.value = true
  confirmVisible.value = false

  // 将已确认的 SQL 保留在聊天记录中（无论后续执行成功与否）
  const sqlForDisplay = confirmSQL.value
  chatHistory.value.push({ role: 'assistant', content: `⏳ 正在执行：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\`` })
  scrollToBottom()

  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/chatStream'
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const controller = new AbortController()
    abortController.value = controller

    const schemas = buildRequestSchemas()
    const primaryConnId = getPrimaryConnId()
    const primarySchema = schemas.length > 0 ? schemas[0].schema : ''
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': auth },
      body: JSON.stringify({
        sessionId: sessionId.value,
        connId: primaryConnId,
        schema: primarySchema,
        schemas,
        question: '执行已确认的 SQL',
        confirmed: true,
        interruptIds: confirmInterruptIds.value,
        checkPointId: confirmCheckPointId.value,
      }),
      signal: controller.signal,
    })

    if (resp.status === 401) {
      const errorData = await resp.json().catch(() => ({}))
      if (errorData.code === 401) {
        ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
        handleSessionExpired()
        return
      }
    }

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    streamingExecContent.value = '' // 清空之前的内容
    const collectedDangerSQLs = []
    let hasError = false
    let errorMsg = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        const trimmed = line.slice(6).trim()
        if (!trimmed) continue
        try {
          const chunk = JSON.parse(trimmed)
          if (chunk.type === 'content') {
            streamingExecContent.value += chunk.content
            scrollToBottom()
          }
          if (chunk.type === 'danger_confirm') {
            // Agent 恢复执行后又遇到新的危险 SQL
            collectedDangerSQLs.push({
              sql: chunk.sql || chunk.content,
              interruptId: chunk.interruptId || '',
              checkPointId: chunk.checkPointId || ''
            })
          }
          if (chunk.type === 'error') {
            hasError = true
            errorMsg = chunk.content || '执行失败'
          }
        } catch (_) { }
      }
    }

    // 将流式内容添加到聊天记录
    const execContent = streamingExecContent.value
    streamingExecContent.value = '' // 清空流式内容

    // 更新执行结果到聊天记录
    if (hasError) {
      chatHistory.value.push({ role: 'assistant', content: `❌ 执行失败：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\`\n${sanitizeError(errorMsg)}` })
    } else {
      chatHistory.value.push({ role: 'assistant', content: `✅ 已执行：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\`` })
    }

    // 如果 Agent 继续输出了内容（比如执行结果说明），追加到聊天记录
    if (execContent) {
      chatHistory.value.push({ role: 'assistant', content: execContent })
    }

    scrollToBottom()

    // 如果恢复执行后又遇到新的危险 SQL，弹出确认框
    if (collectedDangerSQLs.length > 0) {
      loading.value = false
      abortController.value = null
      const cpId = collectedDangerSQLs[0].checkPointId
      const ids = collectedDangerSQLs.map(d => d.interruptId)
      if (collectedDangerSQLs.length === 1) {
        showConfirmDialog(collectedDangerSQLs[0].sql, ids, cpId)
      } else {
        pendingSQLList.value = collectedDangerSQLs.map(item => {
          const analysis = analyzeSQL(item.sql)
          return { sql: item.sql, ...analysis, selected: true }
        })
        selectAllChecked.value = true
        pendingSQLList.interruptIds = ids
        pendingSQLList.checkPointId = cpId
      }
      return
    }
  } catch (e) {
    if (e.name === 'AbortError') {
      chatHistory.value.push({ role: 'assistant', content: `⏹ 已终止：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\`` })
    } else {
      ElMessage({ message: sanitizeError(e) || '执行失败', type: 'error' })
    }
    streamingExecContent.value = ''
  } finally {
    loading.value = false
    abortController.value = null
    scrollToBottom()
  }
}

// 处理取消确认 — 向后端发送 confirmed=false 的 Resume 请求，清理 checkpoint 状态
async function handleConfirmCancel() {
  confirmVisible.value = false
  chatHistory.value.push({ role: 'assistant', content: `已取消执行：\n\`\`\`sql\n${confirmSQL.value}\n\`\`\`` })
  scrollToBottom()

  // 向后端发送取消请求，确保 checkpoint 状态被正确处理
  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/chatStream'
  const auth = sessionStorage.getItem('authentication') || ''
  try {
    await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': auth },
      body: JSON.stringify({
        sessionId: sessionId.value,
        connId: getPrimaryConnId(),
        schema: buildRequestSchemas().length > 0 ? buildRequestSchemas()[0].schema : '',
        schemas: buildRequestSchemas(),
        question: '取消执行',
        confirmed: false,
        interruptIds: confirmInterruptIds.value,
        checkPointId: confirmCheckPointId.value,
      }),
    })
  } catch (_) {
    // 取消请求失败不影响用户体验
  }
}

// ── 多条 SQL 批量确认 ──
async function handleConfirmSelectedSQL() {
  const selectedItems = pendingSQLList.value.filter(item => item.selected)
  if (selectedItems.length === 0) {
    ElMessage.warning('请至少选择一条SQL')
    return
  }

  const allItems = [...pendingSQLList.value]
  const allInterruptIds = pendingSQLList.interruptIds || []
  const checkPointId = pendingSQLList.checkPointId || ''

  // 获取选中项对应的 interruptIds
  const selectedInterruptIds = []
  for (let i = 0; i < allItems.length; i++) {
    if (allItems[i].selected && allInterruptIds[i]) {
      selectedInterruptIds.push(allInterruptIds[i])
    }
  }

  pendingSQLList.value = []
  selectAllChecked.value = false
  loading.value = true

  // 将选中的 SQL 保留在聊天记录中
  for (const item of selectedItems) {
    chatHistory.value.push({ role: 'assistant', content: `⏳ 正在执行：\n\`\`\`sql\n${item.sql}\n\`\`\`` })
  }
  scrollToBottom()

  // 一次性 Resume 所有选中的 interruptId
  await executeBatchResume(selectedItems, selectedInterruptIds, checkPointId)

  loading.value = false
  scrollToBottom()
}

// 批量恢复执行：一次 Resume 传入所有 interruptId
async function executeBatchResume(sqlItems, interruptIds, checkPointId) {
  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/chatStream'
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const schemas = buildRequestSchemas()
    const primaryConnId = getPrimaryConnId()
    const primarySchema = schemas.length > 0 ? schemas[0].schema : ''
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': auth },
      body: JSON.stringify({
        sessionId: sessionId.value,
        connId: primaryConnId,
        schema: primarySchema,
        schemas,
        question: 'resume confirmed SQL',
        confirmed: true,
        interruptIds: interruptIds,
        checkPointId: checkPointId,
      }),
    })

    if (resp.status === 401) {
      const errorData = await resp.json().catch(() => ({}))
      if (errorData.code === 401) {
        ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
        handleSessionExpired()
        return
      }
    }

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    streamingExecContent.value = ''
    const collectedDangerSQLs = []
    let hasError = false
    let errorMsg = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        const trimmed = line.slice(6).trim()
        if (!trimmed) continue
        try {
          const chunk = JSON.parse(trimmed)
          if (chunk.type === 'content') streamingExecContent.value += chunk.content
          if (chunk.type === 'danger_confirm') {
            collectedDangerSQLs.push({
              sql: chunk.sql || chunk.content,
              interruptId: chunk.interruptId || '',
              checkPointId: chunk.checkPointId || ''
            })
          }
          if (chunk.type === 'error') { hasError = true; errorMsg = chunk.content || 'exec failed' }
        } catch (_) { }
      }
    }

    // 将流式内容添加到聊天记录
    const execContent = streamingExecContent.value
    streamingExecContent.value = ''

    // 更新所有 SQL 的执行状态
    for (const item of sqlItems) {
      chatHistory.value.push({ role: 'assistant', content: hasError
          ? `❌ 执行失败：\n\`\`\`sql\n${item.sql}\n\`\`\``
          : `✅ 已执行：\n\`\`\`sql\n${item.sql}\n\`\`\``
      })
    }

    if (execContent) {
      chatHistory.value.push({ role: 'assistant', content: execContent })
    }

    if (collectedDangerSQLs.length > 0) {
      const cpId = collectedDangerSQLs[0].checkPointId
      const ids = collectedDangerSQLs.map(d => d.interruptId)
      if (collectedDangerSQLs.length === 1) {
        showConfirmDialog(collectedDangerSQLs[0].sql, ids, cpId)
      } else {
        pendingSQLList.value = collectedDangerSQLs.map(item => {
          const analysis = analyzeSQL(item.sql)
          return { sql: item.sql, ...analysis, selected: true }
        })
        selectAllChecked.value = true
        pendingSQLList.interruptIds = ids
        pendingSQLList.checkPointId = cpId
      }
    }
  } catch (e) {
    streamingExecContent.value = ''
    ElMessage({ message: sanitizeError(e) || 'exec failed', type: 'error' })
  }
}

async function handleCancelAllSQL() {
  const items = pendingSQLList.value
  const allInterruptIds = pendingSQLList.interruptIds || []
  const checkPointId = pendingSQLList.checkPointId || ''
  pendingSQLList.value = []
  selectAllChecked.value = false
  // 将每条 SQL 保留在聊天记录中
  for (const item of items) {
    chatHistory.value.push({ role: 'assistant', content: `已取消执行：\n\`\`\`sql\n${item.sql}\n\`\`\`` })
  }
  scrollToBottom()

  // 向后端发送取消请求，确保 checkpoint 状态被正确处理
  if (allInterruptIds.length > 0 && checkPointId) {
    const apiBase = import.meta.env.VITE_API_URL || ''
    const url = apiBase + '/ai/agent/chatStream'
    const auth = sessionStorage.getItem('authentication') || ''
    try {
      await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': auth },
        body: JSON.stringify({
          sessionId: sessionId.value,
          connId: getPrimaryConnId(),
          schema: buildRequestSchemas().length > 0 ? buildRequestSchemas()[0].schema : '',
          schemas: buildRequestSchemas(),
          question: '取消执行',
          confirmed: false,
          interruptIds: allInterruptIds,
          checkPointId: checkPointId,
        }),
      })
    } catch (_) {
      // 取消请求失败不影响用户体验
    }
  }
}

async function executeConfirmedSQL(sqlText, interruptId, checkPointId) {
  // 将 SQL 保留在聊天记录中
  chatHistory.value.push({ role: 'assistant', content: `⏳ 正在执行：\n\`\`\`sql\n${sqlText}\n\`\`\`` })
  scrollToBottom()

  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/chatStream'
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const schemas = buildRequestSchemas()
    const primaryConnId = getPrimaryConnId()
    const primarySchema = schemas.length > 0 ? schemas[0].schema : ''
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': auth },
      body: JSON.stringify({
        sessionId: sessionId.value,
        connId: primaryConnId,
        schema: primarySchema,
        schemas,
        question: 'resume confirmed SQL',
        confirmed: true,
        interruptIds: [interruptId],
        checkPointId: checkPointId,
      }),
    })

    if (resp.status === 401) {
      const errorData = await resp.json().catch(() => ({}))
      if (errorData.code === 401) {
        ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
        handleSessionExpired()
        return
      }
    }

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    streamingExecContent.value = ''
    const collectedDangerSQLs = []
    let hasError = false
    let errorMsg = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        const trimmed = line.slice(6).trim()
        if (!trimmed) continue
        try {
          const chunk = JSON.parse(trimmed)
          if (chunk.type === 'content') {
            streamingExecContent.value += chunk.content
            scrollToBottom()
          }
          if (chunk.type === 'danger_confirm') {
            collectedDangerSQLs.push({
              sql: chunk.sql || chunk.content,
              interruptId: chunk.interruptId || '',
              checkPointId: chunk.checkPointId || ''
            })
          }
          if (chunk.type === 'error') {
            hasError = true
            errorMsg = chunk.content || 'exec failed'
          }
        } catch (_) { }
      }
    }

    // 将流式内容添加到聊天记录
    const execContent = streamingExecContent.value
    streamingExecContent.value = ''

    // 更新执行结果
    if (hasError) {
      chatHistory.value.push({ role: 'assistant', content: `❌ 执行失败：\n\`\`\`sql\n${sqlText}\n\`\`\`\n${sanitizeError(errorMsg)}` })
    } else {
      chatHistory.value.push({ role: 'assistant', content: `✅ 已执行：\n\`\`\`sql\n${sqlText}\n\`\`\`` })
    }

    if (execContent) {
      chatHistory.value.push({ role: 'assistant', content: execContent })
    }

    // 如果恢复执行后又遇到新的危险 SQL，弹出确认框
    if (collectedDangerSQLs.length > 0) {
      const cpId = collectedDangerSQLs[0].checkPointId
      const ids = collectedDangerSQLs.map(d => d.interruptId)
      if (collectedDangerSQLs.length === 1) {
        showConfirmDialog(collectedDangerSQLs[0].sql, ids, cpId)
      } else {
        pendingSQLList.value = collectedDangerSQLs.map(item => {
          const analysis = analyzeSQL(item.sql)
          return { sql: item.sql, ...analysis, selected: true }
        })
        selectAllChecked.value = true
        pendingSQLList.interruptIds = ids
        pendingSQLList.checkPointId = cpId
      }
    }
  } catch (e) {
    streamingExecContent.value = ''
    chatHistory.value.push({ role: 'assistant', content: `❌ 执行失败：\n\`\`\`sql\n${sqlText}\n\`\`\`\n${sanitizeError(e)}` })
  }
}

function getCurrentUser() {
  const userStr = sessionStorage.getItem('currentUser')
  if (userStr) {
    try { const user = JSON.parse(userStr); return user.name || 'anonymous' }
    catch { return 'anonymous' }
  }
  return 'anonymous'
}

// ── 重试 ──
function handleRetryContinue() {
  showRetryConfirm.value = false
  if (lastQuestion.value) {
    question.value = lastQuestion.value
    nextTick(() => sendMessage())
  }
}

// ── Excel 上传 ──
async function handleExcelUpload(file) {
  const rawFile = file.raw || file
  const formData = new FormData()
  formData.append('file', rawFile)

  // 前端文件大小校验（20MB）
  if (rawFile.size > 20 * 1024 * 1024) {
    ElMessage.error('文件大小不能超过 20MB')
    return
  }

  excelUploading.value = true
  try {
    const apiBase = import.meta.env.VITE_API_URL || ''
    const auth = sessionStorage.getItem('authentication') || ''
    const resp = await fetch(apiBase + '/ai/agent/uploadExcel', {
      method: 'POST',
      headers: { 'Authorization': auth },
      body: formData,
    })

    if (resp.status === 401) {
      const errorData = await resp.json().catch(() => ({}))
      if (errorData.code === 401) {
        ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
        handleSessionExpired()
        return
      }
    }

    if (!resp.ok) {
      const errData = await resp.json().catch(() => ({}))
      throw new Error(sanitizeError(errData.error) || `上传失败：${resp.status}`)
    }
    const data = await resp.json()

    uploadedExcel.value = {
      fileId: data.fileId,
      name: data.fileName,
      columns: data.columns,
      rows: data.totalRows,
    }

    // 在聊天区显示预览
    let previewText = `📎 已上传文件：**${data.fileName}**\n`
    previewText += `共 ${data.totalRows} 行数据，${data.columns.length} 列\n\n`
    previewText += `列名：\`${data.columns.join('`, `')}\`\n\n`
    const previewRows = data.preview || []
    if (previewRows.length > 0) {
      previewText += `前 ${previewRows.length} 行原始数据预览：\n`
      previewText += '| ' + data.columns.join(' | ') + ' |\n'
      previewText += '| ' + data.columns.map(() => '---').join(' | ') + ' |\n'
      for (const row of previewRows) {
        const cells = data.columns.map((_, i) => {
          const val = row[i] !== undefined && row[i] !== null ? String(row[i]) : ''
          // 保留原始内容，仅对超长值截断（50字符）
          return val.length > 50 ? val.substring(0, 50) + '…' : (val || ' ')
        })
        previewText += '| ' + cells.join(' | ') + ' |\n'
      }
      if (data.totalRows > previewRows.length) {
        previewText += `\n*共 ${data.totalRows} 行，以上仅展示前 ${previewRows.length} 行*\n`
      }
    }

    // 如果已选择了相关表，自动预匹配字段
    const primaryConnId = getPrimaryConnId()
    if (primaryConnId && selectedTables.value.length === 1) {
      try {
        const matchResp = await fetch(apiBase + '/ai/agent/preMatchColumns', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Authorization': auth },
          body: JSON.stringify({
            fileId: data.fileId,
            connId: primaryConnId,
            tableName: selectedTables.value[0],
          }),
        })
        if (matchResp.ok) {
          const matchData = await matchResp.json()
          previewText += `\n### 字段自动匹配预览（目标表：\`${selectedTables.value[0]}\`）\n`
          previewText += `匹配 ${matchData.matchedCount}/${matchData.totalExcel} 列\n\n`
          previewText += '| Excel 列 | 数据库字段 | 状态 |\n'
          previewText += '| --- | --- | --- |\n'
          for (const m of matchData.matches) {
            const status = m.matched ? '✅ 已匹配' : '❌ 未匹配'
            previewText += `| ${m.excelColumn} | ${m.dbColumn || '-'} | ${status} |\n`
          }
          if (matchData.matchedCount < matchData.totalExcel) {
            previewText += `\n⚠️ 有 ${matchData.totalExcel - matchData.matchedCount} 列未匹配，这些列的数据将不会导入。\n`
          }
        }
      } catch (matchErr) {
        console.warn('[App] 预匹配失败，不影响上传:', matchErr)
      }
    }

    chatHistory.value.push({ role: 'assistant', content: previewText, hasSql: false })
    scrollToBottom()
    doRenderMermaidBlocks()
    ElMessage.success(`已上传 ${data.fileName}，请输入导入指令（如：将数据导入到 xxx 表）`)
  } catch (e) {
    console.error('[App] 上传 Excel 文件失败:', e)
    ElMessage.error('上传 Excel 文件失败，请检查文件格式')
  } finally {
    excelUploading.value = false
  }
}

function clearUploadedExcel() {
  uploadedExcel.value = null
}

function handlePromptSendToAI(content, connInfo) {
  promptPopoverVisible.value = false
  promptEditDialogVisible.value = false
  question.value = content
  if (connInfo && connInfo.connSchemas && connInfo.connSchemas.length > 0) {
    selectedSchemas.value = connInfo.connSchemas.map(cs => cs.connId + '::' + cs.schema)
  }
  if (connInfo && connInfo.tables && connInfo.tables.length > 0) {
    const tableNames = connInfo.tables.map(t => typeof t === 'string' ? t : t.name)
    nextTick(() => {
      loadTableListForSchemas().then(() => {
        const availableNames = tableList.value.map(t => t.label || t.name)
        selectedTables.value = tableNames.filter(t => availableNames.includes(t))
        nextTick(() => sendMessage())
      })
    })
  } else {
    selectedTables.value = []
    nextTick(() => sendMessage())
  }
}

function handlePromptAdd() {
  openPromptEditDialog()
}

function handlePromptEdit(promptId) {
  openPromptEditDialog({ promptId })
}

function handlePromptSaved() {
  loadPrompts()
  triggerPromptSaved()
}

async function handleDeletePrompt(prompt) {
  try {
    await http.get('/delPrompt', { params: { id: prompt.id } })
    ElMessage.success('删除成功')
    loadPrompts()
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

function handleViewPromptDetail(prompt) {
  promptDetail.value = prompt
  promptDetailVisible.value = true
}

function handleSendFromDetail() {
  if (promptDetail.value) {
    handlePromptSendToAI(promptDetail.value.content, {
      connSchemas: promptDetail.value.connSchemas,
      tables: promptDetail.value.tables,
    })
    promptDetailVisible.value = false
  }
}

function clearSession(showMsg) {
  stopGeneration()
  chatHistory.value = []
  sessionId.value = ''
  thinkingText.value = ''
  streamingContent.value = ''
  streamingExecContent.value = ''
  lastSql.value = ''
  confirmVisible.value = false
  confirmSQL.value = ''
  pendingSQLList.value = []
  showRetryConfirm.value = false
  uploadedExcel.value = null
  processedLinks.clear()
  resetDetectFlag()
  if(showMsg) {
    ElMessage({ message: '已新建会话', type: 'success' })
  }
}

function insertToEditor() {
  if (!lastSql.value) return
  // emit('insertSql', lastSql.value.trim())
  // emit('update:modelValue', false)
  console.log('SQL 待插入:', lastSql.value.trim())
}

// --- 语音识别 ---
function initSpeechRecognition() {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition
  if (!SR) {
    ElMessage({ message: '浏览器不支持语音识别', type: 'warning' })
    return null
  }
  const recognition = new SR()
  recognition.lang = 'zh-CN'
  recognition.continuous = true
  recognition.interimResults = true
  recognition.onstart = () => { isRecording.value = true }
  recognition.onresult = (event) => {
    let finalTranscript = ''
    for (let i = event.resultIndex; i < event.results.length; i++) {
      if (event.results[i].isFinal) finalTranscript += event.results[i][0].transcript
    }
    if (finalTranscript) question.value += (question.value ? ' ' : '') + finalTranscript
  }
  recognition.onerror = (event) => {
    if (event.error === 'not-allowed') ElMessage({ message: '请允许使用麦克风', type: 'error' })
    isRecording.value = false
  }
  recognition.onend = () => { isRecording.value = false }
  return recognition
}

function toggleRecording() {
  if (isRecording.value) {
    speechRecognition?.stop()
    isRecording.value = false
  } else {
    if (!speechRecognition) speechRecognition = initSpeechRecognition()
    if (!speechRecognition) return
    try {
      speechRecognition.start()
      ElMessage({ message: '开始语音录入...', type: 'info' })
    } catch { ElMessage({ message: '无法启动语音识别', type: 'error' }) }
  }
}

function handleEscKey(e) {
  if (e.key === 'Escape' || e.keyCode === 27) {
    // ESC 退出 mermaid 全屏
    const overlay = document.querySelector('.mermaid-fullscreen-overlay')
    if (overlay) {
      e.preventDefault()
      e.stopPropagation()
      exitMermaidFullscreen()
    }
  }
}

function logout() {
  http.post("/logout")
    .then((resp) => {
      currentUser.value = {}
      loginSucc.value = false
      canUseClassicView.value = false
      ElMessage(resp.data.data)
      sessionStorage.removeItem("authentication")
      sessionStorage.removeItem("currentUser")
      sessionStorage.removeItem("isRemote")
    })
}

const loginDialogRef = useTemplateRef('loginDialogRef')

function showLoginDialog() {
  loginDialogVisible.value = true
}

function handleSessionExpired() {
  loginSucc.value = false
  canUseClassicView.value = false
  sessionStorage.removeItem('authentication')
  sessionStorage.removeItem('currentUser')
  sessionStorage.removeItem('isRemote')
  // 延迟一下再尝试登录，确保 UI 状态已更新
  nextTick(() => {
    loginDialogVisible.value = true
  })
}

function handleSessionExpiredEvent(event) {
  if (window.location.pathname === '/classical') {
    return
  }
  const message = event.detail?.message || ''
  if (message) {
    ElMessage({ message: message, type: 'warning' })
  }
  handleSessionExpired()
}

function handleLoginSuccess(userData) {
  currentUser.value = userData
  loginSucc.value = true
  loadConnList().then(() => {
    if (selectedSchemas.value.length > 0) {
      loadTableListForSchemas()
    }
  })
  loadModelList()
  checkClassicViewPermission()
  loadPromptList()
}

function checkClassicViewPermission() {
  http.get('/canUseClassicView').then(resp => {
    canUseClassicView.value = !!(resp.data.data && resp.data.data.allowed)
  }).catch(() => {
    canUseClassicView.value = false
  })
}

function loadPromptList() {
  loadPrompts()
}

function openSystemManagement() {
  console.log('[App.vue] 打开系统管理页面，当前 currentUser:', currentUser.value)
  // 将 currentUser 存储到 sessionStorage
  sessionStorage.setItem('systemManagement_user', JSON.stringify(currentUser.value))
  router.push('/system-management')
}

function getSysModel() {
  http.get("/sysMode").then((resp) => {
    isRemote.value = resp.data?.isRemote ?? resp.data?.data?.isRemote ?? false
    sessionStorage.setItem("isRemote", isRemote.value.toString())
    if (!loginSucc.value && isRemote.value) {
      loginDialogVisible.value = true
    }
  })
}

// --- 历史会话管理 ---
function formatDate(isoString) {
  if (!isoString) {
    return '未知时间'
  }

  const date = new Date(isoString)
  if (isNaN(date.getTime())) {
    return '未知时间'
  }

  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  const seconds = String(date.getSeconds()).padStart(2, '0')

  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
}

async function loadSessionList() {
  loadingSessions.value = true
  sessionList.value = []
  sessionCurrentPage.value = 1
  sessionSearchKeyword.value = ''

  await fetchSessionList()
}

async function fetchSessionList() {
  loadingSessions.value = true
  const apiBase = import.meta.env.VITE_API_URL || ''
  const params = new URLSearchParams()
  params.set('page', String(sessionCurrentPage.value))
  params.set('pageSize', String(sessionPageSize.value))
  const keyword = sessionSearchKeyword.value.trim()
  if (keyword) {
    params.set('keyword', keyword)
  }
  const url = apiBase + '/ai/agent/sessions?' + params.toString()
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const resp = await fetch(url, {
      method: 'GET',
      headers: { 'Authorization': auth }
    })

    if (resp.status === 401) {
      const errorData = await resp.json().catch(() => ({}))
      if (errorData.code === 401) {
        ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
        handleSessionExpired()
        return
      }
    }

    if (!resp.ok) {
      throw new Error(`请求失败：${resp.status}`)
    }

    const data = await resp.json()
    sessionList.value = data.sessions || []
    sessionListTotal.value = data.total || 0
  } catch (e) {
    console.error('[App] 加载历史会话失败:', e)
    ElMessage({ message: '加载历史会话失败', type: 'error' })
    sessionList.value = []
    sessionListTotal.value = 0
  } finally {
    loadingSessions.value = false
  }
}

let sessionSearchDebounceTimer = null

function handleSessionSearchInput() {
  sessionCurrentPage.value = 1
  if (sessionSearchDebounceTimer) {
    clearTimeout(sessionSearchDebounceTimer)
  }
  sessionSearchDebounceTimer = setTimeout(() => {
    sessionSearchDebounceTimer = null
    fetchSessionList()
  }, 300)
}

function handleSessionPageChange(page) {
  sessionCurrentPage.value = page
  fetchSessionList()
}

function handleSessionSizeChange(size) {
  sessionPageSize.value = size
  sessionCurrentPage.value = 1
  fetchSessionList()
}

async function deleteSession(id) {
  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/session/delete?sessionId=' + encodeURIComponent(id)
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const resp = await fetch(url, {
      method: 'GET',
      headers: { 'Authorization': auth }
    })

    if (resp.status === 401) {
      const errorData = await resp.json().catch(() => ({}))
      if (errorData.code === 401) {
        ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
        handleSessionExpired()
        return
      }
    }

    if (!resp.ok) {
      throw new Error(`请求失败：${resp.status}`)
    }

    ElMessage({ message: '会话已删除', type: 'success' })
    if (sessionList.value.length <= 1 && sessionCurrentPage.value > 1) {
      sessionCurrentPage.value -= 1
    }
    await fetchSessionList()
  } catch (e) {
    console.error('[App] 删除会话失败:', e)
    ElMessage({ message: '删除会话失败', type: 'error' })
  }
}

function handleClickSession(id) {
  // 先关闭 popover，然后加载会话
  sessionHistoryVisible.value = false
  // 延迟一点时间加载会话，让 popover 先关闭
  setTimeout(() => {
    loadSession(id)
  }, 100)
}

async function loadSession(id) {
  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/session?sessionId=' + encodeURIComponent(id)
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const resp = await fetch(url, {
      method: 'GET',
      headers: { 'Authorization': auth }
    })

    if (resp.status === 401) {
      const errorData = await resp.json().catch(() => ({}))
      if (errorData.code === 401) {
        ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
        handleSessionExpired()
        return
      }
    }

    if (!resp.ok) {
      throw new Error(`请求失败：${resp.status}`)
    }

    const data = await resp.json()
    if (data.session) {
      // 清空当前会话
      clearSession()

      // 加载历史消息
      sessionId.value = data.session.id
      for (const msg of data.session.messages) {
        const isSql = /^\s*(SELECT|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|DESCRIBE|EXPLAIN|WITH)\s/i.test(msg.content.trim())
        chatHistory.value.push({
          role: msg.role,
          content: msg.content,
          hasSql: isSql,
          collapsed: true
        })
        if (isSql) lastSql.value = msg.content
      }

      // 恢复会话上下文（当时选择的 schemas 和 tables）
      const ctx = data.session.context
      if (ctx && ctx.schemas && ctx.schemas.length > 0) {
        const schemaValues = ctx.schemas
          .filter(s => s.connId && s.schema)
          .map(s => s.connId + '::' + s.schema)
        if (schemaValues.length > 0) {
          selectedSchemas.value = schemaValues
          await loadTableListForSchemas()
          if (ctx.tables && ctx.tables.length > 0) {
            selectedTables.value = ctx.tables.filter(t => tableList.value.some(tl => tl.label === t || tl.name === t))
          }
        }
      }

      ElMessage({ message: '已加载历史会话', type: 'success' })
      scrollToBottom()
      // 历史会话加载完成后立即渲染 mermaid
      doRenderMermaidBlocks()
    }
  } catch (e) {
    console.error('[App] 加载会话失败:', e)
    ElMessage({ message: '加载会话失败', type: 'error' })
  }
}

watch(selectedTables, (val) => {
  try {
    sessionStorage.setItem('lastSelectedTables', JSON.stringify(val))
  } catch (_) {}
}, { deep: true })

function handleExportLinkClick(e) {
  const link = e.target.closest('a[data-export-link]')
  if (!link) return
  const authToken = sessionStorage.getItem('authentication')
  if (!authToken) return
  e.preventDefault()
  let href = link.getAttribute('href')
  const separator = href.includes('?') ? '&' : '?'
  href = href + separator + 'token=' + encodeURIComponent(authToken)
  window.open(href, '_blank')
}

function handleCodeCopyClick(e) {
  const btn = e.target.closest('.code-copy-btn')
  if (!btn) return
  const encoded = btn.dataset.code
  if (!encoded) return
  try {
    const code = decodeURIComponent(atob(encoded))
    navigator.clipboard.writeText(code).then(() => {
      const orig = btn.textContent
      btn.textContent = '✓'
      setTimeout(() => { btn.textContent = orig }, 1500)
    })
  } catch (_) {}
}

// ── msgContainer 事件绑定/解绑（核心修复：使用 watch 确保 DOM 重建后事件重新绑定） ──
// 当 v-if/v-else 切换导致 msgContainer DOM 元素被销毁重建时，
// 仅在 onMounted 中绑定一次会导致事件丢失。用 watch 监听 ref 变化来自动重绑。
let _prevMsgEl = null
function attachMsgContainerEvents(el) {
  if (!el || el === _prevMsgEl) return
  // 先清理旧元素上的事件（如果有）
  detachMsgContainerEvents(_prevMsgEl)
  _prevMsgEl = el
  el.addEventListener('wheel', handleMermaidWheel, { passive: false })
  el.addEventListener('mousedown', handleMermaidMouseDown)
  el.addEventListener('mousedown', handleMermaidResizeDown)
}
function detachMsgContainerEvents(el) {
  if (!el) return
  el.removeEventListener('wheel', handleMermaidWheel)
  el.removeEventListener('mousedown', handleMermaidMouseDown)
  el.removeEventListener('mousedown', handleMermaidResizeDown)
}

// watch msgContainer ref：每当 DOM 元素出现/变化时重新绑定事件
watch(msgContainer, (newEl, oldEl) => {
  if (oldEl && oldEl !== newEl) {
    detachMsgContainerEvents(oldEl)
  }
  if (newEl) {
    attachMsgContainerEvents(newEl)
  }
}, { flush: 'post' })

onMounted(() => {
  const { initTheme } = useTheme()
  initTheme()
  initHeavyDeps()
  preloadHeavyDeps()
  setupMermaidObserver()
  setSendToAIHandler(handlePromptSendToAI)
  getSysModel()
  loadModelList()
  loadConnList().then(() => {
    if (loginSucc.value || !isRemote.value) {
      loadTableListForSchemas().then(() => {
        const savedTables = sessionStorage.getItem('lastSelectedTables')
        if (savedTables) {
          try {
            const parsedTables = JSON.parse(savedTables)
            if (Array.isArray(parsedTables) && parsedTables.length > 0) {
              selectedTables.value = parsedTables.filter(t => tableList.value.some(tl => tl.label === t || tl.name === t))
            }
          } catch (_) {}
        }
      })
    }
  })
  if (loginSucc.value) {
    checkClassicViewPermission()
  }
  document.addEventListener('keydown', handleEscKey)
  document.addEventListener('keydown', handleMermaidKeyDown)
  document.addEventListener('keyup', handleMermaidKeyUp)
  document.addEventListener('mousemove', handleMermaidMouseMove)
  document.addEventListener('mouseup', handleMermaidMouseUp)
  document.addEventListener('mousemove', handleMermaidResizeMove)
  document.addEventListener('mouseup', handleMermaidResizeUp)
  document.addEventListener('click', handleMermaidToolbarClick)
  window.addEventListener('session-expired', handleSessionExpiredEvent)
  document.addEventListener('click', handleExportLinkClick)
  document.addEventListener('click', handleCodeCopyClick)
  const authorization = new URLSearchParams(window.location.search).get('authorization')
  showLoginBtn.value = !authorization
  // msgContainer 事件绑定由 watch 自动处理，此处做一次兜底确保首次渲染时也能绑定
  nextTick(() => {
    if (msgContainer.value) {
      attachMsgContainerEvents(msgContainer.value)
    }
  })
  // 空闲时预加载 Vditor 模块，用户打开编辑器时无需等待
  if (window.requestIdleCallback) {
    window.requestIdleCallback(() => preloadVditor(), { timeout: 5000 })
  } else {
    setTimeout(() => preloadVditor(), 3000)
  }
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleEscKey)
  document.removeEventListener('keydown', handleMermaidKeyDown)
  document.removeEventListener('keyup', handleMermaidKeyUp)
  document.removeEventListener('mousemove', handleMermaidMouseMove)
  document.removeEventListener('mouseup', handleMermaidMouseUp)
  document.removeEventListener('mousemove', handleMermaidResizeMove)
  document.removeEventListener('mouseup', handleMermaidResizeUp)
  document.removeEventListener('click', handleMermaidToolbarClick)
  window.removeEventListener('session-expired', handleSessionExpiredEvent)
  document.removeEventListener('click', handleExportLinkClick)
  document.removeEventListener('click', handleCodeCopyClick)
  detachMsgContainerEvents(_prevMsgEl)
  _prevMsgEl = null
  teardownMermaidObserver()
  document.body.classList.remove('mermaid-ctrl-held', 'mermaid-dragging')
})
</script>

<style scoped>
/* ========== 外层容器 - 填满整个视口 ========== */
.ai-sql-panel-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 内容容器 - 添加左右留白 */
.container {
  width: 70%;
  margin: 0 auto;
  height: 100%;
  display: flex;
  flex-direction: column;
}

/* 登录按钮容器 - 固定在左下角 */
.login-button-container {
  position: fixed;
  left: 20px;
  bottom: 20px;
  z-index: 1000;
  opacity: 0.6;
  transition: opacity 0.3s ease;
}

.login-button-container:hover {
  opacity: 1;
}

/* 覆盖 Element Plus 的默认按钮间距，确保垂直布局不受影响 */
.login-button-container .el-button + .el-button {
  margin-left: 0 !important;
}

/* ========== 主容器 - 专业蓝灰渐变 ========== */
.container {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 0;
  padding: 0;
  border-radius: 0;
  background: linear-gradient(135deg, #f8f9fa 0%, #e9ecef 100%);
}

/* ========== 聊天消息容器 ========== */
.chat-messages {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 8px 5px;
  min-height: 0;
  background: rgba(255, 255, 255, 0.9);
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.06);
  overflow-x: hidden;
}

/* 自定义滚动条 - 蓝灰色 */
.chat-messages::-webkit-scrollbar {
  width: 6px;
}

.chat-messages::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.03);
  border-radius: 3px;
}

.chat-messages::-webkit-scrollbar-thumb {
  background: linear-gradient(180deg, #546e7a 0%, #37474f 100%);
  border-radius: 3px;
  transition: background 0.3s ease;
}

.chat-messages::-webkit-scrollbar-thumb:hover {
  background: linear-gradient(180deg, #607d8b 0%, #455a64 100%);
}

/* ========== 聊天气泡 ========== */
.chat-bubble {
  border-radius: 16px;
  padding: 12px 16px;
  font-size: 14px;
  line-height: 1.6;
  position: relative;
  animation: slideIn 0.3s ease-out;
  backdrop-filter: blur(10px);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
  transition: all 0.3s ease;
}

.chat-bubble:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
}

@keyframes slideIn {
  from {
    opacity: 0;
    transform: translateY(10px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* 用户消息气泡 - 浅蓝渐变 */
.chat-bubble.user {
  align-self: flex-end;
  background: linear-gradient(135deg, #64b5f6 0%, #f0f0f0 100%);
  color: #fff;
  border-bottom-right-radius: 4px;
  box-shadow: 0 4px 12px rgba(100, 181, 246, 0.25);
}

.chat-bubble.user .bubble-label {
  color: rgba(255, 255, 255, 0.95);
}

/* AI 消息气泡 - 冷白色 */
.chat-bubble.assistant {
  align-self: flex-start;
  background: linear-gradient(135deg, #ffffff 0%, #f5f5f5 100%);
  color: #212121;
  border-bottom-left-radius: 4px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}

.chat-bubble.assistant .bubble-label {
  color: #546e7a;
}

/* ========== 标签样式 ========== */
.bubble-label {
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 4px;
  letter-spacing: 0.3px;
  text-transform: uppercase;
}

.bubble-content {
  word-break: break-word;
}

/* ========== 思考过程块 - 冷色调 ========== */
.thinking-block {
  border: 1px solid rgba(84, 110, 122, 0.2);
  border-radius: 12px;
  background: linear-gradient(135deg, rgba(236, 239, 241, 0.6) 0%, rgba(224, 228, 230, 0.4) 100%);
  padding: 12px;
  margin: 8px 0;
  backdrop-filter: blur(10px);
  box-shadow: 0 2px 8px rgba(84, 110, 122, 0.1);
  transition: all 0.3s ease;
}

.thinking-block:hover {
  box-shadow: 0 4px 12px rgba(84, 110, 122, 0.15);
  transform: translateX(4px);
}

.thinking-label {
  font-size: 13px;
  color: #37474f;
  margin-bottom: 8px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  transition: color 0.3s ease;
}

.thinking-label:hover {
  color: #546e7a;
}

.thinking-content {
  font-size: 13px;
  color: #455a64;
  word-break: break-word;
  max-height: 400px;
  overflow-y: auto;
  margin: 0;
  padding: 8px 12px;
  background: rgba(255, 255, 255, 0.7);
  border-radius: 8px;
  line-height: 1.6;
}

.thinking-content :deep(p) {
  margin-top: 0;
  margin-bottom: 8px;
}

.thinking-content :deep(p:last-child) {
  margin-bottom: 0;
}

.thinking-content :deep(code) {
  padding: 2px 6px;
  background: rgba(0, 0, 0, 0.06);
  border-radius: 4px;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
  color: #c62828;
}

.thinking-content :deep(pre) {
  margin: 8px 0;
  padding: 12px;
  background: rgba(0, 0, 0, 0.04);
  border-radius: 6px;
  overflow: auto;
  font-size: 12px;
  line-height: 1.5;
}

.thinking-content :deep(pre code) {
  padding: 0;
  background: transparent;
  color: inherit;
}

.thinking-content::-webkit-scrollbar {
  width: 4px;
}

.thinking-content::-webkit-scrollbar-thumb {
  background: #78909c;
  border-radius: 2px;
}

/* ========== 工具调用块 - 青绿色 ========== */
.tool-call-block {
  font-size: 13px;
  color: #00796b;
  padding: 10px 14px;
  background: linear-gradient(135deg, rgba(224, 242, 241, 0.9) 0%, rgba(178, 223, 219, 0.7) 100%);
  border-radius: 10px;
  border: 1px solid rgba(0, 121, 107, 0.2);
  font-weight: 500;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  box-shadow: 0 2px 8px rgba(0, 121, 107, 0.1);
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {

  0%,
  100% {
    opacity: 1;
  }

  50% {
    opacity: 0.85;
  }
}

/* ========== SQL 代码块 - VSCode 风格 ========== */
.sql-pre {
  margin: 0;
  padding: 12px;
  overflow: auto;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  background: linear-gradient(180deg, #1e1e1e 0%, #252526 100%);
  color: #d4d4d4;
  border-radius: 8px;
  border: 1px solid #3c3c3c;
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.5);
}

.sql-pre::-webkit-scrollbar {
  height: 6px;
}

.sql-pre::-webkit-scrollbar-thumb {
  background: #505050;
  border-radius: 3px;
}

.cursor-blink {
  animation: blink 1s step-start infinite;
  font-size: 14px;
  color: #569cd6;
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}

/* ========== Markdown 样式（基础容器，子元素样式在 unscoped 块中） ========== */
.markdown-body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif;
  font-size: 14px;
  line-height: 1.7;
  color: #1f2937;
  word-wrap: break-word;
  overflow-wrap: break-word;
}

/* ========== 历史会话项样式 ========== */
.session-history-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 340px;
}

.session-search-box {
  flex-shrink: 0;
}

.session-list-scroll {
  max-height: 400px;
  overflow-y: auto;
  min-height: 60px;
}

.session-pagination {
  display: flex;
  justify-content: center;
  padding-top: 4px;
  border-top: 1px solid var(--border-primary);
}

.session-pagination :deep(.el-pager li),
.session-pagination :deep(.btn-prev),
.session-pagination :deep(.btn-next) {
  background: transparent !important;
}

.session-item {
  display: flex;
  align-items: center;
  padding: 10px 16px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-primary);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
  position: relative;
  overflow: hidden;
}

.session-item::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  width: 3px;
  background: linear-gradient(180deg, var(--accent-color) 0%, var(--text-secondary) 100%);
  opacity: 0;
  transition: opacity 0.3s ease;
}

.session-item:hover {
  border-color: var(--border-secondary);
  background: var(--bg-hover);
}

.session-item:hover::before {
  opacity: 0.8;
}

.session-content {
  flex: 1;
  cursor: pointer;
  min-width: 0;
  padding-right: 8px;
}

.session-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color 0.3s ease;
}

.session-item:hover .session-title {
  color: var(--accent-color);
}

.session-time {
  font-size: 12px;
  color: var(--text-tertiary);
  display: flex;
  align-items: center;
  gap: 4px;
  font-weight: 500;
}

.session-time .el-icon {
  font-size: 12px;
}

.session-actions {
  margin-left: 8px;
  display: flex;
  align-items: center;
  gap: 4px;
  opacity: 1;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  transform: translateX(0);
}

/* ========== 输入区域美化 ========== */
.input-area {
  flex-shrink: 0;
  border-top: 1px solid var(--border-primary);
  padding: 12px 16px;
  background: var(--bg-secondary);
  backdrop-filter: blur(10px);
  box-shadow: 0 -2px 8px rgba(0, 0, 0, 0.05);
  box-sizing: border-box;
}

.input-label {
  margin-bottom: 8px;
  font-size: 13px;
  color: var(--text-secondary);
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}

.input-label span {
  display: flex;
  align-items: center;
  gap: 6px;
}

.input-label span::before {
  content: '💡';
  font-size: 14px;
}

/* 按钮组美化 */
.el-button-group {
  display: flex;
  gap: 6px;
}

/* 工具栏按钮美化 */
.toolbar-btn {
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  border-radius: 6px;
  font-weight: 500;
}

.toolbar-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 122, 204, 0.2);
}

/* 输入框美化 */
:deep(.el-textarea__inner) {
  border-radius: 10px;
  border: 1px solid var(--border-primary);
  transition: all 0.3s ease;
  font-size: 14px;
}

:deep(.el-textarea__inner:hover) {
  border-color: var(--border-secondary);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.08);
}

:deep(.el-textarea__inner:focus) {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.12);
}

/* 选择器美化 */
:deep(.el-select) {
  width: 100%;
}

:deep(.el-select .el-input__inner) {
  border-radius: 10px;
}

:deep(.el-select-dropdown__item.selected) {
  background: linear-gradient(90deg, rgba(0, 122, 204, 0.1) 0%, transparent 100%);
  color: var(--accent-color);
  font-weight: 600;
}

/* 空状态美化 */
:deep(.el-empty) {
  padding: 20px 0;
}

:deep(.el-empty__description) {
  color: var(--text-tertiary);
  font-size: 13px;
}

/* 骨架屏美化 */
:deep(.el-skeleton) {
  border-radius: 8px;
}

:deep(.el-skeleton__item) {
  background: linear-gradient(90deg, var(--bg-hover) 25%, var(--bg-active) 50%, var(--bg-hover) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.5s infinite;
  border-radius: 6px;
}

@keyframes shimmer {
  0% {
    background-position: 200% 0;
  }

  100% {
    background-position: -200% 0;
  }
}

/* 滚动条全局美化 */
:deep(.el-drawer__body) {
  padding: 3px;
  scrollbar-width: thin;
  scrollbar-color: var(--text-tertiary) rgba(0, 0, 0, 0.05);
}

:deep(.el-drawer__header) {
  margin-bottom: 3px;
}

/* Popover 美化 */
:deep(.el-popover) {
  border-radius: 12px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.15);
  border: 1px solid var(--border-primary);
}

/* ========== 输入区域布局 ========== */
.input-action-row {
  display: flex;
  gap: 12px;
  margin-top: 12px;
  flex-shrink: 0;
}

.schema-hints {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
}

.schema-hints-label {
  font-size: 12px;
  color: var(--text-tertiary);
}

.schema-hints-warn {
  font-size: 12px;
  color: var(--danger-color);
}

.question-input {
  flex: 1;
}

.question-input :deep(.el-textarea__inner) {
  border-radius: 12px;
  border: 1.5px solid var(--border-primary);
  transition: all 0.3s ease;
  font-size: 14px;
  line-height: 1.6;
  background: var(--bg-primary);
  backdrop-filter: blur(10px);
}

.question-input :deep(.el-textarea__inner:hover) {
  border-color: var(--border-secondary);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.08);
}

.question-input :deep(.el-textarea__inner:focus) {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.12);
}

.action-buttons {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 50px;
}

/* 发送按钮 - 使用默认 primary 颜色 */
.send-btn {
  padding: 8px 20px;
  min-width: 38px;
  min-height: 30px;
  border-radius: 8px;
}

.send-btn .el-icon {
  margin-right: 0;
}

/* 停止按钮 */
.stop-btn {
  padding: 8px 20px;
  min-width: 38px;
  min-height: 30px;
  border-radius: 8px;
  animation: stopPulse 1.5s ease-in-out infinite;
}

.stop-btn .el-icon {
  margin-right: 0;
}

@keyframes stopPulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.7; }
}

/* 加入编辑器按钮美化 - 柔和青绿 */
.insert-btn {
  border-radius: 8px;
  font-weight: 500;
  font-size: 13px;
  padding: 8px 12px;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  border: 1.5px solid #26a69a;
  background: linear-gradient(135deg, #4db6ac 0%, #26a69a 100%);
  color: #fff;
  box-shadow: 0 2px 8px rgba(77, 182, 172, 0.2);
  min-width: 38px;
  min-height: 38px;
}

.insert-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(77, 182, 172, 0.3);
  background: linear-gradient(135deg, #26a69a 0%, #00897b 100%);
  border-color: #00897b;
}

.insert-btn .el-icon {
  margin-right: 4px;
  font-size: 16px;
}

/* 切换视图链接 - 无下划线超链接样式 */
.switch-view-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #409eff;
  text-decoration: none;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: 4px;
  transition: all 0.3s ease;
}

.switch-view-link:hover {
  color: #66b1ff;
  background-color: rgba(64, 158, 255, 0.05);
}

.switch-view-link .el-icon {
  font-size: 16px;
}

.switch-view-link span {
  font-weight: 500;
}

/* ========== 数据库 / 表选择器 - 现代化设计 ========== */
.table-selector-row {
  display: flex;
  gap: 16px;
  margin-bottom: 12px;
  flex-shrink: 0;
  align-items: flex-start;
}

.table-selector-row .table-selector-container:first-child {
  flex: 0 0 calc(20% - 8px);
}

.table-selector-row .table-selector-container:nth-child(2) {
  flex: 0 0 calc(50% - 8px);
}

.table-selector-row .model-selector-container {
  flex: 0 0 calc(30% - 8px);
}

.table-selector-row .table-selector-container.full-width {
  flex: 1;
}

.table-selector-container {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.selector-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.selector-header .table-selector-label {
  margin-bottom: 0;
}

.table-selector-label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  font-weight: 600;
  letter-spacing: 0.3px;
  text-transform: uppercase;
}

.selector-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 10px;
  font-size: 11px;
  font-weight: 600;
  border-radius: 100px;
  background: linear-gradient(135deg, rgba(0, 122, 204, 0.15) 0%, rgba(0, 122, 204, 0.08) 100%);
  color: var(--accent-color);
  letter-spacing: 0.3px;
  transition: all 0.3s ease;
  white-space: nowrap;
}

.selector-badge.ready {
  background: linear-gradient(135deg, rgba(78, 201, 176, 0.15) 0%, rgba(78, 201, 176, 0.08) 100%);
  color: var(--success-color);
}

@keyframes badgePulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}

.selector-skeleton {
  width: 100%;
  height: 32px;
  border-radius: 10px;
  border: 1.5px solid var(--border-primary);
  overflow: hidden;
  position: relative;
  box-sizing: border-box;
}

.selector-skeleton::after {
  content: '加载中...';
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 14px;
  color: var(--text-placeholder);
  pointer-events: none;
  letter-spacing: 0.3px;
}

.skeleton-shimmer {
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, var(--bg-hover) 0%, var(--bg-active) 35%, var(--bg-hover) 65%);
  background-size: 200% 100%;
  animation: skeletonSlide 1.5s ease-in-out infinite;
  border-radius: 8px;
}

@keyframes skeletonSlide {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

.modern-tree-select {
  width: 100%;
  margin: 0;
}

.modern-select {
  width: 100%;
  margin: 0;
}

.table-option-content {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 0;
  width: 100%;
}

.table-option-name {
  font-weight: 600;
  color: var(--text-primary);
  font-size: 13px;
  flex-shrink: 0;
}

.table-option-comment {
  font-size: 12px;
  color: var(--text-tertiary);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.table-option-comment::before {
  content: '— ';
  color: var(--text-placeholder);
}

.table-option-schema {
  font-size: 11px;
  color: var(--accent-color);
  background: rgba(0, 122, 204, 0.1);
  padding: 1px 6px;
  border-radius: 4px;
  font-weight: 500;
  flex-shrink: 0;
}

.model-option-content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
}

.model-option-name {
  font-weight: 600;
  color: var(--text-primary);
  font-size: 13px;
  flex: 1;
}

.model-option-provider {
  font-size: 11px;
  color: var(--text-tertiary);
  background: var(--bg-active);
  padding: 1px 6px;
  border-radius: 4px;
  flex-shrink: 0;
}

/* 多条 SQL 批量确认 */
.multi-sql-confirm {
  border: 2px solid var(--warning-color);
  border-radius: 8px;
  padding: 16px;
  background: var(--bg-row-changed);
  margin: 8px 16px;
  flex-shrink: 0;
}
.sql-confirm-item {
  margin: 8px 0;
  padding: 8px;
  background: var(--bg-primary);
  border-radius: 6px;
  border: 1px solid var(--border-primary);
}
.sql-confirm-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.sql-preview-code {
  margin: 0;
  padding: 8px;
  background: var(--bg-secondary);
  border-radius: 4px;
  font-family: 'Consolas', monospace;
  font-size: 12px;
  max-height: 100px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

/* 重试确认 */
.retry-confirm-block {
  border: 1px solid var(--warning-color);
  border-radius: 8px;
  padding: 16px;
  background: var(--bg-row-changed);
  margin: 8px 16px;
  flex-shrink: 0;
}

/* Excel 上传 */
.uploaded-file-info {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  background: var(--bg-active);
  border-radius: 6px;
  font-size: 13px;
  color: var(--accent-color);
}

/* 提示词弹窗 */
.prompt-popover-body {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.prompt-popover-body .prompt-tabs {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.prompt-popover-body .el-tabs__content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.prompt-popover-body .el-tab-pane {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.prompt-popover-body .prompt-toolbar {
  flex-shrink: 0;
}

.prompt-popover-body .prompt-search-box {
  flex-shrink: 0;
  padding: 4px 8px 8px;
}

.prompt-popover-body .prompt-pagination {
  flex-shrink: 0;
  display: flex;
  justify-content: center;
  padding-top: 4px;
  border-top: 1px solid var(--border-primary);
}

.prompt-popover-body .prompt-pagination :deep(.el-pager li),
.prompt-popover-body .prompt-pagination :deep(.btn-prev),
.prompt-popover-body .prompt-pagination :deep(.btn-next) {
  background: transparent !important;
}

.prompt-popover-body .prompt-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 8px 8px;
}

.prompt-popover-body .prompt-empty {
  text-align: center;
  color: var(--text-tertiary);
  padding: 40px 0;
  font-size: 14px;
}

.prompt-popover-body .prompt-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 5px;
  border-radius: 5px;
  cursor: pointer;
  transition: all 0.2s;
  margin-bottom: 2px;
}

.prompt-popover-body .prompt-item:hover {
  background: var(--bg-hover);
}

.prompt-popover-body .prompt-item-info {
  flex: 1;
  min-width: 0;
}

.prompt-popover-body .prompt-item-title {
  font-size: 14px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
}

.prompt-popover-body .prompt-item-sub {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--text-tertiary);
  margin-top: 2px;
}

.prompt-popover-body .prompt-item-actions {
  display: flex;
  gap: 0;
  opacity: 0;
  transition: opacity 0.2s;
  flex-shrink: 0;
}

.prompt-popover-body .prompt-item:hover .prompt-item-actions {
  opacity: 1;
}

.prompt-detail-meta {
  margin-bottom: 8px;
}

.prompt-detail-meta-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}

.prompt-detail-meta-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-height: 120px;
  overflow-y: auto;
}

.prompt-detail-content {
  max-height: 50vh;
  overflow-y: auto;
}
</style>

<style>
/* ========== Markdown v-html 内容样式（unscoped，确保 v-html 注入的 DOM 元素能被正确样式化） ========== */
.markdown-body p {
  margin-top: 0;
  margin-bottom: 12px;
}
.markdown-body p:last-child {
  margin-bottom: 0;
}
.markdown-body h1,
.markdown-body h2,
.markdown-body h3,
.markdown-body h4,
.markdown-body h5,
.markdown-body h6 {
  margin-top: 20px;
  margin-bottom: 10px;
  font-weight: 700;
  line-height: 1.3;
  color: #1a202c;
}
.markdown-body h1 { font-size: 24px; border-bottom: 2px solid #e2e8f0; padding-bottom: 6px; }
.markdown-body h2 { font-size: 20px; border-bottom: 1px solid #e2e8f0; padding-bottom: 4px; }
.markdown-body h3 { font-size: 18px; }
.markdown-body h4 { font-size: 16px; }
.markdown-body h5 { font-size: 14px; }
.markdown-body h6 { font-size: 13px; }
.markdown-body ul,
.markdown-body ol {
  padding-left: 2em;
  margin-top: 8px;
  margin-bottom: 12px;
}
.markdown-body ul { list-style-type: disc; }
.markdown-body ul ul { list-style-type: circle; }
.markdown-body ul ul ul { list-style-type: square; }
.markdown-body ol { list-style-type: decimal; }
.markdown-body li {
  margin-top: 6px;
  margin-bottom: 6px;
  line-height: 1.6;
}
.markdown-body li+li { margin-top: 6px; }
.markdown-body code {
  padding: 3px 8px;
  margin: 0;
  font-size: 13px;
  background: linear-gradient(135deg, #eceff1 0%, #cfd8dc 100%);
  border-radius: 6px;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  color: #c62828;
  border: 1px solid rgba(0, 0, 0, 0.08);
}
.markdown-body eq {
  display: inline;
}
.markdown-body eqn {
  display: block;
  text-align: center;
  margin: 1em 0;
}
.markdown-body eq code,
.markdown-body eqn code {
  padding: 0;
  margin: 0;
  font-size: inherit;
  background: none;
  border-radius: 0;
  font-family: inherit;
  color: inherit;
  border: none;
}
.markdown-body pre {
  overflow: auto;
  font-size: 13px;
  line-height: 1.6;
  border-radius: 10px;
  margin-top: 12px;
  margin-bottom: 12px;
  max-width: 100%;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
  border: 1px solid rgba(0, 0, 0, 0.2);
}
.markdown-body pre code {
  display: block;
  padding: 5px;
  margin: 0;
  overflow: auto;
  line-height: inherit;
  word-wrap: normal;
  background-color: transparent;
  border-radius: 0;
  /* color: #cfd8dc; */
  white-space: pre;
  border: none;
}
.markdown-body blockquote {
  padding: 12px 16px;
  color: #546e7a;
  border-left: 4px solid #546e7a;
  margin: 12px 0;
  background: rgba(84, 110, 122, 0.05);
  border-radius: 0 8px 8px 0;
  font-style: italic;
}
.markdown-body table {
  border-collapse: collapse;
  width: 100%;
  max-width: 100%;
  margin-top: 12px;
  margin-bottom: 12px;
  font-size: 13px;
  display: block;
  overflow-x: auto;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}
.markdown-body table th,
.markdown-body table td {
  padding: 10px 14px;
  border: 1px solid #e2e8f0;
  white-space: nowrap;
}
.markdown-body table th {
  font-weight: 700;
  background: linear-gradient(180deg, #eceff1 0%, #cfd8dc 100%);
  color: #263238;
  position: sticky;
  top: 0;
  text-transform: uppercase;
  font-size: 12px;
  letter-spacing: 0.5px;
}
.markdown-body table tr:nth-child(2n) { background-color: #f5f5f5; }
.markdown-body table tr:hover { background-color: #eceff1; }
.markdown-body .table-wrapper {
  overflow-x: auto;
  margin: 12px 0;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}
.markdown-body a {
  color: #1976d2;
  text-decoration: none;
  cursor: pointer;
  font-weight: 500;
  transition: all 0.2s ease;
  border-bottom: 1px solid transparent;
}
.markdown-body a:hover {
  color: #1565c0;
  text-decoration: underline;
  border-bottom-color: #1565c0;
}
.markdown-body a[target="_blank"]::after {
  content: " ↗";
  font-size: 11px;
  margin-left: 2px;
}
.markdown-body hr {
  height: 2px;
  padding: 0;
  margin: 20px 0;
  background: linear-gradient(90deg, #e2e8f0 0%, #cbd5e0 50%, #e2e8f0 100%);
  border: 0;
}
.markdown-body strong {
  font-weight: 700;
  color: #1a202c;
}
.markdown-body em {
  font-style: italic;
  color: #4a5568;
}
.markdown-body img {
  max-width: 100%;
  height: auto;
  border-radius: 8px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  margin: 8px 0;
}

/* 思考区域内的 markdown 样式覆盖（更紧凑） */
.thinking-content.markdown-body { font-size: 13px; color: #455a64; }
.thinking-content.markdown-body h1 { font-size: 18px; }
.thinking-content.markdown-body h2 { font-size: 16px; }
.thinking-content.markdown-body h3 { font-size: 15px; }
.thinking-content.markdown-body p { margin-bottom: 8px; }
.thinking-content.markdown-body pre {
  margin: 8px 0;
  padding: 12px;
  font-size: 12px;
}
.thinking-content.markdown-body code { font-size: 12px; }
.thinking-content.markdown-body ul,
.thinking-content.markdown-body ol { padding-left: 1.5em; margin: 6px 0; }
.thinking-content.markdown-body li { margin: 4px 0; }
.thinking-content.markdown-body blockquote {
  padding: 8px 12px;
  margin: 8px 0;
  border-left-width: 3px;
}
.thinking-content.markdown-body table { font-size: 12px; margin: 8px 0; }
.thinking-content.markdown-body table th,
.thinking-content.markdown-body table td { padding: 6px 10px; }
.thinking-content.markdown-body strong { color: #37474f; }

/* 用户消息气泡中的链接 */
.chat-bubble.user a {
  color: #ffffff;
  text-decoration-color: rgba(255, 255, 255, 0.6);
}
.chat-bubble.user a:hover {
  color: #e2e8f0;
  text-decoration-color: #ffffff;
}

/* Mermaid 容器（unscoped） */
.mermaid-container {
  margin: 12px 0;
  padding: 16px;
  padding-top: 16px;
  border-radius: 10px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  overflow: hidden;
  text-align: center;
  position: relative;
  max-height: 600px;
  cursor: grab;
}
body.mermaid-ctrl-held .mermaid-container {
  cursor: zoom-in !important;
}
body.mermaid-dragging,
body.mermaid-dragging .mermaid-container {
  cursor: grabbing !important;
  user-select: none !important;
}
body.mermaid-dragging .mermaid-container {
  overflow: hidden;
}

/* Mermaid 内容包装器，负责滚动 */
.mermaid-content-wrapper {
  max-height: 500px;
  position: relative;
  z-index: 0;
  overflow: hidden;
}
.mermaid-source-preview {
  margin: 0;
  padding: 12px;
  background: linear-gradient(180deg, #263238 0%, #1c282c 100%);
  border-radius: 8px;
  color: #90a4ae;
  font-size: 12px;
  text-align: left;
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.5;
  font-family: 'Consolas', 'Monaco', monospace;
  user-select: text;
  cursor: text;
  max-height: 400px;
  overflow: auto;
  position: relative;
}
.mermaid-source-preview code {
  background: transparent;
  color: #90a4ae;
  padding: 0;
  border: none;
  font-size: 12px;
  user-select: text;
}
.mermaid-source-preview::selection {
  background: rgba(25, 118, 210, 0.3);
  color: #ffffff;
}

.mermaid-container .edgeLabel,
.mermaid-container .edgeLabel p {
  background: transparent !important;
  border: none !important;
  box-shadow: none !important;
  color: #333;
}
.mermaid-container .labelBkg {
  background: transparent !important;
}

.mermaid-fullscreen-container {
  background: var(--bg-secondary, #1e1e2e);
  box-shadow: 0 8px 40px rgba(255, 255, 255, 0.6);
}

[data-theme="dark"] .mermaid-container .edgeLabel,
[data-theme="dark"] .mermaid-container .edgeLabel p {
  color: #d4d4d4;
}

[data-theme="dark"] .mermaid-source-preview {
  background: linear-gradient(180deg, #1e1e1e 0%, #252526 100%);
  color: var(--text-secondary);
}

.mermaid-error {
  margin: 0;
  padding: 12px;
  background: var(--bg-row-changed);
  border: 1px solid var(--danger-color);
  border-radius: 6px;
  color: var(--danger-color);
  font-size: 12px;
  text-align: left;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 300px;
  overflow: auto;
}
.mermaid-error-hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--warning-color);
  text-align: center;
}
.mermaid-toolbar {
  position: absolute;
  top: 8px;
  right: 8px;
  display: flex;
  align-items: center;
  gap: 2px;
  z-index: 100;
  backdrop-filter: blur(8px);
  padding: 4px;
  opacity: 0;
  transition: opacity 0.2s ease;
}
.mermaid-container:hover .mermaid-toolbar {
  opacity: 1;
}
.mermaid-tb-sep {
  width: 1px;
  height: 16px;
  background: #3c3c3c;
  margin: 0 2px;
}
.mermaid-tb-btn {
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  font-size: 11px;
  color: #9cdcfe;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 4px;
  padding: 0;
  line-height: 1;
  transition: all 0.15s;
  user-select: none;
  font-family: inherit;
  white-space: nowrap;
}
.mermaid-tb-btn:hover {
  color: #0fa1ef;
  background: rgba(0, 122, 204, 0.15);
  border-color: rgba(0, 122, 204, 0.3);
}
.mermaid-tb-btn:active {
  background: rgba(0, 122, 204, 0.25);
}
.mermaid-tb-btn svg {
  flex-shrink: 0;
}
.mermaid-container .node rect,
.mermaid-container .node polygon,
.mermaid-container .node path,
.mermaid-container .cluster rect,
.mermaid-container .cluster path,
.mermaid-container .subgraph rect,
.mermaid-container .subgraph path {
  rx: 8;
  ry: 8;
}
.mermaid-svg-wrap {
  text-align: center;
  transform-origin: 0 0;
}
.mermaid-svg-wrap svg {
  max-width: 100%;
  height: auto;
}
/* Mermaid 高度拖拽手柄 */
.mermaid-resize-handle {
  height: 12px;
  cursor: ns-resize;
  display: flex;
  align-items: center;
  justify-content: center;
  user-select: none;
  border-top: 1px solid #3c3c3c;
  margin-top: 4px;
  opacity: 0;
  transition: opacity 0.2s ease;
}
.mermaid-container:hover .mermaid-resize-handle {
  opacity: 1;
}
.mermaid-resize-dots {
  font-size: 14px;
  color: #808080;
  letter-spacing: 2px;
  line-height: 1;
}
.mermaid-resize-handle:hover .mermaid-resize-dots {
  color: #569cd6;
}
body.mermaid-resizing,
body.mermaid-resizing * {
  cursor: ns-resize !important;
  user-select: none !important;
}

/* ========== Dark Mode Overrides ========== */

/* ── Markdown Dark Mode ── */
[data-theme="dark"] .markdown-body {
  color: #d4d4d4;
}

[data-theme="dark"] .markdown-body h1,
[data-theme="dark"] .markdown-body h2,
[data-theme="dark"] .markdown-body h3,
[data-theme="dark"] .markdown-body h4,
[data-theme="dark"] .markdown-body h5,
[data-theme="dark"] .markdown-body h6 {
  color: #d4d4d4;
}

[data-theme="dark"] .markdown-body h1 {
  border-bottom-color: #3c3c3c;
}

[data-theme="dark"] .markdown-body h2 {
  border-bottom-color: #3c3c3c;
}

[data-theme="dark"] .markdown-body code {
  background: linear-gradient(135deg, #2d2d2d 0%, #3c3c3c 100%);
  color: #d16969;
  border-color: rgba(255, 255, 255, 0.1);
}
[data-theme="dark"] .markdown-body eq code,
[data-theme="dark"] .markdown-body eqn code {
  padding: 0;
  margin: 0;
  font-size: inherit;
  background: none;
  border-radius: 0;
  font-family: inherit;
  color: inherit;
  border: none;
}

[data-theme="dark"] .markdown-body pre {
  background: linear-gradient(180deg, #1e1e1e 0%, #252526 100%);
  border-color: #3c3c3c;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
}

[data-theme="dark"] .markdown-body blockquote {
  color: #9cdcfe;
  border-left-color: #007acc;
  background: rgba(0, 122, 204, 0.1);
}

[data-theme="dark"] .markdown-body table th,
[data-theme="dark"] .markdown-body table td {
  border-color: #3c3c3c;
}

[data-theme="dark"] .markdown-body table th {
  background: linear-gradient(180deg, #2d2d2d 0%, #3c3c3c 100%);
  color: #d4d4d4;
}

[data-theme="dark"] .markdown-body table tr:nth-child(2n) {
  background-color: #252526;
}

[data-theme="dark"] .markdown-body table tr:hover {
  background-color: #2a2d2e;
}

[data-theme="dark"] .markdown-body a {
  color: #569cd6;
}

[data-theme="dark"] .markdown-body a:hover {
  color: #7ab3e8;
}

[data-theme="dark"] .markdown-body hr {
  background: linear-gradient(90deg, #3c3c3c 0%, #505050 50%, #3c3c3c 100%);
}

[data-theme="dark"] .markdown-body strong {
  color: #d4d4d4;
}

[data-theme="dark"] .markdown-body em {
  color: #9cdcfe;
}

[data-theme="dark"] .thinking-content.markdown-body {
  color: #9cdcfe;
}

[data-theme="dark"] .thinking-content.markdown-body strong {
  color: #d4d4d4;
}

[data-theme="dark"] .thinking-content.markdown-body code {
  background: rgba(0, 0, 0, 0.3);
  color: #f44747;
}

[data-theme="dark"] .thinking-content.markdown-body pre {
  background: rgba(0, 0, 0, 0.25);
}

/* ── Layout & Container ── */
[data-theme="dark"] .ai-sql-panel-container {
  background: var(--bg-tertiary);
}

[data-theme="dark"] .container {
  background: var(--bg-tertiary);
}

[data-theme="dark"] .el-config-provider {
  background: var(--bg-tertiary);
}

[data-theme="dark"] .login-button-container .el-button {
  background-color: var(--bg-secondary);
  border-color: var(--border-primary);
  color: var(--text-secondary);
}

[data-theme="dark"] .login-button-container .el-button:hover {
  color: var(--accent-color);
  border-color: var(--accent-color);
}

/* ── Chat Messages ── */
[data-theme="dark"] .chat-messages {
  background: var(--bg-primary);
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.4);
}

[data-theme="dark"] .chat-messages::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.15);
}

[data-theme="dark"] .chat-messages::-webkit-scrollbar-thumb {
  background: linear-gradient(180deg, #505050 0%, #3c3c3c 100%);
}

[data-theme="dark"] .chat-messages::-webkit-scrollbar-thumb:hover {
  background: linear-gradient(180deg, #6a6a6a 0%, #505050 100%);
}

/* ── Chat Bubbles ── */
[data-theme="dark"] .chat-bubble.user {
  background: linear-gradient(135deg, #2a4a7f 0%, #2d2d2d 100%);
  color: #d4d4d4;
  box-shadow: 0 4px 12px rgba(42, 74, 127, 0.3);
}

[data-theme="dark"] .chat-bubble.user .bubble-label {
  color: rgba(212, 212, 212, 0.85);
}

[data-theme="dark"] .chat-bubble.assistant {
  background: linear-gradient(135deg, #252526 0%, #1e1e1e 100%);
  color: #d4d4d4;
  border-color: var(--border-primary);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}

[data-theme="dark"] .chat-bubble.assistant:hover {
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
}

[data-theme="dark"] .chat-bubble.assistant .bubble-label {
  color: var(--text-tertiary);
}

/* ── Thinking Block ── */
[data-theme="dark"] .thinking-block {
  border-color: rgba(128, 128, 128, 0.3);
  background: linear-gradient(135deg, rgba(60, 60, 60, 0.4) 0%, rgba(50, 50, 50, 0.3) 100%);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.25);
}

[data-theme="dark"] .thinking-block:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}

[data-theme="dark"] .thinking-label {
  color: var(--text-secondary);
}

[data-theme="dark"] .thinking-label:hover {
  color: var(--text-primary);
}

[data-theme="dark"] .thinking-content {
  color: var(--text-secondary);
  background: rgba(30, 30, 30, 0.6);
}

[data-theme="dark"] .thinking-content code {
  background: rgba(0, 0, 0, 0.3);
  color: var(--danger-color);
}

[data-theme="dark"] .thinking-content pre {
  background: rgba(0, 0, 0, 0.2);
}

[data-theme="dark"] .thinking-content::-webkit-scrollbar-thumb {
  background: #505050;
}

/* ── Tool Call Block ── */
[data-theme="dark"] .tool-call-block {
  color: #a6e3a1;
  background: linear-gradient(135deg, rgba(166, 227, 161, 0.1) 0%, rgba(166, 227, 161, 0.05) 100%);
  border-color: rgba(166, 227, 161, 0.2);
  box-shadow: 0 2px 8px rgba(166, 227, 161, 0.08);
}

/* ── Input Area ── */
[data-theme="dark"] .input-area {
  background: var(--bg-secondary);
  border-top-color: var(--border-primary);
  box-shadow: 0 -2px 8px rgba(0, 0, 0, 0.2);
}

[data-theme="dark"] .input-label {
  color: var(--text-secondary);
}

[data-theme="dark"] .question-input .el-textarea__inner {
  background: #202032;
  border-color: var(--border-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .question-input .el-textarea__inner:hover {
  border-color: var(--border-secondary);
  box-shadow: 0 0 0 3px rgba(137, 180, 250, 0.06);
}

[data-theme="dark"] .question-input .el-textarea__inner:focus {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px rgba(137, 180, 250, 0.12);
}

[data-theme="dark"] .question-input .el-textarea__inner::placeholder {
  color: var(--text-placeholder);
}

/* ── Textarea (general) ── */
[data-theme="dark"] .el-textarea__inner {
  background-color: #202032;
  color: var(--text-primary);
  border-color: var(--border-primary);
}

[data-theme="dark"] .el-textarea__inner:hover {
  border-color: var(--border-secondary);
}

[data-theme="dark"] .el-textarea__inner:focus {
  border-color: var(--accent-color);
}

/* ── Select / Tree-Select ── */
[data-theme="dark"] .modern-select .el-input__wrapper,
[data-theme="dark"] .modern-tree-select .el-input__wrapper {
  background-color: #202032;
  box-shadow: 0 0 0 1px var(--border-primary) inset;
}

[data-theme="dark"] .modern-select .el-input__wrapper:hover,
[data-theme="dark"] .modern-tree-select .el-input__wrapper:hover {
  box-shadow: 0 0 0 1px var(--border-secondary) inset;
}

[data-theme="dark"] .modern-select .el-input__wrapper.is-focus,
[data-theme="dark"] .modern-tree-select .el-input__wrapper.is-focus {
  box-shadow: 0 0 0 1px var(--accent-color) inset;
}

[data-theme="dark"] .el-select-dropdown__item.selected {
  background: linear-gradient(90deg, rgba(137, 180, 250, 0.12) 0%, transparent 100%);
  color: var(--accent-color);
}

/* ── Table Selector ── */
[data-theme="dark"] .table-selector-label {
  color: var(--text-secondary);
}

[data-theme="dark"] .selector-badge {
  background: linear-gradient(135deg, rgba(137, 180, 250, 0.15) 0%, rgba(137, 180, 250, 0.08) 100%);
  color: var(--accent-color);
}

[data-theme="dark"] .selector-badge.ready {
  background: linear-gradient(135deg, rgba(166, 227, 161, 0.15) 0%, rgba(166, 227, 161, 0.08) 100%);
  color: var(--success-color);
}

[data-theme="dark"] .selector-skeleton {
  border-color: var(--border-primary);
}

[data-theme="dark"] .selector-skeleton::after {
  color: var(--text-placeholder);
}

[data-theme="dark"] .skeleton-shimmer {
  background: linear-gradient(90deg, var(--bg-hover) 0%, var(--bg-active) 35%, var(--bg-hover) 65%);
}

/* ── Option Items ── */
[data-theme="dark"] .table-option-name {
  color: var(--text-primary);
}

[data-theme="dark"] .table-option-comment {
  color: var(--text-tertiary);
}

[data-theme="dark"] .table-option-comment::before {
  color: var(--text-placeholder);
}

[data-theme="dark"] .table-option-schema {
  color: var(--accent-color);
  background: rgba(137, 180, 250, 0.12);
}

[data-theme="dark"] .model-option-name {
  color: var(--text-primary);
}

[data-theme="dark"] .model-option-provider {
  color: var(--text-tertiary);
  background: var(--bg-active);
}

/* ── Schema Hints ── */
[data-theme="dark"] .schema-hints-label {
  color: var(--text-tertiary);
}

[data-theme="dark"] .schema-hints-warn {
  color: var(--danger-color);
}

/* ── Buttons ── */
[data-theme="dark"] .toolbar-btn.el-button--primary {
  background-color: rgba(137, 180, 250, 0.15);
  border-color: rgba(137, 180, 250, 0.25);
}

[data-theme="dark"] .toolbar-btn.el-button--danger {
  background-color: rgba(243, 139, 168, 0.15);
  border-color: rgba(243, 139, 168, 0.25);
}

[data-theme="dark"] .insert-btn {
  border-color: var(--success-color);
  background: linear-gradient(135deg, rgba(166, 227, 161, 0.2) 0%, rgba(166, 227, 161, 0.1) 100%);
  color: var(--success-color);
  box-shadow: 0 2px 8px rgba(166, 227, 161, 0.12);
}

[data-theme="dark"] .insert-btn:hover {
  background: linear-gradient(135deg, rgba(166, 227, 161, 0.3) 0%, rgba(166, 227, 161, 0.15) 100%);
  border-color: var(--success-color);
  box-shadow: 0 4px 12px rgba(166, 227, 161, 0.2);
}

[data-theme="dark"] .switch-view-link {
  color: var(--accent-color);
}

[data-theme="dark"] .switch-view-link:hover {
  color: #99c4ff;
  background-color: rgba(137, 180, 250, 0.08);
}

/* ── SQL Confirm ── */
[data-theme="dark"] .multi-sql-confirm {
  border-color: var(--warning-color);
  background: rgba(249, 226, 175, 0.08);
}

[data-theme="dark"] .sql-confirm-item {
  background: var(--bg-secondary);
  border-color: var(--border-primary);
}

/* ── Retry Confirm ── */
[data-theme="dark"] .retry-confirm-block {
  border-color: var(--warning-color);
  background: rgba(249, 226, 175, 0.08);
}

/* ── Uploaded File Info ── */
[data-theme="dark"] .uploaded-file-info {
  background: rgba(137, 180, 250, 0.1);
  color: var(--accent-color);
}

[data-theme="dark"] .uploaded-file-info .el-button--danger.is-text {
  color: var(--danger-color);
}

/* ── Session Items ── */
[data-theme="dark"] .session-item {
  background: var(--bg-secondary);
  border-color: var(--border-primary);
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.15);
}

[data-theme="dark"] .session-item::before {
  background: linear-gradient(180deg, var(--accent-color) 0%, var(--text-tertiary) 100%);
}

[data-theme="dark"] .session-item:hover {
  background: var(--bg-hover);
  border-color: var(--border-secondary);
}

[data-theme="dark"] .session-title {
  color: var(--text-primary);
}

[data-theme="dark"] .session-time {
  color: var(--text-secondary);
}

/* ── Prompt Popover ── */
[data-theme="dark"] .prompt-popover-body .prompt-item:hover {
  background: var(--bg-hover);
}

[data-theme="dark"] .prompt-popover-body .prompt-item-title {
  color: var(--text-primary);
}

[data-theme="dark"] .prompt-popover-body .prompt-item-sub {
  color: var(--text-tertiary);
}

[data-theme="dark"] .prompt-popover-body .prompt-empty {
  color: var(--text-tertiary);
}

/* ── Popover ── */
[data-theme="dark"] .el-popover {
  --el-popover-bg-color: var(--bg-primary);
  background-color: var(--bg-primary);
  border-color: var(--border-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-popover .el-button--default {
  background-color: var(--bg-secondary);
  border-color: var(--border-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-popover .el-button--default:hover {
  background-color: var(--bg-hover);
}

[data-theme="dark"] .el-popover .el-empty__description {
  color: var(--text-tertiary);
}

[data-theme="dark"] .el-popover .el-skeleton__item {
  background: linear-gradient(90deg, var(--bg-hover) 25%, var(--bg-active) 50%, var(--bg-hover) 75%);
}

[data-theme="dark"] .el-popover .el-tabs__nav-wrap::after {
  background-color: var(--border-primary);
}

[data-theme="dark"] .el-popover .el-tabs__item {
  color: var(--text-secondary);
}

[data-theme="dark"] .el-popover .el-tabs__item.is-active {
  color: var(--accent-color);
}

/* ── Dialog ── */
[data-theme="dark"] .el-dialog {
  --el-dialog-bg-color: var(--bg-primary);
  --el-dialog-title-font-size: 18px;
  background-color: var(--bg-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-dialog .el-dialog__header {
  border-bottom-color: var(--border-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-dialog .el-dialog__title {
  color: var(--text-primary);
}

[data-theme="dark"] .el-dialog .el-dialog__body {
  color: var(--text-secondary);
}

[data-theme="dark"] .el-dialog .el-dialog__footer {
  border-top-color: var(--border-primary);
}

/* ── Drawer ── */
[data-theme="dark"] .el-drawer {
  --el-drawer-bg-color: var(--bg-primary);
  background-color: var(--bg-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-drawer__header {
  color: var(--text-primary);
}

[data-theme="dark"] .el-drawer__body {
  scrollbar-color: var(--scrollbar-thumb) transparent;
}

/* ── Markdown Body (Dark) - 使用统一样式，删除重复定义 ── */

/* ── Prompt Detail Dialog (Dark) ── */
[data-theme="dark"] .prompt-detail-meta-label {
  color: var(--text-tertiary);
}

[data-theme="dark"] .prompt-detail-content {
  color: var(--text-secondary);
}

/* ── Global Search Popover (Dark) ── */
[data-theme="dark"] .global-search-popover {
  background-color: var(--bg-primary) !important;
  border-color: var(--border-primary) !important;
}

[data-theme="dark"] .global-search-popover .el-scrollbar {
  border-color: var(--border-primary);
}

/* ── Code Block Wrapper & Copy Button ── */
.code-block-wrapper {
  position: relative;
  margin: 12px 0;
  border-radius: 10px;
  overflow: hidden;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
  border: 1px solid rgba(0, 0, 0, 0.2);
}
.code-block-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 12px;
  background: linear-gradient(180deg, #2d2d2d 0%, #1e1e1e 100%);
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}
.code-block-lang {
  font-size: 11px;
  color: #9ca3af;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-family: 'Consolas', 'Monaco', monospace;
}
.code-copy-btn {
  font-size: 12px;
  color: #9ca3af;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 4px;
  padding: 2px 10px;
  cursor: pointer;
  transition: all 0.15s;
  font-family: inherit;
  line-height: 1.4;
}
.code-copy-btn:hover {
  color: #e5e7eb;
  background: rgba(255, 255, 255, 0.12);
  border-color: rgba(255, 255, 255, 0.2);
}
.code-block-wrapper pre {
  margin: 0 !important;
  border-radius: 0 !important;
  border: none !important;
  box-shadow: none !important;
}

[data-theme="dark"] .code-block-header {
  background: linear-gradient(180deg, var(--bg-secondary) 0%, var(--bg-active) 100%);
  border-bottom-color: var(--border-primary);
}
[data-theme="dark"] .code-block-lang {
  color: var(--text-tertiary);
}
[data-theme="dark"] .code-copy-btn {
  color: var(--text-tertiary);
  background: rgba(255, 255, 255, 0.04);
  border-color: var(--border-primary);
}
[data-theme="dark"] .code-copy-btn:hover {
  color: var(--text-primary);
  background: rgba(255, 255, 255, 0.08);
}

/* ── Load More Messages ── */
.load-more-msgs {
  text-align: center;
  padding: 10px 0;
  margin: 4px 0;
  font-size: 13px;
  color: #909399;
  cursor: pointer;
  border-radius: 8px;
  transition: all 0.2s;
  user-select: none;
}
.load-more-msgs:hover {
  color: #1976d2;
  background: rgba(25, 118, 210, 0.06);
}
[data-theme="dark"] .load-more-msgs {
  color: var(--text-tertiary);
}
[data-theme="dark"] .load-more-msgs:hover {
  color: var(--accent-color);
  background: rgba(137, 180, 250, 0.06);
}

/* ── Mermaid Smooth Transition ── */
.mermaid-svg-wrap.smooth-transition {
  transition: transform 0.2s ease-out;
}

/* ── Mermaid 全屏模式 ── */
.mermaid-fullscreen-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 99999;
  background: rgba(220, 220, 220, 0.85);
  backdrop-filter: blur(4px);
  display: flex;
  align-items: center;
  justify-content: center;
  animation: mermaid-fs-fadein 0.2s ease;
}
@keyframes mermaid-fs-fadein {
  from { opacity: 0; }
  to { opacity: 1; }
}
body.mermaid-fullscreen-active {
  overflow: hidden !important;
}
.mermaid-fullscreen-container {
  position: relative;
  width: 98vw;
  height: 96vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.mermaid-fullscreen-content {
  flex: 1;
  overflow: hidden;
  cursor: grab;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
}
.mermaid-fullscreen-content .mermaid-svg-wrap {
  transform-origin: center center;
  text-align: center;
}
.mermaid-fullscreen-content .mermaid-svg-wrap svg {
  max-width: none;
  max-height: none;
}
.mermaid-fullscreen-toolbar {
  position: absolute;
  top: 12px;
  right: 12px;
  display: flex;
  align-items: center;
  gap: 4px;
  z-index: 10;
  padding: 6px 8px;
}
.mermaid-fullscreen-toolbar .mermaid-tb-btn {
  width: 32px;
  height: 32px;
  font-size: 13px;
}

/* 全屏模式暗色主题 */
[data-theme="dark"] .mermaid-fullscreen-overlay {
  background: rgba(0, 0, 0, 0.92);
}
[data-theme="dark"] .mermaid-fullscreen-container {
  background: var(--bg-secondary, #1e1e2e);
  box-shadow: 0 8px 40px rgba(0, 0, 0, 0.6);
}
</style>
