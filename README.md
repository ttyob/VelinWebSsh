<div align="center">

# Velin Web SSH

**轻量、自托管的浏览器 SSH 工作区**

在一个页面中管理主机、终端会话、文件、端口转发和内网 Web 服务。

[![Build](https://github.com/ttyob/VelinWebSsh/actions/workflows/container.yml/badge.svg)](https://github.com/ttyob/VelinWebSsh/actions/workflows/container.yml)
[![Release](https://img.shields.io/github/v/release/ttyob/VelinWebSsh?display_name=tag&sort=semver)](https://github.com/ttyob/VelinWebSsh/releases)
[![Container](https://img.shields.io/badge/GHCR-amd64%20%7C%20arm64-2496ED?logo=docker&logoColor=white)](https://github.com/ttyob/VelinWebSsh/pkgs/container/velinwebssh)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)

</div>

![Velin 终端工作区](.github/assets/velin-workspace.png)

## 功能

- **主机管理**：分组、收藏、搜索、批量操作、SSH 跳板机和 OpenSSH 配置导入
- **终端工作区**：多标签、分屏、普通 SSH、tmux 持久会话和断线恢复
- **文件管理**：SFTP 浏览、上传、下载、重命名，以及带行号和语法高亮的文本编辑器
- **网络工具**：本地、远程、动态 SOCKS5 端口转发
- **内网 Web**：通过指定 SSH 主机代理访问 HTTP/HTTPS 服务，并保存到侧栏快速打开
- **安全控制**：AES-GCM 凭据加密、TOTP 两步验证、6 位 PIN 锁屏和操作审计
- **轻量部署**：Go 单文件服务、Vue 前端、SQLite 数据库，支持 Linux `amd64` 和 `arm64`

## 界面预览

<p align="center">
  <img src=".github/assets/velin-web-proxy.png" alt="内网 Web 代理配置" width="900">
  <br>
  <sub>通过 SSH 主机访问内网 Web 服务，支持路径代理与独立主机端口</sub>
</p>

## 快速安装

运行环境：Linux `amd64/arm64`、Docker Engine、Docker Compose v2。

```bash
curl -fsSL https://raw.githubusercontent.com/ttyob/VelinWebSsh/main/install.sh | sh
```

当前用户没有 Docker 权限时：

```bash
curl -fsSL https://raw.githubusercontent.com/ttyob/VelinWebSsh/main/install.sh | sudo sh
```

安装完成后，终端会显示访问地址、管理员账号和随机生成的初始密码：

- root 用户默认安装到 `/opt/velin`
- 普通用户默认安装到 `~/.local/share/velin`
- 默认访问地址为 `http://服务器IP:8377`

指定版本或安装目录：

```bash
curl -fsSL https://raw.githubusercontent.com/ttyob/VelinWebSsh/main/install.sh | VELIN_VERSION=v0.1.0 sh
curl -fsSL https://raw.githubusercontent.com/ttyob/VelinWebSsh/main/install.sh | VELIN_INSTALL_DIR=/srv/velin sh
```

## 其他安装方式

### 下载 Release

从 [GitHub Releases](https://github.com/ttyob/VelinWebSsh/releases) 下载对应架构的运行包：

```bash
tar -xzf velin-linux-amd64.tar.gz
cd velin-linux-amd64
cp .env.example .env
chmod 600 .env
```

编辑 `.env`，至少设置一个强管理员密码，然后启动：

```bash
chmod +x velin
./velin
```

Release 包包含 Go 二进制、构建后的前端和配置示例，不需要安装 Go 或 Node.js。

### 从源码运行

推荐使用 Docker 构建：

```bash
git clone https://github.com/ttyob/VelinWebSsh.git
cd VelinWebSsh
cp .env.example .env
```

编辑 `.env` 中的 `VELIN_ADMIN_PASSWORD`，然后执行：

```bash
docker compose build
docker compose run --rm --no-deps --user root --entrypoint sh velin \
  -c 'chown -R velin:velin /app/data && chmod 700 /app/data'
docker compose up -d
```

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `VELIN_ADDR` | `0.0.0.0:8377` | Web 服务监听地址 |
| `VELIN_ADMIN_USER` | `admin` | 首次启动时创建的管理员账号 |
| `VELIN_ADMIN_PASSWORD` | 随机生成 | 首次启动管理员密码；建议显式设置强密码 |
| `VELIN_COOKIE_SECURE` | `false` | 使用 HTTPS 时设置为 `true` |
| `VELIN_HOST_PORT_ADDR` | `127.0.0.1` | 主机端口模式的 Web 代理监听地址 |
| `VELIN_DATA_DIR` | `data` | 数据库、主密钥和备份目录 |
| `VELIN_WEB_DIST` | `web/dist` | 构建后前端文件目录 |

Velin 默认读取运行目录下的 `.env`；系统环境变量的优先级更高。

## 升级

一键安装版本：

```bash
/opt/velin/update.sh
```

使用了自定义安装目录时，请运行该目录内的 `update.sh`。源码部署可以执行：

```bash
git pull
docker compose up -d --build
```

## 数据与安全

所有持久化数据默认保存在 `data/`。升级、迁移或恢复前，请完整备份该目录，尤其是：

- `velin.db`：用户、主机、设置和审计数据
- `master.key`：凭据加密主密钥，丢失后已有加密凭据无法恢复

首次登录后应立即修改管理员密码并启用 TOTP。公网部署时，请使用 Caddy、Nginx 等反向代理提供 HTTPS，将 `VELIN_COOKIE_SECURE` 设置为 `true`，并通过防火墙限制管理端口和主机端口代理的访问来源。

## 常用命令

```bash
# 查看服务状态
docker compose ps

# 查看实时日志
docker compose logs -f velin

# 重启服务
docker compose restart velin

# 停止服务（不会删除 data 目录）
docker compose down
```

---

<div align="center">
  <sub>Velin Web SSH · 为自托管环境设计</sub>
</div>
