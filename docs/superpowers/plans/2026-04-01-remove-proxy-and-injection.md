# Remove Proxy System & Mobile Injection System

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the dead proxy server (:9001, iProyal), mobile injection chat system, and all related code from backend and frontend. Apps now live in E2B sandboxes — the proxy is no longer needed.

**Architecture:** Pure deletion. Remove backend proxy handler, :9001 server, mobile injection model/DB/controller/views, related routes, Caddyfile block, Docker port. Remove frontend proxy URL construction, injection chat hook, injection API methods, and URL-wrapper flow in app-studio.

**Tech Stack:** Go backend (chi router, Bun ORM), Next.js frontend (TypeScript/React), Caddy reverse proxy, Docker Compose

---

### Task 1: Delete backend proxy files

**Files:**
- Delete: `internal/views/proxy.go`
- Delete: `internal/constants/proxy.go`

- [ ] **Step 1: Delete proxy.go**

```bash
rm internal/views/proxy.go
```

- [ ] **Step 2: Delete proxy constant**

```bash
rm internal/constants/proxy.go
```

- [ ] **Step 3: Verify no remaining references to deleted symbols**

```bash
grep -rn "ProxyHandler\|InitProxyTransport\|SessionCookieName\|customTransport\|rewriteHTML" internal/ cmd/ --include="*.go"
```

Expected: Only hits in `cmd/main.go` (which we clean in Task 3).

- [ ] **Step 4: Commit**

```bash
git add -A internal/views/proxy.go internal/constants/proxy.go
git commit -m "chore: delete proxy handler and proxy constants"
```

---

### Task 2: Delete backend mobile injection files

**Files:**
- Delete: `internal/models/mobile_injection.go`
- Delete: `internal/database/mobile_injection.go`
- Delete: `internal/models/mobile_conversation.go`
- Delete: `internal/database/mobile_conversation.go`
- Delete: `internal/controllers/mobile_chat_controller.go`
- Delete: `internal/constants/ai_mobile_injection.go`
- Delete: `internal/utils/injection_parser.go`

- [ ] **Step 1: Delete all mobile injection/conversation files**

```bash
rm internal/models/mobile_injection.go
rm internal/database/mobile_injection.go
rm internal/models/mobile_conversation.go
rm internal/database/mobile_conversation.go
rm internal/controllers/mobile_chat_controller.go
rm internal/constants/ai_mobile_injection.go
rm internal/utils/injection_parser.go
```

- [ ] **Step 2: Commit**

```bash
git add -A internal/models/mobile_injection.go internal/database/mobile_injection.go internal/models/mobile_conversation.go internal/database/mobile_conversation.go internal/controllers/mobile_chat_controller.go internal/constants/ai_mobile_injection.go internal/utils/injection_parser.go
git commit -m "chore: delete mobile injection models, DB, controller, constants, and parser"
```

---

### Task 3: Clean up cmd/main.go

**Files:**
- Modify: `cmd/main.go`

Remove these sections:

- [ ] **Step 1: Remove MobileInjection and MobileConversation from schema registration (~line 128-130)**

Remove these two lines from the `createSchema()` model list:
```go
		// Mobile injection chat
		(*models.MobileInjection)(nil),
		(*models.MobileConversation)(nil),
```

- [ ] **Step 2: Remove InitProxyTransport call (~line 842-846)**

Remove:
```go
	// Configure upstream proxy transport once at startup (prevents per-request data race)
	views.InitProxyTransport(
		sm.GetSecret("PREVIEW_PROXY_HOST"),
		sm.GetSecret("PREVIEW_PROXY_AUTH"),
	)
```

- [ ] **Step 3: Remove `/v1/proxy` route (~line 987)**

Remove:
```go
			r.HandleFunc("/proxy", httpViews.ProxyHandler)
```

- [ ] **Step 4: Remove public mobile injections route (~line 1176-1179)**

Change the `/v1/mobile/{projectId}` public route block from:
```go
			// Public mobile injections (no auth, rate-limited)
			r.Route("/mobile/{projectId}", func(r chi.Router) {
				r.Use(rateLimiter.RateLimit(120, time.Minute, appMiddleware.IPKey))
				r.Get("/injections", httpViews.GetPublicMobileInjections)
				r.Get("/config", httpViews.GetPublicMobileConfig)
			})
```
to:
```go
			r.Route("/mobile/{projectId}", func(r chi.Router) {
				r.Use(rateLimiter.RateLimit(120, time.Minute, appMiddleware.IPKey))
				r.Get("/config", httpViews.GetPublicMobileConfig)
			})
```

- [ ] **Step 5: Remove injection routes from `/v2/mobile/{projectId}` (~line 1410-1422)**

Change the v2 mobile route block from:
```go
			// Mobile injection chat (URL-wrapper projects)
			r.Route("/mobile/{projectId}", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireSubscription("active", "trial", "free"))

				r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/chat", httpViews.MobileChatStream)
				r.Get("/injections", httpViews.ListMobileInjections)
				r.Put("/injections/{id}", httpViews.UpdateMobileInjection)
				r.Delete("/injections/{id}", httpViews.DeleteMobileInjection)
				r.Delete("/conversation", httpViews.ClearMobileConversation)
				r.Get("/config", httpViews.GetMobileAppConfig)
				r.Put("/config", httpViews.UpdateMobileAppConfig)
			})
```
to:
```go
			r.Route("/mobile/{projectId}", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireSubscription("active", "trial", "free"))

				r.Get("/config", httpViews.GetMobileAppConfig)
				r.Put("/config", httpViews.UpdateMobileAppConfig)
			})
```

- [ ] **Step 6: Remove entire proxy server block (~line 1475-1513)**

Remove the whole block starting with `// Proxy` comment through `proxyRoute.HandleFunc("/*", httpViews.ProxyHandler)`:
```go
	// Proxy
	proxyRoute := chi.NewRouter()
	proxyRoute.Use(func(next http.Handler) http.Handler {
		...
	})

	proxyRoute.Use(cors.Handler(cors.Options{...}))
	proxyRoute.Use(middleware.Logger)
	proxyRoute.Use(middleware.Recoverer)
	proxyRoute.Use(middleware.Timeout(60 * time.Second))
	proxyRoute.HandleFunc("/*", httpViews.ProxyHandler)
```

- [ ] **Step 7: Remove proxyServer definition (~line 1525-1533)**

Remove:
```go
	proxyServer := &http.Server{
		Addr:              ":9001",
		Handler:           proxyRoute,
		MaxHeaderBytes:    1 << 20,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
```

- [ ] **Step 8: Remove proxy server goroutine (~line 1548-1553)**

Remove:
```go
	go func() {
		slog.Info("Proxy server listening on :9001")
		if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Proxy server error", "error", err)
		}
	}()
```

- [ ] **Step 9: Remove proxy server shutdown (~line 1579)**

Remove:
```go
		if err := proxyServer.Shutdown(ctx); err != nil {
			slog.Error("Proxy server shutdown error", "error", err)
		}
```

- [ ] **Step 10: Clean up unused imports**

After all removals, check if `views` package import alias is still needed (it is — other views use it). Remove any imports that are now unused (e.g., if `constants` package is no longer referenced elsewhere in main.go — but it likely still is).

- [ ] **Step 11: Build to verify**

```bash
go build -o /dev/null cmd/main.go
```

Expected: Successful build, no errors.

- [ ] **Step 12: Commit**

```bash
git add cmd/main.go
git commit -m "chore: remove proxy server, injection routes, and schema from main.go"
```

---

### Task 4: Clean up mobile_chat_views.go

**Files:**
- Modify: `internal/views/mobile_chat_views.go`

Remove the injection-related handlers but **keep** `ClassifyMobileIntent`, `GetMobileAppConfig`, `UpdateMobileAppConfig`, `GetPublicMobileConfig`, and their helper functions (`defaultMobileConfigResponse`, `buildMobileConfigResponse`).

- [ ] **Step 1: Remove injection handlers**

Delete these functions entirely:
- `MobileChatStream` (lines 19-60)
- `ListMobileInjections` (lines 63-88)
- `UpdateMobileInjection` (lines 91-178)
- `DeleteMobileInjection` (lines 181-216)
- `ClearMobileConversation` (lines 219-243)
- `GetPublicMobileInjections` (lines 545-583)

**Keep:** `ClassifyMobileIntent`, `GetMobileAppConfig`, `UpdateMobileAppConfig`, `GetPublicMobileConfig`, `defaultMobileConfigResponse`, `buildMobileConfigResponse`.

- [ ] **Step 2: Clean up unused imports**

After removing the handlers, these imports are likely no longer needed in the file:
- `"context"` (used by MobileChatStream)
- `"fmt"` (check — might still be needed by ClassifyMobileIntent)
- `"strings"` (used by MobileChatStream)
- `"github.com/knid/wemob/internal/requests"` (check if still used)
- `"github.com/knid/wemob/internal/responses"` (check if still used)
- `"github.com/knid/wemob/internal/services/remoteconfig"` (used by UpdateMobileInjection)

Keep: `"net/http"`, `"github.com/go-chi/chi/v5"`, `"github.com/google/uuid"`, `"github.com/knid/wemob/internal/models"`, `"github.com/knid/wemob/internal/utils"`.

- [ ] **Step 3: Build to verify**

```bash
go build -o /dev/null cmd/main.go
```

- [ ] **Step 4: Commit**

```bash
git add internal/views/mobile_chat_views.go
git commit -m "chore: remove injection handlers from mobile_chat_views.go"
```

---

### Task 5: Update infrastructure files

**Files:**
- Modify: `Caddyfile`
- Modify: `docker-compose.yaml`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Remove proxy.wemob.io block from Caddyfile**

Remove lines 21-37:
```
proxy.wemob.io {
    encode zstd gzip

    handle {

        reverse_proxy api:9001 {
            header_down -X-Frame-Options
            header_down -Content-Security-Policy
            header_down -Content-Security-Policy-Report-Only
        }
    }

    log {
        output file /var/log/caddy/proxy_access.log
        format console
    }
}
```

- [ ] **Step 2: Remove port 9001 from docker-compose.yaml**

Change:
```yaml
    expose:
      - "8001"
      - "9001"
```
to:
```yaml
    expose:
      - "8001"
```

- [ ] **Step 3: Update CLAUDE.md**

Update the "Two HTTP servers" line (line 48) from:
```
- **Two HTTP servers**: Main API on `:8001`, proxy server on `:9001` (for proxying web content). Caddy sits in front in production, routing `api.wemob.io` → `:8001` and `proxy.wemob.io` → `:9001`.
```
to:
```
- **HTTP server**: Main API on `:8001`. Caddy sits in front in production, routing `api.wemob.io` → `:8001`.
```

- [ ] **Step 4: Build to verify everything still compiles**

```bash
go build -o /dev/null cmd/main.go
```

- [ ] **Step 5: Commit**

```bash
git add Caddyfile docker-compose.yaml CLAUDE.md
git commit -m "chore: remove proxy from Caddyfile, Docker, and docs"
```

---

### Task 6: Clean up frontend — delete injection hook and API methods

**Files:**
- Delete: `../wemob-dashboard/hooks/use-mobile-injection-chat.ts`
- Modify: `../wemob-dashboard/lib/api.ts`

- [ ] **Step 1: Delete the injection chat hook**

```bash
rm ../wemob-dashboard/hooks/use-mobile-injection-chat.ts
```

- [ ] **Step 2: Remove injection API methods from lib/api.ts**

Remove the entire `// --- Mobile Injection Chat ---` section (~lines 2607-2662):
```typescript
  // --- Mobile Injection Chat ---

  async chatMobileInjection(...) { ... }
  async getMobileInjections(...) { ... }
  async updateMobileInjection(...) { ... }
  async deleteMobileInjection(...) { ... }
  async clearMobileConversation(...) { ... }
```

- [ ] **Step 3: Remove the MobileInjection interface from lib/api.ts**

Remove (~lines 2952-2964):
```typescript
export interface MobileInjection {
  id: string
  project_id: string
  name: string
  description: string
  css: string
  js: string
  html: string
  is_enabled: boolean
  order: number
  created_at: string
  updated_at: string
}
```

- [ ] **Step 4: Simplify classifyMobileIntent return type**

Since `ClassifyMobileIntent` backend always returns `from_scratch` now and never returns `project_id` or `proxy_url`, update the method (~line 2562):

From:
```typescript
  async classifyMobileIntent(prompt: string): Promise<{ intent: "url_wrapper" | "from_scratch"; url: string; project_id?: string; proxy_url?: string; welcome_message?: string }> {
    return this.request<{ intent: "url_wrapper" | "from_scratch"; url: string; project_id?: string; proxy_url?: string; welcome_message?: string }>("/v2/mobile/classify-intent", {
```
To:
```typescript
  async classifyMobileIntent(prompt: string): Promise<{ intent: "from_scratch"; url: string; prompt: string }> {
    return this.request<{ intent: "from_scratch"; url: string; prompt: string }>("/v2/mobile/classify-intent", {
```

- [ ] **Step 5: Commit**

```bash
cd ../wemob-dashboard
git add hooks/use-mobile-injection-chat.ts lib/api.ts
git commit -m "chore: remove injection chat hook and injection API methods"
```

---

### Task 7: Clean up frontend — remove proxy URLs and URL-wrapper flow from app-studio-page.tsx

**Files:**
- Modify: `../wemob-dashboard/components/kokonutui/pages/app-studio-page.tsx`

This is the largest frontend change. Remove the entire URL-wrapper/proxy/injection flow while keeping the from_scratch flow intact.

- [ ] **Step 1: Remove imports and state**

Remove the import:
```typescript
import { useMobileInjectionChat } from "@/hooks/use-mobile-injection-chat"
```

Remove state variables:
```typescript
  const [convertProxyUrl, setConvertProxyUrl] = useState<string | null>(null)
  const [urlWrapperProjectId, setUrlWrapperProjectId] = useState<string | null>(null)
```

Remove the hook:
```typescript
  const injectionChat = useMobileInjectionChat(urlWrapperProjectId)
```

Remove the `authedProxyUrl` memo:
```typescript
  const authedProxyUrl = useMemo(() => {
    if (!convertProxyUrl) return null
    const token = typeof window !== "undefined" ? localStorage.getItem("auth_token") : null
    if (!token) return convertProxyUrl
    const sep = convertProxyUrl.includes("?") ? "&" : "?"
    return `${convertProxyUrl}${sep}token=${encodeURIComponent(token)}`
  }, [convertProxyUrl])
```

- [ ] **Step 2: Remove websitePreviewUrl → convertProxyUrl effect (~line 252-256)**

Remove:
```typescript
  useEffect(() => {
    if (chatHook.websitePreviewUrl) {
      setConvertProxyUrl(chatHook.websitePreviewUrl)
    }
  }, [chatHook.websitePreviewUrl])
```

- [ ] **Step 3: Remove URL-wrapper params pickup effect (~line 618-630)**

Remove the `project_id`/`proxy_url` search params handling:
```typescript
    const projectId = searchParams.get("project_id")
    const proxyUrl = searchParams.get("proxy_url")
    if (projectId && proxyUrl) {
      setUrlWrapperProjectId(projectId)
      setConvertProxyUrl(proxyUrl)
      setViewMode("preview")
      injectionChat.fetchInjections()
      return
    }
```

- [ ] **Step 4: Simplify handleSendMessage — remove url_wrapper branch (~line 812-841)**

The intent classification block (~line 813-841) handles the `url_wrapper` case which no longer exists. Since the backend always returns `from_scratch`, this classification step is unnecessary on mobile. Remove the entire `if (isMobile && !activeApp && !convertUrl && !urlWrapperProjectId)` block — mobile and desktop now follow the same path.

- [ ] **Step 5: Remove all urlWrapperProjectId/injectionChat/convertProxyUrl references in JSX and computed values**

Search the file for all remaining references to:
- `urlWrapperProjectId`
- `injectionChat`
- `convertProxyUrl`
- `authedProxyUrl`
- `setConvertProxyUrl`
- `setUrlWrapperProjectId`

Remove or simplify each reference. Key locations:
- `mobileConfigId` (~line 279): change from `urlWrapperProjectId || appId` to just `appId`
- Message list computation (~line 1466-1482): remove `urlWrapperProjectId` branch
- Streaming text computation (~line 1491-1493): remove `urlWrapperProjectId` branch
- Send handler (~line 1497-1526): remove `urlWrapperProjectId` branch
- `onProxyUrlChange` prop (~line 1670): remove
- `hasActiveApp` checks: remove `|| !!urlWrapperProjectId`
- `isStreaming` checks: remove `|| (urlWrapperProjectId ? injectionChat.isStreaming : false)`
- iframe ref callback (~line 1792): remove `injectionChat.setIframeElement`

- [ ] **Step 6: Verify build**

```bash
cd ../wemob-dashboard && npx next build 2>&1 | head -50
```

Or at minimum ensure no TypeScript errors:
```bash
cd ../wemob-dashboard && npx tsc --noEmit 2>&1 | head -50
```

- [ ] **Step 7: Commit**

```bash
cd ../wemob-dashboard
git add components/kokonutui/pages/app-studio-page.tsx
git commit -m "chore: remove URL-wrapper/proxy/injection flow from app studio"
```

---

### Task 8: Clean up frontend — remove proxy URLs from create-app-page and app-edit-page

**Files:**
- Modify: `../wemob-dashboard/components/kokonutui/pages/create-app-page.tsx`
- Modify: `../wemob-dashboard/components/kokonutui/pages/app-edit-page.tsx`

These pages use `proxy.wemob.io` to preview user websites during the URL-wrapper creation/edit flow. Since URL-wrapper flow is dead, these proxy URL constructions are dead code.

- [ ] **Step 1: Clean create-app-page.tsx**

Replace all `https://proxy.wemob.io/?url=${encodeURIComponent(...)}` with the direct URL. There are 4 occurrences:

1. `handleNavbarItemClick` (~line 340): Change `setCurrentIframeUrl(proxyUrl)` to `setCurrentIframeUrl(targetUrl)`
2. `useEffect` when websiteUrl changes (~line 596): Change to `setCurrentIframeUrl(websiteUrl)`
3. Duplicate `useEffect` (~line 748): Change to `setCurrentIframeUrl(websiteUrl)`
4. iframe src fallback (~line 1373): Change to `src={currentIframeUrl || websiteUrl}`

- [ ] **Step 2: Clean app-edit-page.tsx**

Replace the proxy URL construction (~line 504-506):
```typescript
      // CHANGE: Replace API_BASE_URL/v1/proxy with proxy.wemob.io
      const proxyUrl = `https://proxy.wemob.io/?url=${encodeURIComponent(websiteUrl)}`
      setCurrentIframeUrl(proxyUrl)
```
with:
```typescript
      setCurrentIframeUrl(websiteUrl)
```

- [ ] **Step 3: Verify build**

```bash
cd ../wemob-dashboard && npx tsc --noEmit 2>&1 | head -50
```

- [ ] **Step 4: Commit**

```bash
cd ../wemob-dashboard
git add components/kokonutui/pages/create-app-page.tsx components/kokonutui/pages/app-edit-page.tsx
git commit -m "chore: replace proxy URLs with direct URLs in create/edit pages"
```

---

### Task 9: Clean up frontend — remove mobile-url-creation-panel proxy references

**Files:**
- Modify: `../wemob-dashboard/components/app-studio/mobile-url-creation-panel.tsx`

- [ ] **Step 1: Remove onProxyUrlChange prop and proxy URL construction**

Remove `onProxyUrlChange` from the interface:
```typescript
  onProxyUrlChange: (url: string | null) => void
```

Remove from the destructured props:
```typescript
  onProxyUrlChange,
```

Remove the useEffect that builds proxy URLs (~lines 70-76):
```typescript
  useEffect(() => {
    if (isValidUrl(websiteUrl)) {
      onProxyUrlChange(`https://proxy.wemob.io/?url=${encodeURIComponent(websiteUrl)}`)
    } else {
      onProxyUrlChange(null)
    }
  }, [websiteUrl, onProxyUrlChange])
```

Remove the proxy URL in the submit handler (~line 199):
```typescript
        onProxyUrlChange(`https://proxy.wemob.io/?url=${encodeURIComponent(websiteUrl)}&project_id=${response.project_id}`)
```

- [ ] **Step 2: Update callers**

Find any callers passing `onProxyUrlChange` prop and remove it. The main caller is in `app-studio-page.tsx` (~line 1670) — already cleaned in Task 7.

- [ ] **Step 3: Verify build**

```bash
cd ../wemob-dashboard && npx tsc --noEmit 2>&1 | head -50
```

- [ ] **Step 4: Commit**

```bash
cd ../wemob-dashboard
git add components/app-studio/mobile-url-creation-panel.tsx
git commit -m "chore: remove proxy URL construction from mobile-url-creation-panel"
```

---

### Task 10: Clean up misc references

**Files:**
- Modify: `.claude/agents/security-reviewer.md` (if it exists)
- Delete: `err.txt` (contains old proxy error logs)

- [ ] **Step 1: Remove proxy references from .claude/agents/security-reviewer.md**

Remove lines referencing:
- "Proxy server on port 9001 proxies web content"
- "### Proxy Server (port 9001)" section

- [ ] **Step 2: Delete err.txt**

```bash
rm err.txt
```

- [ ] **Step 3: Final full build verification**

```bash
go build -o /dev/null cmd/main.go
```

- [ ] **Step 4: Commit**

```bash
git add -A .claude/agents/security-reviewer.md err.txt
git commit -m "chore: remove proxy references from agent docs and clean up err.txt"
```

---

### Task 11: Final verification

- [ ] **Step 1: Grep for any remaining proxy references in backend**

```bash
grep -rn "proxy\.wemob\|ProxyHandler\|InitProxyTransport\|:9001\|proxyServer\|proxyRoute\|MobileInjection\|mobile_injection\|InjectionStreamParser\|MobileConversation\|mobile_conversation\|MOBILE_INJECTION\|SessionCookieName\|PREVIEW_PROXY" --include="*.go" .
```

Expected: No matches.

- [ ] **Step 2: Grep for any remaining proxy references in frontend**

```bash
grep -rn "proxy\.wemob\|useMobileInjectionChat\|chatMobileInjection\|getMobileInjections\|updateMobileInjection\|deleteMobileInjection\|clearMobileConversation\|urlWrapperProjectId\|convertProxyUrl\|onProxyUrlChange\|injection_applied\|injection_removed" --include="*.ts" --include="*.tsx" ../wemob-dashboard/
```

Expected: No matches.

- [ ] **Step 3: Build both projects**

```bash
go build -o /dev/null cmd/main.go
cd ../wemob-dashboard && npx tsc --noEmit
```

- [ ] **Step 4: Commit any remaining fixes if needed**
