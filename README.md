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

WebSQL 是一个融合 AI 智能体的 Web 数据库管理平台。它基于字节跳动开源的 CloudWeGo Eino ADK 构建了完整的 SQL Agent，支持自然语言查询、多轮对话、流式输出、智能导出（Excel / PPT / Word / 图表），同时内置四级权限体系、WebAuthn 生物识别、危险 SQL 拦截与审计日志。编译产物为单个可执行文件，无任何运行时依赖。

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

基于 Eino ADK 的 ReAct 智能体，9 个内置工具，3 层中间件：

```
用户输入 "查一下本月各区域销售额"
        │
        ▼
┌─────────────────────────────┐
│  ChatModelAgent             │
│                             │
│  System Prompt              │  ← 数据库类型 / Schema / 权限描述 / 安全规则
│  ┌───────────────────────┐  │
│  │  Tools                │  │
│  │  query_data           │  │  ← SELECT / SHOW / DESCRIBE
│  │  exec_sql             │  │  ← INSERT / UPDATE / DELETE / DDL
│  │  get_table_schema     │  │  ← 建表语句 & 结构
│  │  export_excel         │  │  ← 纯数据 Excel
│  │  export_excel_chart   │  │  ← Excel + 折线/柱状/饼图/散点图
│  │  export_ppt           │  │  ← PPTX 演示文稿
│  │  export_image         │  │  ← PNG 分析图表
│  │  export_docx          │  │  ← Word 数据报告
│  │  import_data          │  │  ← Excel 导入（支持 upsert）
│  └───────────────────────┘  │
│  ┌───────────────────────┐  │
│  │  Middleware Chain     │  │
│  │  PermissionMiddleware │  │  ← 列级权限过滤
│  │  SQLSecurityMiddleware│  │  ← 写操作拦截 → 前端确认
│  │  ErrorRecovery        │  │  ← 工具错误自动重试
│  └───────────────────────┘  │
└─────────────┬───────────────┘
              │
              ▼
     SSE 流式输出
     thinking / content / danger_confirm / done
```

**关键设计**：

- **上下文感知**：Agent 自动获取表结构，理解字段关系与数据库方言差异
- **多轮记忆**：JSONL 持久化 + 内存缓存，保留最近 20 轮对话，自动截断防 token 溢出
- **导出复用**：检测到导出请求时，自动从历史消息提取 SQL，不重复生成
- **导入映射**：上传 Excel 后 AI 自动匹配列名与表字段，严格校验后事务写入
- **错误自愈**：工具调用失败时，错误信息反馈给模型重新思考（ReAct 循环），最多 25 轮
- **模型容错**：ChatModel 临时错误（503 / 超时）自动重试，最多 5 次

### 安全体系

**认证**：密码登录 / WebAuthn 指纹面容 / 第三方 Token（OAuth 对接）

**权限**：四级 RBAC，向下继承但不向下传播——拥有连接级权限即拥有该连接下所有权限，拥有 Schema 级权限即拥有该 Schema 下所有表权限，列级权限仅允许访问指定列：

```
连接 (conn)
  └─ Schema
       └─ 表 (table)
            └─ 列 (column)
```

AI Agent 的每次工具调用都经过 PermissionMiddleware 校验——查询结果按列过滤，Schema 查询屏蔽未授权表，写操作和导出操作检查表级权限。

**SQL 安全**：SQLSecurityMiddleware 拦截所有写操作（INSERT / UPDATE / DELETE / DDL），返回 `danger_confirm` 事件推送到前端，用户确认后才真正执行。前端同步进行风险评估（低 / 中 / 高三级），无 WHERE 的 UPDATE/DELETE 标记为高风险。

**审计**：所有经 AI 执行的写操作自动记入 `t_sql_audit` 表，记录 SQL 文本、类型、风险等级、影响行数、执行状态。传统 SQL 编辑器的 UPDATE/DELETE 操作执行前自动备份数据到 `t_history`。

### 数据库管理

**多库支持**：SQLite（纯 Go 实现，CGO-Free）、MySQL / MariaDB、Oracle

**SQL 编辑器**：CodeMirror 6 + SQL 语法高亮 + 自动补全 + sql-formatter 格式化 + F9 执行 + 批量执行

**表管理**：可视化编辑表结构（字段名 / 类型 / 默认值 / 注释）、索引管理、DDL 预览、统计信息、表选项配置

**数据导入导出**：

| 方向 | 格式 | 实现 |
|---|---|---|
| 导出 | Excel (.xlsx) | excelize StreamWriter，内存恒定 |
| 导出 | Excel + 图表 | excelize 内嵌折线 / 柱状 / 饼 / 散点图 |
| 导出 | PPT (.pptx) | 原生 Office Open XML，标题页 + 数据摘要 + 表格预览 |
| 导出 | Word (.docx) | 原生 Office Open XML，结构化章节 + 内嵌图表 |
| 导出 | 图表 (.png) | go-chart 渲染，折线 / 柱状 / 饼图 |
| 导入 | Excel → 数据库 | AI 字段映射 + insert / upsert 模式 + 事务保证 |

### 前端

Vue 3.5 + TypeScript 5.7 + Element Plus 2.13 + Vite 7

- AI 对话面板：侧边栏 / 全屏切换，Markdown 渲染（代码高亮 / 表格 / 链接），思考过程折叠
- SQL 确认组件：风险等级可视化，影响行数提示
- 语音录入：Web Speech API 中文语音识别
- 路由懒加载 + Vite 代码分割

## 界面

![指纹识别](指纹识别对话框.png)
![自动提示](自动提示.png)
![表结构编辑](修改表定义.png)
![数据导出](导表.png)

## 快速开始

### Docker（推荐）

```bash
docker run -d -p 8000:80 \
  -v ./config.json:/app/config.json \
  -v ./data:/app/data \
  zdtjss/websql:v1.5
```

访问 `http://localhost:8000`，首次登录自动创建管理员。

### 本地编译

```bash
git clone https://gitee.com/nway/websql.git
cd websql

# 后端
go mod tidy
go build -o websql

# 前端
cd web-src
npm install
npm run build

# 运行
./websql -port 8080
```

### 开发模式

```bash
# 终端 1：后端
go run main.go -port 8080

# 终端 2：前端
cd web-src && npm run dev
# 访问 http://localhost:5175
```

### 运行参数

| 参数 | 说明 | 默认值 |
|---|---|---|
| `-port` | 监听端口 | 80 |
| `-https` | 启用 HTTPS | false |
| `-sql` | 初始化 SQL 文件路径 | - |

## 配置

`config.json`：

```json
{
  "isRemote": true,
  "db": {
    "type": "sqlite",
    "dsn": "./nway.sqlite3.db"
  },
  "redis": {
    "addr": "",
    "password": "",
    "db": 0
  },
  "https": {
    "organization": "Nway",
    "commonName": "websql.nway.com"
  }
}
```

- `isRemote: true` — 远程模式，启用权限管理与会话管理，适合团队共享
- `isRemote: false` — 本地模式，无权限管理，仅限本机使用
- `redis` — 远程模式下可选配置，用于分布式会话存储
- `https` — 自签名证书自动生成（10 年有效期），启用 WebAuthn 必须 HTTPS

AI 配置通过系统管理页面在线设置，支持 OpenAI 兼容接口和 Ollama 本地部署：

```json
// OpenAI / 兼容接口
{ "provider": "openai", "baseUrl": "https://api.openai.com/v1", "model": "gpt-4o", "apiKey": "sk-xxx" }

// Ollama 本地私有化
{ "provider": "ollama", "baseUrl": "http://localhost:11434", "model": "qwen2.5:7b", "enableThinking": true }
```

## 技术栈

```
后端  Go 1.26
  ├─ Gin                    HTTP 框架
  ├─ CloudWeGo Eino ADK     AI Agent 框架
  ├─ modernc.org/sqlite     纯 Go SQLite（CGO-Free）
  ├─ go-sql-driver/mysql    MySQL 驱动
  ├─ go-ora/v2              Oracle 驱动
  ├─ excelize/v2            Excel 读写 + 图表
  ├─ go-chart/v2            PNG 图表渲染
  ├─ go-redis/v9            分布式会话（可选）
  └─ Office Open XML        DOCX / PPTX 原生生成（零依赖）

前端  Vue 3.5 + TypeScript 5.7
  ├─ Element Plus 2.13      UI 组件库
  ├─ CodeMirror 6           SQL 编辑器
  ├─ Vite 7                 构建工具
  ├─ markdown-it / marked   Markdown 渲染
  ├─ @passwordless-id/webauthn  生物识别
  └─ sql-formatter          SQL 格式化
```

## 安全须知

**生产环境必须**：HTTPS 证书、Redis 会话存储、强密码策略

**WebAuthn 条件**：需要 HTTPS + 有效证书（localhost 开发环境除外）+ 设备硬件支持

**Oracle 限制**：支持 SQL 查询与表结构查看，可视化表编辑与索引管理暂未适配

## 贡献

1. Fork → 创建特性分支 → 提交 → Push → Pull Request
2. Go 遵循 [Effective Go](https://go.dev/doc/effective_go)，Vue 遵循 [Vue Style Guide](https://vuejs.org/style-guide/)
3. 提交信息遵循 [Conventional Commits](https://www.conventionalcommits.org/)

## 许可

[MIT License](LICENSE) — 个人免费、企业商用无需授权、可修改源码二次开发，请保留版权信息。
