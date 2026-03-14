#!/bin/bash
# 设置临时目录到E盘以避免C盘满的问题
export GOTMPDIR=/e/gotmp
export GOCACHE=/e/gocache
mkdir -p /e/gotmp /e/gocache

cd /e/new_oas/oasbackend
source .env 2>/dev/null

# 编译
go build -o /e/new_oas/oasbackend/server_new.exe ./cmd/server
if [ $? -eq 0 ]; then
    echo "编译成功，启动服务..."
    pkill -f server.exe 2>/dev/null || true
    sleep 1
    nohup /e/new_oas/oasbackend/server_new.exe > /e/server.log 2>&1 &
    echo "服务已启动，PID=$!"
else
    echo "编译失败"
fi
