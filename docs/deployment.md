# VPS 部署与恢复

生产环境运行一个 Go 进程；Node.js 只用于镜像构建，不参与运行。推荐 GitHub Actions 构建 GHCR 镜像、VPS 用 Docker Compose 拉取部署；支持 Linux amd64 和 arm64。HTTPS 由宿主机上的 Caddy 或 OpenResty 提供，配置示例在 `deploy/` 中。后文同时保留直接运行二进制的 systemd 方式。

## GitHub 镜像构建

镜像构建方式参考 [SeekMeow](https://github.com/sunnyhmz7010/SeekMeow/blob/main/.github/workflows/docker.yml)。仓库的 `.github/workflows/docker.yml` 支持：

- 推送 `v*` 语义化版本标签：发布完整版本、主次版本、主版本、提交 SHA 与稳定版本的 `latest`。预发布标签不覆盖 `latest`。
- 地址由实际仓库名自动生成并转成小写，例如 `sunnyhmz7010/TinyFaka` 对应 `ghcr.io/sunnyhmz7010/tinyfaka`。Fork 部署需同步修改 Compose 的镜像地址。

构建使用 Node.js 编译前端，再将静态资源嵌入 Go 二进制；运行镜像只含 Alpine、CA 证书和 Go 应用。构建时运行 Go 测试、`go vet` 和前端类型检查；GitHub 先加载 amd64 镜像检查初始化、健康检查、在线备份和重启，再发布 amd64/arm64 多架构镜像。

工作流使用自带 `GITHUB_TOKEN` 登录 GHCR，权限为 `contents: read` 与 `packages: write`，不需要另加个人 Token。仓库需要启用 Actions。首次发布的 GHCR 包默认是私有：仓库所有者需在包的 Package settings 中设为 Public，公开后 VPS 才能匿名拉取；若保留私有，则部署前需 `docker login ghcr.io`。[GitHub Container Registry 说明](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)

工作流只在推送 `v*` 版本标签时发布镜像，推送 `main` 分支不会触发镜像发布。首次部署前需要另行确认发版、推送版本标签，并等待工作流成功完成且公开镜像地址能够拉取；创建仓库和推送代码本身不会发布镜像。

## Docker Compose 部署（推荐）

需要 Linux VPS、Docker Engine 与 Docker Compose V2。默认使用 `network_mode: host`，让容器中的 Go 继续只监听宿主 `127.0.0.1:8080`，无需改变 Go 的监听约束；host 模式不配置端口映射。HTTPS 反向代理运行在宿主机，或自行配置为同一 host 网络。此 YAML 面向 Linux Docker Engine，不作为 Windows Docker Desktop 的默认部署方式。[Docker host 网络说明](https://docs.docker.com/engine/network/drivers/host/)

只需保存仓库根目录的 `compose.yaml`，不需要克隆源码、安装 Go/Node.js 或在 VPS 构建。新建部署目录后，以 root 执行下列命令；目录 UID/GID 与运行镜像的非 root 用户 `10001:10001` 一致。

```sh
mkdir -p /opt/tinyfaka
cd /opt/tinyfaka
# 将仓库提供的 compose.yaml 保存到当前目录。
install -d -m 700 -o 10001 -g 10001 config data secrets
docker compose pull
docker compose run --rm --no-deps tinyfaka -init \
  -config /etc/tinyfaka/config.json \
  -data-dir /var/lib/tinyfaka \
  -site-url https://shop.example.com
```

初始化交互读取管理员密码，并生成 `config/config.json` 中的 Argon2id 哈希、库存加密密钥和去重密钥；文件权限为 `0600`。已存在的配置拒绝覆盖。不要直接复制含占位符的 `config.example.json`。

将 HashPay 创建商户时显示的 RSA 私钥保存到 `secrets/hashpay.pem`，然后执行：

```sh
chown 10001:10001 secrets/hashpay.pem
chmod 600 secrets/hashpay.pem
```

修改 `config/config.json` 的三个字段，其他初始化生成的字段保留：

```json
{
  "hashpay": {
    "base_url": "https://pay.example.com",
    "merchant_id": "你的商户 ID",
    "private_key_file": "/run/secrets/hashpay.pem",
    "manual_policy": "hold"
  }
}
```

上面是字段片段，不是完整配置。`public_url` 应为真实 HTTPS 域名；回调地址是 `https://shop.example.com/api/webhooks/hashpay`。配置文件和密钥不放在 YAML、环境变量或镜像中。`config` 目录以可写方式挂载，后台保存站点设置需要原子替换文件；`secrets` 目录只读挂载；`data` 保存 SQLite 与在线备份。

宿主 HTTPS 代理配置完成后启动：

```sh
docker compose up -d
docker compose ps
docker compose logs -f tinyfaka
```

Docker 健康检查通过同一二进制的 `-healthcheck` 查询回环 `/api/site`，不启动第二个数据库进程或调度器。每 30 秒检查一次，HTTP 超时 3 秒。根文件系统只读；临时文件写入 `/tmp`，配置与数据库写入显式挂载目录。默认日志最多保留三个 10 MB 文件。

## Docker 更新、备份与恢复

更新前先执行在线备份并分开保存配置与密钥，再拉取镜像：

```sh
docker compose exec tinyfaka /usr/local/bin/tinyfaka \
  -config /etc/tinyfaka/config.json \
  -backup /var/lib/tinyfaka/backups/before-update.db
docker compose pull
docker compose up -d
docker compose ps
```

备份文件名每次必须不同。`pull` 与 `up` 不会清除 `config`、`data` 或 `secrets`。固定版本部署时，将 Compose 的 `image` 换成完整版本标签，如 `:0.1.0`（发布后才可用），或 `@sha256:实际摘要`。不要盲目用旧镜像启动已迁移到新版本的数据库。

恢复前 `docker compose stop tinyfaka`，保留现有 `data/tinyfaka.db` 及其 WAL/SHM，再将经校验的备份恢复到独立验证目录并匹配原加密密钥。验证成功后替换正式库，旧 WAL/SHM 移到证据目录，重新设置属主 `10001:10001` 和权限 `0600`，最后 `docker compose up -d`。运行中不得直接覆盖数据库。

## 二进制部署

不使用 Docker 时按下面步骤在构建机编译，再用 systemd 运行。

## 构建

构建机需要 Go 1.27+、Node.js 24+ 和 npm。依赖版本由 `go.sum` 与 `frontend/package-lock.json` 固定。

```sh
npm --prefix frontend ci
npm --prefix frontend run build
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/tinyfaka-linux-amd64 ./backend
```

前端产物放在 `backend/web/`，由根目录 `assets.go` 嵌入。直接跳过前端构建时页面返回 503，不会静默运行残缺前端。`go test` 可以独立执行；Vite 每次构建会清理旧产物，避免二进制积累过期资源。

## 安装服务

将二进制复制到 VPS；以下命令在 VPS 上以 root 执行。替换域名后再初始化。

```sh
useradd --system --home /var/lib/tinyfaka --shell /usr/sbin/nologin tinyfaka
install -d -m 700 -o tinyfaka -g tinyfaka /var/lib/tinyfaka /etc/tinyfaka
install -m 755 bin/tinyfaka-linux-amd64 /usr/local/bin/tinyfaka
sudo -u tinyfaka /usr/local/bin/tinyfaka -init -config /etc/tinyfaka/config.json -data-dir /var/lib/tinyfaka -site-url https://shop.example.com
```

初始化会交互读取密码，不把密码放进命令行、环境变量或进程列表。至少 12 字符。非终端场景可从受限文件或标准输入传入，不要将真实密码保存在脚本中。

将 HashPay 创建商户时显示的 PKCS#8 RSA 私钥保存到 `/etc/tinyfaka/merchant.pem`，文件属主设为 `tinyfaka`，权限 `0600`。修改生成的 `/etc/tinyfaka/config.json` 中 `hashpay.base_url`、`merchant_id` 与 `private_key_file`。后台可修改站点名称、公告、网关地址和人工付款策略；私钥路径、密码、TOTP 与加密密钥只从受限文件维护。存在历史未关闭订单时，后台禁止更换网关或商户。

可选 TOTP：在配置 `totp_secret` 中填写验证器使用的 Base32 密钥，然后重启；请单独保管该密钥。生产模式 Cookie 强制 `Secure`，不能通过明文 HTTP 登录。

```sh
chmod 600 /etc/tinyfaka/config.json /etc/tinyfaka/merchant.pem
chown tinyfaka:tinyfaka /etc/tinyfaka/config.json /etc/tinyfaka/merchant.pem
install -m 644 deploy/tinyfaka.service /etc/systemd/system/tinyfaka.service
systemctl daemon-reload
systemctl enable --now tinyfaka
systemctl status tinyfaka
```

将 `deploy/Caddyfile` 中域名改为真实域名，合入已有 Caddy 配置并通过其配置校验后加载。Go 只监听 `127.0.0.1:8080`。反向代理必须覆盖 `X-Real-IP`，不能信任客户端自己提供的该头。若更改域名，`public_url` 必须同步修改并重启；它用于登录与 CSRF 的 Origin 校验。

HashPay 商户后台回调地址：`https://shop.example.com/api/webhooks/hashpay`。请求签名需要时钟准确，VPS 与 HashPay 均应启用时间同步。代码按 TGDash/HashPay 提交 `7963dd9f3d1e6a7b9aa4492b623f18c643a9423d` 的原生 API 实现，不接入易支付接口。

## 订单异常与库存释放

- 网关创建超时：订单仍为 `creating`，保留库存并复用相同 `merchantNo` 重试，不重新创建本地订单。
- 网关查询失败：继续保留库存，调度器稍后查询。
- `expired`：转 `review`；HashPay 允许之后人工确认为已付款，因此不自动释放。
- `invalid`：仍保留库存，管理员点击“关闭并释放库存”时再次签名查询并核对身份、金额与币种。只接受查询得到的 `invalid`。当前原生商户 API 没有取消接口；若部署的 HashPay 无法让订单进入该状态，则无法通过本系统释放，不能用本地计时器替代可靠终态。
- 人工确认付款：默认 `hold`。店主核实 HashPay 的人工确认流程后，可选择 `accept` 并重新核实订单；更改会影响待处理的人工付款订单。
- 已核实付款、交付失败：库存型和固定文本可重试，已完成结果不会再次分配库存。API 请求结果不明且不能安全重放时转人工；确认外部结果后填写交付内容，不能盲目重试。

“补发”恢复原交付任务，不为已交付订单额外消耗一份库存。管理员可以查看现有交付内容，但系统没有匿名凭证自动找回接口。

## 在线备份与恢复验证

运行中的进程每 24 小时执行 `VACUUM INTO` 一致快照，并校验 `integrity_check` 与外键。进程启动后计时；频繁重启时应额外安排下面的 CLI 备份。备份失败会记录结构化日志，运维需监控失败。备份目录不自动删除旧文件，由运维根据保留策略管理。

```sh
sudo -u tinyfaka /usr/local/bin/tinyfaka -config /etc/tinyfaka/config.json -backup /var/lib/tinyfaka/backups/manual-20261013.db
```

备份目标必须不存在。不要复制运行中的 `.db` 而遗漏 WAL。数据库备份与密钥备份必须分别保存到受限的异地位置；没有原密钥，即使数据库完好也无法解密库存。`dedup_key` 独立于加密密钥，用于重复库存识别，不能随加密密钥轮换而替换。

恢复前先停服务并保留当前数据库及其 `-wal`、`-shm` 作为证据。把备份恢复到独立验证目录，同时放入匹配密钥的配置副本，配置 `development=true`、回环 `public_url` 和独立回环端口，禁用 HashPay 配置，运行并从后台核对商品、库存数量和历史交付解密结果。验证完成后才替换生产数据库；原生产 WAL/SHM 必须移到保留目录，不能与恢复数据库混用。恢复后的生产库要设置属主和 `0600` 权限，再启动服务。API 订单外部副作用不会随数据库回滚，需要逐笔核对幂等键和外部状态。

## 密钥轮换

添加新版本到 `encryption_keys`，再修改 `current_key` 并重启。新记录使用新版本，旧记录继续依原版本解密；已有密钥不能删除。首版不自动重加密历史记录。商户 RSA 密钥轮换需与 HashPay 同步替换私钥文件并重启；轮换期间解不开的历史回调由主动补查兜底。

## 上线验收

本地测试网关不能证明真实 HashPay 或 VPS 部署已生效。上线前用小额真实订单验证签名、币种、付款证据 `payment.tx.confirmedBy`、回调与主动补查、领取链接返回，以及延迟到账和人工确认策略。测试期间不使用真实库存做并发压力测试。
