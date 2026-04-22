<div align="center">

# WebSQL

**AI 原生数据库管理平台**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev/)
[![Vue](https://img.shields.io/badge/Vue-3.5-4FC08D?logo=vue.js)](https://vuejs.org/)
[![Eino](https://img.shields.io/badge/Eino-ADK-334155?logo=bytedance)](https://github.com/cloudwego/eino)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)

*自然语言驱动 · 企业级安全 · 零依赖部署*

</div>

---

> 用一句话描述你想查什么，AI 替你写 SQL、执行、画图、出报告——这就是 WebSQL。

WebSQL 是一个融合 AI 智能体的 Web 数据库管理平台。它基于字节跳动开源的 [CloudWeGo Eino ADK](https://github.com/cloudwego/eino) 构建了完整的 ReAct SQL Agent，支持自然语言查询、多轮对话、流式输出（含思维链）、智能导出（Excel / PPT / Word / 图表），同时内置四级 RBAC 权限体系、WebAuthn 生物识别、危险 SQL 拦截与审计日志。编译产物为单个可执行文件，无任何运行时依赖。

## ✨ 亮点速览

| 特性 | 说明 |
|------|------|
| 🧠 ReAct 智能体 | 基于 Eino ADK 的推理-行动循环，8 个内置工具，最多 20 轮自动纠错 |
| 🛡️ 防篡改审批流 | 危险 SQL 中断 → 服务端保存参数 → 用户确认 → 恢复执行，前端无法篡改 SQL |
| 🔐 四级 RBAC | 连接 → Schema → 表 → 列，AI 每次工具调用都经过权限中间件校验 |
| 📊 零依赖 Office 生成 | DOCX / PPTX 直接构建 Office Open XML，不依赖任何第三方文档库 |
| 💾 写操作自动备份 | UPDATE / DELETE 执行前自动备份原始数据到历史表，支持回溯 |
| 🎙️ 语音输入 | 浏览器原生 Web Speech API，中文语音识别，说完即查 |
| 🚀 单文件部署 | Go 编译为单个二进制，`docker run` 或直接运行，零外部依赖 |
| 🌊 SSE 流式输出 | 实时输出思维链 + 正文，Mermaid 图表流式渲染，5 秒心跳保活 |

## 为什么不是 Navicat / DBeaver / phpMyAdmin？

| | 传统工具 | WebSQL |
|---|---|---|
| 查询方式 | 手写 SQL | 自然语言 → AI 自动生成并执行 |
| 报告产出 | 手动导出 → Excel → 做图 → 粘贴 | 一句话生成带图表的 Excel / PPT / Word |
| 写操作安全 | 执行前靠自觉 | AI 中间件自动拦截，前端二次确认，审计日志全程记录 |
| 权限粒度 | 连接级 | 连接 → Schema → 表 → 列，四级 RBAC |
| 登录方式 | 账号密码 | 密码 / 指纹面容 / 第三方 Token |
| 部署形态 | 安装包 / JVM | 单文件，`docker run` 即用 |
| 协作方式 | 各自安装客户端 | 浏览器打开，团队共享 |
| 错误处理 | SQL 报错 → 手动改 | AI 自动分析错误 → 调整参数重试（ReAct 循环） |
| 长对话 | 上下文溢出 | 超过 10 万 Token 自动摘要压缩 |

## 核心能力

### 🧠 AI SQL Agent

基于 Eino ADK 的 ReAct 智能体，8 个内置工具，4 层中间件：

```
用户输入 "查一下本月各区域销售额"
        │
        ▼
┌─────────────────────────────────┐
│  ChatModelAgent (ReAct Loop)    │
│                                 │
│  System Prompt                  │  ← 数据库类型 / Schema / 权限描述 / 安全规则
│  ┌───────────────────────────┐  │
│  │  Tools                    │  │
│  │  query_data               │  │  ← SELECT / SHOW / DESCRIBE / EXPLAIN
│  │  exec_sql                 │  │  ← INSERT / UPDATE / DELETE / DDL
│  │  get_table_schema         │  │  ← 建表语句 & 结构
│  │  export_excel             │  │  ← 纯数据 Excel（StreamWriter 高性能写入）
│  │  export_excel_with_chart  │  │  ← Excel + 折线/柱状/饼图/散点图
│  │  export_ppt               │  │  ← PPTX 演示文稿
│  │  export_analysis_docx     │  │  ← Word 数据报告（含图表）
│  │  import_data              │  │  ← Excel 导入（支持 upsert）
│  └───────────────────────────┘  │
│  ┌───────────────────────────┐  │
│  │  Middleware Chain          │  │
│  │  PermissionMiddleware     │  │  ← 列级权限过滤
│  │  DangerousSQLApproval     │  │  ← 写操作拦截 → 防篡改确认
│  │  ToolErrorRecovery        │  │  ← 工具错误自动重试
│  │  Summarization            │  │  ← 超 10 万 Token 自动摘要
│  └───────────────────────────┘  │
└─────────────┬───────────────────┘
              │
              ▼
     SSE 流式输出
     session / thinking / content / danger_confirm / done
```

#### 关键设计

- **上下文感知**：Agent 自动获取表结构，理解字段关系与数据库方言差异（MySQL / Oracle / SQLite）
- **多轮记忆**：JSON 持久化 + 内存缓存，保留最近 20 轮对话，自动截断防 token 溢出
- **导出复用**：检测到导出请求时，自动从历史消息提取 SQL，不重复生成
- **导入映射**：上传 Excel 后 AI 自动匹配列名与表字段（精确匹配 → 标准化匹配 → AI 映射），事务写入
- **错误自愈**：工具调用失败时，错误信息反馈给模型重新思考（ReAct 循环），最多 20 轮
- **模型容错**：ChatModel 临时错误（503 / 超时）自动重试，最多 5 次
- **防篡改审批**：危险 SQL 中断时参数保存在服务端 CheckPointStore，恢复执行使用服务端参数，前端无法篡改
- **优雅降级**：图表生成失败不影响文档导出；审计表不存在时静默跳过；Schema 查询失败自动 fallback

### 📝 传统 SQL 编辑器

除了 AI 对话，WebSQL 同样提供完整的传统数据库管理能力：

- **SQL 编辑器**：基于 CodeMirror 6，语法高亮、基于 Schema 的自动补全（表名 + 字段名 + 注释）、格式化
- **数据浏览**：可视化表数据浏览、列过滤（支持等于/LIKE/IN/IS NULL 等操作符）、列排序、编辑、导出
- **表结构管理**：可视化建表、改表、索引管理、DDL 查看
- **数据导入导出**：Excel 导入导出（支持字段映射、新增/修改模式），SQL 导出
- **UPDATE/DELETE 自动备份**：执行前自动备份受影响数据到历史表，支持回溯
- **生产环境保护**：根据 Schema 名称自动识别测试/生产环境，生产库默认禁止写操作

### 🔐 安全体系

#### 认证

三种登录方式，满足不同场景：

| 方式 | 实现 | 场景 |
|------|------|------|
| 密码登录 | MD5 + 盐值哈希 | 传统场景 |
| 生物识别 | WebAuthn 指纹 / 面容 | 安全便捷 |
| 第三方 Token | OAuth 对接外部认证接口 | 企业集成 |

#### 权限

四级 RBAC，向下继承——拥有连接级权限即拥有该连接下所有权限，拥有 Schema 级权限即拥有该 Schema 下所有表权限，列级权限仅允许访问指定列：

```
连接 (conn)
  └─ Schema
       └─ 表 (table)
            └─ 列 (column)
```

AI Agent 的每次工具调用都经过 PermissionMiddleware 校验——查询结果按列过滤，Schema 查询屏蔽未授权表，写操作和导出操作检查表级权限。同时，权限约束以自然语言注入系统提示词，让 LLM 在生成 SQL 时就遵守权限规则，形成**双重保障**。

#### SQL 安全

SQLSecurityMiddleware 拦截所有写操作（INSERT / UPDATE / DELETE / DDL），采用 Eino 标准 **ApprovalMiddleware** 模式：

1. 检测到危险 SQL → 调用 `StatefulInterrupt`，**原始参数保存到服务端 CheckPointStore**
2. 返回 `danger_confirm` 事件推送到前端，展示 SQL 给用户确认
3. 用户确认 → `ResumeWithParams` 使用**服务端保存的参数**恢复执行
4. 前端同步进行风险评估（低 / 中 / 高三级），无 WHERE 的 UPDATE/DELETE 标记为高风险

> ⚡ 关键安全保证：恢复执行使用的是服务端保存的原始参数，不是前端传来的——即使前端被篡改，也无法改变将要执行的 SQL。

#### 审计

所有经 AI 执行的写操作自动记入 `t_sql_audit` 表，记录 SQL 文本、类型、风险等级、影响行数、执行状态。传统 SQL 编辑器的 UPDATE/DELETE 操作执行前自动备份数据到 `t_history`。

## 技术架构

```
┌─────────────────────────────────────────────────────────────┐
│                        前端 (Vue 3)                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────────┐  │
│  │ AI 对话   │ │ SQL 编辑器│ │ 数据浏览  │ │ 系统管理       │  │
│  │ (SSE流式) │ │(CodeMirror)│ │(可视化)  │ │(权限/连接/配置)│  │
│  └──────────┘ └──────────┘ └──────────┘ └───────────────┘  │
│  Element Plus · Markdown-it · Mermaid · highlight.js        │
│  WebAuthn · Web Speech API · sql-formatter                  │
└─────────────────────────┬───────────────────────────────────┘
                          │ HTTP / SSE
┌─────────────────────────┴───────────────────────────────────┐
│                     后端 (Go + Gin)                          │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Eino ADK SQL Agent (v2)                   │   │
│  │  ChatModelAgent → Tools → Middleware → SSE Stream     │   │
│  │  CheckPoint + Interrupt/Resume 防篡改审批              │   │
│  │  支持 OpenAI / Ollama 兼容接口                         │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              传统 API 层                               │   │
│  │  SQL 执行(含自动备份) · 数据导入导出 · 表管理           │   │
│  │  用户/角色/权限 · 连接管理 · Prompt · 审计日志          │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              基础设施                                  │   │
│  │  SQLite(管理库, CGO-free) · 连接池 · AES加密           │   │
│  │  Redis(可选, 分布式Session) · 雪花ID · Gzip压缩       │   │
│  │  自签名HTTPS自动生成 · 凭证脱敏 · IP白名单             │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────┬───────────────────────────────────┘
                          │
          ┌───────────────┼───────────────┐
          │               │               │
     ┌────┴────┐   ┌─────┴─────┐   ┌────┴────┐
     │  MySQL  │   │  Oracle   │   │ SQLite  │
     │ MariaDB │   │           │   │         │
     └─────────┘   └───────────┘   └─────────┘
```

### 后端技术栈

| 组件 | 技术 | 说明 |
|------|------|------|
| Web 框架 | Gin | HTTP 路由、中间件、SSE |
| AI 框架 | Eino ADK v0.8 | ReAct Agent、工具调用、中间件链、CheckPoint |
| LLM 接入 | OpenAI / Ollama | 通过 eino-ext 适配器，支持任何 OpenAI 兼容接口 |
| 数据库驱动 | sqlx + mysql/oracle/sqlite | 多数据库方言支持 |
| Excel | excelize/v2 | 读写 Excel、内嵌图表、StreamWriter 流式写入 |
| 图表 | go-chart/v2 | PNG 图表渲染（折线/柱状/饼图/散点） |
| Office | 原生 Open XML | DOCX/PPTX **零依赖**生成，直接构建 OOXML |
| 管理库 | SQLite (modernc, CGO-free) | 用户/连接/会话/审计，纯 Go 实现 |
| 缓存 | Redis (可选) | 分布式 Session，30 分钟 TTL |
| 加密 | AES-ECB | 数据库连接密码加密存储 |
| ID | 雪花算法 | 分布式唯一 ID，单节点每毫秒 4096 个 |

### 前端技术栈

| 组件 | 技术 | 说明 |
|------|------|------|
| 框架 | Vue 3 + Composition API | 响应式、组合式 API |
| UI | Element Plus | 中文 locale，虚拟滚动表格 |
| SQL 编辑器 | CodeMirror 6 | 语法高亮、Schema 自动补全、格式化 |
| Markdown | markdown-it + mermaid | AI 回复渲染，Mermaid 流式渲染 |
| 认证 | @passwordless-id/webauthn | 指纹 / 面容生物识别 |
| 语音 | Web Speech API | 中文语音输入 |
| 构建 | Vite | 快速开发与构建 |

## 项目结构

```
websql/
├── main.go                    # 入口：HTTP 服务启动、优雅关闭
├── config/
│   ├── config.go              # 配置加载（config.json + 数据库覆盖）
│   ├── db.go                  # 数据库连接池管理、心跳检测
│   └── init_db.go             # SQL 脚本初始化
├── web-api/
│   ├── router.go              # 路由注册、认证/CORS/Recovery/IP白名单中间件
│   ├── sql_exec.go            # 传统 SQL 执行（含自动备份）
│   ├── export.go              # 传统数据导出（StreamWriter 流式写入）
│   ├── import.go              # 传统数据导入（事务保证、列映射）
│   ├── admin/                 # 管理 API
│   │   ├── admin.go           # 用户 CRUD、密码哈希
│   │   ├── login.go           # 三种登录方式
│   │   ├── conn_config.go     # 连接配置管理（AES 加密存储）
│   │   ├── db_operate.go      # 数据库操作 API
│   │   ├── system_config.go   # 系统配置（双层：文件 + 数据库）
│   │   ├── tree_mg.go         # 数据库导航树（权限感知过滤）
│   │   └── prompt.go          # 提示词管理（个人/分享/角色三级）
│   └── ai/
│       ├── ai_config.go       # AI 配置管理
│       ├── ai_handler.go      # AI 路由注册
│       └── agent/v2/          # ★ Eino ADK 智能体
│           ├── agent.go       # SQLAgent 核心：模型构建、系统提示词、流式执行
│           ├── handler.go     # HTTP Handler：SSE 流、Keep-Alive、会话管理
│           ├── tools.go       # 工具实现：query_data / exec_sql / get_table_schema / import_data
│           ├── export_tools.go# 导出工具：Excel / Excel+Chart / PPT / Word
│           ├── export_chart.go# go-chart 图表渲染
│           ├── export_pptx.go # PPTX Open XML 生成（零依赖）
│           ├── export_docx.go # DOCX Open XML 生成（零依赖）
│           ├── middleware.go  # 中间件：危险SQL防篡改审批 / 错误恢复
│           ├── permission.go  # 四级权限：构建/校验/过滤/中间件/提示词注入
│           ├── session_db.go  # 会话持久化（内存缓存 + 数据库）
│           ├── checkpoint_store.go # CheckPoint 存储（15 分钟自动过期）
│           ├── audit.go       # SQL 审计日志
│           └── import_upload.go # Excel 上传暂存（30 分钟自动清理）
├── utils/                     # 工具包
│   ├── security_helper.go     # AES 加密/解密
│   ├── errutil.go             # 凭证脱敏（password/token/DSN/IP 自动替换）
│   ├── id.go                  # 雪花算法 ID 生成器
│   ├── json.go                # JSON Gzip 压缩（≥20 字节自动压缩）
│   └── db/sql_dialect.go      # 多数据库 SQL 方言映射
├── https/                     # HTTPS 自动配置（自签名证书自动生成/续期）
├── logutils/                  # 日志工具
├── web-src/                   # 前端源码 (Vue 3)
│   └── src/
│       ├── App.vue            # 主界面：AI 对话 + SSE 流式 + Mermaid 渲染 + 语音输入
│       ├── views/             # 页面
│       │   ├── SQLEditor2.vue       # SQL 编辑器（CodeMirror 6 + 虚拟滚动表格）
│       │   ├── DataBrowser.vue      # 数据浏览（列过滤/排序/CRUD）
│       │   ├── ClassicalView.vue    # 经典视图（数据库树 + 多标签页）
│       │   ├── TableManager.vue     # 表管理（字段/索引/选项/DDL）
│       │   ├── SystemManagement.vue # 系统管理入口
│       │   ├── RolePermission.vue   # 四级权限配置
│       │   ├── SQLAuditLog.vue      # SQL 审计日志
│       │   └── ...                  # 更多管理页面
│       ├── components/        # 组件
│       │   ├── SQLConfirmInline.vue # 危险 SQL 确认（风险等级 + 关键字高亮）
│       │   ├── ImportPreviewDialog.vue # Excel 导入预览（字段映射 + 预览）
│       │   └── ...
│       └── utils/
│           ├── sqlRiskAssessment.js  # SQL 风险评估（前端）
│           ├── errorHandler.js       # 错误脱敏
│           └── vditorLoader.js       # Vditor 懒加载
├── config.json                # 运行时配置
├── sqlite3-init.sql           # SQLite 初始化脚本
├── mysql-init.sql             # MySQL 初始化脚本
└── Dockerfile                 # Docker 部署
```

## 快速开始

### 环境要求

- Go 1.26+（编译）
- Node.js 18+（前端开发，仅开发时需要）

### 编译运行

```bash
# 克隆项目
git clone <repo-url> && cd websql

# 编译后端
go build -o websql .

# 初始化数据库（首次运行）
./websql -sql sqlite3-init.sql

# 启动服务
./websql -port 8080
```

### Docker 部署

```bash
docker build -t websql .
docker run -d -p 443:443 -v ./data:/app/data websql
```

### 前端开发

```bash
cd web-src
npm install
npm run dev    # 开发服务器 (localhost:5173)
npm run build  # 构建到 static/
```

## 配置说明

### config.json

```json
{
  "isRemote": true,          // true=远程模式(启用权限), false=本地模式(无权限)
  "db": {
    "type": "sqlite",         // 管理库类型: sqlite / mysql
    "dsn": "./nway.sqlite3.db"
  },
  "redis": {
    "addr": "",               // 可选，分布式 Session
    "password": "",
    "db": 0
  },
  "https": {
    "enable": true,
    "organization": "Nway",
    "commonName": "websql.nway.com"
  }
}
```

### AI 配置

通过系统管理界面配置，支持：

| 参数 | 说明 |
|------|------|
| provider | `openai` 或 `ollama` |
| baseUrl | API 地址（支持任何 OpenAI 兼容接口） |
| model | 模型名称 |
| apiKey | API 密钥 |
| temperature | 温度参数 |
| maxTokens | 最大 token 数 |
| enableThinking | 是否启用思考过程（Ollama） |

### 双模式运行

| 模式 | isRemote | 权限管理 | 适用场景 |
|------|----------|---------|---------|
| 本地模式 | false | 无（所有用户可访问所有连接） | 个人开发、内网使用 |
| 远程模式 | true | 严格 RBAC + IP 白名单 | 团队协作、生产环境 |

## 数据库支持

| 数据库 | 查询 | 写操作 | 可视化编辑 | 导入导出 | AI 方言适配 |
|--------|------|--------|-----------|---------|------------|
| MySQL / MariaDB | ✅ | ✅ | ✅ | ✅ | ✅ |
| Oracle | ✅ | ✅ | 部分 | ✅ | ✅ |
| SQLite | ✅ | ✅ | ✅ | ✅ | ✅ |

## 截图

| AI 对话 | SQL 编辑器 |
|---------|-----------|
| 自然语言查询 → AI 生成 SQL → 流式输出结果 | CodeMirror 6 语法高亮 + 自动补全 |

| 数据导出 | 权限管理 |
|---------|---------|
| 一句话生成 Excel/PPT/Word 报告 | 连接→Schema→表→列 四级 RBAC |

## License

[MIT](LICENSE)
