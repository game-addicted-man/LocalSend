#!/bin/sh
# 맥·리눅스(또는 GitHub Actions)에서 Windows용 실행 파일과 설치 파일을 만듭니다.
#   dist/LocalDrop.exe        앱 (설치 없이 바로 실행)
#   dist/LocalDrop-Setup.exe  설치 프로그램
# Go 1.24 이상 필요. 라이브러리는 vendor 폴더에 있어서 인터넷 없이도 빌드돼요.
set -e
cd "$(dirname "$0")"

export GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-mod=vendor
mkdir -p dist

go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/LocalDrop.exe .
cp dist/LocalDrop.exe installer/payload/LocalDrop.exe
go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/LocalDrop-Setup.exe ./installer

echo "만듦: dist/LocalDrop.exe, dist/LocalDrop-Setup.exe"
