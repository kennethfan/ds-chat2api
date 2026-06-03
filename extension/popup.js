(function () {
  const MAX_LOG_ENTRIES = 10;
  let pollInterval = null;

  const portInput = document.getElementById("portInput");
  const connectBtn = document.getElementById("connectBtn");
  const statusDot = document.getElementById("statusDot");
  const statusText = document.getElementById("statusText");
  const activeTabEl = document.getElementById("activeTab");
  const standbyTabsEl = document.getElementById("standbyTabs");
  const logEntries = document.getElementById("logEntries");
  const clearLogBtn = document.getElementById("clearLogBtn");

  function setStatus(state) {
    statusDot.className = "status-dot " + state;
    if (state === "connected") {
      statusText.textContent = "Connected";
      connectBtn.textContent = "Disconnect";
      connectBtn.className = "connected";
    } else if (state === "connecting") {
      statusText.textContent = "Connecting...";
      connectBtn.textContent = "Connecting";
      connectBtn.className = "connected";
    } else {
      statusText.textContent = "Disconnected";
      connectBtn.textContent = "Connect";
      connectBtn.className = "";
    }
  }

  function appendLog(text, type) {
    type = type || "info";
    const entry = document.createElement("div");
    entry.className = "log-entry " + type;
    const ts = document.createElement("span");
    ts.className = "timestamp";
    ts.textContent = new Date().toLocaleTimeString();
    entry.appendChild(ts);
    entry.appendChild(document.createTextNode(text));
    logEntries.appendChild(entry);
    while (logEntries.children.length > MAX_LOG_ENTRIES) {
      logEntries.removeChild(logEntries.firstChild);
    }
    logEntries.scrollTop = logEntries.scrollHeight;
  }

  function updateUI(status) {
    if (!status) return;
    if (status.connected) {
      setStatus("connected");
    } else if (status.connecting) {
      setStatus("connecting");
    } else {
      setStatus("disconnected");
    }
    activeTabEl.textContent = status.activeTab || "—";
    standbyTabsEl.textContent =
      status.standbyTabs != null ? status.standbyTabs : "0";
  }

  function pollStatus() {
    chrome.runtime.sendMessage({ type: "getStatus" }, function (response) {
      if (chrome.runtime.lastError) {
        return;
      }
      if (response) {
        updateUI(response);
      }
    });
  }

  function loadState() {
    chrome.storage.sync.get("wsUrl", function (data) {
      if (chrome.runtime.lastError) {
        appendLog("Failed to load saved port", "error");
        return;
      }
      if (data.wsUrl) {
        portInput.value = data.wsUrl;
      }
    });

    chrome.runtime.sendMessage({ type: "getStatus" }, function (response) {
      if (chrome.runtime.lastError) {
        appendLog("Failed to get status from background", "error");
        setStatus("disconnected");
        return;
      }
      if (response) {
        updateUI(response);
        appendLog(
          "Status: " + (response.connected ? "Connected" : "Disconnected"),
          response.connected ? "info" : "warn",
        );
      }
    });
  }

  function handleConnect() {
    const port = portInput.value.trim();
    if (!port) {
      appendLog("Please enter a WebSocket server address", "error");
      return;
    }
    setStatus("connecting");
    chrome.runtime.sendMessage(
      { type: "connect", port: port },
      function (response) {
        if (chrome.runtime.lastError) {
          appendLog(
            "Connection error: " + chrome.runtime.lastError.message,
            "error",
          );
          setStatus("disconnected");
          return;
        }
        if (response && response.success) {
          appendLog("Connecting to " + port, "info");
        } else {
          appendLog(
            "Failed to connect: " + (response ? response.error : "unknown"),
            "error",
          );
          setStatus("disconnected");
        }
      },
    );
  }

  function handleDisconnect() {
    chrome.runtime.sendMessage({ type: "disconnect" }, function (response) {
      if (chrome.runtime.lastError) {
        appendLog(
          "Disconnect error: " + chrome.runtime.lastError.message,
          "error",
        );
        return;
      }
      setStatus("disconnected");
      appendLog("Disconnected", "warn");
    });
  }

  function handlePortSave() {
    const port = portInput.value.trim();
    if (port) {
      chrome.storage.sync.set({ wsUrl: port }, function () {
        if (chrome.runtime.lastError) {
          appendLog("Failed to save port", "error");
        }
      });
    }
  }

  function handleClearLog() {
    logEntries.innerHTML = "";
  }

  connectBtn.addEventListener("click", function () {
    if (statusDot.classList.contains("connected")) {
      handleDisconnect();
    } else {
      handleConnect();
    }
  });

  portInput.addEventListener("change", handlePortSave);
  clearLogBtn.addEventListener("click", handleClearLog);

  document.addEventListener("DOMContentLoaded", function () {
    loadState();
    pollInterval = setInterval(pollStatus, 2000);
  });

  window.addEventListener("unload", function () {
    if (pollInterval) {
      clearInterval(pollInterval);
      pollInterval = null;
    }
  });
})();
