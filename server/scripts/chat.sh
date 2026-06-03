#!/bin/bash

set -x

message='世界杯'
if [ $# -gt 0 ]; then
	message=$1
fi

curl -v -XPOST 'http://localhost:8766/api/command' \
-H 'content-type: application/json' \
-d '{"method":"chat", "params": {"message":"'"$message"'"}}'
