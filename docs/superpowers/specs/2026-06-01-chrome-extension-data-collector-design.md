# Chrome Extension: 数据采集工具 — 设计文档

## 概述

一个 Chrome 浏览器扩展（Manifest V3），用于在特定页面上执行自动化操作和数据采集。通过 WebSocket 接受外部程序的指令，在目标页面上执行 DOM 操作，拦截网络请求/响应，并将结果返回给外部程序。

## 架构

### 组件

| 组件 | 职责 |
|---|---|
| **Service Worker** (background.js) | 核心控制器。管理 WebSocket 连接、命令路由、Tab 生命周期（Leader 选举）、debugger API 管理、配置持久化 |
| **Content Script** (content.js) | 在目标页面中执行 DOM 操作（点击、填写表单、等待元素）。通过 `chrome.runtime.sendMessage` 与 SW 通信 |
| **Popup Page** (popup.html + popup.js) | 用户操作面板。提供端口输入、连接/断开控制、状态显示 |

### 数据流

```
外部程序 ──(WS 命令)──► Service Worker ──(runtime msg)──► Content Script ──(DOM操作)──► 页面
                ◄──(响应/SSE 流)── Service Worker ◄──(执行结果)── Content Script ◄──(页面事件)
```

### 通信拓扑

```
┌─────────────────────────────────────────────────────┐
│                  Chrome Extension                     │
│  ┌─────────────────┐    ┌─────────────────────────┐ │
│  │  Popup Page      │    │  Service Worker          │ │
│  │  (配置端口/状态)  │◄──►│  - WebSocket 客户端     │ │
│  └─────────────────┘    │  - 命令路由               │ │
│                          │  - Tab 管理器 (Leader)   │ │
│  ┌─────────────────┐    │  - debugger API 管理     │ │
│  │  Content Script  │◄──►│  - chrome.storage 持久化 │ │
│  │  (注入目标页面)   │    └─────────────────────────┘ │
│  └─────────────────┘                                  │
└─────────────────────────────────────────────────────┘
         │
         │ WebSocket (ws://localhost:PORT)
         ▼
┌─────────────────┐
│  外部采集程序     │
│  (Python/Node)   │
│  WebSocket 服务端 │
└─────────────────┘
```

## URL 匹配

扩展通过 `manifest.json` 的 `content_scripts` 配置定义生效范围，例如 `https://www.baidu.com/*`。仅在匹配的页面上注入 Content Script 和执行操作。

## Tab 管理与 Leader 选举

### Tab 状态

```typescript
interface TabState {
  tabId: number;
  url: string;
  status: 'standby' | 'active' | 'inactive';
  attached: boolean;    // debugger 是否 attach
  openedAt: number;     // 打开时间戳
}
```

### 规则

| 事件 | 行为 |
|---|---|
| 新 tab 打开（URL 匹配） | 无 active → 升级为 active，attach debugger，注入 content script；已有 active → 加入 standby |
| Active tab 关闭 | 从 standby 中选最早打开的升级为 active，重新 attach debugger |
| Active tab 导航到不匹配 URL | 降级为 inactive，重新选举 |
| Standby tab 关闭 | 从 standby 列表移除，不影响 active |
| 所有 tab 关闭 | 清空状态 |

### Debugger 约束

`chrome.debugger.attach()` 同时只能 attach 到一个 tab。只有 **active tab** 会 attach debugger。切换 leader 时：detach 旧 tab → attach 新 tab。

## WebSocket 通信协议

### 消息帧格式

所有消息为 JSON 格式：

```json
{
  "type": "request" | "response" | "sse" | "error",
  "id": "uuid",
  "method": "string",     // request 类型
  "params": {},           // request 类型
  "result": {},           // response 类型
  "data": {},             // sse 类型
  "error": {              // error 类型
    "code": "string",
    "message": "string"
  }
}
```

### 命令列表

| 方法 | 参数 | 返回 | 说明 |
|---|---|---|---|
| `ping` | `{}` | `{pong: true}` | 心跳检测 |
| `click` | `{selector, index?}` | `{success: true}` | 点击元素 |
| `fill` | `{selector, value, index?}` | `{success: true}` | 填写表单 |
| `waitElement` | `{selector, timeout?}` | `{found: true}` | 等待元素出现 |
| `waitTimeout` | `{ms}` | `{}` | 等待指定毫秒 |
| `startCapture` | `{}` | → 返回 `{captureId}`，开始 SSE 推送 | 开始网络抓取 |
| `stopCapture` | `{captureId}` | `{stopped, count}` | 停止网络抓取 |
| `getStatus` | `{}` | `{activeTab, tabCount, capturing}` | 获取当前状态 |

### SSE 事件类型（Network 域）

| SSE event | 对应 debugger 事件 | 数据内容 |
|---|---|---|
| `requestWillBeSent` | `Network.requestWillBeSent` | requestId, url, method, headers, postData |
| `responseReceived` | `Network.responseReceived` | requestId, status, headers, body (base64) |
| `dataReceived` | `Network.dataReceived` | requestId, dataLength, encodedDataLength |
| `loadingFinished` | `Network.loadingFinished` | requestId, timestamp |
| `loadingFailed` | `Network.loadingFailed` | requestId, errorText |

## Popup 页面

用户操作面板，功能：

- 端口输入框（默认 `ws://localhost:8765`，配置持久化到 `chrome.storage`）
- 连接/断开按钮
- 连接状态指示（○ 未连接 / ● 已连接 / ◐ 连接中）
- 状态信息区：活跃 Tab 数、待命 Tab 数、抓取状态、最后错误
- 日志面板（可折叠，显示最近通信日志）
- 自动重连：上次连接成功过，下次扩展加载时自动重连

## 文件结构

```
ds-chat2api/
├── manifest.json              # 扩展配置
├── background.js              # Service Worker
├── content.js                 # Content Script
├── popup.html                 # Popup 页面
├── popup.js                   # Popup 逻辑
├── popup.css                  # Popup 样式
└── docs/
    └── superpowers/
        └── specs/
            └── 2026-06-01-chrome-extension-data-collector-design.md
```

## 技术选型

| 项目 | 选型 | 理由 |
|---|---|---|
| 扩展版本 | Manifest V3 | Chrome 当前及未来标准 |
| 网络拦截 | chrome.debugger API | 唯一能拿到完整请求/响应体的方式 |
| 外部通信 | WebSocket (客户端) | 部署最简单，跨平台 |
| UI | Vanilla HTML/CSS/JS | 保持扩展轻量，无额外依赖 |
| 持久化 | chrome.storage.sync | 配置随用户同步 |
