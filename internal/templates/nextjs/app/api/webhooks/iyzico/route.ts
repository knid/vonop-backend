import { NextRequest, NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { getAdapter } from "@/lib/payments";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  const rawBody = await req.text();

  let event;
  try {
    const adapter = getAdapter("iyzico");
    event = await adapter.handleWebhook(rawBody, req.headers);
  } catch (err) {
    return NextResponse.json({ error: (err as Error).message }, { status: 400 });
  }

  if (!event.orderId) return NextResponse.json({ ok: true });

  const nextOrderStatus =
    event.status === "paid" ? "processing" : event.status === "failed" ? "cancelled" : "pending";

  await db
    .update(schema.orders)
    .set({
      paymentStatus: event.status,
      paymentId: event.paymentId,
      status: nextOrderStatus,
      updatedAt: new Date(),
    })
    .where(eq(schema.orders.id, event.orderId));

  // Iyzico expects an HTML page redirecting the user to the success/cancel
  // page. Emit a minimal HTML response with a meta-refresh. The siteUrl is
  // derived from the request to avoid hardcoding.
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
