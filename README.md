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

为单台 VPS 提供维护负担较小的数字商品商城。生产环境只有 Go 单体服务与 SQLite，收款和到账识别独立交给 HashPay。匿名买家无需账号，凭下单时设置的领取密钥查询订单和领取商品。（改）

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

- 支持卡密、账号和固定文本三种交付类型
- 商品与库存池分开管理，多个商品可共享库存
- 支持自动优惠活动、优惠码和多种优惠规则
- 支持库存管理、订单查询和交付异常重试

### 🛠️ 站点管理

- 通过网页完成首次初始化，单管理员后台管理
- 管理商品、库存、订单、优惠及站点和支付配置
- 可启用独立赞赏页面，通过 HashPay 接收赞赏
- 使用 SQLite 保存数据，提供在线备份与校验

## ⚡ 快速开始

### 📋 前置要求

- 一台安装了 Docker Engine 和 Docker Compose V2 的 Linux VPS
- 支付网关：HashPay

### 📦 Docker Compose（推荐）

新建 `compose.yaml`，写入以下内容：

```yaml
services:
  hashdrop:
    image: ghcr.io/sunnyhmz7010/hashdrop:1.0.0
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

### 🖥️ 命令行方式

```bash
docker run -d \
  --name hashdrop \
  --restart unless-stopped \
  -p 8080:8080 \
  -v hashdrop_data:/data \
  ghcr.io/sunnyhmz7010/hashdrop:1.0.0
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

启动后访问 `http://localhost:8080`。

## 📖 使用说明

### 👤 买家购买与领取

1. 在商城选择商品，确认数量及金额。
2. 设置并妥善保存领取密钥，然后前往 HashPay 支付。
3. 支付完成后打开商城的「查询 / 领取」页面，输入领取密钥查看订单并领取商品。

领取密钥是查询和领取订单的凭证，**丢失后无法找回**，建议使用页面自动生成的密钥。

### 🛠️ 商家管理

进入 `/admin` 登录管理后台：

1. **库存管理**：创建库存池，选择卡密、账号或固定文本，并添加交付内容。
2. **商品管理**：创建商品、关联库存池、设置价格和详情后上架。
3. **优惠管理**：按需要设置自动活动或优惠码。
4. **订单管理**：查看付款与交付状态，必要时刷新状态或重试交付。
5. **系统设置**：维护站点信息及 HashPay 配置；还可按需开启赞赏页。

商城入口为 `/`，订单查询入口为 `/claim`，赞赏页（启用后）为 `/donate`。

## 🧠 功能细节



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
├── schema.sql            # v1.0.0 完整数据库初始化结构
├── .github/
│   ├── ISSUE_TEMPLATE/   # 中英双语缺陷反馈与功能建议模板
│   └── workflows/        # 推送 v* 标签时构建并发布 GHCR 多架构镜像
├── Dockerfile            # 前端和 Go 多阶段构建，运行镜像无 Node.js
├── compose.yaml          # VPS 直接拉取镜像部署
├── deploy/               # systemd 服务示例
├── docs/images/          # README 界面截图
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
