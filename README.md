# Web-SQL 🚀

<div align="center">

**下一代智能数据库管理平台 | AI 驱动 · 安全高效 · 开箱即用**

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![Vue Version](https://img.shields.io/badge/Vue-3.5+-4FC08D?logo=vue.js)](https://vuejs.org/)
[![License](https://img.shields.io/badge/License-Open%20Source-blue)](LICENSE)

一款融合 **AI 智能辅助**、**企业级安全**、**极致性能** 的现代化 Web 数据库管理工具

✨ **无需依赖 · 跨平台 · 编译即用** ✨

</div>

---

## 🎯 为什么选择 Web-SQL？

> 💡 **想象一下**：用自然语言描述需求，AI 自动生成精准 SQL；一键导出带图表的 Excel 报告；生物识别秒速登录...
> 
> Web-SQL 不只是数据库管理工具，更是您的 **智能数据助手**！

### 🌟 核心优势

| 特性 | 传统工具 | Web-SQL |
|------|---------|---------|
| **AI 智能化** | ❌ 无 | ✅ 自然语言生成 SQL、智能对话、自动优化 |
| **安全认证** | ⚠️ 基础密码 | ✅ 生物识别 + 会话管理 + 危险操作拦截 |
| **数据导出** | ⚠️ 单纯表格 | ✅ Excel + 图表 + PPT + Word 报告 |
| **部署方式** | ⚠️ 复杂依赖 | ✅ 零依赖、单文件、跨平台 |
| **多模交互** | ❌ 仅键盘鼠标 | ✅ 语音录入 + 可视化编辑 + 流式对话 |
| **权限管控** | ⚠️ 粗放管理 | ✅ 细粒度角色权限 + 审计备份 |

---

## 🔥 核心功能特性

### 🤖 AI 智能引擎 - 让数据库"听懂"你的需求

基于 **CloudWeGo Eino ADK** 框架打造的智能体系统，重新定义人机协作：

#### ✨ 智能 SQL 生成
- **自然语言转 SQL**：描述业务需求，自动生成精准查询语句
- **上下文感知**：自动获取表结构，理解字段关系
- **多轮对话记忆**：支持连续追问，AI 记住完整对话历史
- **流式实时输出**：打字机效果，思考过程可视化

#### 🎯 智能数据导出
```
用户：把刚才的查询结果导出为带图表的 Excel
AI：已基于您的查询生成折线图，[点击下载报告](/exports/analysis.xlsx)
```
- **一键导出**：Excel / PPT / Word / PNG 图表
- **智能图表**：自动识别数据特征，生成折线图、柱状图、饼图、散点图
- **历史复用**：自动提取历史查询 SQL，避免重复执行
- **语音交互**：支持语音录入指令，解放双手

#### 🛡️ 危险操作智能拦截
- **实时风险评估**：前端自动分析 SQL 风险等级（低/中/高）
- **二次确认机制**：UPDATE/DELETE/DDL 操作强制确认
- **影响范围预估**：智能提示操作影响行数
- **审计追踪**：自动备份修改数据，支持回滚

#### 💬 多模态交互
- **语音识别**：Web Speech API 支持中文语音录入
- **全屏沉浸式对话**：720px 侧边栏/全屏模式切换
- **Markdown 富文本**：代码高亮、表格渲染、链接解析
- **思考过程折叠**：可查看 AI 推理过程

---

### 🔐 企业级安全体系

#### 生物识别认证
- **WebAuthn 标准**：支持指纹/面容识别（需硬件）
- **HTTPS 证书校验**：生产环境自动证书验证
- **多因素认证**：密码 + 生物识别 + Token 三重保障

#### 细粒度权限管控
- **角色权限管理**：基于 RBAC 模型的权限分配
- **连接级隔离**：每个数据库连接独立权限
- **会话管理**：Redis 分布式会话存储（可选）
- **IP 访问控制**：本地模式白名单机制

#### 数据安全保护
- **操作审计**：自动记录所有写操作
- **数据备份**：DELETE/UPDATE 前自动备份
- **SQL 风险评估**：三级风险预警系统
- **只读模式**：可配置禁止数据修改

---

### 📊 专业数据库管理

#### 多数据库支持
```yaml
支持的数据库:
  - SQLite: modernc.org/sqlite (纯 Go 实现，零依赖)
  - MySQL: github.com/go-sql-driver/mysql
  - Oracle: github.com/sijms/go-ora/v2
```

#### 可视化表管理
- **表结构编辑**：字段名/类型/默认值/注释在线修改
- **索引管理**：创建/删除索引，查看索引类型
- **DDL 预览**：实时查看建表语句，一键复制
- **统计信息**：行数、数据大小、索引大小等
- **表选项配置**：引擎、字符集、排序规则

#### 智能 SQL 编辑器
- **语法高亮**：Highlight.js SQL 语法高亮
- **智能提示**：表名/字段名自动补全
- **格式化**：sql-formatter 自动美化
- **批量执行**：分号分隔多条语句
- **结果编辑**：在线修改查询结果（可配置）

---

### 📁 强大的数据导入导出

#### Excel 高级导出
- **流式写入**：基于 excelize 的 StreamWriter，支持大数据量
- **字段注释**：自动添加字段注释行
- **类型转换**：智能处理日期/数字/NULL 值
- **自定义 SQL**：支持复杂查询结果导出

#### 多样化报告生成
| 导出类型 | 格式 | 特性 |
|---------|------|------|
| **基础 Excel** | .xlsx | 流式写入、字段注释 |
| **图表 Excel** | .xlsx + Chart | 折线图/柱状图/饼图/散点图 |
| **PPT 演示** | .pptx | 多幻灯片、数据可视化 |
| **Word 报告** | .docx | 结构化章节、数据分析 |
| **分析图表** | .png/.jpg | 热力图/趋势图 |

#### Excel 导入
- **预览确认**：导入前数据预览
- **字段映射**：自动匹配列与字段
- **批量插入**：事务处理保证数据一致性

---

### 🎨 现代化用户界面

#### 专业 UI 设计
- **Element Plus**：企业级组件库
- **渐变美学**：蓝灰色系专业配色
- **流畅动画**：气泡滑入、按钮悬停效果
- **响应式布局**：自适应不同屏幕尺寸

#### 交互式组件
- **SQL 确认对话框**：风险等级可视化展示
- **表选择器**：多选/过滤/标签展示
- **历史会话管理**：时间线展示、快速切换
- **全屏 AI 面板**：沉浸式对话体验

#### 细节打磨
- **代码块美化**：深空灰配色 SQL 预览
- **Markdown 表格**：滚动容器、斑马纹
- **链接智能解析**：相对路径自动补全
- **骨架屏加载**：优雅的加载动画

## 📸 界面截图

![指纹识别对话框.png](指纹识别对话框.png)
*指纹识别对话框*

![自动提示.png](自动提示.png)
*SQL 自动提示*

![修改表定义.png](修改表定义.png)
*表结构编辑*

![导表.png](导表.png)
*数据导出对话框*

## 🚀 快速开始

### 运行参数
```bash
-port     运行端口号，默认 80
-https    是否为 https，默认 false
-sql      初始化 SQL 文件路径（可选）
```

### 配置文件
文件名：`config.json`

```json
{
    // 是否为远程模式，默认 false
    // 远程模式：有严格的权限管理和会话管理，适合团队共享
    // 本地模式：无权限管理，仅建议本机使用
    "isRemote": true,
    
    // 管理数据库配置
    // 详情参考：
    // SQLite: https://pkg.go.dev/modernc.org/sqlite
    // MySQL:  https://pkg.go.dev/github.com/go-sql-driver/mysql
    // Oracle: https://pkg.go.dev/github.com/sijms/go-ora/v2
    "db": {
        "type": "sqlite",  // 支持：sqlite、mysql、oracle
        "dsn": "nway.sqlite3.db"
        // sqlite: 数据库文件路径
        // mysql:  user:password@tcp(host:port)/db?params
        // oracle: 参考 go-ora 文档
    },
    
    // Redis 配置（远程模式可选）
    // 详情参考：https://pkg.go.dev/github.com/redis/go-redis/v9
    "redis": {
        "addr": "",     // host:port
        "password": "",
        "db": 0
    },
    
    // HTTPS 证书配置
    "https": {
      "organization": "Nway",
      "commonName": "websql.nway.com"
    }
}
```

## 🐳 Docker 部署

### 使用官方镜像
```bash
docker run -d -p8000:80 \
  -v ./config.json:/app/config.json \
  -v ./nway.sqlite3.db:/app/nway.sqlite3.db \
  zdtjss/websql:v1.5
```

### 自定义配置
```bash
docker run -d -p8000:80 \
  -v ./config.json:/app/config.json \
  -v ./data:/app/data \
  -v ./logs:/app/logs \
  zdtjss/websql:v1.5
```

## 💻 本地开发

### 后端（Go）
```bash
# 安装依赖
go mod tidy

# 运行
go run main.go -port 8080
```

### 前端（Vue 3 + TypeScript）
```bash
cd web-src

# 安装依赖
npm install

# 开发模式
npm run dev

# 构建生产版本
npm run build
```

### 交叉编译
参考 [交叉编译.md](交叉编译.md) 文档

## 🏗️ 技术架构

<div align="center">

**前后端分离 · 现代化技术栈 · 性能卓越**

</div>

### 📦 技术栈总览

```yaml
后端 (Go 1.26+):
  Web 框架：Gin - 高性能 HTTP 框架
  AI 框架：CloudWeGo Eino - 字节开源的 AI 智能体开发框架
  数据库驱动:
    - SQLite: modernc.org/sqlite (纯 Go 实现，CGO-Free)
    - MySQL: github.com/go-sql-driver/mysql
    - Oracle: github.com/sijms/go-ora/v2
  数据处理:
    - Excel: excelize/v2 (流式读写，支持图表)
    - Redis: go-redis/v9 (会话存储，可选)
  安全加密:
    - openssl: 加密算法
    - golang.org/x/crypto: 密码学工具
  工具库:
    - lancet/v2: Go 工具集合
    - docxgo: PPT/Word 文档生成

前端 (Vue 3.5 + TypeScript):
  核心框架：Vue 3.5.21 + TypeScript 5.7
  UI 组件：Element Plus 2.13
  构建工具：Vite 7 (极速热更新)
  代码编辑:
    - CodeMirror 6: 专业级代码编辑器
    - @codemirror/lang-sql: SQL 语法支持
  数据处理:
    - ExcelJS: 复杂 Excel 操作
    - xlsx: Excel 文件解析
  AI 交互:
    - markdown-it: Markdown 渲染
    - marked: 高性能 Markdown 解析
  安全认证:
    - @passwordless-id/webauthn: WebAuthn 生物识别
  开发工具:
    - unplugin-auto-import: 自动导入
    - unplugin-vue-components: 组件自动注册
    - ESLint + Prettier: 代码规范
```

### 🎯 架构设计亮点

#### 1. AI 智能体架构 (基于 Eino ADK)
```
┌─────────────────────────────────────────────┐
│           用户自然语言输入                    │
└─────────────────┬───────────────────────────┘
                  │
┌─────────────────▼───────────────────────────┐
│      ChatModel Agent (智能体核心)            │
│  ┌─────────────────────────────────────┐   │
│  │  System Prompt (领域知识注入)        │   │
│  │  - 数据库类型感知                      │   │
│  │  - 表结构上下文                        │   │
│  │  - 安全规则约束                        │   │
│  └─────────────────────────────────────┘   │
│  ┌─────────────────────────────────────┐   │
│  │  Tools (工具集合)                    │   │
│  │  - query_data: SELECT 查询           │   │
│  │  - exec_sql: 写操作执行              │   │
│  │  - get_table_schema: 表结构查询      │   │
│  │  - export_excel: Excel 导出          │   │
│  │  - export_excel_with_chart: 图表导出 │   │
│  │  - export_ppt: PPT 生成              │   │
│  │  - export_analysis_docx: Word 报告   │   │
│  └─────────────────────────────────────┘   │
│  ┌─────────────────────────────────────┐   │
│  │  Middleware (中间件)                 │   │
│  │  - SQL 安全检测：危险操作拦截        │   │
│  │  - 会话管理：JSONL 持久化             │   │
│  └─────────────────────────────────────┘   │
└─────────────────┬───────────────────────────┘
                  │
┌─────────────────▼───────────────────────────┐
│         流式输出 (Server-Sent Events)        │
│  - thinking: AI 思考过程                     │
│  - content: 实时内容生成                     │
│  - tool_call: 工具调用通知                   │
│  - danger_confirm: 危险操作确认              │
└─────────────────────────────────────────────┘
```

#### 2. 会话管理设计
- **持久化存储**：JSONL 格式（每行一个 JSON 对象）
- **内存缓存**：map + sync.Mutex 并发安全
- **上下文截断**：保留最近 20 轮对话，防止 token 溢出
- **标题自动生成**：从首条用户消息提取前 60 字

#### 3. 危险 SQL 拦截机制
```
用户请求 → AI 生成 SQL → 调用 exec_sql 工具
                              ↓
                    SQLSecurityMiddleware
                              ↓
                    分析 SQL 类型/风险等级
                              ↓
              ┌───────────────┴───────────────┐
              │                               │
        SELECT 等只读操作                INSERT/UPDATE/DELETE
              │                               ↓
              │                    返回 DangerousSQLError
              │                               ↓
              │                    前端显示确认对话框
              │                               ↓
              │                    用户确认 → 真正执行
              │                               ↓
              └────────────────→  返回结果
```

#### 4. 前端组件化设计
- **SQLConfirmInline**：内联 SQL 确认组件
- **AISqlPanel**：AI 对话侧边栏（支持全屏）
- **TableEditor**：表结构可视化编辑
- **语音识别**：Web Speech API 封装

#### 5. 性能优化
- **流式导出**：excelsize StreamWriter，内存占用恒定
- **代码分割**：Vite 自动拆分 vendor/vue 包
- **懒加载**：路由级代码分割
- **缓存策略**：静态资源浏览器缓存

---

## 💼 应用场景

### 🏢 企业级应用

#### 1. 数据分析报告自动化
```
场景：业务人员需要每日销售报表
传统方式：手动写 SQL → 导出 Excel → 制作图表 → 复制粘贴到 PPT
Web-SQL 方式：
  1. "查询本周各区域销售额"
  2. AI 自动生成 SQL 并执行
  3. "导出为带柱状图的 PPT"
  4. AI 调用 export_ppt 工具生成报告
  5. 下载即可直接使用
效率提升：80%+
```

#### 2. 数据库迁移辅助
```
场景：从旧系统迁移数据到新表结构
传统方式：手动编写大量 INSERT...SELECT 语句
Web-SQL 方式：
  1. 描述字段映射关系
  2. AI 生成数据转换 SQL
  3. 风险评估后批量执行
  4. 自动备份原始数据
安全性：100% 可追溯
```

#### 3. 运维巡检自动化
```
场景：DBA 每日数据库健康检查
传统方式：逐条执行巡检 SQL，手动记录结果
Web-SQL 方式：
  1. "检查所有表的行数和存储空间"
  2. AI 遍历表统计信息
  3. "生成 Word 巡检报告"
  4. 自动包含图表和分析
交付物：专业化巡检报告
```

### 👨‍💻 开发者工具

#### 4. 快速数据验证
```
场景：开发新功能需要验证数据
传统方式：打开命令行客户端 → 连接数据库 → 写 SQL
Web-SQL 方式：
  1. 语音录入："查一下用户表最近的 10 条订单"
  2. AI 立即返回结果
  3. 追问："其中支付成功的有多少"
  4. AI 基于上下文继续查询
交互体验：自然流畅
```

#### 5. 表结构文档生成
```
场景：为新接手的系统编写数据字典
传统方式：手动查看表结构 → 复制粘贴 → 整理格式
Web-SQL 方式：
  1. "导出 user 表的完整结构为 Word"
  2. AI 获取表结构、字段注释、索引
  3. 生成包含 DDL 和数据样本的文档
文档质量：专业规范
```

### 🎓 教学培训

#### 6. SQL 学习助手
```
场景：新手学习 SQL 语法
传统方式：查阅文档 → 死记硬背 → 容易出错
Web-SQL 方式：
  1. "我想查询年龄大于 25 岁的用户"
  2. AI 生成 SQL 并解释语法
  3. "为什么用 WHERE 不用 HAVING"
  4. AI 详细解答区别
学习效果：实践驱动，印象深刻
```

---

## 🚀 快速开始

### 方式一：Docker 部署（推荐）
```bash
# 1. 准备配置文件
docker run -d -p8000:80 \
  -v ./config.json:/app/config.json \
  -v ./data:/app/data \
  zdtjss/websql:v1.5

# 2. 访问 http://localhost:8000
# 3. 首次登录自动创建管理员账号
```

### 方式二：本地编译运行
```bash
# 1. 克隆项目
git clone https://github.com/your-repo/websql.git
cd websql

# 2. 编译后端
go mod tidy
go build -o websql

# 3. 编译前端
cd web-src
npm install
npm run build

# 4. 运行
./websql -port 8080
```

### 方式三：开发模式
```bash
# 后端（热重载）
go run main.go -port 8080

# 前端（热更新）
cd web-src
npm run dev
# 访问 http://localhost:5175
```

---

## ⚙️ 配置说明

### 核心配置项

```json
{
  "isRemote": true,  // 远程模式：开启权限管理，适合团队共享
                     // 本地模式：无权限管理，仅建议本机使用
  
  "db": {
    "type": "sqlite",  // 支持：sqlite、mysql、oracle
    "dsn": "nway.sqlite3.db"
  },
  
  "redis": {
    "addr": "localhost:6379",  // 远程模式建议配置 Redis 会话存储
    "password": "",
    "db": 0
  }
}
```

### AI 配置推荐

#### 使用 OpenAI（云端）
```json
{
  "provider": "openai",
  "baseUrl": "https://api.openai.com/v1",
  "model": "gpt-4o",  // 或 gpt-3.5-turbo
  "apiKey": "sk-xxx"
}
```

#### 使用 Ollama（本地私有化）
```bash
# 1. 安装 Ollama
curl -fsSL https://ollama.com/install.sh | sh

# 2. 下载模型
ollama pull qwen2.5:7b  # 或 llama3.1、deepseek 等

# 3. 配置 Web-SQL
{
  "provider": "ollama",
  "baseUrl": "http://localhost:11434",
  "model": "qwen2.5:7b",
  "enableThinking": true
}
```

---

## 🔒 安全最佳实践

### 1. 生产环境部署
```yaml
必须配置:
  - HTTPS 证书（自动申请 Let's Encrypt，启用生物识别必须配置）
  - Redis 会话存储
  - 强密码策略
  - IP 白名单（可选）

建议配置:
  - 定期备份管理数据库
  - 限制最大查询行数
  - 开启操作审计日志
  - 禁用只读用户的写权限
```

### 2. 生物识别使用条件
```
✅ 支持的场景:
  - 用户设备硬件支持指纹/面容识别
  - HTTPS 协议 + 有效证书
  - HTTP + localhost（开发环境）

❌ 不支持的场景:
  - HTTP 协议远程访问
  - 证书过期或自签名（非 localhost）
```

### 3. Oracle 数据库注意事项
```
当前支持:
  ✅ SQL 查询（SELECT/SHOW/DESCRIBE）
  ✅ 表结构查看
  ⚠️ 部分导出功能（待完善）

暂不支持:
  ❌ 可视化表编辑
  ❌ 索引管理
```

---

## 🤝 贡献指南

我们欢迎各种形式的贡献：

### 如何参与
1. **Fork 项目** 到你的 GitHub 账号
2. **创建特性分支** (`git checkout -b feature/AmazingFeature`)
3. **提交更改** (`git commit -m 'Add some AmazingFeature'`)
4. **推送到分支** (`git push origin feature/AmazingFeature`)
5. **开启 Pull Request**

### 开发环境搭建
```bash
# 1. 克隆项目
git clone https://gitee.com/nway/websql.git
cd websql

# 2. 安装 Go 依赖
go mod tidy

# 3. 安装前端依赖
cd web-src
npm install

# 4. 启动开发服务器
# 终端 1：运行后端
go run main.go -port 8080

# 终端 2：运行前端
cd web-src
npm run dev
```

### 代码规范
- **Go**: 遵循 [Effective Go](https://go.dev/doc/effective_go)
- **Vue**: 遵循 [Vue Style Guide](https://vuejs.org/style-guide/)
- **提交信息**: 使用 [Conventional Commits](https://www.conventionalcommits.org/)

---

## 📄 开源协议

本项目采用 **宽松的开源协议**，您可以自由地使用、修改和分发。

- ✅ 个人项目免费使用
- ✅ 企业商用无需授权
- ✅ 可修改源码二次开发
- ⚠️ 请保留原作者版权信息

详见 [LICENSE](LICENSE) 文件。

---

<div align="center">

**如果这个项目对你有帮助，请给一个 ⭐️ Star 支持！**

[📖 查看文档](./README.md) · [🔧 交叉编译指南](./交叉编译.md) · [📝 更新日志](CHANGELOG.md)

</div>


