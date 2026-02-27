const http = require("http");
const fs = require("fs");
const path = require("path");
const { execFile, spawn } = require("child_process");
const httpProxy = require("http-proxy");

const PORT = 3000;
const NEXT_PORT = 3001;
const SYNC_TOKEN = process.env.SYNC_TOKEN || "";
const PROJECT_ID = process.env.PROJECT_ID || "";
const PREVIEW_DOMAIN = process.env.PREVIEW_DOMAIN || "";
const APP_DIR = "/app";
const SETUP_MARKER = "/tmp/.setup-complete";

// ─── Next.js lifecycle ──────────────────────────────────────────────────────

let nextReady = false;
let nextProcess = null;

// Probe Next.js readiness every 2s — any response (even 500) means it's up
setInterval(() => {
  http
    .get(`http://127.0.0.1:${NEXT_PORT}/`, (res) => {
      if (!nextReady) console.log("Next.js is ready");
      nextReady = true;
      res.resume();
    })
    .on("error", () => {
      if (nextReady) console.log("Next.js became unavailable");
      nextReady = false;
    });
}, 2000);

function startNextJs() {
  if (nextProcess) return;
  console.log("starting Next.js dev server...");

  // Clear stale logs so validation doesn't see old errors
  try { fs.writeFileSync("/tmp/nextjs.log", "", "utf8"); } catch {}

  const logStream = fs.createWriteStream("/tmp/nextjs.log", { flags: "a" });
  const child = spawn(
    "./node_modules/.bin/next",
    ["dev", "--port", String(NEXT_PORT), "--hostname", "0.0.0.0"],
    { cwd: APP_DIR, stdio: ["ignore", "pipe", "pipe"] }
  );

  child.stdout.on("data", (data) => {
    process.stdout.write(data);
    logStream.write(data);
  });
  child.stderr.on("data", (data) => {
    process.stderr.write(data);
    logStream.write(data);
  });

  child.on("exit", (code, signal) => {
    console.log(`Next.js exited (code=${code}, signal=${signal})`);
    nextProcess = null;
    nextReady = false;
    logStream.end();
  });

  nextProcess = child;
}

function stopNextJs() {
  return new Promise((resolve) => {
    if (!nextProcess) {
      resolve();
      return;
    }
    console.log("stopping Next.js for dependency install...");
    nextReady = false;
    const proc = nextProcess;
    proc.once("exit", () => {
      nextProcess = null;
      resolve();
    });
    proc.kill("SIGTERM");
    // Force kill after 5s if SIGTERM doesn't work
    setTimeout(() => {
      if (nextProcess === proc) {
        try { proc.kill("SIGKILL"); } catch {}
      }
    }, 5000);
  });
}

// ─── bun install (stops Next.js first to prevent OOM) ───────────────────────

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

  // Stop Next.js first to free memory and avoid compiling with missing deps
  stopNextJs().then(() => {
    console.log("starting bun install...");
    execFile("bun", ["install"], { cwd: APP_DIR, timeout: 120_000 }, (err) => {
      bunInstallRunning = false;
      if (err) {
        console.error("bun install failed:", err.message);
      } else {
        console.log("bun install completed");
      }

      if (bunInstallQueued) {
        bunInstallQueued = false;
        bunInstall(); // nextProcess is already null, stopNextJs will resolve immediately
      } else {
        // Only restart Next.js when no more queued installs
        startNextJs();
      }
    });
  });
}

// ─── HTTP helpers ───────────────────────────────────────────────────────────

const LOADING_HTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><meta http-equiv="refresh" content="3">
<title>Loading preview...</title>
<style>body{margin:0;display:flex;align-items:center;justify-content:center;
height:100vh;font-family:system-ui;background:#fafafa;color:#666}
.spinner{width:24px;height:24px;border:3px solid #e5e5e5;border-top-color:#999;
border-radius:50%;animation:spin 0.8s linear infinite;margin-right:12px}
@keyframes spin{to{transform:rotate(360deg)}}</style></head>
<body><div class="spinner"></div>Compiling preview&hellip;</body></html>`;

const proxy = httpProxy.createProxyServer({
  target: `http://127.0.0.1:${NEXT_PORT}`,
  ws: true,
});

proxy.on("error", (err, _req, res) => {
  console.error("proxy error:", err.message);
  if (res.writeHead) {
    res.writeHead(502, { "Content-Type": "text/html", "Cache-Control": "no-store, no-cache, must-revalidate" });
    res.end(LOADING_HTML);
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

// ─── HTTP server ────────────────────────────────────────────────────────────

const server = http.createServer(async (req, res) => {
  // Health check — only 200 when Next.js is actually responding
  if (req.url === "/__wemob/health" && req.method === "GET") {
    const code = nextReady ? 200 : 503;
    res.writeHead(code, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: nextReady ? "ok" : "starting" }));
    return;
  }

  // Route guard: if this request is for a different project, bounce it via fly-replay
  // so Fly's proxy re-routes to the correct machine (or the router).
  if (PROJECT_ID && PREVIEW_DOMAIN) {
    const host = (req.headers.host || "").split(":")[0];
    const suffix = "." + PREVIEW_DOMAIN;
    if (host.endsWith(suffix)) {
      const requestedProject = host.slice(0, -suffix.length);
      if (requestedProject && requestedProject !== PROJECT_ID) {
        res.writeHead(200, { "fly-replay": "elsewhere=true" });
        res.end();
        return;
      }
    }
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

  // If Next.js isn't ready yet, serve a loading page for browser requests
  // (let __wemob and _next requests through — they're handled above or by proxy)
  if (!nextReady && !req.url.startsWith("/__wemob/") && !req.url.startsWith("/_next/")) {
    res.writeHead(503, { "Content-Type": "text/html", "Cache-Control": "no-store, no-cache, must-revalidate" });
    res.end(LOADING_HTML);
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

// ─── Startup: manage Next.js from here ──────────────────────────────────────

fs.writeFileSync(SETUP_MARKER, "", "utf8");

// Check if early sync flagged bun install (before we were ready)
if (fs.existsSync("/tmp/.needs-bun-install")) {
  try { fs.unlinkSync("/tmp/.needs-bun-install"); } catch {}
  console.log("running deferred bun install before starting Next.js...");
  execFile("bun", ["install"], { cwd: APP_DIR, timeout: 120_000 }, (err) => {
    if (err) console.error("initial bun install failed:", err.message);
    else console.log("initial bun install completed");
    startNextJs();
  });
} else {
  startNextJs();
}

// ─── Graceful shutdown ──────────────────────────────────────────────────────

process.on("SIGTERM", () => {
  console.log("received SIGTERM, shutting down...");
  if (nextProcess) nextProcess.kill("SIGTERM");
  process.exit(0);
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
