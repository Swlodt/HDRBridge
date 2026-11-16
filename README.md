# HDRBridge

**HDRBridge 的核心功能是将 HDR JPEG XR（`.jxr`）转换为带 ISO gain map 的 HDR HEIC。** 在 macOS 上运行服务后，把 JXR 拖入网页相册，上传完成即可下载转换后的 `.heic` 文件。支持 ISO gain map 的查看器可以还原 HDR，网页相册使用 SDR 缩略图预览。

- 图库按转换产物标注「HDR HEIC」或「SDR HEIC」。无法识别产物的增益图标记时显示「HEIC」。
- 相册也支持上传、预览和下载 JPEG、PNG、GIF、WebP、AVIF、BMP、ICO、SVG、TIFF、HEIC / HEIF 和 JPEG XL，这些格式保留原文件供下载。

## 安装工具链

已在 Apple Silicon 的 macOS 27.3 验证。构建需要 Go 1.27（见 [`go.mod`](go.mod)）、Node.js 22.12+、npm 和 `make`。JXR 转换工具由使用者安装。普通位图的缩略图使用 macOS 自带的 `/usr/bin/sips`。

1. 用 [Homebrew](https://brew.sh/) 安装解码器和 ImageMagick。ImageMagick 需要启用 HDRI 并支持读写 TIFF、EXR 和 JPEG。

   ```sh
   brew install imagemagick-full
   brew install jxrlib
   "$(brew --prefix imagemagick-full)/bin/magick" -version
   ```

   Homebrew 已弃用 [jxrlib 公式](https://formulae.brew.sh/formula/jxrlib)，预计在 2027-02-23 停用。无法通过 Homebrew 安装时，可按下文从源码构建 `JxrDecApp`。

2. 从 [toGainMapHDR 3.3.1](https://github.com/chemharuka/toGainMapHDR/releases/tag/3.3.1) 安装编码器及同一版本的两个 Metal 文件。在仓库根目录执行以下命令。

   ```sh
   (
     set -e
     mkdir -p tools/toGainMapHDR
     release_url="https://github.com/chemharuka/toGainMapHDR/releases/download/3.3.1"
     for asset in toGainMapHDR GainMapKernel.ci.metallib RGBGainMapKernel.ci.metallib; do
       curl -fLsS "$release_url/$asset" -o "tools/toGainMapHDR/$asset"
     done
     chmod u+x tools/toGainMapHDR/toGainMapHDR
   )
   ```

   三个文件放在同一目录。`tools.encoder` 指向可执行文件。用浏览器下载时，macOS 可能弹出 [Gatekeeper 提示](https://support.apple.com/102445)。

### 从源码安装 JxrDecApp

如果 Homebrew 无法安装 jxrlib，在仓库根目录执行以下命令。构建需要 Xcode Command Line Tools（可用 `xcode-select --install` 安装）。

```sh
(
  set -e
  jxr_src="$(mktemp -d "${TMPDIR:-/tmp}/hdrbridge-jxrlib.XXXXXX")"
  trap 'rm -rf "$jxr_src"' EXIT
  curl -fLsS 'https://deb.debian.org/debian/pool/main/j/jxrlib/jxrlib_1.2~git20170615.f752187.orig.tar.xz' \
    -o "$jxr_src/source.tar.xz"
  tar -xf "$jxr_src/source.tar.xz" -C "$jxr_src"
  make -C "$jxr_src/jxrlib-1.2~git20170615.f752187" DIR_BUILD="$jxr_src/build" \
    'CC=cc -include wchar.h -Wno-error=implicit-function-declaration'
  mkdir -p tools/jxrlib
  install -m 755 "$jxr_src/build/JxrDecApp" tools/jxrlib/JxrDecApp
)
```

使用此方式时，将 `config.yaml` 中的 `tools.jxr_dec` 设为 `./tools/jxrlib/JxrDecApp`。

## 配置和运行

首次运行时，从仓库根目录创建本地配置。

```sh
cp -n config.example.yaml config.yaml
```

[示例配置](config.example.yaml) 对应 Apple Silicon 上 Homebrew 的默认安装位置和上述编码器目录。使用其他安装位置时，修改 `config.yaml` 中对应的工具路径。

```yaml
tools:
  jxr_dec: /opt/homebrew/opt/jxrlib/bin/JxrDecApp
  magick: /opt/homebrew/opt/imagemagick-full/bin/magick
  encoder: ./tools/toGainMapHDR/toGainMapHDR
```

Homebrew 工具的实际安装前缀可用 `brew --prefix jxrlib` 和 `brew --prefix imagemagick-full` 查看。程序从当前目录读取 `config.yaml`。数据目录和包含斜杠的相对工具路径以配置文件所在目录为基准。单独填写命令名时，程序会在 `PATH` 中查找。

```sh
make build
./bin/hdrbridge
```

打开 [http://127.0.0.1:8080](http://127.0.0.1:8080)，上传 HDR JXR。上传完成后打开图片详情，点击「下载 HDR HEIC」。可以用仓库里的 [HDR 测试图](internal/testutil/testdata/images/hdr.jxr) 验证转换。前端已嵌入 Go 二进制，运行时只需上述转换工具和本地配置。

`convert.timeout` 默认为 `30s`，控制单张图片的 JXR 转换或普通位图预览生成时限。HEIC 成功生成后，缩略图失败或超时仍可下载 HEIC。网页在同一标签页内逐张上传。默认数据目录为 `./runtime`，单文件限制为 256 MiB，完整请求限制为 1 GiB。服务面向本机使用，默认监听 `127.0.0.1`，没有用户认证。修改配置后重启生效。

## 数据备份与恢复

`storage.data_dir` 保存图库数据。相册和图片元数据在 `hdrbridge.db`，原图、转换产物和缩略图在 `images/`。每张 JXR 的原件保存在 `images/<图片ID>/source.jxr`。

备份时先结束上传，按 Ctrl+C 停止服务，等进程退出后复制整个数据目录。如需保留自定义配置，同时备份 `config.yaml`。恢复时把备份复制到新目录，将 `storage.data_dir` 指向该目录，再启动服务。恢复后可打开相册并下载一张已知图片，确认记录和文件都可用。

## JXR 转 HEIC 的处理流程

JxrDecApp 将 JXR 解码为 TIFF，ImageMagick 再将 TIFF 写为线性 RGB 的浮点 EXR，以保留 HDR 亮度。toGainMapHDR 从 EXR 生成 HEIC。手动复现时，在仓库根目录运行下列命令。将 `SOURCE` 改为 JXR 原件的绝对路径，并让三个工具路径与本地配置一致。结果会写入新建的临时目录。

```sh
SOURCE="/绝对路径/图片.jxr"
JXR_DEC="$(brew --prefix jxrlib)/bin/JxrDecApp"
MAGICK="$(brew --prefix imagemagick-full)/bin/magick"
ENCODER="$PWD/tools/toGainMapHDR/toGainMapHDR"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/hdrbridge.XXXXXX")"

# JXR → TIFF
"$JXR_DEC" -i "$SOURCE" -o "$WORK/intermediate.tif"

# TIFF → 16-bit 浮点 EXR
"$MAGICK" "$WORK/intermediate.tif" \
  -set colorspace RGB \
  -define quantum:format=floating-point \
  -depth 16 \
  "$WORK/intermediate.exr"

# EXR → HEIC（HDR 产物携带 ISO gain map）
"$ENCODER" "$WORK/intermediate.exr" "$WORK" -q 1 -d 10 -r 10 -R 14

# 生成 SDR 缩略图
"$MAGICK" "$WORK/intermediate.exr" \
  -set colorspace RGB \
  -resize '512x512>' \
  -channel RGB \
  -fx 'peak=max(0.75,max(u.r,max(u.g,u.b))); excess=peak-0.75; max(0,u)*(0.75+excess/(1+4*excess))/peak' \
  +channel \
  -colorspace sRGB \
  -quality 85 \
  "$WORK/thumbnail.jpg"

printf 'HEIC: %s\n缩略图: %s\n' "$WORK/intermediate.heic" "$WORK/thumbnail.jpg"
```

TIFF 的像素格式由解码器决定。编码器成功退出且生成的 HEIC 非空后，程序将其保存为 `output.heic`，并读取 `ftyp` 中的 `tmap` 品牌来确定图库标签。缩略图经 JPEG 解码校验后保存为 `thumbnail.jpg`。

## 开发与验证

先按上文安装工具链并创建 `config.yaml`。

```sh
make web                  # 安装依赖并构建前端
go run ./cmd/hdrbridge     # 后端, 在单独终端运行
make dev-web              # Vite 开发服务, 默认 http://127.0.0.1:5173
make test                 # 前端测试、Go race 测试和 vet
```

`web/dist` 由 `make web` 生成。修改前端后用 `make build` 更新嵌入了前端的 Go 二进制。

Vite 将 `/api` 代理到 `127.0.0.1:8080`, 后端端口改变时需调整 `web/vite.config.ts`。生产环境由 Go 同源提供页面和 API。SQL 变更后使用 sqlc v1.31.1 执行 `sqlc generate`。服务启动时通过 `00001_library.sql` 初始化新库。开发阶段修改表结构后, 将 `storage.data_dir` 指向新的目录以重新初始化。

常规测试使用可控转换实现。共享图片样本及生成命令见 [测试图片](internal/testutil/testdata/README.md)。安装工具链、配置 `config.yaml` 并执行 `make web` 后，可用真实的 HDR 和 SDR JXR 样本验证产物标记、缩略图和 HEIC 下载。测试会创建独立的临时图库。

```sh
HDRBRIDGE_REAL_TOOLS=1 go test ./internal/httpapi -run '^TestHTTPRealConversionWorkflow$' -v
```

## API

接口契约以 [OpenAPI 3.0 规范](docs/openapi.yaml) 为准, 可直接导入 Swagger Editor 或其他 OpenAPI 工具查看和调试。相册和全部图片列表采用游标分页, 返回当前页、搜索匹配数和图片总数。服务地址使用实际监听地址, 规范中的相对路径以 `/api` 为前缀。普通错误为 `{"error":"错误说明"}`。批量上传可能同时返回已保存图片和失败项, 客户端应保留结果并在连接中断后先查询, 避免重复上传。

## 架构与运行行为

- `internal/httpapi` 提供 Gin API 和嵌入式前端。`internal/library` 统一处理相册、图片上传与发布、删除事务、启动文件恢复和后台清理
- `internal/store` 管理 SQLite、初始化和 sqlc 查询。`internal/media` 按文件内容识别源格式, 并在配置的时限内运行 JXR 转换或普通位图预览工具。`internal/apperr` 分离公共错误提示和内部错误原因
- `internal/logging` 统一 slog 入口, 默认记录 INFO 及以上的单行文本日志。ERROR 及以上写入 stderr, 低于 ERROR 的日志写入 stdout。可通过 `SetLevel` 动态调整最低级别。HTTP 访问日志通过 slog 记录请求方法、路径、状态码、耗时和客户端 IP, panic 恢复日志包含堆栈。不影响业务返回值的资源关闭使用 `logging.CloseResource`, 关闭失败记录 WARN 日志
- 上传要求扩展名与识别出的格式匹配, 图片处理成功后才发布。数据库保存源 MIME 类型, 磁盘文件由图片 ID 定位
- 上传按源文件的 SHA-256 在整个资料库查重。内容相同的文件返回 409「图片已存在」
- 普通图片保存在 `images/<图片ID>/source`, 保存后立即可用。位图上传时使用 macOS 自带的 `sips` 生成最长边不超过 512 像素的 PNG 缩略图; TIFF、HEIC / HEIF 和 JPEG XL 的详情也使用该预览。浏览器支持的格式可在详情显示原图, 动图在详情中可播放
- 转换失败或请求取消时, 清理暂存文件。暂存目录无法立即删除时, 使用独立超时登记清理任务。启动时将孤立图片目录与残留上传目录交给后台清理
- 删除相册默认将图片移入内置 `uncategorized` 相册。选择同时删除图片时, 事务提交后由后台清理文件, 失败记录每分钟重试。启动时必须能访问存储根目录并登记残留目录

## 许可证

HDRBridge 源码采用 [MIT 许可证](LICENSE)。手动安装的 [jxrlib](https://formulae.brew.sh/formula/jxrlib)、[ImageMagick](https://imagemagick.org/script/license.php) 和 [toGainMapHDR](https://github.com/chemharuka/toGainMapHDR/blob/main/LICENSE) 各自沿用上游许可证。
