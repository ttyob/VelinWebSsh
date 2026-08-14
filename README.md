# Velin Web SSH

Velin Web SSH 是一个自托管的浏览器 SSH 工作区，适合集中管理服务器、终端会话和内网服务。

## 主要功能

- 主机分组、收藏、搜索、批量管理和 OpenSSH 配置导入
- 多工作区、多终端标签、分屏以及 tmux 持久会话
- SFTP 文件管理、文本文件在线编辑和代码高亮
- 本地、远程、动态 SOCKS5 端口转发
- 通过 SSH 主机代理访问内网 HTTP/HTTPS 服务
- 用户管理、操作审计、TOTP 两步验证和自动锁屏
- SQLite 数据存储和 AES-GCM 凭据加密

## 一键安装

适用于已安装 Docker Engine 和 Docker Compose v2 的 Linux `amd64/arm64` 主机：

```bash
curl -fsSL https://raw.githubusercontent.com/ttyob/VelinWebSsh/main/install.sh | sh
```

当前用户没有 Docker 权限时：

```bash
curl -fsSL https://raw.githubusercontent.com/ttyob/VelinWebSsh/main/install.sh | sudo sh
```

安装完成后，终端会显示访问地址、管理员账号和随机生成的初始密码。root 用户默认安装到 `/opt/velin`，普通用户默认安装到 `~/.local/share/velin`。

指定版本或安装目录：

```bash
curl -fsSL https://raw.githubusercontent.com/ttyob/VelinWebSsh/main/install.sh | VELIN_VERSION=v0.1.0 sh
curl -fsSL https://raw.githubusercontent.com/ttyob/VelinWebSsh/main/install.sh | VELIN_INSTALL_DIR=/srv/velin sh
```

## 下载 Release 包

每个 `v*` 版本会在 GitHub Releases 提供 Linux `amd64` 和 `arm64` 运行包。下载适合主机架构的压缩包后执行：

```bash
tar -xzf velin-linux-amd64.tar.gz
cd velin-linux-amd64
cp .env.example .env
chmod 600 .env
```

启动前请编辑 `.env`，至少设置一个强管理员密码，然后直接运行：

```bash
./velin
```

Velin 会默认读取当前目录下的 `.env`；已经由系统或服务管理器设置的环境变量优先级更高。

默认访问地址为 `http://服务器IP:8377`。Release 运行包只包含 Go 二进制、构建后的前端文件和配置示例；GitHub 会另外自动提供源码归档。

## 从源码安装

直接下载源码时，推荐使用 Docker 构建，不需要在宿主机安装 Go 和 Node.js：

```bash
git clone https://github.com/ttyob/VelinWebSsh.git
cd VelinWebSsh
cp .env.example .env
```

编辑 `.env` 中的 `VELIN_ADMIN_PASSWORD`，然后启动：

```bash
docker compose build
docker compose run --rm --no-deps --user root --entrypoint sh velin \
  -c 'chown -R velin:velin /app/data && chmod 700 /app/data'
docker compose up -d
```

查看状态和日志：

```bash
docker compose ps
docker compose logs -f
```

## 升级与卸载

一键安装版本升级：

```bash
/opt/velin/update.sh
```

源码版本升级：

```bash
git pull
docker compose up --build -d
```

停止并移除容器：

```bash
cd /opt/velin
docker compose down
```

该命令不会删除配置和数据。

## 配置与数据

常用环境变量：

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `VELIN_ADDR` | `0.0.0.0:8377` | Web 服务监听地址 |
| `VELIN_ADMIN_USER` | `admin` | 首次启动管理员账号 |
| `VELIN_ADMIN_PASSWORD` | 随机生成 | 首次启动管理员密码 |
| `VELIN_COOKIE_SECURE` | `false` | 使用 HTTPS 时设置为 `true` |
| `VELIN_HOST_PORT_ADDR` | `127.0.0.1` | Web 主机端口代理监听地址 |

数据默认保存在 `data/`。升级和迁移前应完整备份该目录，尤其是 `master.key` 和 SQLite 数据库；丢失 `master.key` 后，已有加密凭据无法恢复。

生产环境建议使用 Caddy、Nginx 等反向代理提供 HTTPS，并通过防火墙限制管理端口和主机端口代理的访问来源。
