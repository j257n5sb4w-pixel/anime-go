#!/bin/sh
# 交叉编译脚本：产出静态二进制，不带 CGO、不带路径信息。
#
#   ./deploy/build.sh              默认 linux/arm64（OrangePi / 树莓派 3+ / 4 / 5）
#   GOARCH=arm GOARM=7 ./deploy/build.sh   ARMv7（跑 32 位系统的树莓派 2/3）
#   GOARCH=amd64 ./deploy/build.sh         x86_64 NAS / 服务器
set -eu

cd "$(dirname "$0")/.."

GOOS=${GOOS:-linux}
GOARCH=${GOARCH:-arm64}
OUT=${OUT:-anime-go}

CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" GOARM="${GOARM:-}" \
	go build -trimpath -ldflags "-s -w" -o "$OUT" .

echo "built $OUT for ${GOOS}/${GOARCH}${GOARM:+/v$GOARM}"
ls -l "$OUT"
