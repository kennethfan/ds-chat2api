const DEFAULT_PORT = 8765;
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30000;
const HEARTBEAT_INTERVAL_MS = 25000;

let wsClient = null;
let reconnectTimer = null;
let reconnectAttempt = 0;
let tabStates = new Map();
let activeTabId = null;
let pendingRequests = new Map();
let requestCounter = 0;

function generateId() {
  return "req_" + ++requestCounter + "_" + Date.now();
}

// ── WebSocket Client ──────────────────────────────────────────────────────

const ws = {
  socket: null,
  connected: false,
  port: DEFAULT_PORT,
  wsUrl: null,
  heartbeatTimer: null,

  connect(portOrUrl) {
    if (typeof portOrUrl === "string" && portOrUrl.startsWith("ws://")) {
      this.wsUrl = portOrUrl;
      chrome.storage.sync.set({ wsUrl: portOrUrl });
    } else if (
      typeof portOrUrl === "number" ||
      (typeof portOrUrl === "string" && /^\d+$/.test(portOrUrl))
    ) {
      const p = Number(portOrUrl);
      this.port = p;
      this.wsUrl = `ws://localhost:${p}`;
      chrome.storage.sync.set({ wsPort: p });
    } else {
      this.wsUrl = portOrUrl || this.wsUrl || `ws://localhost:${this.port}`;
      chrome.storage.sync.set({ wsUrl: this.wsUrl });
    }
    chrome.storage.sync.set({ autoReconnect: true });
    this._doConnect();
  },

  disconnect() {
    this._clearReconnect();
    this._clearHeartbeat();
    if (this.socket) {
      this.socket.onclose = null;
      this.socket.close();
      this.socket = null;
    }
    this.connected = false;
    chrome.storage.sync.set({ wsConnected: false, autoReconnect: false });
    this._broadcastDisconnect();
  },

  _doConnect() {
    if (
      this.socket &&
      (this.socket.readyState === WebSocket.OPEN ||
        this.socket.readyState === WebSocket.CONNECTING)
    ) {
      return;
    }
    const url = this.wsUrl || `ws://localhost:${this.port}`;
    try {
      this.socket = new WebSocket(url);
    } catch (e) {
      this._scheduleReconnect();
      return;
    }

    this.socket.onopen = () => {
      this.connected = true;
      reconnectAttempt = 0;
      chrome.storage.sync.set({ wsConnected: true });
      this._startHeartbeat();
    };

    this.socket.onmessage = (event) => {
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch (e) {
        console.error("onmessage error", e);
        return;
      }
      ws._handleMessage(msg);
    };

    this.socket.onclose = () => {
      console.warn("socket closed");
      this.connected = false;
      this._clearHeartbeat();
      chrome.storage.sync.set({ wsConnected: false });
      this.socket = null;
      this._broadcastDisconnect();
      this._scheduleReconnect();
    };

    this.socket.onerror = (e) => {
      console.error("socket error", e);
    };
  },

  _handleMessage(msg) {
    console.debug("receive message:", msg);
    if (msg.type === "request") {
      this._routeRequest(msg);
    }
  },

  _routeRequest(msg) {
    const method = msg.method;
    const params = msg.params || {};
    const id = msg.id;

    switch (method) {
      case "newSession":
      case "chat":
        this._forwardToTab(method, params, id);
        break;
      case "ping":
        this._send({ type: "response", id, result: { pong: true } });
        break;
      case "getStatus":
        this._send({ type: "response", id, result: ws._getStatus() });
        break;
      default:
        this._send({
          type: "error",
          id,
          error: {
            code: "UNKNOWN_METHOD",
            message: `Unknown method: ${method}`,
          },
        });
    }
  },

  _forwardToTab(method, params, id, retries = 5) {
    console.log("_forwardToTab", method, params, id, "retries:", retries);
    if (!activeTabId) {
      this._send({
        type: "error",
        id,
        error: { code: "NO_ACTIVE_TAB", message: "No active tab" },
      });
      return;
    }
    chrome.tabs.sendMessage(
      activeTabId,
      { type: "execute", method, params, id },
      (response) => {
        console.debug("response data", response);
        console.debug("response error", chrome.runtime.lastError);
        if (chrome.runtime.lastError) {
          if (retries > 0) {
            setTimeout(() => {
              ws._forwardToTab(method, params, id, retries - 1);
            }, 800);
            return;
          }
          ws._send({
            type: "error",
            id,
            error: {
              code: "TAB_ERROR",
              message: chrome.runtime.lastError.message,
            },
          });
          return;
        }
        if (response) {
          ws._send(response);
        }
      },
    );
  },

  _send(data) {
    if (this.socket && this.socket.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify(data));
    } else {
      console.warn(
        "_send: socket not open, dropping message",
        data.type,
        data.id,
      );
    }
  },

  _scheduleReconnect() {
    this._clearReconnect();
    const delay = Math.min(
      RECONNECT_BASE_MS * Math.pow(2, reconnectAttempt),
      RECONNECT_MAX_MS,
    );
    reconnectAttempt++;
    reconnectTimer = setTimeout(() => {
      ws._doConnect();
    }, delay);
  },

  _clearReconnect() {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  },

  _startHeartbeat() {
    this._clearHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
        this.connected = false;
        if (this.socket) {
          try {
            this.socket.close();
          } catch (e) {
            /* ignore */
          }
          this.socket = null;
        }
        this._scheduleReconnect();
      }
    }, HEARTBEAT_INTERVAL_MS);
  },

  _clearHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  },

  _broadcastDisconnect() {
    chrome.tabs.query({ url: "https://chat.deepseek.com/*" }, (tabs) => {
      for (const tab of tabs) {
        chrome.tabs
          .sendMessage(tab.id, { type: "ws_disconnected" })
          .catch(() => {});
      }
    });
  },

  _getStatus() {
    const standbyCount = Array.from(tabStates.values()).filter(
      (s) => s === "standby",
    ).length;
    return {
      connected: ws.connected,
      connecting: false,
      port: ws.port,
      activeTab: activeTabId,
      standbyTabs: standbyCount,
    };
  },
};

// ── Tab Manager & Leader Election ─────────────────────────────────────────

function initTabManager() {
  chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
    if (changeInfo.status === "loading" && tab.url && isTargetUrl(tab.url)) {
      handleTabAvailable(tabId, tab);
    }
  });

  chrome.tabs.onRemoved.addListener((tabId) => {
    handleTabRemoved(tabId);
  });
}

function isTargetUrl(url) {
  return url && url.startsWith("https://chat.deepseek.com/");
}

function handleTabAvailable(tabId, tab) {
  if (tabStates.has(tabId)) return;
  if (!tab.url || !isTargetUrl(tab.url)) return;

  if (activeTabId === null) {
    tabStates.set(tabId, "active");
    activeTabId = tabId;
    attachDebuggerToActive();
    chrome.scripting.registerContentScripts([
      {
        id: "deepseek-interceptor",
        matches: ["https://chat.deepseek.com/*"],
        js: ["interceptor.js"],
        world: "MAIN", // 注入主世界，才能覆写原生 WebSocket
        runAt: "document_start", // 尽早注入，在页面 WebSocket 创建之前
      },
    ]);
  } else {
    tabStates.set(tabId, "standby");
  }
}

function handleTabRemoved(tabId) {
  tabStates.delete(tabId);

  if (tabId === activeTabId) {
    detachDebugger(() => {
      activeTabId = null;
      promoteNextTab();
    });
  }
}

function promoteNextTab() {
  const standbyTabs = [];
  for (const [tabId, state] of tabStates.entries()) {
    if (state === "standby") {
      standbyTabs.push(tabId);
    }
  }

  if (standbyTabs.length === 0) {
    return;
  }

  const nextTabId = standbyTabs[0];
  tabStates.set(nextTabId, "active");
  activeTabId = nextTabId;
  attachDebuggerToActive();
}

// ── Debugger Integration ──────────────────────────────────────────────────

function attachDebuggerToActive() {
  if (activeTabId === null) {
    return;
  }

  const debuggee = {
    tabId: activeTabId,
  };
  chrome.debugger.attach(debuggee, "1.3", () => {
    if (chrome.runtime.lastError) {
      if (
        chrome.runtime.lastError.message &&
        chrome.runtime.lastError.message.includes("already attached")
      ) {
        return;
      }
      tabStates.set(activeTabId, "inactive");
      activeTabId = null;
      promoteNextTab();
      return;
    }
  });
}

function detachDebugger(callback) {
  if (activeTabId === null) {
    if (callback) callback();
    return;
  }
  const debuggee = { tabId: activeTabId };
  chrome.debugger.detach(debuggee, () => {
    if (chrome.runtime.lastError) {
      console.error("debugger detach error", chrome.runtime.lastError);
    }
    if (callback) {
      callback();
    }
  });
}

function debuggerDetachHandler(debuggee) {
  if (debuggee.tabId === activeTabId) {
    streamingSSE = false;
    activeTabId = null;
    tabStates.delete(debuggee.tabId);
    promoteNextTab();
  }
}

// ── Popup Communication ───────────────────────────────────────────────────

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === "connect") {
    ws.connect(message.port);
    sendResponse({ success: true });
  } else if (message.type === "disconnect") {
    ws.disconnect();
    sendResponse({ success: true });
  } else if (message.type === "getStatus") {
    sendResponse(ws._getStatus());
  }
  return true;
});

// ── Content Script Message Handling ───────────────────────────────────────

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === "result" || message.type === "error") {
    if (ws.connected) {
      ws._send(message);
    }
  }
  return false;
});

// ── Initialization ────────────────────────────────────────────────────────

function init() {
  chrome.storage.sync.get(["wsUrl", "wsPort", "autoReconnect"], (result) => {
    if (result.wsUrl) {
      ws.wsUrl = result.wsUrl;
    } else if (result.wsPort) {
      ws.port = result.wsPort;
    }

    // Service Worker 重启后自动重连（前提是之前曾连接过）
    if (result.autoReconnect === true) {
      ws.connect();
    }

    initTabManager();

    chrome.tabs.query({ url: ["http://*/*", "https://*/*"] }, (tabs) => {
      for (const tab of tabs) {
        handleTabAvailable(tab.id, tab);
      }
    });
  });
}

init();
