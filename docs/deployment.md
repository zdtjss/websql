# 部署与构建

本文是 WebSQL 的完整部署与构建指南，项目总览见 [README](../README.md)。

***

## 双形态部署架构

WebSQL 提供 **Web 版**与 **桌面版** 两种部署形态，共享同一套 Go 后端服务与 Vue 前端界面，功能完全一致：

| 维度        | Web 版                          | 桌面版                                     |
| --------- | ------------------------------ | --------------------------------------- |
| **部署形态**  | 解压即用的单服务，部署于服务器                | 原生桌面窗口，双击即用                             |
| **访问方式**  | 浏览器访问 HTTP / HTTPS             | Wails v3 WebView2 原生窗口                  |
| **用户认证**  | 密码 / 指纹·面容 / 第三方 Token         | 免登录，自动以本地用户身份运行                         |
| **权限模式**  | 四级 RBAC 权限控制                  | 本地模式，拥有全部权限                             |
| **管理库**   | MySQL / MariaDB / SQLite       | SQLite（纯 Go 实现，零外部依赖）                    |
| **报告排版**  | Go 原生渲染；Python 可选（Skill / 高级导出） | Go 原生渲染；构建时可选捆绑 Python 运行时                |
| **内网数据库** | SSH 跳板机隧道（TCP 类连接）             | SSH 跳板机隧道（TCP 类连接）                      |
| **数据持久化** | 服务器本地存储                        | exe 同目录的 SQLite 管理库 + 用户数据后端 KV 同步，重启不丢失  |
| **协作模式**  | 多用户共享，团队协作                     | 单用户本地使用                                 |
| **适用场景**  | 企业团队共享、远程运维、多用户权限管控            | 个人开发者、本地快速操作、涉密离线环境                     |

### Web 版

- **解压即用，零外部运行时**：纯 Go 编译（`CGO_ENABLED=0`），无 CGO、无 JVM、无 Node 依赖；SQLite 迁移脚本随二进制内嵌，前端静态资源与技能包随发行包分发；标准报告导出不依赖 Python
- **跨平台编译**：支持在任意平台交叉编译 Windows / Linux / macOS（amd64 / arm64）产物
- **HTTPS 开箱可用**：`-https` 启动即自动生成自签名证书（有效期 10 年，证书信息取自 `config.json`），浏览器首次访问需手动信任
- **容器化友好**：仓库内置 `Dockerfile`，一条命令构建镜像

### 桌面版

基于 [Wails v3](https://wails.io/) 构建，将 Web 前端包装为原生桌面应用：

- **原生窗口体验**：Windows Mica 背景效果、系统主题跟随、任务栏闪烁提醒
- **单实例运行**：同一时间仅允许运行一个实例，重复启动时激活已有窗口
- **全量内嵌**：前端资源、配置与迁移脚本均通过 `go:embed` 嵌入二进制；`skills/` 随包分发，Python 运行时构建时可选捆绑（默认不捆绑）；标准报告导出无需 Python，真正的开箱即用
- **零配置启动**：无需安装数据库驱动、无需配置文件，双击即可使用；本地监听随机可用端口，避免端口冲突
- **进程级资源清理**：Windows 下通过 Job Object 在主进程退出时自动终止 WebView2 子进程，无残留
- **支持平台**：Windows / macOS / Linux（因 Wails 依赖 CGO，需在目标平台构建，Wails v3.0.0）

> **关于发行包下载**：仓库不提供预编译发行包。Gitee 对单文件大小有严格限制（普通仓库 100MB，且二进制大文件上传稳定性较差），作者多次上传均失败。请按下方指引自行编译打包，整个过程已脚本化，环境就绪后一条命令即可完成。

***

## 一、环境准备

### 通用环境（Web 版与桌面版均需要）

| 工具          | 版本要求      | 安装说明                                                          |
| ----------- | --------- | ------------------------------------------------------------- |
| **Go**      | 1.27+     | https://go.dev/dl/                                           |
| **Node.js** | 22.18+（Vite 8 最低 20.19，部分构建依赖要求 22.18+） | https://nodejs.org/ （推荐使用 22 LTS 最新版） |
| **npm**     | 随 Node.js | 用于构建前端                                                       |
| **Python**  | 3.8+      | 用于执行打包脚本（`scripts/build_release.py`、`scripts/build_desktop.py`）；Skill / docx·pdf 解析为可选项 |

### 桌面版额外环境（Wails v3 依赖 CGO，无法交叉编译）

| 平台        | 额外依赖                                                                          |
| --------- | ----------------------------------------------------------------------------- |
| **Windows** | WebView2 Runtime（Win10/11 通常自带，缺失可从微软官网下载）                                    |
| **macOS** | Xcode Command Line Tools（`xcode-select --install`）                            |
| **Linux** | `libgtk-3-dev`、`libwebkit2gtk-4.1-dev`（Debian/Ubuntu：`apt install`；CentOS：`dnf install`） |

并安装 Wails v3 CLI：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

### 验证环境

```bash
go version          # 应输出 go1.27+
node --version      # 应输出 v22.18+（最低 v20.19）
wails3 version      # 仅桌面版必需
```

***

## 二、获取源码

```bash
git clone https://gitee.com/nway/websql.git
cd websql
```

***

## 三、编译打包 Web 版

Web 版采用 `CGO_ENABLED=0` 纯 Go 编译，SQLite 迁移脚本通过 `go:embed` 嵌入二进制。**可在任意平台交叉编译全部目标平台**，无需在目标平台运行。

**推荐：使用跨平台打包脚本 `scripts/build_release.py`（Windows / Linux / macOS 均可运行）：**

```bash
python scripts/build_release.py                          # 交互式选择目标平台
python scripts/build_release.py --platform windows       # 仅 Windows
python scripts/build_release.py --platform linux         # 仅 Linux
python scripts/build_release.py --platform macos         # macOS（amd64 + arm64）
python scripts/build_release.py --platform all           # 全部平台
python scripts/build_release.py --python-mode external   # 外置模式：自动下载安装 Python（推荐）
python scripts/build_release.py --python-mode bundle     # 内置模式：Python 运行时打进包（仅 Windows）
python scripts/build_release.py --skip-frontend          # 跳过前端构建
python scripts/build_release.py --skip-build             # 跳过 Go 编译（使用已有二进制）
python scripts/build_release.py --skip-db                # 跳过全新数据库创建
```

### Python 运行时打包模式

可选配置项 `--python-mode`，不传时交互式询问，默认外置：

| 模式 | 包内 Python 资源 | 首次启动 | 适用场景 |
| --- | --- | --- | --- |
| `external` | 不含运行时，附 `python_bootstrap.py` + `setup-python.bat/.ps1/.sh` | 自动检测并下载 Python、安装依赖，写 `python-runtime.json` | 可联网环境、包体敏感、全平台（Windows/Linux/macOS） |
| `bundle` | 内置 `python/` 运行时（Python 3.15.2） | 解压即用，无需联网 | 内网/离线、拷贝即用；当前仅 Windows 由本地 embeddable 包构建 |

- 外置模式启动脚本会自动执行 `setup-python.sh|bat --ensure`，无需人工操作；首次下载需要访问
  `python-build-standalone` 发布源，内网可通过 `WEBSQL_PYTHON_MIRROR` 指定镜像，pip 可通过
  `WEBSQL_PIP_INDEX_URL` 指定内网 PyPI。
- 两种模式都可用 `WEBSQL_PYTHON` 显式指定解释器；应用启动按
  `WEBSQL_PYTHON` → `python-runtime.json` → 随包 `python/` → 系统 `PATH` 的顺序发现 Python。
- 内网离线部署：在可联网机器执行一次 `setup-python.bat|sh`，把生成的 `python/` 与
  `python-runtime.json` 一并拷贝到目标机即可，目标机不会再次下载。
- 每个发行包内都包含 `PYTHON-RUNTIME.md`，标注当前模式、两种模式的差异、安装命令与注意事项。

### Windows 批处理脚本（备选）

`scripts\release.bat` 仅做基础打包，不含 Python 运行时相关资源，需要 Python 能力请使用 `build_release.py`：

```bat
scripts\release.bat              :: 交互式选择目标平台
scripts\release.bat windows      :: 仅 Windows
scripts\release.bat linux        :: 仅 Linux
scripts\release.bat macos        :: 仅 macOS（amd64 + arm64）
scripts\release.bat all          :: 全部平台
```

### 支持的 Web 版目标平台

| 平台标识            | 说明                  |
| --------------- | ------------------- |
| `windows-amd64` | Windows x86_64      |
| `linux-amd64`   | Linux x86_64        |
| `macos-amd64`   | macOS Intel         |
| `macos-arm64`   | macOS Apple Silicon |

**产物：** `dist-pack/websql-web-{platform}.zip`，包内含可执行文件、前端静态资源（`static/`）、迁移脚本（`migrations/`）、配置文件（`config.json`）、预置 SQLite 管理库（`nway.sqlite3.db`）、`db_migrate.py` 升级工具、`skills/` 目录、`AGENTS.md` 运行时规范、`startup` 启动脚本及 `PYTHON-RUNTIME.md` 模式说明，解压即可运行；外置模式另含 Python 自动安装脚本，内置模式另含 `python/` 运行时。

### 手动单平台快速编译（不打包，仅产出二进制）

```bash
# Linux / macOS
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o WebSql main.go

# Windows（PowerShell）
$env:CGO_ENABLED=0; $env:GOOS="windows"; $env:GOARCH="amd64"; go build -o WebSql.exe main.go
```

> 注：手动编译产出的裸二进制已内嵌 SQLite 迁移脚本，但**不包含前端静态资源与配置文件**（Web 版前端为外置 `static/` 目录），需自行准备 `static/`、`config.json` 等附属文件才能正常运行。生产部署推荐使用打包脚本。

***

## 四、编译打包桌面版

桌面版基于 Wails v3，依赖 CGO，**必须在目标平台原生构建**，无法交叉编译。脚本会自动检测当前平台。

```bash
python scripts/build_desktop.py                          # 自动检测当前平台并完整构建（交互式询问是否捆绑 Python，默认否）
python scripts/build_desktop.py --skip-frontend          # 跳过前端构建（适用于已构建过前端的场景）
python scripts/build_desktop.py --package                # 调用 wails3 build 完整构建与打包
python scripts/build_desktop.py --check                  # 仅检查环境，不构建
```

> 是否捆绑 Python 运行时在构建过程中交互式询问（`[y/N]`，直接回车即跳过）；跳过时标准报告导出仍由 Go 原生引擎完成，Skill 脚本与 pdf 解析需 Python（未捆绑时使用系统 Python）；docx 解析优先 Python、缺失时回退内置 Go 实现。

**产物：** `dist-pack/websql-desktop-{platform}.zip`，包内为单个可执行文件（前端资源、配置、迁移脚本均通过 `go:embed` 嵌入）、随包分发的 `skills/` 技能目录，以及可选捆绑的 `python/` 运行时。

***

## 五、启动与部署（Web 版）

### 使用打包产物部署（推荐）

```bash
# 解压产物（包内已含预置 SQLite 管理库，开箱即用）
unzip websql-web-linux-amd64.zip -d websql
cd websql

# 启动服务（默认端口 80，可通过 -port 指定）
./WebSql -port 8080

# 启用 HTTPS（默认端口 443，自签名证书自动生成于程序同目录）
./WebSql -https

# 后台运行（Linux）
nohup ./WebSql -port 8080 > websql.log 2>&1 &

# 或使用包内 startup 脚本
./startup.sh    # Linux / macOS
startup.bat     # Windows
```

### 使用 MySQL / MariaDB 作为管理库（可选）

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

### 可选配置（`config.json`）

- `db.connectTimeoutSec`：受管数据库连接超时（秒），默认 10
- `db.queryTimeoutSec`：受管数据库语句超时（秒），默认 300
- `https.enable` / `https.organization` / `https.commonName`：自签名证书信息
- `security.aesKey`：非本地（远程）模式必填，base64 编码的 16 / 24 / 32 字节密钥，缺失时拒绝启动

只读标记与 SSH 跳板机配置保存在管理库 `t_system_config`（key 前缀分别为 `conn.readOnly.`、`conn.ssh.`），不新增表 / 列，升级无需额外迁移。

***

## 六、Docker 容器化部署（Web 版）

项目根目录提供 `Dockerfile`，基于 Ubuntu 22.04，构建时自动安装 Go 与 Node.js、克隆源码、编译前后端并打入镜像，同时预装 Python 3 与 `python-docx` / `pdfplumber` 供 AI 文档解析使用，产出可直接运行的镜像，容器内默认以 HTTP 80 端口启动。

```bash
# 构建镜像
docker build -t websql .

# 运行容器（容器内监听 80，映射为宿主 8080 端口）
docker run -d -p 8080:80 -v websql-logs:/app/logs websql

# 启用 HTTPS（端口 443）
docker run -d -p 443:443 websql /app/WebSql -https -port 443
```

> 持久化说明：容器内管理库默认位于 `/app/nway.sqlite3.db`、日志位于 `/app/logs`。如需持久化 SQLite 管理库，请在构建前把 `config.json` 的 `db.dsn` 改为 `./data/nway.sqlite3.db`（`Dockerfile` 会将该文件复制进镜像），再挂载 `-v websql-data:/app/data`；如需使用 MySQL 管理库，同样在构建前修改 `config.json` 即可。

***

## 七、前端二次开发

```bash
cd web-src && npm install
npm run dev     # 启动开发服务器（热重载）
npm run build   # 构建生产产物到 web-src/dist/
```

> 前端构建产物默认位于 `web-src/dist/`，Web 版将其作为外置 `static/` 目录随包分发，桌面版通过 `scripts/build_desktop.py` 自动复制到 `cmd/desktop/static/` 并内嵌进二进制。

***

## 八、升级与数据保留

- **Web 版**：重新拉取代码并打包，替换部署目录中的二进制与 `static/`、`migrations/` 目录，保留 `config.json` 与 `nway.sqlite3.db`（或 MySQL 管理库），重启服务。SQLite 管理库会自动迁移；MySQL/MariaDB 管理库需手动执行增量脚本。只读标记与 SSH 配置保存在 `t_system_config`，升级无需额外迁移。
- **桌面版**：重新构建并替换可执行文件即可，用户数据保存在 exe 同目录（`nway.sqlite3.db` 管理库、`logs/` 日志，exe 目录无写权限时日志回退到 `%APPDATA%/WebSQL/logs`），不会丢失。
