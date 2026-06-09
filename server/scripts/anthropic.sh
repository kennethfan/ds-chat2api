#!/bin/bash

message='世界杯'
if [ $# -gt 0 ]; then
	message=$1
fi

curl -v -XPOST 'http://localhost:8766/v1/messages' \
-H 'Content-Type: application/json' \
-H 'x-api-key: '"$API_KEY"'' \
-d '{
  "model": "claude-3-5-sonnet-20241022",
  "max_tokens": 1024,
  "messages": [
    {"role": "user", "content": "'"$message"'"}
  ]
}' \
| python3 -m json.tool 2>/dev/null || echo ""
