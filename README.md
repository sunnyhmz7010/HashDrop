<div align="center">
  <img src="https://raw.githubusercontent.com/sunnyhmz7010/HashDrop/main/frontend/public/favicon.svg" alt="HashDrop Logo" width="120" />
  <h1>HashDrop</h1>
  <p>对接 HashPay 的匿名数字商品交付系统</p>
</div>

<p align="center">
  <a href="https://github.com/sunnyhmz7010/HashDrop/releases"><img src="https://img.shields.io/github/v/release/sunnyhmz7010/HashDrop?label=Release&color=3b82f6" alt="Release" /></a>
  <a href="https://github.com/sunnyhmz7010/HashDrop/blob/main/LICENSE"><img src="https://img.shields.io/github/license/sunnyhmz7010/HashDrop?color=10b981" alt="License" /></a>
</p>

---

## ✨ 为什么做这个项目

HashPay 已经提供了加密货币收款能力，但要真正销售数字商品，还需要商品展示、库存管理、订单查询和自动交付。HashDrop 的目标，就是对接 HashPay，提供一套轻量、易部署的数字商品商城解决方案，而不是重新开发支付系统。

- 直接接入 HashPay，将收款与支付状态核验交给支付网关
- 为买家提供无需注册账号的下单、支付和凭密钥领取流程
- 通过 Go 单体服务与 SQLite 简化部署和维护，适合在 VPS 上独立运行

## 📸 截图预览

![商城首页](./images/store.png)

![商品详情](./images/product.png)

![下单弹窗](./images/checkout.png)

## 🚀 核心能力

### 🛍️ 匿名商城

- 无需注册或登录，选择商品即可下单
- 对接 HashPay 收款，支付核验后自动交付
- 凭领取密钥查询订单、继续支付和领取商品
- 支持商品详情、隐藏商品链接、中英文界面和移动端访问

### 📦 商品与库存

- 商品交付可自由组合文字、图片、文件、链接与库存取货项，无需选择商品类型
- 卡密可按行批量导入，库存条目也可保存配对信息、专属文件或组合内容
- 库存池绑定商品中的具体取货项，可设置一次性消耗或重复发放
- 同一取货项的多个池互为备选，不同必需项一起预留；缺少任何一项即停止下单
- 支持每单购买数量上下限和交付说明，暂不收集买家信息
- 商品支持随机分配、先绑定的库存池优先或后绑定的库存池优先
- 支持自动优惠活动、优惠码和多种优惠规则
- 支持库存管理、订单查询和交付异常重试

### 🛠️ 站点管理

- 通过网页完成首次初始化，单管理员后台管理
- 管理商品、库存、订单、优惠及站点和支付配置
- 可启用独立赞赏页面，通过 HashPay 接收赞赏
- 使用 SQLite 保存数据，内置定时备份与完整性校验

## ⚡ 快速开始

### 📋 前置要求

- 一台安装了 Docker Engine 的 Linux VPS（使用 Compose 部署时，还需 Docker Compose V2）
- 支付网关：[HashPay](https://github.com/tgdash/HashPay)（可在初始化后配置，正式收款前需要接入）

### 📦 Docker Compose（推荐）

新建 `compose.yaml`，写入以下内容：

```yaml
services:
  hashdrop:
    image: ghcr.io/sunnyhmz7010/hashdrop:1.2.0
    container_name: hashdrop
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - hashdrop_data:/data

volumes:
  hashdrop_data:
```

然后启动：

```bash
docker compose up -d
```

查看日志：

```bash
docker compose logs -f
```

> 上面的 `8080:8080` 会将宿主机端口对外监听。生产环境建议先配置 HTTPS 反向代理，并将端口映射改为 `127.0.0.1:8080:8080`，避免直接向公网暴露管理与初始化页面。

### 🖥️ 命令行方式

```bash
docker run -d \
  --name hashdrop \
  --restart unless-stopped \
  -p 8080:8080 \
  -v hashdrop_data:/data \
  ghcr.io/sunnyhmz7010/hashdrop:1.2.0
```

### 🛠️ 自行构建镜像

如果你想自己构建而不是使用预构建镜像：

```bash
git clone https://github.com/sunnyhmz7010/HashDrop.git
cd HashDrop
docker build -t hashdrop .
docker run -d \
  --name hashdrop \
  --restart unless-stopped \
  -p 8080:8080 \
  -v hashdrop_data:/data \
  hashdrop
```

如果使用 Docker Compose，把 `compose.yaml` 里的 `image: ghcr.io/...` 换成 `build: .`，然后执行：`docker compose up -d --build`。

### 🔧 首次初始化

1. 服务启动后，访问商城的 `/setup` 页面。在服务器本机可打开 `http://localhost:8080/setup`；远程生产环境请先配置 HTTPS 域名，例如 `https://shop.example.com/setup`。
2. 填写站点名称、站点地址以及管理员账号和密码。站点地址应与实际访问地址一致。
3. 配置 HashPay 网关地址（HTTPS）、商户 ID 与 RSA 私钥。三项可以全部留空，稍后在后台的「系统设置」中补充。
4. 初始化完成后会自动登录并进入 `/center`，创建库存池、上架商品，即可开始使用。若当前访问地址与配置的站点地址不同，则自动前往配置站点的登录页。

使用 HTTPS 站点时，后台会显示 HashPay 回调地址 `https://你的商城域名/api/webhooks/hashpay`；HTTP 站点仅通过签名查询补查付款，不接受支付回调。**不要通过公网明文 HTTP 提交管理员密码或商户私钥。**

## 📖 使用说明

### 👤 买家购买与领取

1. 在商城选择商品，阅读交付说明，在允许的数量范围内确认金额。
2. 设置并妥善保存领取密钥，然后前往 HashPay 支付。
3. 支付完成后进入「个人中心」查看订单，点击「领取内容」打开已完成的交付。文字、图片、文件与链接按商家编排展示，附件经授权后下载；多份订单已完成的部分可以先领取。其他设备可凭领取密钥导入订单。

领取密钥是查询和领取订单的凭证，**丢失后无法找回**，建议使用页面自动生成的密钥。

当前浏览器长期保存已下单或导入的订单信息与领取密钥，不保存交付内容。可在个人中心主动清除本地历史；清除前请备份密钥，清除浏览器数据后也需要凭密钥重新导入。订单历史不会自动同步到其他设备。

订单状态查询、主动领取和附件访问分别处理。状态查询不返回交付正文；领取与附件请求使用请求头中的凭证，密钥不写入下载地址。支付请求中断时，当前标签页暂存下单信息以恢复同一笔请求。

### 🛠️ 商家管理

进入 `/center`，点击「管理员登录」并在弹窗登录后，在个人菜单下追加管理菜单：

1. **商品**：设置价格、购买数量与交付说明，编排文字、图片、文件、链接，以及每份商品需要的库存取货项。每个取货项有独立名称与所需数量；固定内容无需库存池。交付说明在购买前公开，不能包含卡密、密码等秘密。
2. **库存**：创建库存池，设置是否重复发放，并绑定商品的具体取货项。可按行批量导入卡密，也可使用内容编辑器添加组合库存。缺少必需资源的商品无法下单。
3. **优惠**：按需要设置自动活动或优惠码。
4. **订单**：按订单号、商品名及付款、交付状态筛选。已核验付款的异常订单可以重试原分配，或用同一内容编辑器补交未完成部分；补交归还原来未使用的独占库存，成功结果不可覆盖。
5. **系统设置**：维护站点信息及 HashPay 配置；还可按需开启赞赏页。

商城入口为 `/`，个人中心与管理入口统一为 `/center`，赞赏页（启用后）为 `/donate`。个人订单与捐赠记录始终可见，管理员登录后追加管理菜单。赞赏时自行填写昵称，并选择是否展示在公开名单；昵称已有记录时，须确认是否为再次赞赏，也可更换昵称或选择派生昵称建议。捐赠记录与商品订单分开保存，可凭昵称找回该昵称的全部赞赏并导入本地历史，也可独立清除本地记录。管理员概览下方以浮动小卡片按昵称展示赞赏，卡片内可展开历史、核验逐笔状态，到账金额按币种分别汇总。

昵称用于公开查询，不是私密密码：不展示在名单中仅影响公开列表，知道昵称的人仍可查询金额和付款状态。赞赏统一使用昵称查询，不生成包含凭证的查询链接。后台「赞赏页」按币种添加金额，无需输入逗号分隔文本，可同时配置 CNY、USD、USDT 等币种，每个币种分别设置预设金额与自定义金额范围。

### 🔒 HTTPS 与访问控制

- 生产环境建议通过 Nginx、1Panel OpenResty 等反向代理提供 HTTPS，并限制对 `8080` 后端端口的直接访问
- 如反向代理部署在同一台主机，可将 Compose 的端口映射由 `8080:8080` 改成 `127.0.0.1:8080:8080`；使用 `docker run` 时则改为 `-p 127.0.0.1:8080:8080`
- 请使用实际的 HTTPS 商城域名完成初始化与 HashPay 回调配置
- 使用附件交付时，请将反向代理请求体上限设为至少 101 MiB（预留 multipart 开销），并允许附件上传与下载持续最多 15 分钟；普通 API 保持原有超时限制

### 💾 数据持久化与备份

- Docker 数据卷 `hashdrop_data` 挂载到 `/data`，保存 SQLite 数据库、私有附件目录 `assets/`、站点配置、加密密钥、HashPay 私钥及备份文件；更新容器时务必保留该数据卷
- 服务运行期间约每 24 小时创建包含数据库和附件的完整备份，校验数据库及附件完整性；每次备份是 `/data/backups/` 下的独立目录，包含 `hashdrop.db`、`assets/` 和 `manifest.json`
- 如需手动创建完整备份，指定一个尚不存在的目录：

```bash
docker exec hashdrop /app/hashdrop \
  -config /data/config.json \
  -backup "/data/backups/manual-$(date +%Y%m%d-%H%M%S)"
```

恢复时需要一起恢复备份中的 `hashdrop.db` 与 `assets/`，并保留配套的 `config.json`（包含加密密钥）和 HashPay 私钥文件。备份清单记录数据库与加密附件的 SHA-256；备份不导出密钥。自动备份与原数据位于同一数据卷，应另外保存至其他存储位置，并自行管理历史备份数量。

数据库结构版本为 7，按全新安装维护，仅接受当前版本，不迁移旧开发数据库。验证新版时请使用新建的数据目录，并保留原有开发数据。

## 🧠 功能细节

### 🧾 匿名下单与领取

- 下单前校验商品、购买数量和可用库存，并计算优惠后的实际金额
- 买家自行设置领取密钥，无需注册账号；领取密钥不能重复用于不同订单
- 凭领取密钥查询订单，查看支付状态、继续支付或领取已交付的内容
- 支持一次购买多份商品，并分别记录每份商品的交付结果

### 💳 HashPay 支付与订单同步

- 通过 HashPay 商户 API 创建收款订单，买家前往 HashPay 收银台完成支付
- 接收加密支付回调，并定期补查未完成订单，避免仅依赖浏览器跳转判断支付结果
- 核对支付订单身份、金额和币种，确认付款后才触发自动交付
- 已核实的付款不会因后续网关状态回退而重新变成未付款或释放库存；手动刷新仍核验原网关身份与金额
- 支付订单终止时释放已预留的库存；异常订单可在后台重新核验或重试交付

### 📦 库存、资源与交付编排

- 商品描述卖什么，交付计划描述如何组合内容，库存池负责资源分配，订单记录实际交付结果
- 例如“说明文字 + 教程图片 + 安装包 + 激活码”：前三项可共享，激活码每购买一份从库存取一条；购买两份只消耗两条卡密
- 库存池绑定到具体取货项；同项多池按随机、先绑定或后绑定优先分配，共享池的可售量按全部取货需求共同计算
- 一次性库存按录入顺序使用，重复库存按持久化顺序循环；每份取货数量可独立设置
- 下单在一个事务中预留全部资源和优惠次数，并冻结组合内容及附件引用。后续修改商品、库存或绑定不改变已有订单
- 每份购买内容一起完成交付；已付款后的执行只依赖本地快照，支付网关暂时不可用不会阻塞待执行交付
- 失败项可重试或人工补交，重试不重新抽取库存。已完成结果禁止覆盖，相同补交请求可安全重试
- 附件上限为每个 100 MiB，使用私有加密存储；图片与文件经授权读取，尚未引用的上传会在保留期后清理，历史订单引用的附件继续保留
- 支持自动优惠和优惠码，可配置折扣方式、适用范围、门槛及使用限制

领取授权独立于内容编排和库存执行。当前使用领取密钥；OIDC 身份认证、阅后即焚等访问保护尚未实现。

### 🛠️ 后台与站点维护

- 首次启动通过网页初始化，后续使用单管理员账号管理商城
- 集中维护商品、库存、订单、优惠规则及 HashPay 配置
- 使用 SQLite 持久化数据，支持数据库备份与校验
- 可按需启用独立赞赏页面，通过 HashPay 收款；支持多币种金额、同昵称再次赞赏确认与历史归组，公开名单只展示主动选择公开且已核验付款的记录
- 赞赏创建采用固定请求标识，网络中断后的重试恢复同一笔记录，避免重复创建

## 🧱 技术栈

- 后端：Go、SQLite
- 前端：Vue 3、TypeScript
- 部署：Docker Compose，亦可使用 systemd 部署

## 🗂️ 项目结构

```text
HashDrop/
├── backend/              # 路由、业务、安全、支付与内置调度
│   └── web/              # 前端构建产物，嵌入二进制
├── frontend/
│   ├── src/pages/        # 商城、订单、登录与后台入口
│   ├── src/components/   # 管理页面组件
│   └── tests/            # 核心购买与管理流程浏览器验证
├── schema.sql            # SQLite 数据库初始化结构
├── .github/
│   ├── ISSUE_TEMPLATE/   # 中英双语缺陷反馈与功能建议模板
│   └── workflows/        # 推送 v* 标签时构建并发布 GHCR 多架构镜像
├── Dockerfile            # 前端和 Go 多阶段构建，运行镜像无 Node.js
├── compose.yaml          # VPS 直接拉取镜像部署
├── deploy/               # systemd 服务示例
├── images/               # README 界面截图
├── assets.go
└── go.mod
```

## 👨‍💻 本地开发

### 🧰 环境

- Go 1.27+
- Node.js 24+

### ⚙️ 命令

```sh
go test ./...
go vet ./...
npm ci --prefix frontend
npm --prefix frontend run check
npm --prefix frontend run build
cd frontend
npx playwright install chromium
npm run test:e2e
```

## 🔐 安全报告

如果发现安全问题，请不要公开披露细节。请优先参考仓库中的 [SECURITY.md](./SECURITY.md) 提交安全报告。

## 📄 许可证

本项目基于 [GPL-3.0](./LICENSE) 开源。

<div align="center">
  <sub>Built with ❤️ by Sunny</sub>
</div>
