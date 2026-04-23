import { NextRequest, NextResponse } from "next/server";
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
  const siteUrl = process.env.NEXT_PUBLIC_SITE_URL ?? "";
  const target =
    event.status === "paid"
      ? `${siteUrl}/checkout/success?order=${event.orderId}`
      : `${siteUrl}/checkout/cancel?order=${event.orderId}`;

  return new NextResponse(
    `<!DOCTYPE html><html><head><meta http-equiv="refresh" content="0;url=${target}"></head><body>Redirecting…</body></html>`,
    { status: 200, headers: { "content-type": "text/html; charset=utf-8" } },
  );
}
