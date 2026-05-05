import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

// CORS for /api/*. Native iOS/Android builds don't send an Origin header,
// so they bypass CORS entirely. The studio's web preview iframe runs on
// *.preview.wemob.app while the deployed backend lives at {slug}.wemob.app,
// which the browser treats as cross-origin and gates with a preflight.
// Without this middleware, Next.js' default OPTIONS responder returns 204
// with no Access-Control-* headers and the preflight fails.

const ALLOWED_HOST_SUFFIXES = [".wemob.app", "localhost"];
const DEFAULT_ALLOW_METHODS = "GET, POST, PUT, PATCH, DELETE, OPTIONS";
const DEFAULT_ALLOW_HEADERS = "content-type, authorization";

function isAllowedOrigin(origin: string | null): boolean {
  if (!origin) return false;
  let url: URL;
  try {
    url = new URL(origin);
  } catch {
    return false;
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") return false;
  const host = url.hostname;
  return ALLOWED_HOST_SUFFIXES.some((suffix) => {
    const bare = suffix.startsWith(".") ? suffix.slice(1) : suffix;
    return host === bare || host.endsWith(suffix);
  });
}

export function middleware(req: NextRequest) {
  const origin = req.headers.get("origin");
  const allowed = isAllowedOrigin(origin);

  if (req.method === "OPTIONS") {
    const res = new NextResponse(null, { status: 204 });
    if (allowed && origin) {
      res.headers.set("Access-Control-Allow-Origin", origin);
      res.headers.set("Vary", "Origin");
      res.headers.set(
        "Access-Control-Allow-Methods",
        req.headers.get("access-control-request-method") ?? DEFAULT_ALLOW_METHODS,
      );
      res.headers.set(
        "Access-Control-Allow-Headers",
        req.headers.get("access-control-request-headers") ?? DEFAULT_ALLOW_HEADERS,
      );
      res.headers.set("Access-Control-Allow-Credentials", "true");
      res.headers.set("Access-Control-Max-Age", "86400");
    }
    return res;
  }

  const res = NextResponse.next();
  if (allowed && origin) {
    res.headers.set("Access-Control-Allow-Origin", origin);
    res.headers.append("Vary", "Origin");
    res.headers.set("Access-Control-Allow-Credentials", "true");
  }
  return res;
}

export const config = {
  matcher: "/api/:path*",
};
