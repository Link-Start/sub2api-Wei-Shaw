#!/usr/bin/env bash
# 构建 demo-external 插件包：交叉编译 manifest 声明的全部平台二进制，
# 连同 manifest.json 与 webapp/ 打成可直接上传的 zip（dist/ 下）。
#
# 用法: ./build.sh
# 依赖: go、zip
set -euo pipefail

cd "$(dirname "$0")"

VERSION="$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' manifest.json | head -n1)"
OUT="dist/demo-external-${VERSION}.zip"

rm -rf bin dist
mkdir -p bin dist

# 平台清单必须与 manifest.json 的 backend.executables 保持一致。
platforms=(
  "linux amd64"
  "linux arm64"
  "darwin arm64"
)
for platform in "${platforms[@]}"; do
  read -r goos goarch <<<"${platform}"
  echo "building ${goos}-${goarch}..."
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
    go build -trimpath -ldflags="-s -w" -o "bin/plugin-${goos}-${goarch}" .
done

zip -r "${OUT}" manifest.json webapp bin >/dev/null
echo "packaged ${OUT}"
