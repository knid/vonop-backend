import { NextRequest, NextResponse } from "next/server";
import { cookies } from "next/headers";
import { and, eq, isNull } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { getCart as getCartServer } from "@/lib/wemob-store-server";

const CART_SESSION_COOKIE = "wemob_cart_session";

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

  const now = new Date();
  const [discount] = await db
    .select()
    .from(schema.discounts)
    .where(
      and(
        eq(schema.discounts.code, code),
        eq(schema.discounts.isActive, true),
        isNull(schema.discounts.deletedAt),
      ),
    );

  if (!discount) {
    return NextResponse.json({ error: "invalid_code" }, { status: 404 });
  }
  if (discount.startsAt && discount.startsAt > now) {
    return NextResponse.json({ error: "not_yet_valid" }, { status: 400 });
  }
  if (discount.endsAt && discount.endsAt < now) {
    return NextResponse.json({ error: "expired" }, { status: 400 });
  }
  if (discount.maxUses > 0 && discount.usedCount >= discount.maxUses) {
    return NextResponse.json({ error: "exhausted" }, { status: 400 });
  }

  await db
    .update(schema.carts)
    .set({ discountId: discount.id, updatedAt: new Date() })
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
