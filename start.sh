#!/bin/bash
cd "$(dirname "$0")"

# 加载 .env 文件
if [ -f .env ]; then
  export $(cat .env | grep -v '^#' | xargs)
fi

# 启动后端
./star-meet-backend
