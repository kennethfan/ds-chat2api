(() => {
  "use strict";
  // const _XHR = XMLHttpRequest.prototype.open;
  // XMLHttpRequest.prototype.open = function (method, url) {
  //   if (url === "/api/v0/chat/completion")
  //     this.addEventListener("load", () => {
  //       console.log("XHR:", method, url, this.responseText, this);
  //     });
  //   return _XHR.call(this, method, url);
  // };

  // 拦截 open（记录 URL/方法）
  const originalXHROpen = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function (method, url) {
    this._interceptUrl = url;
    this._interceptMethod = method;
    this._interceptRequestHeaders = {}; // 每次重新初始化
    return originalXHROpen.apply(this, arguments);
  };

  // 拦截 setRequestHeader（收集请求头）
  const originalSetRequestHeader = XMLHttpRequest.prototype.setRequestHeader;
  XMLHttpRequest.prototype.setRequestHeader = function (name, value) {
    if (!this._interceptRequestHeaders) this._interceptRequestHeaders = {};
    this._interceptRequestHeaders[name] = value;
    return originalSetRequestHeader.apply(this, arguments);
  };

  // 拦截 send（捕获请求体和响应）
  const originalXHRSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.send = function (body) {
    if (this._interceptUrl === "/api/v0/chat/completion") {
      this.addEventListener("load", function () {
        window.postMessage(
          {
            type: "DEEPSEEK_RESPONSE",
            url: this._interceptUrl,
            method: this._interceptMethod,
            requestHeaders: this._interceptRequestHeaders || {},
            requestBody: body,
            responseHeaders: parseHeaders(this.getAllResponseHeaders()),
            status: this.status,
            response: this.responseText,
          },
          "*",
        );
      });
    }
    return originalXHRSend.apply(this, arguments);
  };

  function parseHeaders(headerStr) {
    const headers = {};
    headerStr?.split("\r\n").forEach((line) => {
      const [key, ...rest] = line.split(": ");
      if (key) headers[key.trim()] = rest.join(": ").trim();
    });
    return headers;
  }
})();
