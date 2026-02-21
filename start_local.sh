#!/bin/bash
cd e:/new_oas/oasbackend
set -a
source .env
set +a
export SERVE_FRONTEND=true
./server.exe > e:/oasbackend.log 2>&1 &
echo "Server started with PID: $!"
sleep 3
tail -20 e:/oasbackend.log
