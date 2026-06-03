# 通过接口操作deepseek聊天界面

# 目标
* 通过接口操作聊天界面，并拦截接口返回，转换成api格式
* 接口格式支持openai, anthropic

# 项目说明

## extension
chrome浏览器插件
### 功能
* background.js 负责通信，主要管理websocket声明周期和tab页管理
* content.js 页面操作管理
* interceptor.js 请求拦截，工具文件
* popup.xxx 辅助页面

## server
api server

### 功能
* ws.go 通信相关
* server.go http server
