#!/usr/bin/env bash
set -ex

# 获取当前脚本所在目录
script_dir=$(cd $(dirname "${BASH_SOURCE[0]}") && pwd)

# 查找 Git 根目录
git_root=$(git rev-parse --show-toplevel 2>/dev/null)

# 如果 Git 根目录不存在，认为当前脚本所在目录为项目根目录
if [ -z "$git_root" ]; then
    PROJECT_ROOT="$script_dir"
else
    PROJECT_ROOT="$git_root"
fi

cd "${PROJECT_ROOT}"/

rm -rf build/amd/csiplugin-connector.go build/amd/csiplugin-connector-svc build/amd/csiplugin-connector

cp build/lib/csiplugin-connector.go build/amd/csiplugin-connector.go
cp build/lib/csiplugin-connector.service build/amd/csiplugin-connector.service
cp build/lib/amd64-nsenter build/amd/nsenter
cp build/lib/freezefs.sh build/amd/freezefs.sh
cp build/lib/amd64-entrypoint.sh build/amd/amd64-entrypoint.sh

export GOARCH="amd64"
export GOOS="linux"

VERSION="v1.22.14-hack"
GIT_HASH=`git rev-parse --short HEAD || echo "HEAD"`
GIT_BRANCH=`git symbolic-ref --short -q HEAD`
BUILD_TIME=`date +%FT%T%z`

CGO_ENABLED=0 go build -ldflags "-X main.VERSION=${VERSION} -X main.BRANCH=${GIT_BRANCH} -X main.REVISION=${GIT_HASH} -X main.BUILDTIME=${BUILD_TIME}" -o plugin.csi.alibabacloud.com

cd "${PROJECT_ROOT}"/build/amd/
CGO_ENABLED=0 go build csiplugin-connector.go

if [ "$1" == "" ]; then
  mv "${PROJECT_ROOT}"/plugin.csi.alibabacloud.com ./
  docker build --platform=amd64 -t=wujunyi792/oss-csi-lite-plugin:amd64-$VERSION-"$GIT_HASH" ./
  find . -type f ! -name 'Dockerfile' -delete -o -type d -empty -delete
  docker push wujunyi792/oss-csi-lite-plugin:amd64-$VERSION-"$GIT_HASH"
fi
