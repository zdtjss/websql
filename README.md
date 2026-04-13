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

用一句话描述你想查什么，AI 替你写 SQL、执行、画图、出报告——这就是 WebSQL。

WebSQL 是一个融合 AI 智能体的 Web 数据库管理平台。它基于字节跳动开源的 [CloudWeGo Eino ADK](https://github.com/cloudwego/eino) 构建了完整的 SQL Agent，支持自然语言查询、多轮对话、流式输出、智能导出（Excel / PPT / Word / 图表），同时内置四级权限体系、WebAuthn 生物识别、危险 SQL 拦截与审计日志。编译产物为单个可执行文件，无任何运行时依赖。

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

## 核心能力

### AI SQL Agent

基于 Eino ADK 的 ReAct 智能体，8 个内置工具，3 层中间件：

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
│  │  export_excel             │  │  ← 纯数据 Excel
│  │  export_excel_with_chart  │  │  ← Excel + 折线/柱状/饼图/散点图
│  │  export_ppt               │  │  ← PPTX 演示文稿
│  │  export_analysis_docx     │  │  ← Word 数据报告
│  │  import_data              │  │  ← Excel 导入（支持 upsert）
│  └───────────────────────────┘  │
│  ┌───────────────────────────┐  │
│  │  Middleware Chain          │  │
│  │  PermissionMiddleware     │  │  ← 列级权限过滤
│  │  SQLSecurityMiddleware    │  │  ← 写操作拦截 → 前端确认
│  │  ToolErrorRecovery        │  │  ← 工具错误自动重试
│  └───────────────────────────┘  │
└─────────────┬───────────────────┘
              │
              ▼
     SSE 流式输出
     thinking / content / danger_confirm / done
```

**关键设计**：

- **上下文感知**：Agent 自动获取表结构，理解字段关系与数据库方言差异
- **多轮记忆**：JSON 持久化 + 内存缓存，保留最近 20 轮对话，自动截断防 token 溢出
- **导出复用**：检测到导出请求时，自动从历史消息提取 SQL，不重复生成
- **导入映射**：上传 Excel 后 AI 自动匹配列名与表字段，严格校验后事务写入
- **错误自愈**：工具调用失败时，错误信息反馈给模型重新思考（ReAct 循环），最多 25 轮
- **模型容错**：ChatModel 临时错误（503 / 超时）自动重试，最多 5 次

### 传统 SQL 编辑器

除了 AI 对话，WebSQL 同样提供完整的传统数据库管理能力：

- **SQL 编辑器**：基于 CodeMirror 6，支持语法高亮、自动补全、格式化
- **数据浏览**：可视化表数据浏览、编辑、导出
- **表结构管理**：可视化建表、改表、索引管理
- **数据导入导出**：Excel 导入导出，SQL 导出
- **UPDATE/DELETE 自动备份**：执行前自动备份受影响数据，支持回溯

### 安全体系

**认证**：密码登录 / WebAuthn 指纹面容 / 第三方 Token（OAuth 对接）

**权限**：四级 RBAC，向下继承——拥有连接级权限即拥有该连接下所有权限，拥有 Schema 级权限即拥有该 Schema 下所有表权限，列级权限仅允许访问指定列：

```
连接 (conn)
  └─ Schema
       └─ 表 (table)
            └─ 列 (column)
```

AI Agent 的每次工具调用都经过 PermissionMiddleware 校验——查询结果按列过滤，Schema 查询屏蔽未授权表，写操作和导出操作检查表级权限。

**SQL 安全**：SQLSecurityMiddleware 拦截所有写操作（INSERT / UPDATE / DELETE / DDL），返回 `danger_confirm` 事件推送到前端，用户确认后才真正执行。前端同步进行风险评估（低 / 中 / 高三级），无 WHERE 的 UPDATE/DELETE 标记为高风险。

**审计**：所有经 AI 执行的写操作自动记入 `t_sql_audit` 表，记录 SQL 文本、类型、风险等级、影响行数、执行状态。传统 SQL 编辑器的 UPDATE/DELETE 操作执行前自动备份数据到 `t_history`。


## 技术架构

```
┌─────────────────────────────────────────────────────────────┐
│                        前端 (Vue 3)                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────────┐  │
│  │ AI 对话   │ │ SQL 编辑器│ │ 数据浏览  │ │ 系统管理       │  │
│  │ (SSE流式) │ │(CodeMirror)│ │(可视化)  │ │(权限/连接/配置)│  │
│  └──────────┘ └──────────┘ └──────────┘ └───────────────┘  │
│  Element Plus · Markdown-it · Mermaid · highlight.js        │
└─────────────────────────┬───────────────────────────────────┘
                          │ HTTP / SSE
┌─────────────────────────┴───────────────────────────────────┐
│                     后端 (Go + Gin)                          │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Eino ADK SQL Agent (v2)                   │   │
│  │  ChatModelAgent → Tools → Middleware → SSE Stream     │   │
│  │  支持 OpenAI / Ollama 兼容接口                         │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              传统 API 层                               │   │
│  │  SQL 执行 · 数据导入导出 · 表管理 · 用户/角色/权限     │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              基础设施                                  │   │
│  │  SQLite(管理库) · 连接池 · AES加密 · Redis(可选)       │   │
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
| AI 框架 | Eino ADK v0.8 | ReAct Agent、工具调用、中间件链 |
| LLM 接入 | OpenAI / Ollama | 通过 eino-ext 适配器 |
| 数据库驱动 | sqlx + mysql/oracle/sqlite | 多数据库方言支持 |
| Excel | excelize/v2 | 读写 Excel、内嵌图表 |
| 图表 | go-chart/v2 | PNG 图表渲染 |
| Office | 原生 Open XML | DOCX/PPTX 零依赖生成 |
| 管理库 | SQLite (modernc, CGO-free) | 用户/连接/会话/审计 |
| 缓存 | Redis (可选) | 分布式 Session |

### 前端技术栈

| 组件 | 技术 |
|------|------|
| 框架 | Vue 3 + Composition API |
| UI | Element Plus |
| SQL 编辑器 | CodeMirror 6 |
| Markdown | markdown-it + mermaid |
| 认证 | @passwordless-id/webauthn |
| 构建 | Vite |

## 项目结构

```
websql/
├── main.go                    # 入口：HTTP 服务启动、优雅关闭
├── config/
│   ├── config.go              # 配置加载（config.json + 数据库覆盖）
│   ├── db.go                  # 数据库连接池管理
│   └── init_db.go             # SQL 脚本初始化
├── web-api/
│   ├── router.go              # 路由注册、认证/CORS/Recovery 中间件
│   ├── sql_exec.go            # 传统 SQL 执行（含自动备份）
│   ├── export.go              # 传统数据导出
│   ├── import.go              # 传统数据导入
│   ├── admin/                 # 管理 API（用户/角色/权限/连接/配置）
│   └── ai/
│       ├── ai_config.go       # AI 配置管理
│       └── agent/v2/          # ★ Eino ADK 智能体
│           ├── agent.go       # SQLAgent 核心：模型构建、系统提示词、流式执行
│           ├── handler.go     # HTTP Handler：SSE 流、会话管理、审计日志
│           ├── tools.go       # 工具实现：query_data / exec_sql / get_table_schema / import_data
│           ├── export_tools.go# 导出工具：Excel / Excel+Chart / PPT / Word
│           ├── export_chart.go# go-chart 图表渲染
│           ├── export_pptx.go # PPTX Open XML 生成
│           ├── export_docx.go # DOCX Open XML 生成
│           ├── middleware.go  # 中间件：SQL安全拦截 / 错误恢复
│           ├── permission.go  # 四级权限：构建/校验/过滤/中间件
│           ├── session_db.go  # 会话持久化（内存+数据库）
│           ├── audit.go       # SQL 审计日志
│           └── import_upload.go # Excel 上传暂存
├── utils/                     # 工具包：加密/JSON/文件/ID生成
├── logutils/                  # 日志工具
├── web-src/                   # 前端源码 (Vue 3)
│   └── src/
│       ├── App.vue            # 主界面：AI 对话 + 数据库选择 + 登录
│       ├── views/             # 页面：SQL编辑器/数据浏览/系统管理/审计日志
│       ├── components/        # 组件：SQL确认/导入预览/表编辑器
│       └── utils/             # 前端工具：风险评估/错误处理
├── static/                    # 前端构建产物
├── config.json                # 运行时配置
├── sqlite3-init.sql           # SQLite 初始化脚本
└── mysql-init.sql             # MySQL 初始化脚本
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

## 数据库支持

| 数据库 | 查询 | 写操作 | 可视化编辑 | 导入导出 |
|--------|------|--------|-----------|---------|
| MySQL / MariaDB | ✅ | ✅ | ✅ | ✅ |
| Oracle | ✅ | ✅ | 部分 | ✅ |
| SQLite | ✅ | ✅ | ✅ | ✅ |

## 截图

| AI 对话 | SQL 编辑器 |
|---------|-----------|
| 自然语言查询 → AI 生成 SQL → 流式输出结果 | CodeMirror 6 语法高亮 + 自动补全 |

| 数据导出 | 权限管理 |
|---------|---------|
| 一句话生成 Excel/PPT/Word 报告 | 连接→Schema→表→列 四级 RBAC |

## License

[MIT](LICENSE)
