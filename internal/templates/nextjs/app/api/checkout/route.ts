import { NextRequest, NextResponse } from "next/server";
import { cookies } from "next/headers";
import { randomUUID } from "crypto";
import { and, eq, isNull, or, gte, sql } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { resolveDiscountById } from "@/lib/discounts";
import { getAdapter } from "@/lib/payments";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const CART_SESSION_COOKIE = "wemob_cart_session";

type CheckoutBody = {
  provider: string;      // "stripe" | "iyzico" | "cod"
  email: string;
  name: string;
  phone?: string;        // raw input; iyzico requires gsmNumber on buyer
  shippingAddress: Record<string, unknown>;
  billingAddress?: Record<string, unknown>;
  identityNumber?: string;  // 11-digit TC kimlik for iyzico
};

// Iyzico's checkoutFormInitialize rejects every call without buyer.gsmNumber,
// so we normalize the storefront-collected phone number to E.164 here and
// stash it on order.metadata for the adapter to read. Non-digits are
// stripped, a leading "00" is rewritten to "+", and a +90 prefix is added
// when the number looks like a Turkish 10-digit local number. Anything that
// doesn't end up matching E.164 returns "" — the iyzico adapter falls back
// to a placeholder so the API call still succeeds, at the cost of weaker
// fraud scoring on that transaction.
function normalizePhone(raw: string | undefined): string {
  if (!raw) return "";
  let s = raw.trim();
  if (s.startsWith("00")) s = "+" + s.slice(2);
  const hasPlus = s.startsWith("+");
  const digits = s.replace(/\D/g, "");
  if (digits.length === 0) return "";
  // Bare 10-digit Turkish format (5XXXXXXXXX) — assume +90.
  if (!hasPlus && digits.length === 10 && digits.startsWith("5")) {
    return "+90" + digits;
  }
  // Bare 11-digit with leading 0 (05XXXXXXXXX).
  if (!hasPlus && digits.length === 11 && digits.startsWith("05")) {
    return "+90" + digits.slice(1);
  }
  if (hasPlus && digits.length >= 8 && digits.length <= 15) {
    return "+" + digits;
  }
  return "";
}

// Collision-resistant order numbers. Math.random() collisions over a short
// window were possible on the previous 4-char base36 suffix and could crash
// checkout with a UNIQUE violation mid-flow.
function generateOrderNumber(): string {
  const ts = Date.now().toString(36).toUpperCase();
  const rand = randomUUID().replace(/-/g, "").slice(0, 8).toUpperCase();
  return `W-${ts}-${rand}`;
}

function resolveClientIp(req: NextRequest): string {
  const fwd = req.headers.get("x-forwarded-for")?.split(",")[0]?.trim();
  if (fwd) return fwd;
  return req.headers.get("x-real-ip")?.trim() ?? "";
}

// Shared with /api/reviews: lowercase + strip plus-addressing so mixed-case
// and `user+tag` variants of the same mailbox compare equal for the
// verified-purchase lookup.
function normalizeEmail(raw: string): string {
  const email = raw.trim().toLowerCase();
  const at = email.lastIndexOf("@");
  if (at < 1) return email;
  const local = email.slice(0, at);
  const domain = email.slice(at + 1);
  const plus = local.indexOf("+");
  const baseLocal = plus >= 0 ? local.slice(0, plus) : local;
  return `${baseLocal}@${domain}`;
}

export async function POST(req: NextRequest) {
  const body = (await req.json()) as CheckoutBody;
  if (!body.provider || !body.email || !body.shippingAddress) {
    return NextResponse.json({ error: "missing_fields" }, { status: 400 });
  }

  // Match the normalization used in /api/reviews so the verified-purchase
  // check finds orders for a customer who paid as `Buyer+tag@gmail.com` and
  // reviews as `buyer@gmail.com` (or vice versa). Lowercase + plus-strip;
  // both sides land on the same canonical form.
  const normalizedEmail = normalizeEmail(body.email);

  // Sweep abandoned pending orders. Cutoff is 45min to stay safely outside
  // Iyzico's 30min hosted-form lifetime + a buffer for delayed webhooks; a
  // tighter cutoff risks marking a paid-but-late-webhook order as failed.
  try {
    const cutoff = new Date(Date.now() - 45 * 60 * 1000);
    const stale = await db
      .select({ id: schema.orders.id })
      .from(schema.orders)
      .where(
        and(
          eq(schema.orders.paymentStatus, "pending"),
          sql`${schema.orders.createdAt} < ${cutoff}`,
        ),
      )
      .limit(50);

    for (const s of stale) {
      const updated = await db
        .update(schema.orders)
        .set({
          paymentStatus: "failed",
          status: "cancelled",
          updatedAt: new Date(),
        })
        .where(
          and(
            eq(schema.orders.id, s.id),
            eq(schema.orders.paymentStatus, "pending"),
          ),
        )
        .returning({ id: schema.orders.id });

      if (updated.length === 0) continue;

      const staleItems = await db
        .select({
          productId: schema.orderItems.productId,
          quantity: schema.orderItems.quantity,
        })
        .from(schema.orderItems)
        .where(eq(schema.orderItems.orderId, s.id));

      for (const item of staleItems) {
        await db
          .update(schema.products)
          .set({
            inventory: sql`CASE WHEN ${schema.products.trackInventory} THEN ${schema.products.inventory} + ${item.quantity} ELSE ${schema.products.inventory} END`,
            updatedAt: new Date(),
          })
          .where(eq(schema.products.id, item.productId));
      }
    }
  } catch (err) {
    console.warn("abandoned-order sweep failed", err);
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
      productStatus: schema.products.status,
      productDeletedAt: schema.products.deletedAt,
      trackInventory: schema.products.trackInventory,
      inventory: schema.products.inventory,
    })
    .from(schema.cartItems)
    .innerJoin(schema.products, eq(schema.cartItems.productId, schema.products.id))
    .where(eq(schema.cartItems.cartId, cart.id));

  if (items.length === 0) return NextResponse.json({ error: "empty_cart" }, { status: 400 });

  // Reject the checkout if any line item points at a product the merchant
  // archived or soft-deleted since it was added to the cart. Surfacing this
  // as a distinct error lets the client prompt the user to remove it rather
  // than present a misleading "insufficient stock" message.
  const unavailable = items.find(
    (i) => i.productDeletedAt !== null || i.productStatus !== "active",
  );
  if (unavailable) {
    return NextResponse.json(
      { error: "product_unavailable", productName: unavailable.productName },
      { status: 409 },
    );
  }

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

  // Discount re-validation: the apply path can be permissive, this block is
  // the final word. Shared rules live in lib/discounts.ts so every call site
  // gates on the same criteria (expiry, max_uses, min_order).
  let discountCents = 0;
  if (cart.discountId) {
    const resolution = await resolveDiscountById(cart.discountId, subtotal);
    if (resolution.kind === "applied") {
      discountCents = resolution.cents;
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
  //
  // neon-http has no interactive transactions (only db.batch, which can't
  // chain results — the order.id is needed for the order_items inserts), so
  // we can't atomically combine inventory decrement + order insert. The
  // window for orphaned reserved stock is squeezed as small as possible
  // here: we catch any throw from the order/items inserts and compensate
  // the inventory decrement before bubbling the error.
  const orderNumber = generateOrderNumber();
  // viewToken scopes /checkout/success and /checkout/cancel to the customer
  // who just paid — without it, anyone guessing or scraping an order UUID
  // could read another customer's name, email, address, and line items.
  const viewToken = randomUUID();
  let order: typeof schema.orders.$inferSelect;
  try {
    const [inserted] = await db
      .insert(schema.orders)
      .values({
        orderNumber,
        customerEmail: normalizedEmail,
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
        // cartId: applyPaymentOutcome drops the cart on pending→paid.
        // viewToken: gates /checkout/success + /checkout/cancel against IDOR.
        // phone: iyzico adapter reads this to populate buyer.gsmNumber.
        metadata: {
          cartId: cart.id,
          viewToken,
          ...(typeof body.identityNumber === "string" && /^\d{11}$/.test(body.identityNumber)
            ? { identityNumber: body.identityNumber }
            : {}),
          ...(normalizePhone(body.phone) ? { phone: normalizePhone(body.phone) } : {}),
        },
      })
      .returning();
    order = inserted;

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
  } catch (err) {
    await compensateInventory(decrementedTrackedIds);
    return NextResponse.json(
      { error: "order_insert_failed", message: (err as Error).message },
      { status: 500 },
    );
  }

  // discounts.used_count used to be incremented here; it now moves to the
  // webhook on the pending → paid transition so abandoned checkouts don't
  // drain the coupon. See lib/payments/apply-outcome.ts.

  // --- Payment session --------------------------------------------------
  const clientIp = resolveClientIp(req);
  let result;
  try {
    const adapter = getAdapter(body.provider);
    result = await adapter.createSession(order, cfg, { clientIp });
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
  // COD also clears the cart now (the order is committed to fulfilment);
  // async providers defer cart deletion to applyPaymentOutcome so a user
  // who cancels on the provider's page keeps their cart.
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

    await db.delete(schema.cartItems).where(eq(schema.cartItems.cartId, cart.id));
    await db.delete(schema.carts).where(eq(schema.carts.id, cart.id));
  }

  return NextResponse.json({ orderId: order.id, viewToken, ...result });
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
