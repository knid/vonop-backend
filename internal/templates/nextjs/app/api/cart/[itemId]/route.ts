import { NextRequest, NextResponse } from "next/server";
import { cookies } from "next/headers";
import { and, eq } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { getCart as getCartServer } from "@/lib/wemob-store-server";

const CART_SESSION_COOKIE = "wemob_cart_session";

// Ensure the cart item belongs to the cart owned by the session in the cookie.
// Prevents a client from mutating another session's cart by guessing an itemId.
async function scopeToSession(itemId: string): Promise<string | null> {
  const jar = await cookies();
  const sid = jar.get(CART_SESSION_COOKIE)?.value;
  if (!sid) return null;

  const [row] = await db
    .select({ sessionId: schema.carts.sessionId })
    .from(schema.cartItems)
    .innerJoin(schema.carts, eq(schema.cartItems.cartId, schema.carts.id))
    .where(eq(schema.cartItems.id, itemId));

  if (!row || row.sessionId !== sid) return null;
  return sid;
}

export async function PUT(
  req: NextRequest,
  { params }: { params: Promise<{ itemId: string }> },
) {
  const { itemId } = await params;
  const body = (await req.json()) as { quantity: number };
  if (typeof body.quantity !== "number" || body.quantity < 1) {
    return NextResponse.json({ error: "invalid_quantity" }, { status: 400 });
  }

  const sid = await scopeToSession(itemId);
  if (!sid) {
    return NextResponse.json({ error: "not_found" }, { status: 404 });
  }

  await db
    .update(schema.cartItems)
    .set({ quantity: body.quantity, updatedAt: new Date() })
    .where(eq(schema.cartItems.id, itemId));

  const refreshed = await getCartServer(sid);
  return NextResponse.json(refreshed);
}

export async function DELETE(
  _req: NextRequest,
  { params }: { params: Promise<{ itemId: string }> },
) {
  const { itemId } = await params;

  const sid = await scopeToSession(itemId);
  if (!sid) {
    return NextResponse.json({ error: "not_found" }, { status: 404 });
  }

  await db.delete(schema.cartItems).where(eq(schema.cartItems.id, itemId));

  const refreshed = await getCartServer(sid);
  return NextResponse.json(refreshed);
}
