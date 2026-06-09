#!/bin/bash

message='世界杯'
if [ $# -gt 0 ]; then
	message=$1
fi

curl -v -XPOST 'http://localhost:8766/v1/chat/completions' \
-H 'Content-Type: application/json' \
-H 'Authorization: Bearer '"$API_KEY"'' \
-d '{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "'"$message"'"}
  ]
}' \
| python3 -m json.tool 2>/dev/null || echo ""
