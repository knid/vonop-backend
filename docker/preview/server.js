const http = require("http");
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");
const httpProxy = require("http-proxy");

const PORT = 3000;
const NEXT_PORT = 3001;
const SYNC_TOKEN = process.env.SYNC_TOKEN || "";
const APP_DIR = "/app";

const proxy = httpProxy.createProxyServer({
  target: `http://127.0.0.1:${NEXT_PORT}`,
  ws: true,
});

proxy.on("error", (err, _req, res) => {
  console.error("proxy error:", err.message);
  if (res.writeHead) {
    res.writeHead(502, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "backend unavailable" }));
  }
});

function parseBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => {
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString()));
      } catch (e) {
        reject(e);
      }
    });
    req.on("error", reject);
  });
}

function writeFiles(files) {
  let packageChanged = false;
  for (const [filePath, content] of Object.entries(files)) {
    const full = path.join(APP_DIR, path.normalize(filePath));
    if (!full.startsWith(APP_DIR)) {
      console.warn("skipping path traversal attempt:", filePath);
      continue;
    }
    fs.mkdirSync(path.dirname(full), { recursive: true });
    fs.writeFileSync(full, content, "utf8");
    if (filePath === "package.json") packageChanged = true;
  }
  return packageChanged;
}

function npmInstall() {
  try {
    execFileSync("npm", ["install", "--prefer-offline"], {
      cwd: APP_DIR,
      timeout: 120_000,
      stdio: "pipe",
    });
    console.log("npm install completed");
  } catch (e) {
    console.error("npm install failed:", e.message);
  }
}

const server = http.createServer(async (req, res) => {
  // Health check
  if (req.url === "/__wemob/health" && req.method === "GET") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: "ok" }));
    return;
  }

  // File sync endpoint
  if (req.url === "/__wemob/sync" && req.method === "POST") {
    const auth = req.headers.authorization || "";
    if (SYNC_TOKEN && auth !== `Bearer ${SYNC_TOKEN}`) {
      res.writeHead(401, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "unauthorized" }));
      return;
    }

    try {
      const body = await parseBody(req);
      const files = body.files || {};
      const count = Object.keys(files).length;

      const packageChanged = writeFiles(files);
      if (packageChanged) {
        npmInstall();
      }

      console.log(`synced ${count} files`);
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ status: "ok", files_synced: count }));
    } catch (e) {
      console.error("sync error:", e.message);
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: e.message }));
    }
    return;
  }

  // Everything else proxied to Next.js
  proxy.web(req, res);
});

// WebSocket upgrade for HMR
server.on("upgrade", (req, socket, head) => {
  proxy.ws(req, socket, head);
});

server.listen(PORT, "0.0.0.0", () => {
  console.log(`wemob preview proxy listening on :${PORT}, forwarding to :${NEXT_PORT}`);
});
