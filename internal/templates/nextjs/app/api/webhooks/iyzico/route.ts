import { NextRequest, NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { getAdapter } from "@/lib/payments";
import { applyPaymentOutcome } from "@/lib/payments/apply-outcome";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  const rawBody = await req.text();

  let event;
  try {
    const adapter = getAdapter("iyzico");
    event = await adapter.handleWebhook(rawBody, req.headers);
  } catch (err) {
    // Iyzico's retrieve() call rejects foreign tokens because our merchant
    // secret can only decrypt our own sessions — that is the real signature
    // check. Surface verification failures as 400 so Iyzico can retry if
    // they chose to.
    return NextResponse.json({ error: (err as Error).message }, { status: 400 });
  }

  if (event.orderId) {
    await applyPaymentOutcome({
      orderId: event.orderId,
      paymentId: event.paymentId,
      status: event.status,
    });
  }

  // Iyzico posts the callback from the CUSTOMER'S browser (not server-to-
  // server), so we redirect the customer to the right Wemob page. If an
  // attacker posts here directly without going through a real payment,
  // applyPaymentOutcome above will no-op because their orderId (if any)
  // is either unknown or already terminal.
  //
  // The viewToken scopes the success/cancel page against IDOR (see
  // /api/checkout). We look it up from the order we just touched — the
  // customer's browser doesn't carry it across the iyzico round-trip.
  const siteUrl = process.env.NEXT_PUBLIC_SITE_URL ?? "";
  let tokenParam = "";
  if (event.orderId) {
    const [row] = await db
      .select({ metadata: schema.orders.metadata })
      .from(schema.orders)
      .where(eq(schema.orders.id, event.orderId));
    const viewToken =
      (row?.metadata as Record<string, unknown> | null)?.viewToken;
    if (typeof viewToken === "string" && viewToken) {
      tokenParam = `&t=${viewToken}`;
    }
  }
  // event.status can be "pending" when Iyzico's paymentStatus is one of the
  // in-flight 3DS / fraud-check states (INIT_THREEDS, CALLBACK_THREEDS,
  // BANK_FRAUD_CHECK). The customer's payment hasn't failed — it's still
  // settling — so route them to /checkout/success rather than /cancel. The
  // success page reads order.payment_status from the DB and will reflect
  // "pending" until the next callback flips it terminal.
  const target =
    event.status === "failed"
      ? `${siteUrl}/checkout/cancel?order=${event.orderId}${tokenParam}`
      : `${siteUrl}/checkout/success?order=${event.orderId}${tokenParam}`;

  // 303 See Other: converts the iyzico POST callback into a GET redirect
  // to the success/cancel page. Faster and more reliable than the old
  // meta-refresh HTML, and preserves POST→GET semantics.
  return NextResponse.redirect(target, 303);
}
