import { drizzle } from "drizzle-orm/neon-http";
import { neon } from "@neondatabase/serverless";
import * as schema from "./schema";

// Lazy initialization. Ecommerce projects ship this file at creation time, but
// DATABASE_URL is only injected after wemob finishes Neon provisioning on the
// first agent turn. Accessing any property on `db` before then throws; merely
// importing the module does not. This keeps `next build` alive during the
// pre-provision window.
let _db: ReturnType<typeof drizzle> | null = null;

function getDb(): ReturnType<typeof drizzle> {
  if (_db) return _db;
  const connectionString = process.env.DATABASE_URL;
  if (!connectionString) {
    throw new Error(
      "DATABASE_URL is not set. The backend preview is missing server env vars — " +
        "restart or resync the sandbox preview rather than provisioning a second database.",
    );
  }
  const client = neon(connectionString);
  _db = drizzle(client, { schema });
  return _db;
}

export const db = new Proxy({} as ReturnType<typeof drizzle>, {
  get(_, prop) {
    return Reflect.get(getDb(), prop);
  },
});

export { schema };
