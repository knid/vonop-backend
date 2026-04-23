import { drizzle } from "drizzle-orm/neon-http";
import { neon } from "@neondatabase/serverless";
import * as schema from "./schema";

const connectionString = process.env.DATABASE_URL;

if (!connectionString) {
  throw new Error("DATABASE_URL is not set. Ecommerce apps require a Neon database.");
}

const client = neon(connectionString);
export const db = drizzle(client, { schema });
export { schema };
