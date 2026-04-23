import { NextRequest, NextResponse } from "next/server";
import { cookies } from "next/headers";
import { and, eq, isNull, or, gte, sql } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { getAdapter } from "@/lib/payments";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

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

  const [cfg] = await db.select().from(schema.config).limit(1);
  if (!cfg) return NextResponse.json({ error: "no_config" }, { status: 500 });

  const [cart] = await db
    .select()
    .from(schema.carts)
    .where(eq(schema.carts.sessionId, sessionId));
  if (!cart) return NextResponse.json({ error: "cart_not_found" }, { status: 404 });

  const items = await db
    .select({
      quantity: schema.cartItems.quantity,
      priceAtAddCents: schema.cartItems.priceAtAddCents,
      productId: schema.cartItems.productId,
      productName: schema.products.name,
      trackInventory: schema.products.trackInventory,
      inventory: schema.products.inventory,
    })
    .from(schema.cartItems)
    .innerJoin(schema.products, eq(schema.cartItems.productId, schema.products.id))
    .where(eq(schema.cartItems.cartId, cart.id));

  if (items.length === 0) return NextResponse.json({ error: "empty_cart" }, { status: 400 });

  for (const item of items) {
    if (item.quantity <= 0) {
      return NextResponse.json(
        { error: "invalid_quantity", productName: item.productName },
        { status: 400 },
      );
    }
  }

  const subtotal = items.reduce(
    (sum, i) => sum + Number(i.priceAtAddCents) * i.quantity,
    0,
  );

  // --- Discount re-validation -------------------------------------------
  //
  // The bug prior to this was only checking is_active. Expired / exhausted /
  // soft-deleted coupons still got applied at checkout because the
  // /api/discount apply path let them through earlier and nothing caught up.
  // Re-verifying against the live row here means the apply path can be as
  // permissive as it needs to be — final word is always this block.
  let discountCents = 0;
  if (cart.discountId) {
    const now = new Date();
    const [discount] = await db
      .select()
      .from(schema.discounts)
      .where(
        and(
          eq(schema.discounts.id, cart.discountId),
          eq(schema.discounts.isActive, true),
          isNull(schema.discounts.deletedAt),
        ),
      );
    if (discount) {
      const expired =
        (discount.startsAt && discount.startsAt > now) ||
        (discount.endsAt && discount.endsAt < now);
      const exhausted =
        discount.maxUses > 0 && discount.usedCount >= discount.maxUses;
      const minOrder = Number(discount.minOrderCents ?? 0);
      const belowMin = subtotal < minOrder;

      if (!expired && !exhausted && !belowMin) {
        if (discount.type === "percentage") {
          discountCents = Math.min(
            Math.floor((subtotal * Number(discount.value)) / 100),
            subtotal,
          );
        } else if (discount.type === "fixed") {
          discountCents = Math.min(Number(discount.value), subtotal);
        }
      }
    }
  }

  const taxableBase = subtotal - discountCents;
  const taxCents = Math.floor((taxableBase * Number(cfg.taxRate)) / 100);
  const shippingCents = 0;
  const totalCents = taxableBase + taxCents + shippingCents;

  // --- Inventory decrement ----------------------------------------------
  //
  // Conditional UPDATE per line item. For track_inventory=true rows we
  // require inventory >= quantity; for track_inventory=false rows the
  // UPDATE is a cheap no-op via CASE WHEN. A successful UPDATE returns one
  // row; a failed one (insufficient stock, product gone) returns none.
  //
  // If any decrement fails mid-loop we compensate the earlier ones before
  // bailing, so we don't phantom-reserve stock. This isn't a full
  // interactive transaction — neon-http doesn't support those — but the
  // conditional WHERE prevents negative inventory under concurrent
  // checkouts, and a worst-case concurrent race oversells by a single
  // unit rather than an unbounded amount.
  const decrementedTrackedIds: { id: string; qty: number }[] = [];
  for (const item of items) {
    const [updated] = await db
      .update(schema.products)
      .set({
        inventory: sql`CASE WHEN ${schema.products.trackInventory} THEN ${schema.products.inventory} - ${item.quantity} ELSE ${schema.products.inventory} END`,
        updatedAt: new Date(),
      })
      .where(
        and(
          eq(schema.products.id, item.productId),
          isNull(schema.products.deletedAt),
          or(
            eq(schema.products.trackInventory, false),
            gte(schema.products.inventory, item.quantity),
          ),
        ),
      )
      .returning({
        id: schema.products.id,
        trackInventory: schema.products.trackInventory,
      });

    if (!updated) {
      await compensateInventory(decrementedTrackedIds);
      return NextResponse.json(
        { error: "insufficient_stock", productName: item.productName },
        { status: 409 },
      );
    }
    if (updated.trackInventory) {
      decrementedTrackedIds.push({ id: item.productId, qty: item.quantity });
    }
  }

  // --- Order + line items -----------------------------------------------
  const orderNumber = generateOrderNumber();
  const [order] = await db
    .insert(schema.orders)
    .values({
      orderNumber,
      customerEmail: body.email,
      customerName: body.name,
      shippingAddress: body.shippingAddress as unknown as Record<string, unknown>,
      billingAddress: (body.billingAddress ?? body.shippingAddress) as unknown as Record<string, unknown>,
      subtotalCents: subtotal,
      discountCents,
      taxCents,
      shippingCents,
      totalCents,
      currency: cfg.currency,
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

  // discounts.used_count used to be incremented here; it now moves to the
  // webhook on the pending → paid transition so abandoned checkouts don't
  // drain the coupon. See lib/payments/apply-outcome.ts.

  // --- Payment session --------------------------------------------------
  let result;
  try {
    const adapter = getAdapter(body.provider);
    result = await adapter.createSession(order, cfg);
  } catch (err) {
    // Adapter failed — cancel the order and put inventory back so the
    // customer can retry without leaking stock.
    await db
      .update(schema.orders)
      .set({ status: "cancelled", paymentStatus: "failed" })
      .where(eq(schema.orders.id, order.id));
    await compensateInventory(decrementedTrackedIds);
    return NextResponse.json(
      { error: "payment_init_failed", message: (err as Error).message },
      { status: 502 },
    );
  }

  // COD has no upstream settlement. Flip the order to "processing" so the
  // merchant's orders view surfaces it as ready to fulfil, but keep
  // payment_status=pending — cash hasn't been collected yet. used_count
  // for COD discounts ticks here because there's no webhook to do it.
  if (result.kind === "placed") {
    await db
      .update(schema.orders)
      .set({ status: "processing" })
      .where(eq(schema.orders.id, order.id));

    if (order.discountId && discountCents > 0) {
      await db
        .update(schema.discounts)
        .set({ usedCount: sql`${schema.discounts.usedCount} + 1` })
        .where(eq(schema.discounts.id, order.discountId));
    }
  }

  // Clear the cart after session init. Stripe/Iyzico are about to redirect
  // the customer away so the cart rarely matters; COD has placed the order;
  // the success page reads the order directly, not the cart.
  await db.delete(schema.cartItems).where(eq(schema.cartItems.cartId, cart.id));
  await db.delete(schema.carts).where(eq(schema.carts.id, cart.id));

  return NextResponse.json({ orderId: order.id, ...result });
}

async function compensateInventory(
  entries: { id: string; qty: number }[],
): Promise<void> {
  // Only tracked products reach this list, so the increment is unconditional.
  // We accept that a Postgres failure leaves inventory short; callers treat
  // the checkout attempt as failed regardless.
  for (const e of entries) {
    try {
      await db
        .update(schema.products)
        .set({
          inventory: sql`${schema.products.inventory} + ${e.qty}`,
          updatedAt: new Date(),
        })
        .where(eq(schema.products.id, e.id));
    } catch {
      // Swallow — we're in a failure path already. Better to lose
      // observability on the compensation than to hide the original error.
    }
  }
}
