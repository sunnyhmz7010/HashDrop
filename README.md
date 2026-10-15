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

- 商品卡片提供详情与下单两个图标入口，详情支持 Markdown、图片和视频，数量确认后进入支付与凭证领取，支持中英文与移动端。
- 卡密与账号共用加密库存，原子预留、防重复导入与重复交付。
- 固定文本使用购买时的加密快照，支付后直接领取，不消耗库存。
- 网页完成首次初始化；一位管理员管理商品、库存、订单补偿及站点和支付设置。
- HashPay RSA 签名请求、加密回调和签名二次查询，付款与交付状态分别保存。
- 内置补查、交付恢复、在线备份校验，管理员 Argon2id 密码。

## ⚡ 快速开始

### 📋 前置要求

- 构建机：Go 1.27+、Node.js 24+、npm。
- 生产 VPS：Linux、Docker Engine、Docker Compose V2；无需 Go、Node.js、Redis 或 MySQL。
- 支付：可用的 HashPay 原生 REST API 商户与商户 RSA 私钥。

### 📦 安装与运行

推荐使用 GitHub Actions 构建并发布的 GHCR 镜像，并使用与镜像版本匹配的 Compose 配置。`v0.0.3` 使用[对应的 Compose 文件](https://github.com/sunnyhmz7010/TinyFaka/blob/v0.0.3/compose.yaml)，可将镜像固定为 `ghcr.io/sunnyhmz7010/tinyfaka:0.0.3`。将 `compose.yaml` 保存到部署目录，然后执行：

```sh
docker compose pull
docker compose up -d
```

`v0.0.3` 使用无凭证的管理员账号初始化流程；继续使用 `v0.0.2` 时请按[该版本说明](https://github.com/sunnyhmz7010/TinyFaka/blob/v0.0.2/README.md)初始化。

浏览器访问 `http://服务器地址:8080`，自动进入初始化页面，直接填写管理员账号和密码，无需初始化凭证。页面同时提供站点名称、地址和 HashPay 商户信息；商城公告在初始化后的后台系统设置中填写。密码不要求最小长度或字符组合，只须非空并与确认密码一致。配置与密钥由服务端生成并保存到 Docker 持久化卷；无需填写环境变量或手工编辑配置文件。HTTP 会明文传输密码与私钥，应在可信网络中完成初始化。

容器中 `/app/tinyfaka` 是程序，`/data` 统一保存配置、私钥、数据库和备份，Compose 只挂载一个数据卷。`v0.0.1` 使用[旧版 Compose 文件](https://github.com/sunnyhmz7010/TinyFaka/blob/v0.0.1/compose.yaml)的两个目录；升级前需备份并迁移原配置、密钥和数据库到 `/data`，同步修改配置中的数据库、备份和私钥路径，不能重新初始化生成加密密钥。数据库结构由程序启动时自动迁移。

默认镜像为 `ghcr.io/sunnyhmz7010/tinyfaka:latest`，可换成具体版本标签或摘要。只有推送 `v*` 版本标签才会发布镜像，推送 `main` 不触发发布。更新执行 `docker compose pull` 和 `docker compose up -d`，已有初始化信息保留。

## 📖 使用说明

旧部署没有管理员账号字段时，账号默认 `admin`，原密码继续有效。管理员登录仅使用账号和密码。商品仅支持卡密、账号和固定文本；升级时旧 API 商品保留为下架的固定文本商品，需填写交付内容后才能上架，历史订单及领取结果保留。

顶栏右侧可切换中文和 English，语言偏好保存在当前浏览器。商城与后台共用顶栏，点击站点品牌返回首页；商城根页面在语言选择右侧提供“管理后台”按钮，后台在同一位置提供“退出登录”按钮。

管理员入口 `/admin`。HashPay 可以在初始化时填写，也可以在后台系统设置补充或替换商户私钥，保存后不会回显。新建商品后导入库存并上架，固定文本商品直接填写交付内容。HTTP 站点通过签名查询补查付款，无需配置回调；HashPay 回调只接受 HTTPS 地址。使用 HTTPS 站点时，可在 HashPay 设置 `站点地址/api/webhooks/hashpay`。

已付款且交付完成、已关闭或已失效的历史订单不阻止更换 HashPay 地址或商户。待核验付款或尚未完成交付的订单仍保护原配置，即使已从后台删除也不例外。更换后原领取链接继续可用，旧订单的支付状态查询须使用原网关和商户配置。

首页商品卡片的眼睛图标打开详情，购物袋图标直接打开下单弹窗。详情页由顶部商品信息和下方详情内容两块组成，点击下单后再选择数量、确认金额。下单弹窗不显示库存数量，打开时仍重新读取商品价格与库存以限制购买数量，最终以服务端校验为准。

后台可为商品独立开启“隐藏商品”。已上架的隐藏商品不出现在首页及公开商品列表，只能通过详情链接访问和购买，详情页不显示返回商城按钮；下架商品仍无法查看或下单。后台点击已上架的商品名称即可打开详情链接。已有商品默认不隐藏，隐藏设置与上架状态分别保存。

后台支持删除商品。删除后从商城及后台商品列表移除，并停止新的下单；对应订单、库存预留、付款补查和领取结果保留，已创建的订单仍可完成付款与交付。商品采用逻辑删除，避免破坏订单关联。

后台商品编辑将商品介绍和商品详情分别保存。介绍显示在首页卡片和商品页顶部：首页最多展示三行，商品页顶部展示完整介绍，两处共用标题、库存、价格的样式与排列。下方仅展示独立的 Markdown 详情，未填写详情时不显示该内容块；原有商品说明保留为顶部介绍。详情支持编辑与预览。图片写作 `![图片说明](https://example.com/image.jpg)`；视频直链可写作 `![视频说明](https://example.com/video.mp4)`，支持 `.mp4`、`.webm`、`.ogv`，也可使用 `<video controls src="https://example.com/video.mp4"></video>`。媒体由浏览器加载 HTTPS 直链，视频能否播放取决于浏览器支持的编码；暂不提供文件上传或 iframe 视频嵌入。脚本、事件属性及危险链接会被过滤。

购买者创建订单后先保存领取链接，再跳转 HashPay。链接中的 `#token=` 是私密凭证，前端仅通过 Bearer 请求本地 API；不会传给 HashPay。丢失链接且当前浏览器会话没有保存凭证时，原则上无法自动找回。

后台订单详情提供刷新状态、删除订单和重试交付。付款状态由 HashPay 签名查询结果决定，店主不能在本站手动改为已付款或关闭。网关 `expired` 不代表永不付款，因此继续保留库存；核验为 `invalid` 后自动释放尚未交付的库存。若原网关、原商户查询明确返回 `404` 和原生错误码 `errors.order_not_found`，未付款订单会自动关闭、释放预留库存并停止补查；已核验付款的订单保留付款和领取结果，未完成交付继续执行。普通 404、网关或商户配置不符、尚未核验过归属的历史订单不会触发释放；历史订单在一次成功的完整查询核验后自动记录归属。本站后台删除只从列表及统计中移除订单，保留网关映射、补查任务和购买者领取结果，不取消网关订单。

Markdown 详情支持 `markdown-it-container` 的 `:::` 容器。`tip` / `success`、`info` / `note`、`warning`、`danger` / `error` 分别显示提示样式，`details` / `spoiler` 显示可点击展开的折叠内容；其他小写英文名称显示普通容器。标题可选，容器内支持 Markdown、图片和视频。嵌套时，外层使用更多冒号，例如 `:::: warning` 配合内层 `::: tip`；代码块内的 `:::` 保持为代码。

```markdown
::: warning 购买须知
请先确认商品适合你的用途。
:::

::: details 更多说明
这里可以填写列表、图片或视频。
:::
```

## 🧠 功能细节

五张核心表：`products`、`stock_items`、`orders`、`deliveries`、`admin_sessions`。数据库启用 WAL、外键、busy timeout 和单连接事务。金额按币种精度以整数保存，JSON 金额按十进制解析核对。支持 CNY、USD、EUR、GBP、HKD、JPY、USDT 和 USDC。

库存、固定内容和交付结果使用 AES-256-GCM 加密，并通过记录 ID 的附加认证数据防跨记录替换。去重密钥独立保留；领取 Token 和管理员会话只保存 SHA-256 哈希。配置文件与商户私钥不由静态服务提供，不在 API 返回。

HashPay 创建超时重试复用 `merchantNo`。回调解密成功不能证明来源，付款确认必须签名查询并匹配商户、订单号、金额、币种和 `paid`；HashPay 自动确认及管理员确认均按网关状态接受。启动时按版本事务迁移 SQLite，保留现有订单、库存、交付与外键关系。

## 🧱 技术栈

- 后端：Go、`net/http`、modernc SQLite、`x/crypto` Argon2id。
- 前端：Vue 3、TypeScript、Vue Router、Naive UI、Vite、SCSS；markdown-it 与 markdown-it-container 解析详情，DOMPurify 清理展示内容。
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

在另一终端运行 `npm --prefix frontend run dev`，访问 `http://127.0.0.1:5173`，在初始化页填写管理员账号和密码。站点地址保留为 Vite 的实际访问地址；它用于登录和 CSRF 的 Origin 校验。HTTP 使用 HttpOnly、SameSite Cookie，HTTPS 地址额外启用 Secure。测试网关仅存在于 Go 测试代码中，生产程序没有模拟付款接口。

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
