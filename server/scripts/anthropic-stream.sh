#!/bin/bash

message='世界杯'
if [ $# -gt 0 ]; then
	message=$1
fi

# 流式输出：逐行打印 SSE 事件，过滤掉 event: 行只打印 data:
curl -s -XPOST 'http://localhost:8766/v1/messages' \
-H 'Content-Type: application/json' \
-H 'x-api-key: '"$API_KEY"'' \
-d '{
  "model": "claude-3-5-sonnet-20241022",
  "max_tokens": 1024,
  "stream": true,
  "messages": [
    {"role": "user", "content": "'"$message"'"}
  ]
}' \
| while IFS= read -r line; do
	if [[ "$line" == data:* ]]; then
		# 只打印 data: 后的 JSON 部分
		echo "${line#data: }" | python3 -m json.tool 2>/dev/null || echo "${line#data: }"
		echo "---"
	fi
  done
