import "server-only";
// lib/payments/apply-outcome.ts — Shared order-state machine for webhook
// deliveries. Both Stripe and Iyzico funnel here so the idempotency +
// discount-redemption rules live in exactly one place.
//
// Behaviour:
//   - "pending" status is a no-op.
//   - Once an order reaches "paid" or "failed" it is terminal; further
//     webhook deliveries (duplicates, retries, out-of-order events) do not
//     change paymentStatus. This is the fix for the bug where a late
//     payment_intent.payment_failed could demote an already-paid order.
//   - The discount's used_count is incremented only on the pending → paid
//     transition, never at checkout init. Abandoned checkouts no longer
//     drain coupons.

import { and, eq, sql } from "drizzle-orm";

import { db, schema } from "@/lib/db";

export type PaymentOutcomeStatus = "paid" | "failed" | "pending";

export interface PaymentOutcome {
  orderId: string;
  paymentId: string;
  status: PaymentOutcomeStatus;
}

export async function applyPaymentOutcome(input: PaymentOutcome): Promise<void> {
  if (input.status === "pending") return;

  const [order] = await db
    .select({
      id: schema.orders.id,
      paymentStatus: schema.orders.paymentStatus,
      discountId: schema.orders.discountId,
      discountCents: schema.orders.discountCents,
      metadata: schema.orders.metadata,
    })
    .from(schema.orders)
    .where(eq(schema.orders.id, input.orderId));
  if (!order) return;

  if (order.paymentStatus === "paid" || order.paymentStatus === "failed") {
    // Terminal state — ignore duplicate / out-of-order webhook deliveries.
    return;
  }

  const nextOrderStatus =
    input.status === "paid" ? "processing" : "cancelled";

  const updated = await db
    .update(schema.orders)
    .set({
      paymentStatus: input.status,
      paymentId: input.paymentId,
      status: nextOrderStatus,
      updatedAt: new Date(),
    })
    .where(
      and(
        eq(schema.orders.id, input.orderId),
        // Guard against a concurrent writer that already terminated the
        // order between our SELECT and UPDATE.
        eq(schema.orders.paymentStatus, "pending"),
      ),
    )
    .returning({ id: schema.orders.id });

  // If the CAS UPDATE affected 0 rows, another concurrent webhook won the
  // race and already committed a terminal status. Bail without doing the
  // downstream side-effects (discount increment, cart delete, inventory
  // restore) or the concurrent writer's side-effects get duplicated.
  if (updated.length === 0) return;

  if (
    input.status === "paid" &&
    order.discountId &&
    order.discountCents > 0
  ) {
    // Atomic increment so two concurrent paid webhooks for the same order
    // (belt and suspenders on top of the terminal-state check above) don't
    // race and under-count.
    await db
      .update(schema.discounts)
      .set({ usedCount: sql`${schema.discounts.usedCount} + 1` })
      .where(eq(schema.discounts.id, order.discountId));
  }

  if (input.status === "paid") {
    // Cart deletion is deferred from checkout init to here so a cancelled
    // payment leaves the cart intact. cartId was stashed on the order
    // metadata by /api/checkout. Cascade drops cart_items via FK.
    const cartId = (order.metadata as Record<string, unknown> | null)?.cartId;
    if (typeof cartId === "string" && cartId) {
      await db.delete(schema.carts).where(eq(schema.carts.id, cartId));
    }
  }

  if (input.status === "failed") {
    // Restore inventory reserved at checkout init for tracked products so a
    // cancelled/failed payment doesn't permanently drain stock. A repeat
    // checkout with the same cart would otherwise decrement inventory a
    // second time. Untracked products are a no-op (decrement was a
    // CASE-WHEN noop too). Best-effort: a DB error here leaves stock short
    // and is treated as a failure path we've already committed to.
    const items = await db
      .select({
        productId: schema.orderItems.productId,
        quantity: schema.orderItems.quantity,
      })
      .from(schema.orderItems)
      .where(eq(schema.orderItems.orderId, input.orderId));
    for (const item of items) {
      await db
        .update(schema.products)
        .set({
          inventory: sql`CASE WHEN ${schema.products.trackInventory} THEN ${schema.products.inventory} + ${item.quantity} ELSE ${schema.products.inventory} END`,
          updatedAt: new Date(),
        })
        .where(eq(schema.products.id, item.productId));
    }
  }
}
