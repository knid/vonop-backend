import { NextRequest, NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db, schema } from "@/lib/db";
import { getAdapter } from "@/lib/payments";

// Stripe's webhook signature requires the raw body. Ensure Next.js doesn't
// mutate it.
export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  const rawBody = await req.text();

  let event;
  try {
    const adapter = getAdapter("stripe");
    event = await adapter.handleWebhook(rawBody, req.headers);
  } catch (err) {
    // Signature verification failed or malformed payload — return 400 so
    // Stripe retries.
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

  return NextResponse.json({ ok: true });
}
