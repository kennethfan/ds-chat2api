#!/bin/bash

message='世界杯'
if [ $# -gt 0 ]; then
	message=$1
fi

# 流式输出：逐行打印 SSE data: 事件，过滤 [DONE] 标记
curl -s -XPOST 'http://localhost:8766/v1/chat/completions' \
-H 'Content-Type: application/json' \
-H 'Authorization: Bearer '"$API_KEY"'' \
-d '{
  "model": "gpt-4o",
  "stream": true,
  "messages": [
    {"role": "user", "content": "'"$message"'"}
  ]
}' \
| while IFS= read -r line; do
	if [[ "$line" == data:* ]]; then
		data="${line#data: }"
		if [[ "$data" == "[DONE]" ]]; then
			echo "=== [DONE] ==="
		else
			echo "$data" | python3 -m json.tool 2>/dev/null || echo "$data"
			echo "---"
		fi
	fi
  done
