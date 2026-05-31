# 部署自有 GitHub 反代（Cloudflare Workers）

基于 [hunshcn/gh-proxy](https://github.com/hunshcn/gh-proxy)，部署在 `gh.wp-panel.org`，只代理 WP Panel 仓库。

## 前置条件

1. **Cloudflare 账号**（免费注册 cloudflare.com）
2. **`wp-panel.org` 域名的 DNS 已托管在 Cloudflare**（你已经在用了）
3. **Node.js 18+**（本机安装即可）

## 第一步：安装 Wrangler CLI

在你本机打开终端，执行：

```bash
npm install -g wrangler
```

验证安装：
```bash
wrangler --version
```

## 第二步：登录 Cloudflare

```bash
wrangler login
```

浏览器会弹出 Cloudflare 授权页面，点击 Allow。

## 第三步：创建项目目录并安装 gh-proxy

```bash
mkdir gh-proxy && cd gh-proxy
npm install @hono/node-server  # gh-proxy 依赖 Hono 框架
```

或者更简单，直接使用 gh-proxy 的单文件部署：

```bash
mkdir gh-proxy && cd gh-proxy
```

创建 `wrangler.toml` 配置文件：

```toml
name = "gh-proxy"
main = "src/index.js"
compatibility_date = "2025-01-01"
```

## 第四步：编写 Worker 代码

在 `src/index.js`（创建 `src` 目录）：

```javascript
// gh-proxy Cloudflare Worker — 仅代理 naibabiji/wp-panel
// 基于 hunshcn/gh-proxy 精简

const HOSTS = ["github.com", "api.github.com", "raw.githubusercontent.com"];

const WHITELIST = /naibabiji\/wp-panel/;

function isWhitelisted(url) {
  try {
    const u = new URL(url);
    return WHITELIST.test(u.host + u.pathname) || WHITELIST.test(u.pathname);
  } catch {
    return false;
  }
}

async function handleRequest(request) {
  const url = new URL(request.url);
  let targetUrl = url.pathname.substring(1); // 去掉开头的 /

  // 如果路径不含协议，尝试拼装
  if (!targetUrl.startsWith("http")) {
    targetUrl = "https://" + targetUrl;
  }

  const target = new URL(targetUrl);

  // 仅允许已授权的主机
  if (!HOSTS.includes(target.host)) {
    return new Response("Forbidden: host not allowed", { status: 403 });
  }

  // 仅允许白名单路径
  if (!isWhitelisted(targetUrl)) {
    return new Response("Forbidden: path not whitelisted", { status: 403 });
  }

  // 构建转发请求
  const headers = new Headers(request.headers);
  headers.delete("host");
  headers.delete("cookie");
  headers.delete("authorization");
  headers.set("user-agent", "gh-proxy-wp-panel");

  const proxyRequest = new Request(targetUrl, {
    method: request.method,
    headers,
    body: request.method !== "GET" && request.method !== "HEAD" ? await request.arrayBuffer() : undefined,
    redirect: "follow",
  });

  const response = await fetch(proxyRequest);

  // 返回响应（透传）
  const responseHeaders = new Headers(response.headers);
  responseHeaders.set("access-control-allow-origin", "*");
  responseHeaders.set("cache-control", "public, max-age=300");

  return new Response(response.body, {
    status: response.status,
    headers: responseHeaders,
  });
}

export default {
  async fetch(request) {
    return handleRequest(request);
  },
};
```

## 第五步：部署

```bash
wrangler deploy
```

部署成功后会得到 `https://gh-proxy.<你的账号>.workers.dev`。

## 第六步：绑定自定义域名

1. 打开 Cloudflare 控制台 → Workers 和 Pages → gh-proxy
2. 点击「触发器」→「自定义域」→「添加自定义域」
3. 输入 `gh.wp-panel.org`
4. Cloudflare 会自动添加 DNS 记录和签发 SSL 证书

等待 1-2 分钟 DNS 生效。

## 第七步：验证

```bash
# 测试版本检查
curl -v https://gh.wp-panel.org/https://api.github.com/repos/naibabiji/wp-panel/releases/latest

# 测试白名单阻止其他仓库
curl https://gh.wp-panel.org/https://api.github.com/repos/other/repo/releases/latest
# 应该返回 403

# 测试二进制下载
curl -I https://gh.wp-panel.org/https://github.com/naibabiji/wp-panel/releases/latest/download/wp-panel
```

## 维护

- **更新 Worker 代码**：修改 `src/index.js` 后执行 `wrangler deploy`
- **查看日志**：Cloudflare 控制台 → Workers → gh-proxy → 日志
- **监控用量**：免费额度每天 10 万次请求，面板更新场景完全够用
