# Velin Web SSH

Velin Web SSH 是一个自托管的浏览器 SSH 工作区。前端使用 Vue 3、TypeScript、Vite、Element Plus 和 xterm.js，后端使用 Go 与 SQLite。

核心特性：

- 用户登录、设备会话、管理员用户管理和审计
- 主机分组、搜索、收藏、快速连接、连接测试、密码及私钥凭据
- 主机列表/紧凑表格、原子批量分组/标签/收藏/删除，以及 OpenSSH config 导入
- 多终端标签、递归水平/垂直分屏、拖拽调整和移动端快捷键
- 多工作区创建、重命名和切换；会话筛选、置顶及受控批量恢复
- 标签快速切换时保持 WebSocket 与终端缓冲，分屏支持右键复制、粘贴、最大化和关闭
- 远程 tmux 持久会话，刷新、关闭浏览器和 Go 服务重启后可恢复
- 独立 `velin-webssh-<deployment_id>` tmux socket，不干扰远程已有 tmux 会话
- 同一终端单写多读、控制权转移和唯一 PTY 尺寸控制
- SQLite WAL、AES-GCM 凭据加密、一致性备份和工作区版本控制
- OpenSSH config 预览导入、脱敏导出、个人命令片段和响铃通知
- TOTP 双因素认证、一次性恢复码、任务静默关注和多终端命令确认
- 流式 SFTP 文件管理，以及独立启停的本地、远程和动态 SOCKS5 转发
- 通过指定 SSH 主机访问内网 HTTP/HTTPS 服务，并在独立浏览器标签中打开

完整需求和恢复边界见 [需求文档](docs/requirements.md)。

## 环境要求

- Go 1.24+
- Node.js 18.20.4+（构建链锁定为兼容 Node 18 的 Vite 6）
- 被连接的远程 Linux/Unix 主机已安装 `tmux`
- 生产环境使用 HTTPS/WSS 反向代理

tmux 安装在远程 SSH 主机，而不是 Velin 服务主机。Velin 不会自动修改远程软件源或安装软件。

## 本地构建与运行

```bash
cd web
npm install
npm run build
cd ..

export VELIN_ADMIN_PASSWORD='请替换为强密码'
go run ./cmd/velin
```

访问 `http://127.0.0.1:8377`。首次启动创建管理员 `admin`。若未设置 `VELIN_ADMIN_PASSWORD`，服务会生成随机密码并仅在首次启动日志中显示一次。

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `VELIN_ADDR` | `0.0.0.0:8377` | IPv4 HTTP 监听地址 |
| `VELIN_DATA_DIR` | `data` | SQLite、主密钥、部署 ID 和备份目录 |
| `VELIN_WEB_DIST` | `web/dist` | 前端构建产物目录 |
| `VELIN_ADMIN_USER` | `admin` | 首次启动管理员用户名 |
| `VELIN_ADMIN_PASSWORD` | 随机生成 | 仅在数据库无用户时使用 |
| `VELIN_COOKIE_SECURE` | `false` | HTTPS 部署时必须设为 `true` |

`data/master.key` 用于加密 SSH 密码和私钥，必须与 SQLite 一起备份并严格限制权限。丢失该文件后，已有加密凭据无法恢复。

端口转发监听地址固定为 `127.0.0.1`。当前版本不允许普通用户绑定局域网或公网地址；对外暴露需由管理员在反向代理或系统防火墙层明确配置。SFTP 与端口转发要求主机已绑定保存的凭据，临时密码不会被持久化。

内网 Web 配置保存在 SQLite 并展示在左侧资源栏，可选择“路径代理”或“主机端口”模式。两种模式都通过所选主机的 SSH `direct-tcpip` 通道访问 HTTP/HTTPS 目标。路径代理复用 Velin 站点端口和登录鉴权，并对常见 HTML/CSS 根路径及浏览器网络 API 做适配；主机端口模式在 Velin 主机的 `0.0.0.0:<端口>` 建立独立 HTTP 监听，以根路径原样代理不兼容子路径的应用。主机端口不继承 Velin 登录保护，部署时必须通过防火墙或反向代理限制访问。默认 Compose 使用 host 网络，使配置的端口直接监听在宿主机上。

数据库备份创建后会自动执行 SQLite 完整性、schema 版本和必需表验证，生成同名 `.sha256` 文件，并保留最近 10 份。恢复时必须同时提供原 `data/master.key`，否则加密 SSH 凭据不可用。

## 会话生命周期

- 刷新页面、关闭浏览器标签页、退出登录或 Go 服务重启：不终止远程 tmux 任务。
- 点击 Velin 终端标签的关闭按钮并确认：精确终止对应 tmux session 和其中任务。
- 需要关闭界面但保留任务：从标签菜单选择“移到后台”。
- 标签内可以递归分屏；关闭单个分屏或整个标签时可选择终止远程任务或移到后台。
- 远程主机重启或远程 tmux 进程被终止：原任务无法恢复。
- 临时凭据不会落库；Go 服务重启后需重新输入凭据才能附着仍在运行的 tmux session。

## 验证

```bash
go test ./...
go vet ./...
cd web
npm run build
VELIN_TEST_PASSWORD='管理员密码' npm run test:e2e
```

自动化覆盖数据库迁移、备份恢复验证、输出环形缓冲、多设备控制租约、TOTP 恢复码、OpenSSH 解析、SFTP 写路径、端口转发校验和批量主机原子性。SSH/tmux、SFTP 和端口转发仍需在部署环境用实际目标主机验收。详细覆盖状态见 [需求覆盖矩阵](docs/coverage.md)。

端到端测试使用系统 `/usr/bin/chromium`，验证桌面和移动端登录、页面渲染、控制台错误及横向溢出。

## Docker

```bash
VELIN_ADMIN_PASSWORD='请替换为强密码' docker compose up --build -d
```

compose 将数据保存在 `./data`，并使用 host 网络支持动态的 Web 主机端口监听。生产部署应通过 Caddy、Nginx 或其他网关提供 HTTPS，并设置 `VELIN_COOKIE_SECURE=true`；同时应通过防火墙限制主机端口模式所开放的端口。
