import { NextRequest, NextResponse } from "next/server";
import { cookies } from "next/headers";
import { and, eq, sql } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { getAdapter } from "@/lib/payments";

const CART_SESSION_COOKIE = "wemob_cart_session";

type CheckoutBody = {
  provider: string;      // "stripe" | "iyzico" | "cod"
  email: string;
  name: string;
  shippingAddress: Record<string, unknown>;
  billingAddress?: Record<string, unknown>;
};

function generateOrderNumber(): string {
  const ts = Date.now().toString(36).toUpperCase();
  const rand = Math.random().toString(36).slice(2, 6).toUpperCase();
  return `W-${ts}-${rand}`;
}

export async function POST(req: NextRequest) {
  const body = (await req.json()) as CheckoutBody;
  if (!body.provider || !body.email || !body.shippingAddress) {
    return NextResponse.json({ error: "missing_fields" }, { status: 400 });
  }

  const jar = await cookies();
  const sessionId = jar.get(CART_SESSION_COOKIE)?.value;
  if (!sessionId) return NextResponse.json({ error: "no_cart_session" }, { status: 400 });

  const [store] = await db.select().from(schema.stores).limit(1);
  if (!store) return NextResponse.json({ error: "no_store" }, { status: 500 });

  const [cart] = await db
    .select()
    .from(schema.carts)
    .where(and(eq(schema.carts.storeId, store.id), eq(schema.carts.sessionId, sessionId)));
  if (!cart) return NextResponse.json({ error: "cart_not_found" }, { status: 404 });

  const items = await db
    .select({
      quantity: schema.cartItems.quantity,
      priceAtAddCents: schema.cartItems.priceAtAddCents,
      productId: schema.cartItems.productId,
      productName: schema.products.name,
    })
    .from(schema.cartItems)
    .innerJoin(schema.products, eq(schema.cartItems.productId, schema.products.id))
    .where(eq(schema.cartItems.cartId, cart.id));

  if (items.length === 0) return NextResponse.json({ error: "empty_cart" }, { status: 400 });

  // Compute totals. This mirrors the server-side cart discount logic in
  // lib/wemob-store-server.ts but recomputed here because we also need
  // tax/shipping for the order row (cart responses omit them).
  const subtotal = items.reduce((sum, i) => sum + Number(i.priceAtAddCents) * i.quantity, 0);

  let discountCents = 0;
  if (cart.discountId) {
    const [discount] = await db
      .select()
      .from(schema.discounts)
      .where(and(eq(schema.discounts.id, cart.discountId), eq(schema.discounts.isActive, true)));
    if (discount) {
      const minOrder = discount.minOrderCents != null ? Number(discount.minOrderCents) : 0;
      if (subtotal >= minOrder) {
        if (discount.type === "percentage") {
          discountCents = Math.min(Math.floor((subtotal * Number(discount.value)) / 100), subtotal);
        } else if (discount.type === "fixed") {
          discountCents = Math.min(Number(discount.value), subtotal);
        }
      }
    }
  }

  // Tax + shipping come from store config. For PR 3 keep it simple: tax is a
  // flat percentage of the discounted subtotal; shipping is 0 (free). Stores
  // that want richer logic can extend this route later.
  const taxableBase = subtotal - discountCents;
  const taxCents = Math.floor((taxableBase * Number(store.taxRate)) / 100);
  const shippingCents = 0;
  const totalCents = taxableBase + taxCents + shippingCents;

  const orderNumber = generateOrderNumber();
  const [order] = await db
    .insert(schema.orders)
    .values({
      storeId: store.id,
      orderNumber,
      customerEmail: body.email,
      customerName: body.name,
      shippingAddress: body.shippingAddress as any,
      billingAddress: (body.billingAddress ?? body.shippingAddress) as any,
      subtotalCents: subtotal,
      discountCents,
      taxCents,
      shippingCents,
      totalCents,
      currency: store.currency,
      status: "pending",
      paymentStatus: "pending",
      paymentProvider: body.provider,
      discountId: cart.discountId,
    })
    .returning();

  for (const item of items) {
    await db.insert(schema.orderItems).values({
      orderId: order.id,
      productId: item.productId,
      productName: item.productName,
      quantity: item.quantity,
      priceInCents: Number(item.priceAtAddCents),
      totalCents: Number(item.priceAtAddCents) * item.quantity,
    });
  }

  // Bump the discount's used_count atomically via a SQL expression so we
  // don't race against concurrent checkouts using the same coupon.
  if (cart.discountId && discountCents > 0) {
    await db
      .update(schema.discounts)
      .set({ usedCount: sql`${schema.discounts.usedCount} + 1` })
      .where(eq(schema.discounts.id, cart.discountId));
  }

  let result;
  try {
    const adapter = getAdapter(body.provider);
    result = await adapter.createSession(order, store);
  } catch (err) {
    // Adapter failed — rollback order status so the client can retry.
    await db
      .update(schema.orders)
      .set({ status: "cancelled", paymentStatus: "failed" })
      .where(eq(schema.orders.id, order.id));
    return NextResponse.json(
      { error: "payment_init_failed", message: (err as Error).message },
      { status: 502 },
    );
  }

  // Clear the cart on successful checkout init (user is being redirected or
  // the order is already placed for COD). If the payment ultimately fails the
  // webhook will mark the order accordingly; the user can place a new order.
  await db.delete(schema.cartItems).where(eq(schema.cartItems.cartId, cart.id));
  await db.delete(schema.carts).where(eq(schema.carts.id, cart.id));

  return NextResponse.json({ orderId: order.id, ...result });
}
