import { NextRequest, NextResponse } from "next/server";
import { cookies, headers } from "next/headers";
import { randomUUID } from "crypto";
import { and, desc, eq, isNull } from "drizzle-orm";
import { db, schema } from "@/lib/db";

const CART_SESSION_COOKIE = "wemob_cart_session";

// 30 days — shared lifetime with the cart session cookie set by
// /api/cart so a returning visitor sees both cart and wishlist intact.
const CART_SESSION_MAX_AGE = 60 * 60 * 24 * 30;

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

// Read-or-mint the session id. Setting the cookie is deferred to the
// response via stampSession() below — relying on cookies().set() inside a
// Route Handler leaves the Set-Cookie header up to Next's implicit merge,
// which has silently failed in prod builds. See app/api/cart/route.ts for
// the full rationale; keep both files in sync.
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

// GET /api/wishlist — return wishlist items for the cookie session joined to
// product data (name, slug, image, price) so a client wishlist page can
// render in a single round-trip. Soft-deleted and archived products are
// filtered out so customers don't see dead entries.
export async function GET() {
  const jar = await cookies();
  const sid = jar.get(CART_SESSION_COOKIE)?.value;
  if (!sid) return NextResponse.json([]);

  const rows = await db
    .select({
      id: schema.wishlists.id,
      productId: schema.wishlists.productId,
      createdAt: schema.wishlists.createdAt,
      productName: schema.products.name,
      productSlug: schema.products.slug,
      productImages: schema.products.images,
      productPriceInCents: schema.products.priceInCents,
      productInventory: schema.products.inventory,
      productTrackInventory: schema.products.trackInventory,
    })
    .from(schema.wishlists)
    .innerJoin(
      schema.products,
      and(
        eq(schema.wishlists.productId, schema.products.id),
        eq(schema.products.status, "active"),
        isNull(schema.products.deletedAt),
      ),
    )
    .where(eq(schema.wishlists.sessionId, sid))
    .orderBy(desc(schema.wishlists.createdAt));

  const items = rows.map((w) => {
    const imgs = (w.productImages as string[] | null) ?? [];
    return {
      id: w.id,
      product_id: w.productId,
      created_at: w.createdAt.toISOString(),
      product_name: w.productName,
      product_slug: w.productSlug,
      product_image: imgs.length > 0 ? imgs[0] : "",
      price_cents: w.productPriceInCents,
      in_stock: !w.productTrackInventory || w.productInventory > 0,
    };
  });

  return NextResponse.json(items);
}

// POST /api/wishlist — add a product to the wishlist. Accepts both
// `productId` and legacy `product_id` in the body to avoid breaking callers.
export async function POST(req: NextRequest) {
  const body = (await req.json()) as {
    productId?: string;
    product_id?: string;
  };
  const productId = body.productId ?? body.product_id;
  if (!productId) {
    return NextResponse.json(
      { error: "missing_product_id" },
      { status: 400 },
    );
  }

  // Reject archived/soft-deleted products. The FK alone would catch fully
  // deleted rows but not status != "active".
  const [product] = await db
    .select({ id: schema.products.id })
    .from(schema.products)
    .where(
      and(
        eq(schema.products.id, productId),
        eq(schema.products.status, "active"),
        isNull(schema.products.deletedAt),
      ),
    );
  if (!product) {
    return NextResponse.json({ error: "product_not_found" }, { status: 404 });
  }

  const sid = await resolveSessionId();

  // Idempotent add: upsert via unique (session_id, product_id).
  await db
    .insert(schema.wishlists)
    .values({ productId, sessionId: sid })
    .onConflictDoNothing();

  const response = NextResponse.json({ ok: true });
  stampSession(response, sid);
  return response;
}
