import { NextRequest, NextResponse } from "next/server";
import { cookies } from "next/headers";
import { eq } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { resolveDiscountByCode } from "@/lib/discounts";
import { getCart as getCartServer } from "@/lib/wemob-store-server";

const CART_SESSION_COOKIE = "wemob_cart_session";

const INVALID_REASON_STATUS: Record<string, number> = {
  not_found: 404,
  not_yet_valid: 400,
  expired: 400,
  exhausted: 400,
  below_minimum: 400,
};

async function getCartForSession() {
  const jar = await cookies();
  const sid = jar.get(CART_SESSION_COOKIE)?.value;
  if (!sid) return { cart: null, sid: null };

  const [cart] = await db
    .select()
    .from(schema.carts)
    .where(eq(schema.carts.sessionId, sid));
  return { cart: cart ?? null, sid };
}

// POST /api/discount — apply a discount code to the cookie's cart.
// Returns the refreshed StorefrontCart so the CartProvider can replace state.
export async function POST(req: NextRequest) {
  const { code } = (await req.json()) as { code?: string };
  if (!code) {
    return NextResponse.json({ error: "missing_code" }, { status: 400 });
  }

  const { cart, sid } = await getCartForSession();
  if (!cart || !sid) {
    return NextResponse.json({ error: "no_cart" }, { status: 404 });
  }

  // Subtotal drives the min-order check inside resolveDiscountByCode.
  const itemRows = await db
    .select({
      qty: schema.cartItems.quantity,
      price: schema.cartItems.priceAtAddCents,
    })
    .from(schema.cartItems)
    .where(eq(schema.cartItems.cartId, cart.id));
  const subtotal = itemRows.reduce(
    (acc, r) => acc + Number(r.price) * r.qty,
    0,
  );

  const result = await resolveDiscountByCode(code, subtotal);
  if (result.kind === "invalid") {
    const status = INVALID_REASON_STATUS[result.reason] ?? 400;
    const errorKey = result.reason === "not_found" ? "invalid_code" : result.reason;
    return NextResponse.json({ error: errorKey }, { status });
  }

  await db
    .update(schema.carts)
    .set({ discountId: result.discount.id, updatedAt: new Date() })
    .where(eq(schema.carts.id, cart.id));

  const refreshed = await getCartServer(sid);
  return NextResponse.json(refreshed);
}

// DELETE /api/discount — clear the applied discount on the cookie's cart.
// Returns the refreshed StorefrontCart.
export async function DELETE() {
  const { cart, sid } = await getCartForSession();
  if (!cart || !sid) {
    return NextResponse.json({ error: "no_cart" }, { status: 404 });
  }

  await db
    .update(schema.carts)
    .set({ discountId: null, updatedAt: new Date() })
    .where(eq(schema.carts.id, cart.id));

  const refreshed = await getCartServer(sid);
  return NextResponse.json(refreshed);
}
