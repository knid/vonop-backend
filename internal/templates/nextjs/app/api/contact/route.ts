import { NextRequest, NextResponse } from "next/server";
import { and, eq, gte, sql } from "drizzle-orm";
import { db, schema } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type ContactBody = {
  name?: string;
  email?: string;
  subject?: string;
  message?: string;
};

const MAX_NAME_LEN = 100;
const MAX_SUBJECT_LEN = 200;
const MAX_MESSAGE_LEN = 5000;
const MESSAGES_PER_IP_PER_HOUR = 5;

// POST /api/contact — accept a message from the public contact form and
// persist it to the tenant's contact_messages table. The merchant reads
// new messages from the wemob admin (Store → Messages). The contact form
// in the generated storefront posts to this route; it used to fake a
// `setTimeout` success and drop the message on the floor.
export async function POST(req: NextRequest) {
  const body = (await req.json()) as ContactBody;

  const name = body.name?.trim();
  const email = body.email?.trim().toLowerCase();
  const subject = body.subject?.trim();
  const message = body.message?.trim();

  if (!name || !email || !message) {
    return NextResponse.json({ error: "missing_fields" }, { status: 400 });
  }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    return NextResponse.json({ error: "invalid_email" }, { status: 400 });
  }
  if (
    name.length > MAX_NAME_LEN ||
    (subject && subject.length > MAX_SUBJECT_LEN) ||
    message.length > MAX_MESSAGE_LEN
  ) {
    return NextResponse.json({ error: "text_too_long" }, { status: 400 });
  }

  // Read the client IP for rate limiting. x-forwarded-for is the canonical
  // upstream header (Caddy / Vercel / Cloudflare all set it). We only use
  // the first hop — the rest are proxy chains. If no IP can be resolved,
  // reject the write rather than silently bypass the rate limit — a client
  // that strips both headers would otherwise have unlimited write access.
  const ip =
    req.headers.get("x-forwarded-for")?.split(",")[0]?.trim() ||
    req.headers.get("x-real-ip")?.trim() ||
    "";
  if (!ip) {
    return NextResponse.json({ error: "ip_unresolved" }, { status: 400 });
  }

  const oneHourAgo = new Date(Date.now() - 60 * 60 * 1000);
  const [{ count: recentCount }] = await db
    .select({ count: sql<number>`count(*)::int` })
    .from(schema.contactMessages)
    .where(
      and(
        eq(schema.contactMessages.ipAddress, ip),
        gte(schema.contactMessages.createdAt, oneHourAgo),
      ),
    );
  if (Number(recentCount) >= MESSAGES_PER_IP_PER_HOUR) {
    return NextResponse.json({ error: "rate_limited" }, { status: 429 });
  }

  await db.insert(schema.contactMessages).values({
    name,
    email,
    subject: subject || null,
    body: message,
    status: "new",
    ipAddress: ip,
  });

  return NextResponse.json({ ok: true });
}
