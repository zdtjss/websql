# 常见问题

## Q1：桌面版构建失败提示 "CGO disabled" 或链接错误？

Wails v3 依赖 CGO，请确保：

- 环境变量 `CGO_ENABLED=1`
- 已安装对应平台的 C 编译器（Windows 为 MinGW-w64，macOS 为 Xcode CLT，Linux 为 gcc）
- Windows 平台需安装 WebView2 Runtime（Win10/11 通常自带）

## Q2：没有预编译安装包，如何分发已编译产物？

仓库不提供预编译发行包，因为 Gitee 对单文件大小有严格限制。自行打包后可通过以下方式分发：

- GitHub Releases（支持单文件最大 2GB）
- 阿里云 OSS / 腾讯云 COS 等对象存储服务
- 将 zip 包分卷压缩后上传（如 `split -b 50m websql-web-linux-amd64.zip`）

## Q3：交叉编译桌面版失败？

Wails v3 不支持交叉编译，**必须在目标平台原生构建**。如需 Windows 桌面版，请在 Windows 上运行 `python scripts/build_desktop.py`；如需 macOS 桌面版，请在 macOS 上运行。Linux 桌面版需先安装 `libgtk-3-dev` 和 `libwebkit2gtk-4.1-dev`。

## Q4：启动后浏览器无法访问？

- 检查端口是否被占用：`netstat -an | grep 8080`
- 检查防火墙是否放行对应端口
- 查看日志：`./logs/websql-YYYY-MM-DD.log`（日志按天轮转，保留 7 天）或容器内 `/app/logs/`
- 若以 `-https` 启动，访问地址应为 `https://host:port`，自签名证书需浏览器手动信任

## Q5：`build_release.py` 与 `release.bat` 有何区别？

两者均产出 `dist-pack/websql-web-{platform}.zip`。`build_release.py` 是 Python 脚本，可在 Windows / Linux / macOS 任意平台运行，并会用全量脚本预置一个全新的 SQLite 管理库；`release.bat` 是 Windows 批处理脚本，仅限 Windows，不含预置管理库（首次启动自动建库）。推荐优先使用 `build_release.py`。

## Q6：如何升级到新版本？

- **Web 版**：重新拉取代码并打包，替换部署目录中的二进制与 `static/`、`migrations/` 目录，保留 `config.json` 与 `nway.sqlite3.db`（或 MySQL 管理库），重启服务。SQLite 管理库会自动迁移；MySQL/MariaDB 管理库需手动执行增量脚本。只读标记与 SSH 配置保存在 `t_system_config`，升级无需额外迁移。
- **桌面版**：重新构建并替换可执行文件即可，用户数据保存在 exe 同目录（`nway.sqlite3.db` 管理库、`logs/` 日志，exe 目录无写权限时日志回退到 `%APPDATA%/WebSQL/logs`），不会丢失。

## Q7：AI 分析报错或想更换模型怎么办？

系统支持 OpenAI 兼容接口与本地 Ollama 两类模型，可在系统设置中维护模型列表与默认模型（含 baseUrl、apiKey、温度等参数），并为每个模型声明 `vision` / `video` 能力（留空时按模型名推断）。模型调用遇到 429、5xx、超时、连接中断会自动退避重试；401 / 403 或密钥错误不会重试，请检查模型配置。

## Q8：没有安装 Python 会影响 AI 报告导出吗？

不会。Excel / PPT / Word / HTML 标准报告由 Go 原生渲染引擎完成，不依赖 Python；docx 解析在无 Python 时回退内置 Go 实现，pdf 解析与 Skill 脚本（模板驱动排版、热力图等）需要 Python。桌面版构建时可选择捆绑 Python 运行时。

## Q9：连接设为只读后如何临时写入？

在「连接管理」中编辑该连接，点击「临时解锁写入」，解锁窗口为 **5 分钟**（界面固定 5 分钟；`POST /api/conn/readonly/unlock` 另支持 `durationSecs=60` 的 1 分钟档，仅接受 60/300 秒）；解锁状态仅保存在内存中，到期自动恢复只读，进程重启也会立即失效。只读限制作用于经典 SQL 写语句与表结构变更。

## Q10：SSH 跳板机隧道建立失败怎么办？

先确认跳板机地址、用户与密码 / 私钥正确；配置了 `knownHostsPath` 时会严格校验主机密钥，未配置时接受任意主机密钥并记录警告（TOFU）。隧道建立失败不会回退为直连，请先修复跳板机配置。SQLite 为本地文件数据库，不支持 SSH 隧道。

## Q11：结果集只显示一部分行怎么办？

结果区默认每页 200 行，底部会固定展示「已显示前 N 行 / 共 N 行」，点击「继续加载」可增量取数；完整数据建议使用 Excel / CSV / JSON / SQL 导出。SQL 执行过程中可点击「终止」，服务端会按 `requestId` 主动取消并释放数据库连接。

## Q12：SQL 编辑器里的 `Ctrl+F` 行为和以前不一样？

编辑器内 `Ctrl+F` 现在打开 CodeMirror 的查找 / 替换面板，不再触发页内查找；如需恢复旧的页内查找行为，可在浏览器控制台执行 `localStorage.setItem('editorSearch','0')` 后刷新页面。

