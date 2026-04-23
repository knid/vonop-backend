import { NextRequest, NextResponse } from "next/server";
import { cookies } from "next/headers";
import { randomUUID } from "crypto";
import { desc, eq } from "drizzle-orm";
import { db, schema } from "@/lib/db";

const CART_SESSION_COOKIE = "wemob_cart_session";

// 30 days — shared lifetime with the cart session cookie set by
// /api/cart so a returning visitor sees both cart and wishlist intact.
const CART_SESSION_MAX_AGE = 60 * 60 * 24 * 30;

async function ensureSession(): Promise<string> {
  const jar = await cookies();
  let sid = jar.get(CART_SESSION_COOKIE)?.value;
  if (!sid) {
    sid = randomUUID();
    jar.set(CART_SESSION_COOKIE, sid, {
      httpOnly: true,
      sameSite: "lax",
      path: "/",
      maxAge: CART_SESSION_MAX_AGE,
    });
  }
  return sid;
}

// GET /api/wishlist — return wishlist items for the cookie session, shaped
// as `WishlistItem[]` (snake_case product_id) so the existing useWishlist
// hook can consume it unchanged.
export async function GET() {
  const jar = await cookies();
  const sid = jar.get(CART_SESSION_COOKIE)?.value;
  if (!sid) return NextResponse.json([]);

  const rows = await db
    .select()
    .from(schema.wishlists)
    .where(eq(schema.wishlists.sessionId, sid))
    .orderBy(desc(schema.wishlists.createdAt));

  const items = rows.map((w) => ({
    id: w.id,
    product_id: w.productId,
    created_at: w.createdAt.toISOString(),
  }));

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

  const sid = await ensureSession();

  // Idempotent add: upsert via unique (session_id, product_id).
  await db
    .insert(schema.wishlists)
    .values({ productId, sessionId: sid })
    .onConflictDoNothing();

  return NextResponse.json({ ok: true });
}
