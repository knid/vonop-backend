import { NextRequest, NextResponse } from "next/server";
import { getAdapter } from "@/lib/payments";
import { applyPaymentOutcome } from "@/lib/payments/apply-outcome";

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
    // Stripe retries. Returning 200 here would cause silent drops.
    return NextResponse.json({ error: (err as Error).message }, { status: 400 });
  }

  if (!event.orderId) return NextResponse.json({ ok: true });

  await applyPaymentOutcome({
    orderId: event.orderId,
    paymentId: event.paymentId,
    status: event.status,
  });

  return NextResponse.json({ ok: true });
}
