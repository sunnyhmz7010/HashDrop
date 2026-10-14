<div align="center">
  <h1>TinyFaka</h1>
  <p>在单台 VPS 上提供匿名购买、HashPay 收款与安全自动发卡。</p>
</div>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/License-GPL--3.0-10b981" alt="License" /></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.27-00add8" alt="Go" /></a>
  <a href="https://vuejs.org/"><img src="https://img.shields.io/badge/Vue-3-42b883" alt="Vue" /></a>
</p>

---

## ✨ 为什么做这个项目

为单台 VPS 提供维护负担较小的数字商品商城。生产环境只有 Go 单体服务与 SQLite，收款和到账识别独立交给 HashPay。匿名买家无需账号，凭订单专属链接领取商品。

## 🚀 核心能力

- 商品列表、详情、数量选择、支付状态和凭证领取，支持中英文与移动端。
- 卡密与账号共用加密库存，原子预留、防重复导入与重复交付。
- 固定文本使用购买时的加密快照；API 商品通过预置适配器扩展，未部署适配器不能上架。
- 网页完成首次初始化；一位管理员管理商品、库存、订单补偿及站点和支付设置。
- HashPay RSA 签名请求、加密回调和签名二次查询，付款与交付状态分别保存。
- 内置补查、交付恢复、在线备份校验，管理员 Argon2id 密码和可选 TOTP。

## ⚡ 快速开始

### 📋 前置要求

- 构建机：Go 1.27+、Node.js 24+、npm。
- 生产 VPS：Linux、Docker Engine、Docker Compose V2；无需 Go、Node.js、Redis 或 MySQL。
- 支付：可用的 HashPay 原生 REST API 商户与商户 RSA 私钥。

### 📦 安装与运行

推荐使用 GitHub Actions 构建并发布的 GHCR 镜像，并使用与镜像版本匹配的 Compose 配置。`v0.0.2` 使用[对应的 Compose 文件](https://github.com/sunnyhmz7010/TinyFaka/blob/v0.0.2/compose.yaml)，可将镜像固定为 `ghcr.io/sunnyhmz7010/tinyfaka:0.0.2`。将 `compose.yaml` 保存到部署目录，然后执行：

```sh
docker compose pull
docker compose up -d
docker compose logs tinyfaka
```

浏览器访问 `http://服务器地址:8080`，自动进入初始化页面。复制当前启动日志的 `setup_token`，在网页填写站点地址、管理员密码、可选 TOTP 和 HashPay 商户信息。配置与密钥由服务端生成并保存到 Docker 持久化卷；无需填写环境变量或手工编辑配置文件。HTTP 会明文传输密码与私钥，应在可信网络中完成初始化。

容器中 `/app/tinyfaka` 是程序，`/data` 统一保存配置、私钥、数据库和备份，Compose 只挂载一个数据卷。`v0.0.1` 使用[旧版 Compose 文件](https://github.com/sunnyhmz7010/TinyFaka/blob/v0.0.1/compose.yaml)的两个目录；升级前需备份并迁移原配置、密钥和数据库到 `/data`，同步修改配置中的数据库、备份和私钥路径，不能重新初始化生成加密密钥。数据库结构由程序启动时自动迁移。

默认镜像为 `ghcr.io/sunnyhmz7010/tinyfaka:latest`，可换成具体版本标签或摘要。只有推送 `v*` 版本标签才会发布镜像，推送 `main` 不触发发布。更新执行 `docker compose pull` 和 `docker compose up -d`，已有初始化信息保留。

## 📖 使用说明

顶栏右侧可切换中文和 English，语言偏好保存在当前浏览器。商城与后台共用顶栏，点击站点品牌返回首页；退出登录位于管理员左侧栏底部，手机端位于管理菜单底部。

管理员入口 `/admin`。HashPay 可以在初始化时填写，也可以在后台系统设置补充或替换商户私钥，保存后不会回显。新建商品后导入库存并上架，固定文本商品直接填写交付内容。HTTP 站点通过签名查询补查付款，无需配置回调；HashPay 回调只接受 HTTPS 地址。使用 HTTPS 站点时，可在 HashPay 设置 `站点地址/api/webhooks/hashpay`。

购买者创建订单后先保存领取链接，再跳转 HashPay。链接中的 `#token=` 是私密凭证，前端仅通过 Bearer 请求本地 API；不会传给 HashPay。丢失链接且当前浏览器会话没有保存凭证时，原则上无法自动找回。

后台订单详情提供刷新状态、删除订单和重试交付。付款状态由 HashPay 签名查询结果决定，店主不能在本站手动改为已付款或关闭。网关 `expired` 不代表永不付款，因此继续保留库存；核验为 `invalid` 后自动释放尚未交付的库存。删除从后台列表及统计中移除订单，保留网关映射、补查任务和购买者领取结果，不取消网关订单。

## 🧠 功能细节

五张核心表：`products`、`stock_items`、`orders`、`deliveries`、`admin_sessions`。数据库启用 WAL、外键、busy timeout 和单连接事务。金额按币种精度以整数保存，JSON 金额按十进制解析核对。支持 CNY、USD、EUR、GBP、HKD、JPY、USDT 和 USDC。

库存、固定内容和交付结果使用 AES-256-GCM 加密，并通过记录 ID 的附加认证数据防跨记录替换。去重密钥独立保留；领取 Token 和管理员会话只保存 SHA-256 哈希。配置文件与商户私钥不由静态服务提供，不在 API 返回。

HashPay 创建超时重试复用 `merchantNo`。回调解密成功不能证明来源，付款确认必须签名查询并匹配商户、订单号、金额、币种和 `paid`；HashPay 自动确认及管理员确认均按网关状态接受。API 请求结果不明时禁止盲目重试。启动时按版本事务迁移 SQLite，保留现有订单、库存、交付与外键关系。

## 🧱 技术栈

- 后端：Go、`net/http`、modernc SQLite、`x/crypto` Argon2id。
- 前端：Vue 3、TypeScript、Vue Router、Naive UI、Vite、SCSS。
- 运行：Go `embed` 静态资源、SQLite、Docker Compose；也可使用 systemd 部署二进制。
- 验证：Go 集成测试、Playwright 真实浏览器与测试 HashPay 边界。

## 🗂️ 项目结构

```text
TinyFaka/
├── backend/              # 路由、业务、安全、支付与内置调度
│   └── web/              # 前端构建产物，嵌入二进制
├── frontend/
│   ├── src/pages/        # 商城、订单、登录与后台入口
│   ├── src/components/   # 管理页面组件
│   └── tests/            # 核心购买与管理流程浏览器验证
├── migrations/           # SQLite 初始化与版本迁移
├── .github/workflows/docker.yml # GitHub 构建并发布 GHCR 镜像
├── Dockerfile            # 前端和 Go 多阶段构建，运行镜像无 Node.js
├── compose.yaml          # VPS 直接拉取镜像部署
├── deploy/               # systemd 服务示例
├── assets.go
└── go.mod
```

## 👨‍💻 本地开发

### 🧰 环境

Go 1.27+ 和 Node.js 24+。Windows 本地服务只监听回环地址。Windows 使用 ACL 限制配置及私钥访问；POSIX 系统强制配置和私钥 `0600`。

安装前端依赖后启动后端，配置尚不存在时自动进入网页初始化：

```sh
npm --prefix frontend ci
go run ./backend
```

在另一终端运行 `npm --prefix frontend run dev`，访问 `http://127.0.0.1:5173`，填写后端日志的初始化凭证。站点地址保留为 Vite 的实际访问地址；它用于登录和 CSRF 的 Origin 校验。HTTP 使用 HttpOnly、SameSite Cookie，HTTPS 地址额外启用 Secure。测试网关仅存在于 Go 测试代码中，生产程序没有模拟付款接口。

### ⚙️ 命令

```sh
go test ./...
go vet ./...
npm --prefix frontend run check
npm --prefix frontend run build
cd frontend
npx playwright install chromium
npm run test:e2e
```

浏览器测试运行临时 SQLite、临时 RSA 私钥、回环后端与 TLS 测试网关，覆盖购买、领取、商品保存后重新读取、库存去重、设置持久化和代表性视口。测试不接触真实商户或真实付款。已有 Chromium 时可用 `PLAYWRIGHT_CHROMIUM_EXECUTABLE` 指定路径。

## 🔐 安全报告

如果发现安全问题，请不要公开披露细节。请优先参考仓库中的 [SECURITY.md](./SECURITY.md) 提交安全报告。

## 📄 许可证

本项目基于 [GPL-3.0](./LICENSE) 开源。

<div align="center">
  <sub>Built with ❤️ by Sunny</sub>
</div>
