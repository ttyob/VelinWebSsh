# Velin Web SSH

Velin Web SSH 是一个自托管的浏览器 SSH 工作区。前端使用 Vue 3、TypeScript、Vite、Element Plus 和 xterm.js，后端使用 Go 与 SQLite。

核心特性：

- 用户登录、设备会话、管理员用户管理和审计
- 主机分组、搜索、收藏、密码及私钥凭据
- 多终端标签、单窗/双窗/四窗布局和移动端快捷键
- 远程 tmux 持久会话，刷新、关闭浏览器和 Go 服务重启后可恢复
- 独立 `velin-webssh-<deployment_id>` tmux socket，不干扰远程已有 tmux 会话
- 同一终端单写多读、控制权转移和唯一 PTY 尺寸控制
- SQLite WAL、AES-GCM 凭据加密、一致性备份和工作区版本控制

完整需求和恢复边界见 [需求文档](docs/requirements.md)。

## 环境要求

- Go 1.24+
- Node.js 20+
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

访问 `http://127.0.0.1:8080`。首次启动创建管理员 `admin`。若未设置 `VELIN_ADMIN_PASSWORD`，服务会生成随机密码并仅在首次启动日志中显示一次。

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `VELIN_ADDR` | `:8080` | HTTP 监听地址 |
| `VELIN_DATA_DIR` | `data` | SQLite、主密钥、部署 ID 和备份目录 |
| `VELIN_WEB_DIST` | `web/dist` | 前端构建产物目录 |
| `VELIN_ADMIN_USER` | `admin` | 首次启动管理员用户名 |
| `VELIN_ADMIN_PASSWORD` | 随机生成 | 仅在数据库无用户时使用 |
| `VELIN_COOKIE_SECURE` | `false` | HTTPS 部署时必须设为 `true` |

`data/master.key` 用于加密 SSH 密码和私钥，必须与 SQLite 一起备份并严格限制权限。丢失该文件后，已有加密凭据无法恢复。

## 会话生命周期

- 刷新页面、关闭浏览器标签页、退出登录或 Go 服务重启：不终止远程 tmux 任务。
- 点击 Velin 终端标签的关闭按钮并确认：精确终止对应 tmux session 和其中任务。
- 需要关闭界面但保留任务：从标签菜单选择“移到后台”。
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

端到端测试使用系统 `/usr/bin/chromium`，验证桌面和移动端登录、页面渲染、控制台错误及横向溢出。

## Docker

```bash
VELIN_ADMIN_PASSWORD='请替换为强密码' docker compose up --build -d
```

compose 将数据保存在 `./data`。生产部署应通过 Caddy、Nginx 或其他网关提供 HTTPS，并设置 `VELIN_COOKIE_SECURE=true`。
