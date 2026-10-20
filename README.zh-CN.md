# wingmanager

[English](README.md)

为管理 1–20 个单元的个人房东设计的开源 PMS，同时支持短租和长租。一个 Go
可执行文件，SQLite 数据库，内嵌中英文网页界面，无需外部数据库或前端构建。

“今天”页面回答：谁入住或退房、哪些租客欠租、今年赚了多少。v0.1 包含手动订单、
iCal 导入导出、租约和房租应收、收款和押金、现金制报表及 Schedule E CSV、任务和
附件。iCal 用于日历同步，不提供双向渠道管理。

## 下载或构建

从 [Releases](https://github.com/gaw008/wingmanager/releases) 下载对应平台的文件，
重命名为 `wingmanager`（Windows 为 `wingmanager.exe`）。发布平台为 Linux
amd64/arm64、macOS amd64/arm64、Windows amd64。在 macOS/Linux 上赋予执行权限：

```sh
chmod +x ./wingmanager
```

也可以在源码目录中，使用 Go 1.27.3 或更新版本构建：

```sh
go build -o wingmanager ./cmd/wingmanager
./wingmanager version
./wingmanager --help
```

本地构建显示 `dev`，发布构建显示注入的 `v*` 标签。以下示例使用 macOS/Linux
shell；Windows 用户可在终端中使用相同子命令及 `.exe` 文件名。

## 几分钟体验演示

在二进制所在目录操作，确保尚无 `./data` 实例：

```sh
./wingmanager seed-demo --data-dir ./data
./wingmanager serve --addr 127.0.0.1:8080 --data-dir ./data
```

保持服务运行，打开 <http://127.0.0.1:8080>，使用 `seed-demo` 打印的邮箱和随机密码
登录。“今天”页面显示入住、退房、逾期租客、保洁任务和 **625.00 USD** 的年内净收入。
两个房产及所有人员、订单、账目均为模拟数据，日期相对于实例当前日期生成。示例
日历源使用保留的 `.invalid` 地址，因此同步报错是预期行为，不会访问真实房东的
日历。在“设置”中可以修改语言、时区和币种。

已有账号的实例不能运行演示播种。播种通过已有领域操作逐步提交，失败时可能留下
部分演示数据；命令会报告错误，并打印已经创建的账号凭据。如需重试，请使用新的
数据目录。

自己的空实例可跳过播种，直接启动 `serve`，首次访问会进入房东账号设置页面。
密码至少 8 个字符、最多 72 个 UTF-8 字节。用 Ctrl-C 停止前台服务。

## 数据与命令选项

```sh
./wingmanager serve --help
```

默认值为 `--addr 127.0.0.1:8080`、`--data-dir ./data` 和
`--behind-https-proxy=false`。各数据命令支持 `--data-dir`；备份选项必须放在目标
目录前。数据目录包含 `wingmanager.db` 和上传的 `files/`，启动时自动迁移。
网页资源和时区数据已内嵌；外部 iCal 日历源需要网络连接。

## 备份和恢复

服务运行时，在另一个终端执行：

```sh
./wingmanager backup --data-dir ./data ./backup-v01
```

目标必须是 `data/files` 之外的新目录，已有文件或目录会被拒绝。备份包含
`VACUUM INTO` 生成的数据库快照和 `files/`；生成快照、复制文件期间，写操作等待。
将整个备份目录复制到备份存储即可。

恢复时先停止服务，保留旧目录，再将备份复制到原位置（以下两个目标名称均须未占用）：

```sh
mv ./data ./data-before-restore
cp -R ./backup-v01 ./data
./wingmanager serve --addr 127.0.0.1:8080 --data-dir ./data
```

恢复整个目录及附件，并确保服务用户有写权限。无需恢复子命令。

## 忘记密码

在服务器终端通过标准输入传入新密码。操作真实数据前，请替换示例密码：

```sh
printf '%s\n' 'a-new-example-password' | ./wingmanager reset-password --data-dir ./data --password-stdin
```

密码规则与首次设置相同，重置会撤销全部会话。使用原邮箱和新密码重新登录。
命令移除末尾的一个换行符，保留密码中的空格。

## Docker

在源码目录中，确保 Docker Engine 已启动：

```sh
docker build -t wingmanager:local .
docker run --rm -p 127.0.0.1:8080:8080 -v wingmanager-data:/data wingmanager:local
```

打开同一本地地址，完成房东账号设置。容器停止后命名卷仍保留数据。镜像采用多阶段
构建、关闭 CGO，以非 root 用户在 distroless static 中运行；默认
监听 `0.0.0.0:8080`，数据目录为 `/data`。使用主机目录挂载时，目录须允许
UID/GID 65532 写入。发布标签还会将同版本镜像推送到 `ghcr.io/gaw008/wingmanager`。

## 用 Caddy 部署 HTTPS

将域名 DNS 指向服务器，让 Caddy 接收 80/443 端口的请求。启动本地后端并开启
安全会话 Cookie：

```sh
./wingmanager serve --addr 127.0.0.1:8080 --data-dir ./data --behind-https-proxy
```

使用以下 Caddyfile，替换域名后通过 Caddy 服务加载：

```caddyfile
rentals.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy 管理 HTTPS 证书（[官方部署指南](https://caddyserver.com/docs/quick-starts/reverse-proxy)）。
通过 HTTPS 域名访问；即使 Caddy 与后端之间使用 HTTP，代理参数也会让会话 Cookie
始终带有 `Secure`。转发请求头不会改变 Cookie 安全设置。后端保持监听本机回环地址。

## 开发

安装固定版本 sqlc，修改领域查询后重新生成：

```sh
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
sqlc generate
```

必须通过的检查：

```sh
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
sqlc diff
go test -race ./...
```

测试使用临时真实 SQLite 数据库，仅在网络边界使用模拟。独立发布工作流在 `v*`
标签推送时构建五个平台二进制及 Linux amd64/arm64 GHCR 镜像，现有 CI 工作流保持
不变。参见 [PLAN](docs/PLAN.md) 和 [M8 决策](docs/decisions/0009-m8.md)。

## 许可证

AGPL-3.0，见 [LICENSE](LICENSE)。内嵌第三方网页资源的原许可证保留在
`internal/web/static/`。
