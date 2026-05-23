# WebSQL

**AI 原生数据库管理平台**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev/)
[![Vue](https://img.shields.io/badge/Vue-3.5-4FC08D?logo=vue.js)](https://vuejs.org/)
[![Eino](https://img.shields.io/badge/Eino-0.8-334155?logo=bytedance)](https://github.com/cloudwego/eino)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)

自然语言驱动数据库操作。企业级安全保障。零依赖单文件部署。

---

## 概述

WebSQL 将 **AI 对话** 与 **经典数据库管理** 融为一体，提供两种互补的工作方式：

- **AI 对话模式**：用自然语言描述需求，AI 自动理解 Schema、生成 SQL、执行查询，并产出含图表的 Excel、PPT、Word 文档。写操作经双智能体架构审核，危险 SQL 自动拦截。
- **经典视图模式**：提供基于 CodeMirror 6 的 SQL 编辑器（语法高亮、Schema 感知补全）、可视化数据浏览与编辑、ER 图、备份恢复、结构对比与数据同步、全局搜索、实时监控等全套传统数据库管理能力。

整个系统编译为单个可执行文件，无需任何外部运行时依赖。

---

## 核心优势

### AI 驱动，降低门槛

自然语言替代手写 SQL，AI 自动感知表结构、跨库关联分析、生成图文报告。写操作经 **SQLAgent + PermissionAgent 双智能体** 审核，危险 SQL 自动拦截并需人工确认。长对话自动摘要压缩，ReAct 循环自动纠错重试，兼顾效率与安全。

### 经典全覆盖，不妥协

SQL 编辑器（语法高亮 + Schema 感知补全）、数据浏览编辑、ER 图逆向/正向工程、选择性备份与 AES 加密恢复、Schema 对比与数据同步、全局跨对象搜索、QPS/TPS 实时监控——覆盖 Navicat、DBeaver 等工具的日常场景，且享有统一的连接—Schema—表级权限控制、UPDATE/DELETE 自动备份和生产环境保护。

### 部署即交付

`./websql -port 8080` 即刻启动。支持自签名 HTTPS、本地 Ollama 离线运行 AI、Docker 一键部署。不依赖外部运行时，不连接授权服务器，不包含任何遥测代码——数据主权完全在你手中。

---

## 对标传统工具

| 维度       | 传统工具                    | WebSQL                                                                 |
| ---------- | --------------------------- | ---------------------------------------------------------------------- |
| 查询方式   | 手写 SQL                    | AI 自然语言驱动 + 经典手写 SQL（语法高亮 + Schema 感知补全）              |
| 报告产出   | 导出数据，手动做图黏贴       | 一句话生成含图表的 Excel / PPT / Word                                    |
| 写操作安全 | 依赖操作人员自觉             | AI 双智能体拦截危险 SQL + 前端确认；经典模式自动备份 + 生产环境保护        |
| 权限粒度   | 连接级                      | AI 模式：连接—Schema—表—列，四级 RBAC；经典模式：连接—Schema—表，三级     |
| 认证方式   | 账号密码                    | 密码 / WebAuthn 生物识别 / 第三方 Token                                  |
| 部署形态   | 逐台安装客户端              | 单文件，`docker run` 或直接运行                                          |
| 协作方式   | 各自安装客户端              | 浏览器打开，团队共享                                                     |
| 错误处理   | SQL 报错，手动修改           | AI 自动分析错误，调整参数重试（ReAct 循环）                               |
| 跨库查询   | 逐个连接切换，手动合并       | AI 感知连接拓扑，自动拆分/路由 SQL                                       |
| SQL 优化   | 手动 EXPLAIN 分析            | AI 驱动 EXPLAIN 计划推理，给出可执行的优化建议；编辑器内置 EXPLAIN 执行    |
| 结构对比   | 人工比对                    | 自动化 Schema Diff，直接生成 ALTER 语句                                   |
| 数据同步   | 手动导出导入                | 自动差异识别，生成同步 SQL，分块处理大数据                                |
| 全局搜索   | 逐表检查                    | 跨 Schema 并发扫描，覆盖表/列/索引/视图及数据内容                          |
| 实时监控   | 依赖外部工具                | 内置 QPS/TPS、连接池、Buffer Pool、进程列表监控                           |

---

## 在线体验

> [WebSQL 演示环境](http://180.184.30.223:8001/)
>
> - 账号：`admin`
> - 密码：`1`
>
> 演示服务器上行带宽有限，首次加载可能较慢。请文明使用。

---

## 核心能力

### AI 双智能体架构（基于 Eino ADK）

- **SQLAgent**：ReAct 模式主智能体，配备 9 个工具（查询、写入、导出 Excel/PPT/Word、导入数据等）和 7 层中间件链（权限审核、危险 SQL 拦截、错误恢复、上下文摘要压缩等）
- **PermissionAgent**：独立权限审核智能体，每次 SQL 工具调用前校验用户授权范围，解析 SQL 提取表/字段，按"最具体优先"原则逐表逐字段比对。LLM 不可用时自动降级为程序化权限检查

### 防篡改审批流

危险写操作（DROP/TRUNCATE/无 WHERE 的 UPDATE/DELETE）触发中断机制：原始参数保存至 CheckPointStore（15 分钟过期），前端无法篡改 SQL，用户确认后使用服务端保存的原始参数执行。批量 SQL 逐条展示，可按条选择确认/取消。

### Skill 扩展系统

内置 Python Skill 引擎，将 AI Agent 能力扩展至 Python 生态：专业 PPT 生成（8 种图表类型）、Word 报告生成（含封面/摘要/可视化）、跨库大数据量分析。Python 不可用时自动回退 Go 原生实现，保证基础可用。

### 安全体系

- **认证**：密码（MD5 + 盐值）、WebAuthn 生物识别、第三方 Token 三种登录方式
- **授权**：四级 RBAC（连接 → Schema → 表 → 列），"最具体优先"原则。权限以自然语言注入 AI 系统提示词
- **审计**：所有 AI 写操作自动记入审计表；经典模式 UPDATE/DELETE 自动备份至历史表
- **韧性**：SQL/AI 双熔断器 + IP 级限流 + 登录限流（每 IP 每分钟 10 次）

---

## 快速开始

```bash
# 编译
git clone <repo-url> && cd websql
go build -o websql .

# 初始化数据库（首次运行）
./websql -sql sqlite3-init.sql

# 启动
./websql -port 8080
```

### Docker

```bash
docker build -t websql .
docker run -d -p 443:443 -v ./data:/app/data websql
```

### 前端开发

```bash
cd web-src && npm install
npm run dev     # 开发服务器
npm run build   # 构建到 static/
```

---

## 数据库支持

| 数据库          | 查询 | 写操作 | 导入导出 | AI 方言适配 |
| --------------- | ---- | ------ | -------- | ----------- |
| MySQL / MariaDB | ✓    | ✓      | ✓        | ✓           |
| Oracle          | ✓    | ✓      | ✓        | ✓           |
| SQLite          | ✓    | ✓      | ✓        | ✓           |

---

## 开源优势

- **MIT 许可证**：无限制商用、自由修改、自由分发，无 Copyleft 限制
- **数据主权**：单文件部署在你自己的服务器上，所有数据不离开服务器。支持本地 Ollama 离线运行 AI
- **零遥测**：不包含任何遥测、行为追踪或数据收集代码，代码完全公开可审计
- **零供应商锁定**：基于标准 SQL 和 HTTP/SSE 协议，Go + Vue 3 技术栈，无商业授权服务器依赖
- **社区驱动**：所有功能模块均无"企业版"限制，欢迎通过 GitHub Issues/Pull Requests 贡献

---

## License

[MIT](LICENSE)