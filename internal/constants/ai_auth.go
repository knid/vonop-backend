package constants

// NEXTJS_AUTH_TEMPLATE_FILE_PATHS are template-managed files injected only for projects that need auth.
var NEXTJS_AUTH_TEMPLATE_FILE_PATHS = []string{
	"lib/wemob-auth.ts",
	"lib/wemob-auth-client.tsx",
	"app/api/auth/[...action]/route.ts",
}

// AUTH_DEFAULT_DATA_MODELS defines the standard users table for auth-enabled projects.
const AUTH_DEFAULT_DATA_MODELS = `[
  {
    "name": "users",
    "columns": [
      {"name": "id", "type": "uuid", "primary_key": true},
      {"name": "email", "type": "text", "nullable": false},
      {"name": "password_hash", "type": "text", "nullable": false},
      {"name": "name", "type": "text", "nullable": true},
      {"name": "avatar_url", "type": "text", "nullable": true},
      {"name": "role", "type": "text", "nullable": false, "default": "'user'"},
      {"name": "email_verified", "type": "boolean", "nullable": false, "default": "false"},
      {"name": "created_at", "type": "timestamp", "nullable": false, "default": "now()"},
      {"name": "updated_at", "type": "timestamp", "nullable": false, "default": "now()"}
    ]
  }
]`

// NEXTJS_TEMPLATE_WEMOB_AUTH is the server-side auth library template.
// Uses jose for JWT (edge-compatible), bcryptjs for passwords, next/headers cookies.
const NEXTJS_TEMPLATE_WEMOB_AUTH = `import { SignJWT, jwtVerify } from "jose";
import bcrypt from "bcryptjs";
import { cookies } from "next/headers";
import { db } from "./db";
import { users } from "./schema";
import { eq } from "drizzle-orm";

// --- Types ---

export interface AuthUser {
  id: string;
  email: string;
  name: string | null;
  avatarUrl: string | null;
  role: string;
  emailVerified: boolean;
}

export interface Session {
  user: AuthUser;
  expires: Date;
}

export interface AuthResult {
  success: boolean;
  error?: string;
  user?: AuthUser;
}

// --- Config ---

const SESSION_COOKIE = "wemob_auth_session";
const SESSION_DURATION = 7 * 24 * 60 * 60; // 7 days in seconds

function getSecretKey() {
  const secret = process.env.AUTH_SECRET;
  if (!secret) throw new Error("AUTH_SECRET environment variable is not set");
  return new TextEncoder().encode(secret);
}

// --- Helpers ---

function sanitizeUser(row: typeof users.$inferSelect): AuthUser {
  return {
    id: row.id,
    email: row.email,
    name: row.name,
    avatarUrl: row.avatarUrl,
    role: row.role,
    emailVerified: row.emailVerified,
  };
}

async function createSessionToken(user: AuthUser): Promise<string> {
  return new SignJWT({ sub: user.id, email: user.email, role: user.role })
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime(` + "`" + `${SESSION_DURATION}s` + "`" + `)
    .sign(getSecretKey());
}

async function verifySessionToken(token: string): Promise<{ sub: string } | null> {
  try {
    const { payload } = await jwtVerify(token, getSecretKey());
    return payload as { sub: string };
  } catch {
    return null;
  }
}

// --- Auth Functions ---

/** Register a new user with email and password */
export async function register(
  email: string,
  password: string,
  name?: string
): Promise<AuthResult> {
  try {
    const existing = await db
      .select()
      .from(users)
      .where(eq(users.email, email.toLowerCase().trim()))
      .limit(1);

    if (existing.length > 0) {
      return { success: false, error: "An account with this email already exists" };
    }

    const passwordHash = await bcrypt.hash(password, 12);

    const [newUser] = await db
      .insert(users)
      .values({
        email: email.toLowerCase().trim(),
        passwordHash,
        name: name || null,
        role: "user",
        emailVerified: false,
      })
      .returning();

    const user = sanitizeUser(newUser);
    const token = await createSessionToken(user);

    const cookieStore = await cookies();
    cookieStore.set(SESSION_COOKIE, token, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      sameSite: "lax",
      maxAge: SESSION_DURATION,
      path: "/",
    });

    return { success: true, user };
  } catch (error) {
    console.error("Registration error:", error);
    return { success: false, error: "Registration failed. Please try again." };
  }
}

/** Log in with email and password */
export async function login(
  email: string,
  password: string
): Promise<AuthResult> {
  try {
    const [row] = await db
      .select()
      .from(users)
      .where(eq(users.email, email.toLowerCase().trim()))
      .limit(1);

    if (!row) {
      return { success: false, error: "Invalid email or password" };
    }

    const valid = await bcrypt.compare(password, row.passwordHash);
    if (!valid) {
      return { success: false, error: "Invalid email or password" };
    }

    const user = sanitizeUser(row);
    const token = await createSessionToken(user);

    const cookieStore = await cookies();
    cookieStore.set(SESSION_COOKIE, token, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      sameSite: "lax",
      maxAge: SESSION_DURATION,
      path: "/",
    });

    return { success: true, user };
  } catch (error) {
    console.error("Login error:", error);
    return { success: false, error: "Login failed. Please try again." };
  }
}

/** Log out the current user */
export async function logout(): Promise<void> {
  const cookieStore = await cookies();
  cookieStore.delete(SESSION_COOKIE);
}

/** Get the current session (returns null if not authenticated) */
export async function getSession(): Promise<Session | null> {
  const cookieStore = await cookies();
  const token = cookieStore.get(SESSION_COOKIE)?.value;
  if (!token) return null;

  const payload = await verifySessionToken(token);
  if (!payload?.sub) return null;

  const [row] = await db
    .select()
    .from(users)
    .where(eq(users.id, payload.sub))
    .limit(1);

  if (!row) return null;

  return {
    user: sanitizeUser(row),
    expires: new Date(Date.now() + SESSION_DURATION * 1000),
  };
}

/** Get the current user or null */
export async function getCurrentUser(): Promise<AuthUser | null> {
  const session = await getSession();
  return session?.user || null;
}

/** Require authentication — throws redirect if not logged in. Use in server components. */
export async function requireAuth(redirectTo = "/login"): Promise<AuthUser> {
  const session = await getSession();
  if (!session) {
    const { redirect } = await import("next/navigation");
    redirect(redirectTo);
  }
  return session.user;
}

/** Get session from API route request (reads cookie from headers) */
export async function getApiSession(): Promise<Session | null> {
  return getSession();
}
`

// NEXTJS_TEMPLATE_WEMOB_AUTH_CLIENT is the client-side auth library template.
const NEXTJS_TEMPLATE_WEMOB_AUTH_CLIENT = `"use client";

import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  type ReactNode,
} from "react";

// --- Types ---

interface AuthUser {
  id: string;
  email: string;
  name: string | null;
  avatarUrl: string | null;
  role: string;
  emailVerified: boolean;
}

interface AuthContextType {
  user: AuthUser | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (email: string, password: string) => Promise<{ success: boolean; error?: string }>;
  register: (email: string, password: string, name?: string) => Promise<{ success: boolean; error?: string }>;
  logout: () => Promise<void>;
  refreshSession: () => Promise<void>;
}

// --- Context ---

const AuthContext = createContext<AuthContextType | null>(null);

// --- Provider ---

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  const refreshSession = useCallback(async () => {
    try {
      const res = await fetch("/api/auth/session");
      if (res.ok) {
        const data = await res.json();
        setUser(data.user || null);
      } else {
        setUser(null);
      }
    } catch {
      setUser(null);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    refreshSession();
  }, [refreshSession]);

  const login = useCallback(
    async (email: string, password: string) => {
      try {
        const res = await fetch("/api/auth/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ email, password }),
        });
        const data = await res.json();
        if (data.success && data.user) {
          setUser(data.user);
        }
        return { success: data.success, error: data.error };
      } catch {
        return { success: false, error: "Network error. Please try again." };
      }
    },
    []
  );

  const register = useCallback(
    async (email: string, password: string, name?: string) => {
      try {
        const res = await fetch("/api/auth/register", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ email, password, name }),
        });
        const data = await res.json();
        if (data.success && data.user) {
          setUser(data.user);
        }
        return { success: data.success, error: data.error };
      } catch {
        return { success: false, error: "Network error. Please try again." };
      }
    },
    []
  );

  const logout = useCallback(async () => {
    try {
      await fetch("/api/auth/logout", { method: "POST" });
    } finally {
      setUser(null);
    }
  }, []);

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: !!user,
        login,
        register,
        logout,
        refreshSession,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

// --- Hook ---

export function useAuth(): AuthContextType {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
`

// NEXTJS_TEMPLATE_AUTH_ROUTE is the catch-all API route for auth actions.
// Template-managed so the LLM doesn't generate a broken version.
const NEXTJS_TEMPLATE_AUTH_ROUTE = `import { NextRequest, NextResponse } from "next/server";
import { register, login, logout, getSession } from "@/lib/wemob-auth";

export async function POST(
  req: NextRequest,
  { params }: { params: Promise<{ action: string[] }> }
) {
  const { action } = await params;
  const actionName = action[0];

  try {
    switch (actionName) {
      case "register": {
        const { email, password, name } = await req.json();
        if (!email || !password) {
          return NextResponse.json(
            { success: false, error: "Email and password are required" },
            { status: 400 }
          );
        }
        const result = await register(email, password, name);
        return NextResponse.json(result, {
          status: result.success ? 200 : 400,
        });
      }
      case "login": {
        const { email, password } = await req.json();
        if (!email || !password) {
          return NextResponse.json(
            { success: false, error: "Email and password are required" },
            { status: 400 }
          );
        }
        const result = await login(email, password);
        return NextResponse.json(result, {
          status: result.success ? 200 : 401,
        });
      }
      case "logout": {
        await logout();
        return NextResponse.json({ success: true });
      }
      default:
        return NextResponse.json(
          { success: false, error: "Unknown action" },
          { status: 404 }
        );
    }
  } catch (error) {
    console.error("Auth API error:", error);
    return NextResponse.json(
      { success: false, error: "Internal server error" },
      { status: 500 }
    );
  }
}

export async function GET(
  _req: NextRequest,
  { params }: { params: Promise<{ action: string[] }> }
) {
  const { action } = await params;
  const actionName = action[0];

  if (actionName === "session") {
    const session = await getSession();
    if (!session) {
      return NextResponse.json({ user: null }, { status: 200 });
    }
    return NextResponse.json({ user: session.user }, { status: 200 });
  }

  return NextResponse.json(
    { success: false, error: "Unknown action" },
    { status: 404 }
  );
}
`
