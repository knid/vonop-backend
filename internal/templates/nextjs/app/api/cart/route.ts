import { NextRequest, NextResponse } from "next/server";
import { cookies, headers } from "next/headers";
import { randomUUID } from "crypto";
import { and, eq, isNull } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { getCart as getCartServer } from "@/lib/wemob-store-server";

const CART_SESSION_COOKIE = "wemob_cart_session";

function isLocalHostHost(host: string): boolean {
  return (
    host === "localhost" ||
    host === "127.0.0.1" ||
    host.startsWith("localhost:") ||
    host.startsWith("127.0.0.1:") ||
    host.startsWith("[::1]") ||
    host === "::1"
  );
}

function isSecureCookie(): boolean {
  const requestHeaders = headers();
  const xProto = requestHeaders.get("x-forwarded-proto")?.toLowerCase();
  if (xProto) {
    const proto = xProto.split(",")[0]?.trim();
    if (proto === "https" || proto === "http") return proto === "https";
  }

  const origin = requestHeaders.get("origin")?.toLowerCase();
  if (origin) {
    return origin.startsWith("https://");
  }

  const host = requestHeaders.get("host") ?? "";
  if (isLocalHostHost(host)) {
    return false;
  }

  return process.env.NODE_ENV === "production";
}

async function ensureCart(sessionId: string) {
  let [cart] = await db
    .select()
    .from(schema.carts)
    .where(eq(schema.carts.sessionId, sessionId));
  if (!cart) {
    [cart] = await db
      .insert(schema.carts)
      .values({ sessionId })
      .returning();
  }
  return cart;
}

// 30 days. Without maxAge the cookie is a session cookie and some browsers
// (iOS Safari evicting backgrounded tabs, private-mode quirks, full browser
// restarts) drop it even between simple refreshes — the cart then appears
// empty on reload because the server can't resolve the caller back to a
// cart row.
const CART_SESSION_MAX_AGE = 60 * 60 * 24 * 30;

// Read-or-mint the session id. Setting the cookie is deferred to the
// response via stampSession() below — relying on cookies().set() inside a
// Route Handler leaves the actual Set-Cookie header up to Next's implicit
// merge, which has silently failed in prod builds (Add-to-cart succeeds,
// cookie never reaches the browser, refresh shows an empty cart).
async function resolveSessionId(): Promise<string> {
  const jar = await cookies();
  return jar.get(CART_SESSION_COOKIE)?.value ?? randomUUID();
}

function stampSession(response: NextResponse, sid: string): void {
  response.cookies.set(CART_SESSION_COOKIE, sid, {
    httpOnly: true,
    sameSite: "lax",
    secure: isSecureCookie(),
    path: "/",
    maxAge: CART_SESSION_MAX_AGE,
  });
}

// GET /api/cart — returns the current cart + items for the cookie session
// in the same StorefrontCart shape produced by the server helper so client
// state management (CartProvider) can consume the response unchanged.
export async function GET() {
  const jar = await cookies();
  const sid = jar.get(CART_SESSION_COOKIE)?.value;
  if (!sid) {
    return NextResponse.json(null);
  }

  const cart = await getCartServer(sid);
  return NextResponse.json(cart);
}

// POST /api/cart — add a product to the cart. Body: { productId, quantity }
// If the product is already in the cart, increment quantity.
// Returns the refreshed StorefrontCart so the client can replace state.
export async function POST(req: NextRequest) {
  const body = (await req.json()) as { productId: string; quantity?: number };
  if (!body.productId) {
    return NextResponse.json({ error: "missing_product_id" }, { status: 400 });
  }
  const qty = body.quantity ?? 1;

  const sid = await resolveSessionId();

  const [product] = await db
    .select()
    .from(schema.products)
    .where(
      and(
        eq(schema.products.id, body.productId),
        eq(schema.products.status, "active"),
        isNull(schema.products.deletedAt),
      ),
    );
  if (!product) {
    // Covers unknown id, archived (status !== "active") and soft-deleted —
    // merging them is fine because the customer sees the same "that product
    // isn't available" story either way. Stamp the cookie anyway so the next
    // attempt against a valid product doesn't need to bootstrap from zero.
    const response = NextResponse.json(
      { error: "product_not_found" },
      { status: 404 },
    );
    stampSession(response, sid);
    return response;
  }

  const cart = await ensureCart(sid);

  // Merge with existing line item if present.
  const [existing] = await db
    .select()
    .from(schema.cartItems)
    .where(
      and(
        eq(schema.cartItems.cartId, cart.id),
        eq(schema.cartItems.productId, product.id),
      ),
    );

  if (existing) {
    await db
      .update(schema.cartItems)
      .set({ quantity: existing.quantity + qty, updatedAt: new Date() })
      .where(eq(schema.cartItems.id, existing.id));
  } else {
    await db.insert(schema.cartItems).values({
      cartId: cart.id,
      productId: product.id,
      quantity: qty,
      priceAtAddCents: Number(product.priceInCents),
    });
  }

  const refreshed = await getCartServer(sid);
  const response = NextResponse.json(refreshed);
  // Stamp on every POST — re-affirming the cookie extends maxAge for
  // returning visitors, and is the only path that guarantees Set-Cookie
  // actually lands in the response when the session is new.
  stampSession(response, sid);
  return response;
}
