# VPS 部署与恢复

生产环境运行一个 Go 进程和一个 SQLite 数据库。GitHub Actions 构建 GHCR 镜像，VPS 通过 Docker Compose 拉取部署；支持 Linux amd64 和 arm64。Node.js 只参与构建。

## 镜像构建与发布

构建方式参考 [SeekMeow](https://github.com/sunnyhmz7010/SeekMeow/blob/main/.github/workflows/docker.yml)。`.github/workflows/docker.yml` 仅在推送 `v*` 版本标签时发布，推送 `main` 不发布。首次部署需等待另行确认发版、镜像构建成功且镜像公开；当前提交代码不会生成镜像。

镜像地址为 `ghcr.io/sunnyhmz7010/tinyfaka`，提供完整版本、主次版本、主版本及 `latest` 标签。Fork 时修改 Compose 中的镜像地址。

工作流沿用 SeekMeow 的检出、QEMU、Buildx、GHCR 登录、标签生成和构建发布步骤。Dockerfile 构建 Vue 前端、编译 Go，再打包运行镜像。工作流使用 GitHub 自带 Token 登录 GHCR。首次发布后，仓库所有者须在包的 Package settings 中设为 Public，才可匿名拉取。[GitHub Container Registry 说明](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)

## Docker Compose 部署

VPS 需要 Docker Engine 与 Docker Compose V2。将仓库根目录的 `compose.yaml` 保存到部署目录，执行：

```sh
docker compose pull
docker compose up -d
docker compose logs tinyfaka
```

打开 `http://服务器地址:8080`。首次启动自动进入 `/setup`，在网页中填写：

- 当前启动日志中的 `setup_token`，用于防止他人抢先初始化；重启会更换未使用的凭证。
- 站点名称、实际访问地址、公告和管理员密码（至少 12 字符）。
- 可选 TOTP 的 Base32 密钥及当前动态验证码；密钥应另外备份。
- HashPay 地址、商户 ID 和 PEM 格式 RSA 私钥，三项一起填写或全部留空稍后设置。

初始化成功后入口关闭，跳转管理员登录。无需配置环境变量、运行初始化命令或手工创建密钥文件。HTTP 会明文传输表单中的密码和私钥，应在可信网络中完成初始化。

默认映射 `8080:8080`；修改 YAML 左侧端口后，在初始化页面填写对应的实际访问地址。站点地址用于登录 Origin 校验与支付返回链接，必须与浏览器访问地址一致。Go 在容器内部监听 `0.0.0.0:8080`，Windows 本地开发仍只监听回环地址。

Docker 自动创建 `tinyfaka_config` 与 `tinyfaka_data` 命名卷，实际名称带 Compose 项目前缀。配置卷保存生成的密码哈希、加密密钥和受限商户私钥文件；数据卷保存 SQLite 与在线备份。容器使用 UID/GID `10001:10001`。不要执行 `docker compose down -v`，它会删除持久化卷。

## HashPay 接入

初始化时未配置支付的，可登录 `/admin/settings` 填写 HashPay 地址、商户 ID 和私钥。私钥保存到配置卷，不通过 API 回显，也不放入 YAML 或镜像。后台留空私钥表示保留已有私钥；填写有效 PEM 表示配置或替换。存在历史未关闭订单时，后台禁止更换支付网关或商户映射。

HashPay 只接受 HTTPS 回调地址。HTTP 站点无需填写回调，内置调度会签名查询待付款订单，核实付款后交付；待付款订单通常约 30 秒补查一次。若已有 HTTPS 站点地址，可设置回调 `站点地址/api/webhooks/hashpay`；回调仍必须二次查询。VPS 与 HashPay 的系统时钟应保持准确。

接入使用 HashPay 原生商户 REST API，代码根据 [TGDash/HashPay](https://github.com/TGDash/HashPay) 提交 `7963dd9f3d1e6a7b9aa4492b623f18c643a9423d` 实现。上线前通过小额真实订单验证签名、金额、币种、付款证据与领取；本地测试不能替代真实支付验证。

## 更新与备份

更新前做一致快照，文件名每次必须不同：

```sh
docker compose exec tinyfaka /usr/local/bin/tinyfaka \
  -config /etc/tinyfaka/config.json \
  -backup /var/lib/tinyfaka/backups/before-update-20261014.db
docker compose cp tinyfaka:/var/lib/tinyfaka/backups/before-update-20261014.db ./before-update-20261014.db
docker compose pull
docker compose up -d
docker compose ps
```

`pull` 与 `up` 保留命名卷。固定版本时将 YAML 的镜像换成完整版本标签或摘要。不要用旧镜像直接启动已迁移到新版本的数据库。

服务每 24 小时执行 SQLite `VACUUM INTO`，校验完整性与外键。频繁重启会重置计时，应额外执行在线备份。备份失败会记录结构化日志；旧备份由运维按保留策略管理。

配置和私钥须另外导出并受限保存，例如：

```sh
mkdir -m 700 ./credentials-backup
docker compose cp tinyfaka:/etc/tinyfaka/. ./credentials-backup/
chmod -R go-rwx ./credentials-backup
```

数据库与凭据备份分开保存到受限的异地位置。没有原加密密钥无法解密库存；丢失配置但仍有数据库时，服务会拒绝重新初始化，避免生成错误密钥。

## 恢复验证

先 `docker compose stop tinyfaka`，保留当前数据库及其 WAL/SHM。把一致快照和对应凭据复制到独立验证目录，调整配置副本中的数据库、备份路径、回环监听与访问地址，并清空 HashPay 配置，避免触发真实支付或外部交付。

用当前二进制启动验证副本，从后台核对商品、库存数量及历史交付解密结果。验证成功后，在停机状态恢复数据卷内的数据库和配置卷内的凭据；原 WAL/SHM 移到保留目录，不能与恢复数据库混用。设置属主 `10001:10001` 与文件权限 `0600`，再启动服务。运行中不得覆盖数据库。API 订单的外部副作用不会随数据库回滚，需要核对幂等键与外部状态。

## 二进制部署

构建机需要 Go 1.27+、Node.js 24+ 和 npm：

```sh
npm --prefix frontend ci
npm --prefix frontend run build
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/tinyfaka-linux-amd64 ./backend
```

前端构建到 `backend/web/` 并嵌入二进制。VPS 上以 root 创建受限目录并安装服务：

```sh
useradd --system --home /var/lib/tinyfaka --shell /usr/sbin/nologin tinyfaka
install -d -m 700 -o tinyfaka -g tinyfaka /var/lib/tinyfaka /etc/tinyfaka
install -m 755 bin/tinyfaka-linux-amd64 /usr/local/bin/tinyfaka
install -m 644 deploy/tinyfaka.service /etc/systemd/system/tinyfaka.service
systemctl daemon-reload
systemctl enable --now tinyfaka
journalctl -u tinyfaka
```

浏览器访问服务器的 8080 端口，从日志复制初始化凭证并在网页填写；流程与 Docker 相同。

## 订单异常与密钥维护

- 网关创建超时：保留库存，复用同一 `merchantNo` 重试，不创建新的本地订单。
- 查询失败或 `expired`：继续保留库存；HashPay 允许过期后人工确认付款，不能按本地时钟释放。
- `invalid`：管理员明确点击关闭后再次签名查询，匹配身份、金额及币种才释放。原生商户 API 无取消接口。
- 人工确认付款：默认 `hold`，店主核实后可改为 `accept` 并重新核实。
- 已付款交付失败：恢复原交付任务，完成的份数不重复分配。API 结果不明且不能安全重放时转人工处理。

商户 RSA 私钥可在后台系统设置替换，须与 HashPay 同步。库存加密密钥轮换需在受限配置中添加新版本并调整 `current_key`；已有版本不可删除。独立的 `dedup_key` 用于去重，不随加密密钥轮换替换。
