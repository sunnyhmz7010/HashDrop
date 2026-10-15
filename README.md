<div align="center">
  <h1>HashDrop</h1>
  <p>对接 HashPay 的轻量级匿名数字商品商城与自动交付系统</p>
</div>

<p align="center">
  <a href="https://github.com/sunnyhmz7010/HashDrop/releases"><img src="https://img.shields.io/github/v/release/sunnyhmz7010/HashDrop?label=Release&color=3b82f6" alt="Release" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/License-GPL--3.0-10b981" alt="License" /></a>
</p>

---

## ✨ 为什么做这个项目

HashDrop 面向希望在自己的 VPS 上销售数字商品的个人和小型站点。

不需要买家注册账号，也不需要自己开发支付系统。商家通过网页管理商品和库存，买家下单后前往 HashPay 支付，再凭领取密钥查询订单和领取商品。

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
- 使用 SQLite 保存数据，支持备份与恢复相关维护

## ⚡ 快速开始

### 📋 前置要求

- 一台安装了 Docker Engine 和 Docker Compose V2 的 Linux VPS
- 可用的 HashPay 商户信息及商户 RSA 私钥

生产服务器不需要额外安装 Go、Node.js、MySQL 或 Redis。

### 📦 Docker 部署

下载仓库提供的 Compose 配置并启动：

```bash
curl -fsSL https://raw.githubusercontent.com/sunnyhmz7010/HashDrop/main/compose.yaml -o compose.yaml
docker compose up -d
```

启动后访问：

```text
http://服务器IP:8080
```

按网页提示设置管理员账号、密码及站点信息。HashPay 商户配置可在初始化时填写，也可以稍后在后台补充。

> 正式对外使用前，请配置 HTTPS。首次初始化会提交管理员密码及支付私钥，不应通过不可信网络上的 HTTP 进行。

## 📖 使用说明

### 👤 买家购买与领取

1. 在商城选择商品，确认数量及金额。
2. 设置并妥善保存领取密钥，然后前往 HashPay 支付。
3. 支付完成后打开商城的「查询 / 领取」页面，输入领取密钥查看订单并领取商品。

领取密钥是查询和领取订单的凭证，**丢失后无法自动找回**，建议使用页面自动生成的密钥。

### 🛠️ 商家管理

进入 `/admin` 登录管理后台：

1. **库存管理**：创建库存池，选择卡密、账号或固定文本，并添加交付内容。
2. **商品管理**：创建商品、关联库存池、设置价格和详情后上架。
3. **优惠管理**：按需要设置自动活动或优惠码。
4. **订单管理**：查看付款与交付状态，必要时刷新状态或重试交付。
5. **系统设置**：维护站点信息及 HashPay 配置；还可按需开启赞赏页。

商城入口为 `/`，订单查询入口为 `/claim`，赞赏页（启用后）为 `/donate`。

## 🔄 更新与数据

在部署目录执行：

```bash
docker compose pull
docker compose up -d
```

默认 Compose 使用已发布的 `1.0.0` 镜像；如需升级到新版本，请先确认并调整 `compose.yaml` 中的镜像标签。

商品、订单、配置和密钥保存在 Docker 数据卷中。**请定期备份数据卷，不要使用 `docker compose down -v` 删除数据。**

如果从旧 TinyFaka 部署迁移，请先备份数据并复用原有数据卷；新建空数据卷不会自动继承旧数据。

## 🧱 技术栈

- 后端：Go、SQLite
- 前端：Vue 3、TypeScript
- 部署：Docker Compose，亦可使用 systemd 部署

## 🔐 安全报告

如果发现安全问题，请不要公开披露细节，参见 [SECURITY.md](./SECURITY.md)。

## 📄 许可证

本项目基于 [GPL-3.0](./LICENSE) 开源。

<div align="center">
  <sub>Built with ❤️ by Sunny</sub>
</div>
