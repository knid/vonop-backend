# Admin Panel Frontend — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Next.js admin panel at `~/Projects/wemob-admin-panel` with dark theme, sidebar layout, and full CRUD for all Wemob platform entities.

**Architecture:** Next.js 15 App Router SPA. All data fetching via TanStack Query hitting the wemob-backend admin API (`/v1/admin/*`). Auth via existing login endpoint with token in localStorage.

**Tech Stack:** Next.js 15, TypeScript, shadcn/ui, Tailwind CSS, TanStack Table, TanStack Query, Recharts, next-themes

**Spec:** `docs/superpowers/specs/2026-04-08-admin-panel-design.md`

---

## File Structure

```
~/Projects/wemob-admin-panel/
├── src/
│   ├── app/
│   │   ├── layout.tsx            # Root layout (providers, sidebar)
│   │   ├── page.tsx              # Redirect to /dashboard
│   │   ├── login/
│   │   │   └── page.tsx          # Login page
│   │   ├── dashboard/
│   │   │   └── page.tsx          # Dashboard with KPI cards + charts
│   │   ├── users/
│   │   │   ├── page.tsx          # User list
│   │   │   └── [userId]/
│   │   │       └── page.tsx      # User detail
│   │   ├── apps/
│   │   │   ├── page.tsx          # App list
│   │   │   └── [appId]/
│   │   │       └── page.tsx      # App detail
│   │   ├── projects/
│   │   │   ├── page.tsx          # Project list
│   │   │   └── [projectId]/
│   │   │       └── page.tsx      # Project detail
│   │   ├── billing/
│   │   │   └── page.tsx          # Subscriptions + plans
│   │   ├── credits/
│   │   │   └── page.tsx          # Credits overview (tabs: transactions, balances, usage)
│   │   ├── templates/
│   │   │   └── page.tsx          # Template list (reuses existing admin API)
│   │   ├── organizations/
│   │   │   ├── page.tsx          # Org list
│   │   │   └── [orgId]/
│   │   │       └── page.tsx      # Org detail with members
│   │   ├── tickets/
│   │   │   ├── page.tsx          # Ticket list
│   │   │   └── [ticketId]/
│   │   │       └── page.tsx      # Ticket detail with reply
│   │   ├── jobs/
│   │   │   ├── page.tsx          # Job list
│   │   │   └── [jobId]/
│   │   │       └── page.tsx      # Job detail with events
│   │   ├── deployments/
│   │   │   └── page.tsx          # Deployment list
│   │   └── analytics/
│   │       └── page.tsx          # Platform analytics charts
│   ├── components/
│   │   ├── sidebar.tsx           # Navigation sidebar
│   │   ├── data-table.tsx        # Reusable TanStack Table wrapper
│   │   ├── kpi-card.tsx          # Dashboard stat card
│   │   ├── page-header.tsx       # Page title + actions bar
│   │   ├── confirm-dialog.tsx    # Confirmation modal
│   │   └── pagination.tsx        # Pagination controls
│   ├── lib/
│   │   ├── api.ts                # API client (fetch wrapper with auth)
│   │   ├── auth.ts               # Auth helpers (login, logout, getToken)
│   │   ├── queries.ts            # TanStack Query hooks for all entities
│   │   └── utils.ts              # Formatters, helpers
│   └── providers.tsx             # QueryClientProvider + ThemeProvider
```

---

### Task 1: Project Scaffold

**Files:**
- Create: entire project at `~/Projects/wemob-admin-panel`

- [ ] **Step 1: Create Next.js project**

```bash
cd ~/Projects
npx create-next-app@latest wemob-admin-panel --typescript --tailwind --eslint --app --src-dir --import-alias "@/*" --no-turbopack
```

- [ ] **Step 2: Install dependencies**

```bash
cd ~/Projects/wemob-admin-panel
npm install @tanstack/react-query @tanstack/react-table recharts next-themes
npx shadcn@latest init -d
```

When shadcn init asks, select:
- Style: New York
- Base color: Zinc
- CSS variables: Yes

- [ ] **Step 3: Add shadcn components**

```bash
npx shadcn@latest add button card input label table badge tabs dialog dropdown-menu separator scroll-area sheet select textarea toast sonner avatar
```

- [ ] **Step 4: Configure dark theme**

Edit `src/app/layout.tsx` to wrap with ThemeProvider locked to dark. Edit `tailwind.config.ts` to set `darkMode: "class"`.

- [ ] **Step 5: Commit**

```bash
cd ~/Projects/wemob-admin-panel
git init
git add .
git commit -m "feat: scaffold Next.js admin panel with shadcn/ui"
```

---

### Task 2: Auth & API Client

**Files:**
- Create: `src/lib/auth.ts`
- Create: `src/lib/api.ts`
- Create: `src/app/login/page.tsx`
- Create: `src/providers.tsx`

- [ ] **Step 1: Create auth helpers**

Create `src/lib/auth.ts`:

```typescript
const TOKEN_KEY = "wemob_admin_token";

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export function isAuthenticated(): boolean {
  return !!getToken();
}
```

- [ ] **Step 2: Create API client**

Create `src/lib/api.ts`:

```typescript
import { getToken, clearToken } from "./auth";

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8001";

export async function api<T = any>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const token = getToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(options.headers as Record<string, string>),
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers,
  });

  if (res.status === 401) {
    clearToken();
    window.location.href = "/login";
    throw new Error("Unauthorized");
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.message || body.error || `Request failed: ${res.status}`);
  }

  return res.json();
}
```

- [ ] **Step 3: Create providers**

Create `src/providers.tsx`:

```typescript
"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ThemeProvider } from "next-themes";
import { useState } from "react";
import { Toaster } from "@/components/ui/sonner";

export function Providers({ children }: { children: React.ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { staleTime: 30_000, retry: 1 },
        },
      })
  );

  return (
    <ThemeProvider attribute="class" defaultTheme="dark" forcedTheme="dark">
      <QueryClientProvider client={queryClient}>
        {children}
        <Toaster />
      </QueryClientProvider>
    </ThemeProvider>
  );
}
```

- [ ] **Step 4: Create login page**

Create `src/app/login/page.tsx`:

```typescript
"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import { setToken } from "@/lib/auth";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleLogin(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      const res = await api<{ token: string }>("/v1/users/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
      });
      setToken(res.token);
      router.push("/dashboard");
    } catch (err: any) {
      setError(err.message || "Login failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-background">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">Wemob Admin</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleLogin} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </div>
            {error && <p className="text-sm text-destructive">{error}</p>}
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? "Signing in..." : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 5: Update root layout to use providers**

Update `src/app/layout.tsx`:

```typescript
import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";
import { Providers } from "@/providers";

const inter = Inter({ subsets: ["latin"] });

export const metadata: Metadata = {
  title: "Wemob Admin",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className={inter.className}>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
```

- [ ] **Step 6: Verify it runs**

```bash
cd ~/Projects/wemob-admin-panel && npm run dev
```

Open `http://localhost:3000/login` — should see the login card.

- [ ] **Step 7: Commit**

```bash
git add .
git commit -m "feat: add auth, API client, login page, and providers"
```

---

### Task 3: Sidebar Layout & Navigation

**Files:**
- Create: `src/components/sidebar.tsx`
- Create: `src/components/page-header.tsx`
- Create: `src/app/(admin)/layout.tsx` — authenticated layout with sidebar
- Move pages into `(admin)` route group

- [ ] **Step 1: Create sidebar component**

Create `src/components/sidebar.tsx`:

```typescript
"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";
import {
  LayoutDashboard, Users, AppWindow, FolderKanban, CreditCard,
  Sparkles, LayoutTemplate, Building2, Ticket, Zap, Globe, BarChart3,
} from "lucide-react";

const navItems = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/users", label: "Users", icon: Users },
  { href: "/apps", label: "Apps", icon: AppWindow },
  { href: "/projects", label: "Projects", icon: FolderKanban },
  { href: "/billing", label: "Billing", icon: CreditCard },
  { href: "/credits", label: "AI Credits", icon: Sparkles },
  { href: "/templates", label: "Templates", icon: LayoutTemplate },
  { href: "/organizations", label: "Organizations", icon: Building2 },
  { href: "/tickets", label: "Tickets", icon: Ticket },
  { href: "/jobs", label: "Jobs", icon: Zap },
  { href: "/deployments", label: "Deployments", icon: Globe },
  { href: "/analytics", label: "Analytics", icon: BarChart3 },
];

export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="w-56 h-screen bg-zinc-950 border-r border-zinc-800 flex flex-col fixed left-0 top-0">
      <div className="p-4 border-b border-zinc-800">
        <h1 className="text-sm font-semibold text-zinc-100">Wemob Admin</h1>
      </div>
      <nav className="flex-1 p-2 space-y-0.5 overflow-y-auto">
        {navItems.map((item) => {
          const active = pathname.startsWith(item.href);
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "flex items-center gap-2 px-3 py-2 rounded-md text-sm transition-colors",
                active
                  ? "bg-zinc-800 text-zinc-100"
                  : "text-zinc-400 hover:text-zinc-100 hover:bg-zinc-900"
              )}
            >
              <item.icon className="h-4 w-4" />
              {item.label}
            </Link>
          );
        })}
      </nav>
    </aside>
  );
}
```

- [ ] **Step 2: Create page header component**

Create `src/components/page-header.tsx`:

```typescript
export function PageHeader({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between mb-6">
      <div>
        <h1 className="text-2xl font-semibold">{title}</h1>
        {description && <p className="text-sm text-muted-foreground mt-1">{description}</p>}
      </div>
      {children && <div className="flex items-center gap-2">{children}</div>}
    </div>
  );
}
```

- [ ] **Step 3: Create authenticated layout with auth guard**

Create `src/app/(admin)/layout.tsx`:

```typescript
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { isAuthenticated } from "@/lib/auth";
import { Sidebar } from "@/components/sidebar";

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  const router = useRouter();

  useEffect(() => {
    if (!isAuthenticated()) {
      router.replace("/login");
    }
  }, [router]);

  return (
    <div className="min-h-screen bg-background">
      <Sidebar />
      <main className="ml-56 p-6">{children}</main>
    </div>
  );
}
```

- [ ] **Step 4: Create root page redirect**

Update `src/app/page.tsx`:

```typescript
import { redirect } from "next/navigation";
export default function Home() {
  redirect("/dashboard");
}
```

- [ ] **Step 5: Create placeholder dashboard page**

Create `src/app/(admin)/dashboard/page.tsx`:

```typescript
import { PageHeader } from "@/components/page-header";

export default function DashboardPage() {
  return <PageHeader title="Dashboard" description="Platform overview" />;
}
```

Create placeholder pages for all other sections (each in their respective `(admin)/` subfolder) with the same pattern — just a PageHeader with the section name. These will be fleshed out in subsequent tasks.

- [ ] **Step 6: Verify layout works**

```bash
cd ~/Projects/wemob-admin-panel && npm run dev
```

Login, verify sidebar appears with all nav items, clicking navigates between pages.

- [ ] **Step 7: Commit**

```bash
git add .
git commit -m "feat: add sidebar layout, navigation, and auth guard"
```

---

### Task 4: Reusable Data Table Component

**Files:**
- Create: `src/components/data-table.tsx`
- Create: `src/components/pagination.tsx`

- [ ] **Step 1: Create pagination component**

Create `src/components/pagination.tsx`:

```typescript
import { Button } from "@/components/ui/button";
import { ChevronLeft, ChevronRight } from "lucide-react";

interface PaginationProps {
  page: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  total: number;
}

export function Pagination({ page, totalPages, onPageChange, total }: PaginationProps) {
  return (
    <div className="flex items-center justify-between mt-4">
      <p className="text-sm text-muted-foreground">{total} total results</p>
      <div className="flex items-center gap-2">
        <Button variant="outline" size="sm" onClick={() => onPageChange(page - 1)} disabled={page <= 1}>
          <ChevronLeft className="h-4 w-4" />
        </Button>
        <span className="text-sm">
          {page} / {totalPages || 1}
        </span>
        <Button variant="outline" size="sm" onClick={() => onPageChange(page + 1)} disabled={page >= totalPages}>
          <ChevronRight className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Create reusable data table**

Create `src/components/data-table.tsx`:

```typescript
"use client";

import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useRouter } from "next/navigation";

interface DataTableProps<T> {
  columns: ColumnDef<T, any>[];
  data: T[];
  onRowClick?: (row: T) => void;
}

export function DataTable<T>({ columns, data, onRowClick }: DataTableProps<T>) {
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <div className="rounded-md border border-zinc-800">
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id}>
                  {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows?.length ? (
            table.getRowModel().rows.map((row) => (
              <TableRow
                key={row.id}
                className={onRowClick ? "cursor-pointer hover:bg-zinc-900" : ""}
                onClick={() => onRowClick?.(row.original)}
              >
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : (
            <TableRow>
              <TableCell colSpan={columns.length} className="h-24 text-center text-muted-foreground">
                No results.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add .
git commit -m "feat: add reusable data table and pagination components"
```

---

### Task 5: Query Hooks & API Utilities

**Files:**
- Create: `src/lib/queries.ts`
- Create: `src/lib/utils.ts` (extend existing)

- [ ] **Step 1: Create TanStack Query hooks**

Create `src/lib/queries.ts`:

```typescript
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";

interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

interface ListParams {
  page?: number;
  page_size?: number;
  search?: string;
  status?: string;
  sort_by?: string;
  sort_dir?: string;
  from?: string;
  to?: string;
}

function buildQuery(params: ListParams): string {
  const q = new URLSearchParams();
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== "") q.set(k, String(v));
  });
  const str = q.toString();
  return str ? `?${str}` : "";
}

// Dashboard
export function useDashboard() {
  return useQuery({ queryKey: ["admin", "dashboard"], queryFn: () => api("/v1/admin/dashboard") });
}

// Users
export function useAdminUsers(params: ListParams) {
  return useQuery({
    queryKey: ["admin", "users", params],
    queryFn: () => api<PaginatedResponse<any>>(`/v1/admin/users${buildQuery(params)}`),
  });
}
export function useAdminUser(userId: string) {
  return useQuery({
    queryKey: ["admin", "users", userId],
    queryFn: () => api(`/v1/admin/users/${userId}`),
    enabled: !!userId,
  });
}
export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, data }: { userId: string; data: any }) =>
      api(`/v1/admin/users/${userId}`, { method: "PUT", body: JSON.stringify(data) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin", "users"] }),
  });
}
export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api(`/v1/admin/users/${userId}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin", "users"] }),
  });
}
export function useImpersonateUser() {
  return useMutation({
    mutationFn: (userId: string) => api(`/v1/admin/users/${userId}/impersonate`, { method: "POST" }),
  });
}
export function useResetPassword() {
  return useMutation({
    mutationFn: (userId: string) => api(`/v1/admin/users/${userId}/reset-password`, { method: "POST" }),
  });
}
export function useGrantCredits() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, data }: { userId: string; data: { amount: number; reason: string } }) =>
      api(`/v1/admin/users/${userId}/credits`, { method: "POST", body: JSON.stringify(data) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin", "users"] }),
  });
}
export function useForceSubscription() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, data }: { userId: string; data: any }) =>
      api(`/v1/admin/users/${userId}/subscription`, { method: "POST", body: JSON.stringify(data) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin", "users"] }),
  });
}

// Apps
export function useAdminApps(params: ListParams) {
  return useQuery({
    queryKey: ["admin", "apps", params],
    queryFn: () => api<PaginatedResponse<any>>(`/v1/admin/apps${buildQuery(params)}`),
  });
}
export function useAdminApp(appId: string) {
  return useQuery({
    queryKey: ["admin", "apps", appId],
    queryFn: () => api(`/v1/admin/apps/${appId}`),
    enabled: !!appId,
  });
}

// Projects
export function useAdminProjects(params: ListParams) {
  return useQuery({
    queryKey: ["admin", "projects", params],
    queryFn: () => api<PaginatedResponse<any>>(`/v1/admin/projects${buildQuery(params)}`),
  });
}

// Subscriptions
export function useAdminSubscriptions() {
  return useQuery({
    queryKey: ["admin", "subscriptions"],
    queryFn: () => api("/v1/admin/subscriptions"),
  });
}

// Credits
export function useAdminCreditTransactions(params: ListParams) {
  return useQuery({
    queryKey: ["admin", "credits", "transactions", params],
    queryFn: () => api<PaginatedResponse<any>>(`/v1/admin/credits/transactions${buildQuery(params)}`),
  });
}
export function useAdminCreditBalances(params: ListParams) {
  return useQuery({
    queryKey: ["admin", "credits", "balances", params],
    queryFn: () => api<PaginatedResponse<any>>(`/v1/admin/credits/balances${buildQuery(params)}`),
  });
}
export function useAdminTokenUsage() {
  return useQuery({
    queryKey: ["admin", "credits", "usage"],
    queryFn: () => api("/v1/admin/credits/usage"),
  });
}

// Organizations
export function useAdminOrganizations(params: ListParams) {
  return useQuery({
    queryKey: ["admin", "organizations", params],
    queryFn: () => api<PaginatedResponse<any>>(`/v1/admin/organizations${buildQuery(params)}`),
  });
}
export function useAdminOrganization(orgId: string) {
  return useQuery({
    queryKey: ["admin", "organizations", orgId],
    queryFn: () => api(`/v1/admin/organizations/${orgId}`),
    enabled: !!orgId,
  });
}

// Tickets
export function useAdminTickets(params: ListParams) {
  return useQuery({
    queryKey: ["admin", "tickets", params],
    queryFn: () => api<PaginatedResponse<any>>(`/v1/admin/tickets${buildQuery(params)}`),
  });
}
export function useAdminTicket(ticketId: string) {
  return useQuery({
    queryKey: ["admin", "tickets", ticketId],
    queryFn: () => api(`/v1/admin/tickets/${ticketId}`),
    enabled: !!ticketId,
  });
}
export function useUpdateTicket() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ticketId, data }: { ticketId: string; data: any }) =>
      api(`/v1/admin/tickets/${ticketId}`, { method: "PUT", body: JSON.stringify(data) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin", "tickets"] }),
  });
}

// Jobs
export function useAdminJobs(params: ListParams) {
  return useQuery({
    queryKey: ["admin", "jobs", params],
    queryFn: () => api<PaginatedResponse<any>>(`/v1/admin/jobs${buildQuery(params)}`),
  });
}
export function useAdminJob(jobId: string) {
  return useQuery({
    queryKey: ["admin", "jobs", jobId],
    queryFn: () => api(`/v1/admin/jobs/${jobId}`),
    enabled: !!jobId,
  });
}

// Deployments
export function useAdminDeployments(params: ListParams) {
  return useQuery({
    queryKey: ["admin", "deployments", params],
    queryFn: () => api<PaginatedResponse<any>>(`/v1/admin/deployments${buildQuery(params)}`),
  });
}

// Analytics
export function useAdminAnalytics() {
  return useQuery({
    queryKey: ["admin", "analytics"],
    queryFn: () => api("/v1/admin/analytics/overview"),
  });
}
```

- [ ] **Step 2: Commit**

```bash
git add .
git commit -m "feat: add TanStack Query hooks for all admin API endpoints"
```

---

### Task 6: Dashboard Page

**Files:**
- Create: `src/components/kpi-card.tsx`
- Modify: `src/app/(admin)/dashboard/page.tsx`

- [ ] **Step 1: Create KPI card component**

Create `src/components/kpi-card.tsx`:

```typescript
import { Card, CardContent } from "@/components/ui/card";

interface KPICardProps {
  label: string;
  value: string | number;
  subtitle?: string;
}

export function KPICard({ label, value, subtitle }: KPICardProps) {
  return (
    <Card className="bg-zinc-900 border-zinc-800">
      <CardContent className="p-4">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className="text-2xl font-semibold mt-1">{value}</p>
        {subtitle && <p className="text-xs text-muted-foreground mt-1">{subtitle}</p>}
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 2: Build dashboard page**

Update `src/app/(admin)/dashboard/page.tsx`:

```typescript
"use client";

import { PageHeader } from "@/components/page-header";
import { KPICard } from "@/components/kpi-card";
import { useDashboard } from "@/lib/queries";
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts";

export default function DashboardPage() {
  const { data, isLoading } = useDashboard();

  if (isLoading) return <p className="text-muted-foreground">Loading...</p>;
  if (!data) return null;

  return (
    <div>
      <PageHeader title="Dashboard" description="Platform overview" />

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-8">
        <KPICard label="Total Users" value={data.users?.total_users ?? 0} subtitle={`+${data.users?.new_users_today ?? 0} today`} />
        <KPICard label="Active Subscriptions" value={data.users?.active_users ?? 0} />
        <KPICard label="Total Apps" value={data.total_apps ?? 0} subtitle={`${data.deployed_apps ?? 0} deployed`} />
        <KPICard label="AI Credits Used" value={(data.ai_usage?.total_credits_used ?? 0).toLocaleString()} />
        <KPICard label="Active Deploys" value={data.active_deploys ?? 0} />
        <KPICard label="Open Tickets" value={data.open_tickets ?? 0} />
        <KPICard label="Total Builds" value={data.users?.total_builds ?? 0} />
        <KPICard label="New This Week" value={data.users?.new_users_this_week ?? 0} />
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add .
git commit -m "feat: build dashboard page with KPI cards"
```

---

### Task 7: Users List & Detail Pages

**Files:**
- Modify: `src/app/(admin)/users/page.tsx`
- Create: `src/app/(admin)/users/[userId]/page.tsx`
- Create: `src/components/confirm-dialog.tsx`

- [ ] **Step 1: Create confirm dialog**

Create `src/components/confirm-dialog.tsx`:

```typescript
"use client";

import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  onConfirm: () => void;
  destructive?: boolean;
}

export function ConfirmDialog({ open, onOpenChange, title, description, onConfirm, destructive }: ConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">{description}</p>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button variant={destructive ? "destructive" : "default"} onClick={() => { onConfirm(); onOpenChange(false); }}>
            Confirm
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

- [ ] **Step 2: Build users list page**

Build `src/app/(admin)/users/page.tsx` with:
- Search input
- Status filter dropdown (all, active, inactive, free, trial)
- DataTable with columns: email, name, subscription status (as badge), provider, created date
- Pagination
- Click row to navigate to `/users/{id}`

Use `useAdminUsers` hook with the filter params.

- [ ] **Step 3: Build user detail page**

Build `src/app/(admin)/users/[userId]/page.tsx` with:
- User info header (name, email, subscription badge, created date)
- Action buttons: Impersonate, Reset Password, Grant Credits, Change Subscription, Delete
- Stats cards (credits balance, app count, project count, org count)
- Each action opens appropriate modal/dialog
- Impersonate opens dashboard URL in new tab with the returned token
- Reset password shows the temporary password in a copy-to-clipboard dialog

Use `useAdminUser`, `useImpersonateUser`, `useResetPassword`, `useGrantCredits`, `useForceSubscription`, `useDeleteUser` hooks.

- [ ] **Step 4: Verify it works**

```bash
npm run dev
```

Navigate to Users page, verify table loads (needs backend running), click a user to see detail.

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: add users list and detail pages with god mode actions"
```

---

### Task 8: Remaining List & Detail Pages

**Files:**
- All remaining pages under `src/app/(admin)/`

- [ ] **Step 1: Build apps list + detail pages**

`apps/page.tsx` — DataTable with name, user email, status badge, project type, created date. Search + status filter.
`apps/[appId]/page.tsx` — App detail with prompt, settings, screenshot, deployment info.

- [ ] **Step 2: Build projects list page**

`projects/page.tsx` — DataTable with package name, user email, build count, created date.

- [ ] **Step 3: Build billing page**

`billing/page.tsx` — Card per subscription plan showing name, price, user count, limits. Edit button opens dialog to modify limits.

- [ ] **Step 4: Build credits page**

`credits/page.tsx` — Tabs: Transactions (paginated table), Balances (table sorted by balance), Token Usage (table of aggregates by model/action).

- [ ] **Step 5: Build organizations list + detail pages**

`organizations/page.tsx` — DataTable with name, owner email, member count.
`organizations/[orgId]/page.tsx` — Org detail with member list.

- [ ] **Step 6: Build tickets list + detail pages**

`tickets/page.tsx` — DataTable with subject, sender, status badge, priority, created date. Filter by status.
`tickets/[ticketId]/page.tsx` — Ticket detail with message, reply textarea, status dropdown, save button.

- [ ] **Step 7: Build jobs list + detail pages**

`jobs/page.tsx` — DataTable with type, status badge, user ID, created date. Filter by status.
`jobs/[jobId]/page.tsx` — Job detail with events JSON viewer.

- [ ] **Step 8: Build deployments page**

`deployments/page.tsx` — DataTable with app name, user email, platform, status, URL.

- [ ] **Step 9: Build analytics page**

`analytics/page.tsx` — Recharts line charts for signups/day and active users/day (last 30 days).

- [ ] **Step 10: Build templates page**

`templates/page.tsx` — DataTable fetching from existing `/v2/admin/templates` endpoint.

- [ ] **Step 11: Verify all pages work**

```bash
npm run dev
```

Navigate through all sidebar items, verify tables render and navigation works.

- [ ] **Step 12: Commit**

```bash
git add .
git commit -m "feat: add all remaining admin pages"
```

---

### Task 9: Polish & Environment Config

**Files:**
- Create: `.env.local`
- Create: `.env.example`
- Verify: all pages handle loading/error states

- [ ] **Step 1: Add environment files**

Create `.env.local`:
```
NEXT_PUBLIC_API_URL=http://localhost:8001
```

Create `.env.example`:
```
NEXT_PUBLIC_API_URL=http://localhost:8001
```

- [ ] **Step 2: Add loading and error states to all pages**

Ensure every page shows a loading spinner/skeleton while data fetches and an error message if the query fails. Use a consistent pattern across all list pages.

- [ ] **Step 3: Verify full flow end to end**

1. Start backend: `cd ~/Projects/wemob/wemob-backend && make dev`
2. Start frontend: `cd ~/Projects/wemob-admin-panel && npm run dev`
3. Navigate to `http://localhost:3000`
4. Login with admin credentials
5. Verify dashboard loads with real data
6. Navigate through each section
7. Test a god mode action (grant credits, impersonate)

- [ ] **Step 4: Final commit**

```bash
git add .
git commit -m "feat: add environment config and polish loading/error states"
```
