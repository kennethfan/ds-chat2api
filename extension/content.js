(() => {
  "use strict";

  let _cancelChat = null; // WS 断开时取消 pending 请求

  async function newSession() {
    return new Promise((resolve, reject) => {
      const spans = document.getElementsByTagName("span");
      for (const span of spans) {
        if (span.textContent === "开启新对话") {
          console.debug("找到目标元素:", span);
          span.click();
          return resolve({ code: "SUCCESS", message: "" });
        }
      }

      return reject({
        code: "NEW_SESSION_BUTTON_NOT_FUND",
        message: "new session button not fund",
      });
    });
  }

  async function chat({ message = "" }) {
    return new Promise((resolve, reject) => {
      const textarea = document.getElementsByName("search")[0];
      if (!textarea) {
        return resolve({
          code: "NO_TEXTAREA",
          message: "textarea not found",
        });
      }
      textarea.click();
      textarea.focus();

      for (const char of message) {
        const key = char === "\n" ? "Enter" : char;

        textarea.dispatchEvent(
          new KeyboardEvent("keydown", {
            key,
            code: char === "\n" ? "Enter" : `Key${char.toUpperCase()}`,
            keyCode: char === "\n" ? 13 : char.charCodeAt(0),
            bubbles: true,
          }),
        );

        const setter = Object.getOwnPropertyDescriptor(
          HTMLTextAreaElement.prototype,
          "value",
        ).set;
        setter.call(textarea, textarea.value + char);

        textarea.dispatchEvent(
          new KeyboardEvent("keyup", { key, bubbles: true }),
        );
      }

      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      textarea.dispatchEvent(new Event("change", { bubbles: true }));

      textarea.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Enter",
          code: "Enter",
          keyCode: 13,
          which: 13,
          bubbles: true,
        }),
      );

      // 注册取消函数
      _cancelChat = () => {
        window.removeEventListener("message", onMessage);
        resolve({ code: "CANCELLED", message: "WebSocket disconnected" });
      };

      // 等待 interceptor.js 拦截到的 XHR 响应
      const onMessage = (event) => {
        if (event.data.type !== "DEEPSEEK_RESPONSE") return;
        _cancelChat = null;
        window.removeEventListener("message", onMessage);
        resolve({
          code: "SUCCESS",
          data: {
            status: event.data.status,
            response: event.data.response,
            responseHeaders: event.data.responseHeaders,
            requestBody: event.data.requestBody,
          },
        });
      };
      window.addEventListener("message", onMessage);

      // 60s 超时兜底
      setTimeout(() => {
        if (!_cancelChat) return;
        _cancelChat = null;
        window.removeEventListener("message", onMessage);
        resolve({ code: "TIMEOUT", message: "等待响应超时" });
      }, 60000);
    });
  }

  function waitTimeout({ ms }) {
    return new Promise((resolve) =>
      setTimeout(() => resolve({ done: true }), ms),
    );
  }

  const handlers = {
    newSession,
    chat,
  };

  chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    const msg =
      typeof message === "string"
        ? (() => {
            try {
              return JSON.parse(message);
            } catch {
              return null;
            }
          })()
        : message;
    if (!msg || msg.type !== "execute") {
      return;
    }
    const { method, params, id } = msg;
    if (!method || !handlers[method]) {
      sendResponse({
        type: "error",
        id,
        error: {
          code: "UNKNOWN_METHOD",
          message: `Unknown method "${method}"`,
        },
      });
      return;
    }
    if (!params || typeof params !== "object") {
      sendResponse({
        type: "error",
        id,
        error: { code: "INVALID_PARAMS", message: "Invalid or missing params" },
      });
      return;
    }
    handlers[method](params).then(
      (result) => sendResponse({ type: "result", id, result }),
      (err) =>
        sendResponse({
          type: "error",
          id,
          error: {
            code: err.code || "UNKNOWN",
            message: err.message || String(err),
          },
        }),
    );
    return true;
  });

  // WS 断开时取消 pending 请求
  chrome.runtime.onMessage.addListener((message) => {
    if (message.type === "ws_disconnected" && _cancelChat) {
      const fn = _cancelChat;
      _cancelChat = null;
      fn();
    }
  });
})();
