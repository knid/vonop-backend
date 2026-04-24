import React from "react";
import Link from "next/link";
import { ShoppingBag } from "lucide-react";
import { getCategories, getProducts } from "@/lib/wemob-store-server";
import { EmptyState } from "@/components/ecommerce/empty-state";
import { CategoryProductsGrid } from "@/components/ecommerce/category-products-grid";

// Cache the rendered HTML for 60s so Caddy serves repeat hits without a DB
// round-trip. Catalog updates land within a minute of the admin edit.
export const revalidate = 60;

export default async function CategoryPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = await params;

  // Resolve categories and products in parallel. Products are filtered by
  // the category slug on the server so the page receives only the relevant
  // rows.
  const [categories, categoryProducts] = await Promise.all([
    getCategories(),
    getProducts({ category: slug }),
  ]);

  const category = categories.find((c) => c.slug === slug);
  const categoryName =
    category?.name ??
    slug.replace(/-/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  const siblingCategories = categories.filter((c) => c.slug !== slug).slice(0, 6);

  return (
    <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
      {/* Breadcrumbs */}
      <nav className="mb-6 flex items-center gap-2 text-sm text-muted-foreground/50">
        <Link href="/" className="transition-colors hover:text-foreground">Home</Link>
        <span>/</span>
        <Link href="/products" className="transition-colors hover:text-foreground">Products</Link>
        <span>/</span>
        <span className="font-medium text-foreground">{categoryName}</span>
      </nav>

      <div className="mb-10 rounded-2xl bg-gradient-to-br from-muted/50 to-muted/20 p-8 sm:p-10">
        <h1 className="text-4xl font-semibold tracking-tight text-foreground sm:text-5xl">
          {categoryName}
        </h1>
        {category?.description && (
          <p className="mt-3 max-w-2xl text-muted-foreground">{category.description}</p>
        )}
        <p className="mt-2 text-sm text-muted-foreground">
          {categoryProducts.length} {categoryProducts.length === 1 ? "product" : "products"}
        </p>
      </div>

      {/* Browse other categories */}
      {siblingCategories.length > 0 && (
        <div className="mb-8 flex flex-wrap gap-2">
          {siblingCategories.map((c) => (
            <Link
              key={c.id}
              href={"/categories/" + c.slug}
              className="rounded-full border border-border/60 px-4 py-1.5 text-sm text-muted-foreground transition-colors hover:border-foreground hover:text-foreground"
            >
              {c.name}
            </Link>
          ))}
        </div>
      )}

      {categoryProducts.length === 0 ? (
        <EmptyState
          icon={<ShoppingBag className="h-10 w-10" />}
          title="No products in this category"
          description="Check back soon for new arrivals in this category."
          action={<Link href="/products" className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90">Browse All Products</Link>}
        />
      ) : (
        <CategoryProductsGrid products={categoryProducts} />
      )}
    </div>
  );
}
