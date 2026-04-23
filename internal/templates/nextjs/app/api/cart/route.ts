import { NextRequest, NextResponse } from "next/server";
import { cookies } from "next/headers";
import { randomUUID } from "crypto";
import { and, eq } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { getCart as getCartServer } from "@/lib/wemob-store-server";

const CART_SESSION_COOKIE = "wemob_cart_session";

async function ensureStore() {
  const [store] = await db.select().from(schema.stores).limit(1);
  if (!store) throw new Error("Store not provisioned");
  return store;
}

async function ensureCart(storeId: string, sessionId: string) {
  let [cart] = await db
    .select()
    .from(schema.carts)
    .where(
      and(eq(schema.carts.storeId, storeId), eq(schema.carts.sessionId, sessionId)),
    );
  if (!cart) {
    [cart] = await db
      .insert(schema.carts)
      .values({ storeId, sessionId })
      .returning();
  }
  return cart;
}

async function getOrSetSession(): Promise<string> {
  const jar = await cookies();
  let sid = jar.get(CART_SESSION_COOKIE)?.value;
  if (!sid) {
    sid = randomUUID();
    jar.set(CART_SESSION_COOKIE, sid, {
      httpOnly: true,
      sameSite: "lax",
      path: "/",
    });
  }
  return sid;
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

  const store = await ensureStore();
  const sid = await getOrSetSession();
  const cart = await ensureCart(store.id, sid);

  const [product] = await db
    .select()
    .from(schema.products)
    .where(eq(schema.products.id, body.productId));
  if (!product) {
    return NextResponse.json({ error: "product_not_found" }, { status: 404 });
  }

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
  return NextResponse.json(refreshed);
}
