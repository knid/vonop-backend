const http = require("http");
const fs = require("fs");
const path = require("path");
const { execFile } = require("child_process");
const httpProxy = require("http-proxy");

const PORT = 3000;
const NEXT_PORT = 3001;
const SYNC_TOKEN = process.env.SYNC_TOKEN || "";
const APP_DIR = "/app";
const SETUP_MARKER = "/tmp/.setup-complete";

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

function deleteFiles(paths) {
  let deleted = 0;
  for (const filePath of paths) {
    const full = path.join(APP_DIR, path.normalize(filePath));
    if (!full.startsWith(APP_DIR)) {
      console.warn("skipping path traversal attempt (delete):", filePath);
      continue;
    }
    try {
      fs.unlinkSync(full);
      deleted++;
      // Clean empty parent dirs up to APP_DIR
      let dir = path.dirname(full);
      while (dir !== APP_DIR && dir.startsWith(APP_DIR)) {
        try {
          fs.rmdirSync(dir);
          dir = path.dirname(dir);
        } catch {
          break; // dir not empty or other error
        }
      }
    } catch (e) {
      if (e.code !== "ENOENT") {
        console.error("delete failed:", filePath, e.message);
      }
    }
  }
  return deleted;
}

let bunInstallRunning = false;
let bunInstallQueued = false;

function bunInstall() {
  // If startup setup hasn't finished, flag for later (avoid concurrent installs)
  if (!fs.existsSync(SETUP_MARKER)) {
    fs.writeFileSync("/tmp/.needs-bun-install", "", "utf8");
    console.log("setup not complete, flagged for bun install after setup");
    return;
  }

  if (bunInstallRunning) {
    bunInstallQueued = true;
    console.log("bun install already running, queued for later");
    return;
  }

  bunInstallRunning = true;
  console.log("starting bun install...");
  execFile("bun", ["install"], { cwd: APP_DIR, timeout: 60_000 }, (err) => {
    bunInstallRunning = false;
    if (err) {
      console.error("bun install failed:", err.message);
    } else {
      console.log("bun install completed");
    }
    if (bunInstallQueued) {
      bunInstallQueued = false;
      bunInstall();
    }
  });
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

      const deletePaths = body.delete || [];
      const deleted = deleteFiles(deletePaths);

      const packageChanged = writeFiles(files);
      if (packageChanged) {
        bunInstall();
      }

      console.log(`synced ${count} files, deleted ${deleted} files`);
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ status: "ok", files_synced: count, files_deleted: deleted }));
    } catch (e) {
      console.error("sync error:", e.message);
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: e.message }));
    }
    return;
  }

  // Logs endpoint — returns last 200 lines of Next.js output
  if (req.url === "/__wemob/logs" && req.method === "GET") {
    const auth = req.headers.authorization || "";
    if (SYNC_TOKEN && auth !== `Bearer ${SYNC_TOKEN}`) {
      res.writeHead(401, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "unauthorized" }));
      return;
    }

    const logPath = "/tmp/nextjs.log";
    let logs = "";
    try {
      if (fs.existsSync(logPath)) {
        const content = fs.readFileSync(logPath, "utf8");
        // Strip ANSI escape codes (colors, cursor moves, etc.)
        const clean = content.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, "");
        const lines = clean.split("\n");
        logs = lines.slice(-200).join("\n");
      }
    } catch (e) {
      console.error("failed to read logs:", e.message);
    }

    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ logs }));
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

// Truncate log file periodically to prevent disk fill
setInterval(() => {
  try {
    const stats = fs.statSync("/tmp/nextjs.log");
    if (stats.size > 5 * 1024 * 1024) {
      const content = fs.readFileSync("/tmp/nextjs.log", "utf8");
      fs.writeFileSync("/tmp/nextjs.log", content.split("\n").slice(-1000).join("\n"), "utf8");
      console.log("truncated /tmp/nextjs.log (was", stats.size, "bytes)");
    }
  } catch {}
}, 5 * 60 * 1000);
