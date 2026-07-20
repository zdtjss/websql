# WebSQL

## AI 原生数据库管理与智能分析平台

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev/)
[![Vue](https://img.shields.io/badge/Vue-3.5-4FC08D?logo=vue.js)](https://vuejs.org/)
[![Wails](https://img.shields.io/badge/Wails-v3-DE4B3B)](https://wails.io/)
[![Eino](https://img.shields.io/badge/Eino-0.9-334155?logo=bytedance)](https://github.com/cloudwego/eino)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)

> **让数据库会思考。**
>
> 自然语言提问，数据库思考作答 —— 洞察分析、出图出报表，端到端智能闭环；
> 双智能体护航写操作，四级权限纵深防御，数据主权尽在掌控。

一款面向数据库管理与数据分析场景的 **AI 原生平台**，将大语言模型能力与经典数据库管理工具有机融合，以 **"自然语言驱动 + 智能体协同 + 自主可控部署"** 为核心理念，为政企用户提供安全、高效、可审计的智能化数据操作体验。

提供 **Web 版**（单文件即服务，浏览器访问）与 **桌面版**（原生窗口，本地免登录）两种部署形态，功能完全一致，覆盖从个人开发者到企业团队的多元场景。

***

## 一、建设背景与行业痛点

数字化转型的深化，使数据成为政企核心资产，但传统数据库管理工具长期面临"操作门槛高、分析效率低、安全保障弱"三大顽疾：

- **使用门槛高**：业务人员依赖 SQL 技能与 DBA 排期，数据价值释放迟缓
- **分析链路长**：从需求提出到报告产出，需跨越查询、导出、制图、排版多道工序
- **安全风险大**：写操作依赖人员自觉，权限粒度粗，审计追溯能力薄弱
- **部署运维重**：客户端逐台安装，版本难以统一，数据上云又引发主权顾虑
- **替代迫切**：商业工具授权成本高、存在断供风险，自主替代需求迫切

WebSQL 正是为破解上述痛点而生 —— 以 AI 原生架构重塑数据库交互范式，以自主可控的工程实现回应数据主权诉求。

***

## 二、产品定位与核心价值

### 产品定位

WebSQL 定位于 **"AI 原生、自主可控、双形态交付"** 的企业级数据库智能管理平台，提供 Web 版与桌面版两种部署形态，适用于数据查询分析、报表自动化生成、数据库日常运维、数据安全保障四大典型场景。

### 核心价值主张

| 维度       | 价值交付                                            |
| -------- | ----------------------------------------------- |
| **降本增效** | 业务人员无需 SQL 技能即可完成数据分析，从需求到报告大幅缩短周期              |
| **安全合规** | 双智能体纵深防御 + 四级 RBAC + 全链路审计，满足等保合规设计要求           |
| **自主可控** | Web 版单文件部署于自有服务器，桌面版本地运行，数据不出域，支持本地离线 AI        |
| **信创友好** | MIT 开源协议，Go + Vue 标准技术栈，纯 Go 编译无外部运行时依赖，可适配信创环境 |
| **自主替代** | 对标商业数据库工具，无授权绑定、无遥测代码、无供应商锁定                  |

***

## 三、核心能力体系

### 1. AI 智能分析引擎

以大语言模型为核心，构建"理解—生成—执行—纠错—呈现"的端到端智能分析链路：

- **自然语言驱动**：以业务语言描述分析目标，系统自动识别意图、生成准确优化的 SQL
- **Schema 自动感知**：自动获取目标库表结构与表间关系，生成符合方言语法的查询语句
- **跨库关联分析**：感知多数据库实例连接拓扑，自动拆分子查询、路由执行、合并结果
- **ReAct 自纠错机制**：SQL 执行异常时自动分析错误成因，调整参数重新生成并重试
- **AI 性能推理**：基于 EXPLAIN 执行计划，推理给出可执行的优化建议
- **流式实时输出**：基于 SSE 流式传输，分析过程实时呈现，无需等待

### 2. 智能报告生成引擎

一句话生成专业级分析报告，彻底替代"导出数据—手动制图—排版整合"的传统流程：

- **四大格式全覆盖**：Excel / PowerPoint / Word / HTML，满足不同汇报场景
- **十种可视化图表**：柱状图、横向柱状图、饼图、环形图、折线图、散点图、雷达图、热力图、面积图、堆叠柱状图
- **Python Skill 扩展引擎**：以 Python 生态扩展专业排版能力；Python 环境不可用时自动回退 Go 原生实现，保障基础可用
- **HTML 报告增强**：支持 Mermaid 流程图与 KaTeX 数学公式，适配复杂数据可视化呈现

### 3. 双智能体安全体系（核心创新）

区别于"前端套壳调 LLM"的简易方案，WebSQL 构建了具备工程深度的双智能体纵深防御架构：

- **SQLAgent 主智能体**：基于 ReAct 模式，配备 **12 个工具**（5 核心常驻 + 7 延迟加载）与最多 **13 层中间件链**，覆盖查询、Schema 获取、文件读写、导入导出等全场景
- **PermissionAgent 权限智能体**：独立审核每一次数据操作，按"连接—Schema—表—列"四级粒度逐项校验，LLM 不可用时自动降级为程序化权限检查，守住安全底线
- **防篡改审批流**：DROP / TRUNCATE / 无条件 UPDATE·DELETE 等危险写操作触发中断机制，原始参数服务端保存（15 分钟过期），前端无法篡改 SQL；批量 SQL 逐条展示，可按条确认或取消

**13 层中间件链**构成纵深防御：工具调用日志、权限审核、危险 SQL 拦截、Skill 守卫、错误恢复、上下文摘要压缩、会话同步、崩溃恢复（patchtoolcalls 自动补齐 dangling tool\_calls）、动态工具搜索、文件系统隔离、Skill 沙箱等。

### 4. 经典数据库管理

AI 之外，提供全套企业级运维能力，确保"智能"与"专业"并重：

- **智能 SQL 编辑器**：基于 CodeMirror 6，语法高亮 + Schema 感知智能补全 + 内置 EXPLAIN 执行
- **可视化数据操作**：数据浏览、筛选、排序、编辑，支持批量操作
- **ER 图双向工程**：逆向工程（数据库自动生成 ER 图）+ 正向工程（设计图生成 DDL）；支持 AI 智能分析表关系、手动增删改连线，区分物理外键 / AI 推断 / 手动新增三类来源
- **备份与恢复**：选择性备份 + AES 加密恢复，保障数据安全
- **Schema 结构对比**：自动化 Schema Diff，直接生成差异 ALTER 语句
- **数据同步**：自动识别源与目标差异、生成同步 SQL、分块处理大数据量场景
- **跨 Schema 全局搜索**：并发扫描表、列、索引、视图及数据内容
- **实时性能监控**：内置 QPS/TPS、连接池、缓存命中率、进程列表等关键指标监控

### 5. 权限与审计体系

- **四级 RBAC 权限模型**：连接 → Schema → 表 → 列，遵循"最具体优先"匹配原则；权限以自然语言注入 AI 系统提示词，实现"权限即上下文"
- **三种认证方式**：密码认证（MD5 + 盐值）/ WebAuthn 生物识别认证 / 第三方 Token 认证
- **全链路审计**：AI 写操作自动记录审计日志；经典模式 UPDATE/DELETE 自动备份至历史表，支持操作追溯
- **四层韧性保障**：SQL 执行熔断器 + AI 调用熔断器 + IP 级限流 + 登录限流（每 IP 持续速率 30 次/分钟，突发 5 次）；主模型故障自动转移至备用模型

***

## 四、双形态部署架构

WebSQL 提供 **Web 版**与 **桌面版**两种部署形态，共享同一套 Go 后端服务与 Vue 前端界面，功能完全一致：

| 维度       | Web 版                            | 桌面版                                    |
| -------- | -------------------------------- | -------------------------------------- |
| **部署形态** | 单文件二进制，服务器部署                     | 原生桌面窗口，双击即用                            |
| **访问方式** | 浏览器访问 HTTP/HTTPS                 | Wails WebView2 原生窗口                    |
| **用户认证** | 密码 / WebAuthn / 第三方 Token        | 免登录，自动以本地用户身份运行                        |
| **权限模式** | 四级 RBAC 权限控制                     | 本地模式，拥有全部权限                            |
| **管理库**  | MySQL / MariaDB / SQLite          | SQLite（纯 Go 实现，零外部依赖）                  |
| **数据持久化** | 服务器本地存储                           | 用户数据目录，应用重启不丢失                         |
| **协作模式** | 多用户共享，团队协作                       | 单用户本地使用                                |
| **适用场景** | 企业团队共享、远程运维、多用户权限管控              | 个人开发者、本地快速操作、无服务器环境                    |

### Web 版

- **单文件即服务**：Go 编译产物通过 `go:embed` 内嵌前端静态资源与 SQLite 迁移脚本，一个二进制即可启动完整服务
- **跨平台编译**：纯 Go 编译（CGO_ENABLED=0），支持 Linux / macOS / Windows 交叉编译
- **容器化友好**：支持 Docker 一键部署

### 桌面版

基于 [Wails v3](https://wails.io/) 构建，将 Web 前端包装为原生桌面应用：

- **原生窗口体验**：支持 Windows Mica 背景效果、系统主题跟随、任务栏闪烁提醒
- **单实例运行**：同一时间仅允许运行一个实例，重复启动时激活已有窗口
- **全量嵌入**：前端资源、配置文件、迁移脚本均通过 `go:embed` 嵌入二进制，真正的单文件分发
- **零配置启动**：无需安装数据库驱动、无需配置文件，双击即可使用
- **进程级资源清理**：主进程退出时操作系统自动终止所有子进程（WebView2 等），无残留
- **支持平台**：Windows / macOS / Linux（需在目标平台构建，因 Wails 依赖 CGO）

***

## 五、技术架构与先进性

### 架构理念

WebSQL 秉承 **"AI 原生、双形态交付、数据不出域"** 三大设计理念，以工程化方式将大模型能力深度嵌入数据库管理全链路，而非简单的外挂式调用。

### 技术栈

| 维度          | 技术选型                                  | 先进性说明                                         |
| ----------- | ------------------------------------- | --------------------------------------------- |
| **智能体框架**   | 字节跳动 Eino ADK v0.9                    | ReAct 模式 + 中断恢复 + CheckPoint 持久化，工业级智能体框架     |
| **大模型支持**   | 云端 API（OpenAI 兼容）+ 本地 Ollama          | 云端高性能 + 本地离线，主备模型故障自动转移                       |
| **后端**      | Go 1.26 + Gin                         | 高并发、低内存、编译型安全                                 |
| **桌面框架**    | Wails v3                              | Go + WebView2 原生桌面应用，单实例、系统主题、任务栏通知           |
| **数据库引擎**   | 纯 Go SQLite（modernc.org/sqlite）       | 无 CGO 依赖，跨平台编译，管理库零外部依赖                      |
| **前端**      | Vue 3.5 + TypeScript + Element Plus   | 响应式现代 Web，浏览器即客户端                             |
| **SQL 编辑器** | CodeMirror 6                          | 语法高亮 + Schema 感知智能补全                          |
| **数据可视化**   | ECharts 6 + AntV X6 + Mermaid + KaTeX | 图表、ER 图、流程图、公式全场景覆盖                           |
| **通信协议**    | HTTP / SSE                            | 流式实时输出，支持自签名 HTTPS 加密传输                       |
| **安全加密**    | AES + WebAuthn                        | 备份加密 + 生物识别认证                                 |
| **管理库迁移**   | 嵌入式增量迁移 + 全量脚本兜底                      | 版本化迁移系统，启动时自动升级；SQLite 自动执行，MySQL/MariaDB 手动执行 |

### 架构优势

- **双形态交付**：Web 版单文件部署，桌面版双击即用，同一套代码两种交付形态
- **零运行时依赖**：Web 版纯 Go 编译（无 CGO），桌面版仅依赖系统 WebView，可快速部署于物理服务器、虚拟化平台及容器化环境
- **数据主权可控**：所有数据不离开服务器/本机，支持本地 Ollama 离线运行 AI，满足数据不出域的合规要求
- **高韧性设计**：双熔断器 + 限流 + 故障转移，保障服务持续可用
- **全量嵌入**：前端、配置、迁移脚本均通过 `go:embed` 打入二进制，打包与部署零依赖外部文件

***

## 六、对标分析

| 维度     | 传统工具          | WebSQL                              |
| ------ | ------------- | ----------------------------------- |
| 查询方式   | 手写 SQL        | AI 自然语言驱动 + 经典手写 SQL 双模式            |
| 报告产出   | 导出数据，手动制图黏贴   | 一句话生成含图表的 Excel / PPT / Word / HTML |
| 写操作安全  | 依赖操作人员自觉      | AI 双智能体拦截 + 防篡改审批流 + 生产环境保护         |
| 权限粒度   | 连接级           | 四级 RBAC（连接—Schema—表—列）              |
| 认证方式   | 账号密码          | 密码 / WebAuthn 生物识别 / 第三方 Token      |
| 部署形态   | 逐台安装客户端       | Web 版单文件 / 桌面版双击即用 / Docker         |
| 协作方式   | 各自安装客户端       | 浏览器打开团队共享 / 桌面版本地免登录               |
| 错误处理   | SQL 报错，手动修改   | AI 自动分析错误，调整参数重试（ReAct 循环）          |
| 跨库查询   | 逐个连接切换，手动合并   | AI 感知连接拓扑，自动拆分/路由 SQL               |
| SQL 优化 | 手动 EXPLAIN 分析 | AI 驱动 EXPLAIN 计划推理，给出可执行建议          |
| 授权模式   | 商业授权，存在断供风险   | MIT 开源，无授权绑定，无供应商锁定                 |
| 数据主权   | 数据可能需上云       | 数据不出服务器/本机，支持离线 AI                  |

***

## 七、数据库支持

| 数据库             | 查询 | 写操作 | 导入导出 | AI 方言适配 |
| --------------- | -- | --- | ---- | ------- |
| MySQL / MariaDB | ✓  | ✓   | ✓    | ✓       |
| Oracle          | ✓  | ✓   | ✓    | ✓       |
| SQLite          | ✓  | ✓   | ✓    | ✓       |

***

## 八、在线体验

> [WebSQL 演示环境](http://180.184.30.223:8001/)
>
> - 账号：`admin`
> - 密码：`1`
>
> 演示服务器上行带宽有限，首次加载可能较慢。请文明使用。

***

## 九、快速开始

> **关于发行包下载的说明**
>
> 作者多次尝试在 Gitee 仓库中上传打包后的可执行文件（Web 版二进制、桌面版安装包），但由于 Gitee 对单文件大小有严格限制（普通仓库 100MB，且对二进制大文件上传稳定性较差），多次上传均以失败告终。**仓库不提供预编译发行包**，请用户按照下方指引自行编译打包。整个过程已脚本化，环境准备就绪后一条命令即可完成。

### 9.1 环境准备

#### 通用环境（Web 版与桌面版均需要）

| 工具        | 版本要求      | 安装说明                                                          |
| --------- | --------- | ------------------------------------------------------------- |
| **Go**    | 1.26+     | https://go.dev/dl/                                           |
| **Node.js** | 18+       | https://nodejs.org/ （建议使用 LTS 版本）                             |
| **npm**   | 随 Node.js | 用于构建前端                                                       |
| **Python** | 3.8+      | 用于执行打包脚本（`scripts/build_desktop.py`、`scripts/deploy-linux.py`） |

#### 桌面版额外环境（Wails v3 依赖 CGO，**无法交叉编译**）

| 平台       | 额外依赖                                                                          |
| -------- | ----------------------------------------------------------------------------- |
| **Windows** | WebView2 Runtime（Win10/11 通常自带，缺失可从微软官网下载）                                    |
| **macOS** | Xcode Command Line Tools（`xcode-select --install`）                            |
| **Linux** | `libgtk-3-dev`、`libwebkit2gtk-4.1-dev`（Debian/Ubuntu：`apt install`；CentOS：`dnf install`） |

并安装 Wails v3 CLI：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

#### 验证环境

```bash
go version          # 应输出 go1.26+
node --version      # 应输出 v18+
wails3 version      # 仅桌面版必需
```

### 9.2 获取源码

```bash
git clone https://gitee.com/<your-username>/websql.git
cd websql
```

### 9.3 编译打包 Web 版（纯 Go，支持交叉编译）

Web 版采用 `CGO_ENABLED=0` 纯 Go 编译，前端静态资源通过 `go:embed` 嵌入二进制，单文件即可部署。**可在任意平台交叉编译全部目标平台**，无需在目标平台运行。

**推荐：使用跨平台打包脚本 `scripts/build_release.py`（Windows / Linux / macOS 均可运行）：**

```bash
python scripts/build_release.py                          # 交互式选择目标平台
python scripts/build_release.py --platform windows       # 仅 Windows
python scripts/build_release.py --platform linux         # 仅 Linux
python scripts/build_release.py --platform macos         # macOS（amd64 + arm64）
python scripts/build_release.py --platform all           # 全部平台
python scripts/build_release.py --skip-frontend          # 跳过前端构建
python scripts/build_release.py --skip-build             # 跳过 Go 编译（使用已有二进制）
python scripts/build_release.py --skip-db                # 跳过全新数据库创建
```

**Windows 平台也可使用批处理脚本 `scripts\release.bat`（功能等价）：**

```bat
scripts\release.bat              :: 交互式选择目标平台
scripts\release.bat windows      :: 仅 Windows
scripts\release.bat linux        :: 仅 Linux
scripts\release.bat macos        :: 仅 macOS（amd64 + arm64）
scripts\release.bat all          :: 全部平台
```

**支持的 Web 版目标平台：**

| 平台标识          | 说明                |
| ------------- | ----------------- |
| `windows-amd64` | Windows x86_64    |
| `linux-amd64`   | Linux x86_64      |
| `macos-amd64`   | macOS Intel       |
| `macos-arm64`   | macOS Apple Silicon |

**产物：** `dist-pack/websql-web-{platform}.zip`，包内含可执行文件、前端静态资源（`static/`）、迁移脚本（`migrations/`）、配置文件（`config.json`）、预置 SQLite 管理库（`nway.sqlite3.db`）、`db_migrate.py` 升级工具、`skills/` 目录及 `startup` 启动脚本，解压即可运行。

**手动单平台快速编译（不打包，仅产出二进制）：**

```bash
# Linux / macOS
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o WebSql main.go

# Windows（PowerShell）
$env:CGO_ENABLED=0; $env:GOOS="windows"; $env:GOARCH="amd64"; go build -o WebSql.exe main.go
```

> 注：手动编译仅生成裸二进制，**不包含前端静态资源、迁移脚本、配置文件**，需自行准备 `static/`、`config.json` 等附属文件才能正常运行。生产部署推荐使用打包脚本。

### 9.4 编译打包桌面版（Wails v3，需在目标平台运行）

桌面版基于 Wails v3，依赖 CGO，**必须在目标平台原生构建**，无法交叉编译。

```bash
python scripts/build_desktop.py                          # 自动检测当前平台并完整构建
python scripts/build_desktop.py --skip-frontend          # 跳过前端构建（适用于已构建过前端的场景）
python scripts/build_desktop.py --package                # 调用 wails3 build 完整构建与打包
python scripts/build_desktop.py --check                  # 仅检查环境，不构建
```

**支持的桌面版目标平台：**

| 平台标识          | 说明                |
| ------------- | ----------------- |
| `windows-amd64` | Windows x86_64    |
| `macos-amd64`   | macOS Intel       |
| `macos-arm64`   | macOS Apple Silicon |
| `linux-amd64`   | Linux x86_64      |

**产物：** `dist-pack/websql-desktop-{platform}.zip`，包内为单个可执行文件（前端、配置、迁移脚本均通过 `go:embed` 嵌入）。

### 9.5 启动与部署（Web 版）

**使用打包产物部署（推荐）：**

```bash
# 解压产物（包内已含预置 SQLite 管理库，开箱即用）
unzip websql-web-linux-amd64.zip -d websql
cd websql

# 启动服务（默认端口 80，可通过 -port 指定）
./WebSql -port 8080

# 后台运行（Linux）
nohup ./WebSql -port 8080 > websql.log 2>&1 &

# 或使用包内 startup 脚本
./startup.sh    # Linux / macOS
startup.bat     # Windows
```

**使用 MySQL / MariaDB 作为管理库（可选）：**

打包产物默认使用 SQLite 管理库（`nway.sqlite3.db`），开箱即用无需配置。如需切换为 MySQL / MariaDB 管理库，需手动执行全量初始化脚本并修改 `config.json`：

```bash
# 1. 在 MySQL 中创建数据库并执行全量初始化脚本
mysql -u root -p -e "CREATE DATABASE websql DEFAULT CHARSET utf8mb4;"
mysql -u root -p websql < migrations/full/mysql_full.sql

# 2. 修改 config.json，将 db.type 改为 "mysql"，db.dsn 改为 MySQL 连接串
# {
#   "isRemote": true,
#   "db": {
#     "type": "mysql",
#     "dsn": "user:password@tcp(127.0.0.1:3306)/websql?charset=utf8mb4&parseTime=true",
#     ...
#   }
# }

# 3. 启动服务（后续版本升级时，由系统管理员手动执行增量脚本）
./WebSql -port 8080
```

> 注：SQLite 管理库的版本升级由程序启动时自动执行迁移脚本完成；MySQL / MariaDB 管理库的版本升级需要系统管理员手动执行 `migrations/full/mysql_full.sql` 中的增量部分。

### 9.6 Docker 容器化部署（Web 版）

项目根目录提供 `Dockerfile`，基于 Ubuntu 22.04，构建时会自动安装 Go 与 Node.js、克隆源码、编译后端与前端，最终产出可运行镜像。默认配置启用 HTTPS（端口 443），通过 `-remote` 标志以远程模式启动。

```bash
# 构建镜像
docker build -t websql .

# 运行容器（默认 HTTPS 443 端口）
docker run -d -p 443:443 -v ./data:/app/data websql

# 如需自定义端口，可通过 -port 参数指定
docker run -d -p 8080:8080 -v ./data:/app/data websql /app/WebSql -remote -port 8080
```

> 注：容器内数据目录为 `/app`，挂载 `./data` 可持久化 SQLite 管理库与日志。如需使用 MySQL 管理库，请挂载并修改 `/app/config.json`。

### 9.7 前端二次开发

```bash
cd web-src && npm install
npm run dev     # 启动开发服务器（热重载）
npm run build   # 构建生产产物到 web-src/dist/
```

> 前端构建产物默认位于 `web-src/dist/`，Web 版通过 `go:embed` 嵌入到 `static/` 目录，桌面版通过 `scripts/build_desktop.py` 自动复制到 `cmd/desktop/static/`。

### 9.8 常见问题

**Q1：桌面版构建失败提示 "CGO disabled" 或链接错误？**

Wails v3 依赖 CGO，请确保：
- 环境变量 `CGO_ENABLED=1`
- 已安装对应平台的 C 编译器（Windows 为 MinGW-w64，macOS 为 Xcode CLT，Linux 为 gcc）
- Windows 平台需安装 WebView2 Runtime（Win10/11 通常自带）

**Q2：Gitee 上传大文件失败如何分发已编译产物？**

Gitee 对单文件大小有严格限制，建议改用以下方式分发：
- GitHub Releases（支持单文件最大 2GB）
- 阿里云 OSS / 腾讯云 COS 等对象存储服务
- 将 zip 包分卷压缩后上传（如 `split -b 50m websql-web-linux-amd64.zip`）

**Q3：交叉编译桌面版失败？**

Wails v3 不支持交叉编译，**必须在目标平台原生构建**。如需 Windows 桌面版，请在 Windows 上运行 `python scripts/build_desktop.py`；如需 macOS 桌面版，请在 macOS 上运行。Linux 桌面版需先安装 `libgtk-3-dev` 和 `libwebkit2gtk-4.1-dev`。

**Q4：启动后浏览器无法访问？**

- 检查端口是否被占用：`netstat -an | grep 8080`
- 检查防火墙是否放行对应端口
- 查看日志：`./logs/websql-YYYY-MM-DD.log` 或容器内 `/app/logs/`
- 默认配置启用 HTTPS，访问地址应为 `https://host:port`（自签名证书需浏览器手动信任）

**Q5：Web 版打包脚本 `build_release.py` 与 `release.bat` 有何区别？**

两者功能等价，均产出 `dist-pack/websql-web-{platform}.zip`。`build_release.py` 是 Python 脚本，可在 Windows / Linux / macOS 任意平台运行；`release.bat` 是 Windows 批处理脚本，仅限 Windows。推荐优先使用 `build_release.py`。

**Q6：如何升级到新版本？**

- **Web 版**：重新拉取代码并打包，替换部署目录中的二进制与 `static/`、`migrations/` 目录，保留 `config.json` 与 `nway.sqlite3.db`（或 MySQL 管理库），重启服务。SQLite 管理库会自动迁移；MySQL/MariaDB 管理库需手动执行增量脚本。
- **桌面版**：重新构建并替换可执行文件即可，用户数据保存在 `%APPDATA%/WebSQL/`（Windows）或对应平台用户数据目录，不会丢失。

***

## 十、开源与自主可控优势

- **MIT 开源许可**：无限制商用、自由修改、自由分发，无 Copyleft 限制，无"企业版"功能阉割
- **数据主权可控**：Web 版单文件部署于自有服务器，桌面版本地运行，数据不出域；支持本地 Ollama 离线运行 AI，满足数据不出域要求
- **零遥测代码**：不包含任何遥测、行为追踪或数据收集代码，代码完全公开可审计
- **零供应商锁定**：基于标准 SQL 与 HTTP/SSE 协议，Go + Vue 3 标准技术栈，无商业授权服务器依赖
- **信创环境适配**：纯 Go 编译，无 CGO 依赖（Web 版），可适配信创操作系统与硬件环境
- **社区驱动演进**：所有功能模块均无"企业版"限制，欢迎通过 Issues / Pull Requests 贡献

***

## License

[MIT](LICENSE)
