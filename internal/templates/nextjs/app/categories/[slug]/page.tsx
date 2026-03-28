"use client";

import React, { useState, useMemo } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { ShoppingBag, ChevronDown } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useProducts, useCategories } from "@/lib/wemob-store";
import { ProductCard } from "@/components/ecommerce/product-card";
import { EmptyState } from "@/components/ecommerce/empty-state";
import { Skeleton } from "@/components/ui/skeleton";

const containerVariants = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.06 } },
};
const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  show: { opacity: 1, y: 0 },
};

type CatSort = "featured" | "price-asc" | "price-desc" | "name";

export default function CategoryPage() {
  const params = useParams();
  const slug = params?.slug as string;
  const { products, isLoading: productsLoading } = useProducts();
  const { categories, isLoading: categoriesLoading } = useCategories();
  const [sort, setSort] = useState<CatSort>("featured");
  const [showSort, setShowSort] = useState(false);

  const sortLabels: Record<CatSort, string> = {
    featured: "{{t.sortByFeatured}}",
    "price-asc": "{{t.sortByPriceLow}}",
    "price-desc": "{{t.sortByPriceHigh}}",
    name: "Name A\u2013Z",
  };

  const isLoading = productsLoading || categoriesLoading;
  const category = categories?.find((c) => c.slug === slug);
  const categoryProducts = products?.filter(
    (p) => p.category === category?.name
  ) ?? [];

  const filtered = useMemo(() => {
    var result = [...categoryProducts];
    switch (sort) {
      case "price-asc": return result.sort((a, b) => a.price_in_cents - b.price_in_cents);
      case "price-desc": return result.sort((a, b) => b.price_in_cents - a.price_in_cents);
      case "name": return result.sort((a, b) => a.name.localeCompare(b.name));
      default: return result;
    }
  }, [categoryProducts, sort]);

  const siblingCategories = categories?.filter((c) => c.slug !== slug).slice(0, 6) ?? [];

  if (isLoading) {
    return (
      <div className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8">
        <Skeleton className="mb-2 h-5 w-48" />
        <Skeleton className="mb-8 h-12 w-64" />
        <div className="grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-3 xl:grid-cols-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <div key={i} className="space-y-3">
              <Skeleton className="aspect-[3/4] w-full rounded-2xl" />
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  const categoryName = category?.name ?? slug.replace(/-/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());

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
          {filtered.length} {filtered.length === 1 ? "product" : "products"}
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

      {/* Sort dropdown */}
      {filtered.length > 1 && (
        <div className="mb-6 flex justify-end">
          <div className="relative">
            <button
              onClick={() => setShowSort(!showSort)}
              className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-background px-4 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              {sortLabels[sort]}
              <ChevronDown className={"h-3.5 w-3.5 transition-transform " + (showSort ? "rotate-180" : "")} />
            </button>
            <AnimatePresence>
              {showSort && (
                <>
                  <div className="fixed inset-0 z-10" onClick={() => setShowSort(false)} />
                  <motion.div
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: 4 }}
                    transition={{ duration: 0.15 }}
                    className="absolute right-0 top-full z-20 mt-1 w-48 rounded-xl border border-border/40 bg-background py-1 shadow-lg"
                  >
                    {(Object.entries(sortLabels) as [CatSort, string][]).map(([key, label]) => (
                      <button
                        key={key}
                        onClick={() => { setSort(key); setShowSort(false); }}
                        className={"w-full px-4 py-2 text-left text-sm transition-colors hover:bg-muted " + (sort === key ? "font-medium text-foreground" : "text-muted-foreground")}
                      >
                        {label}
                      </button>
                    ))}
                  </motion.div>
                </>
              )}
            </AnimatePresence>
          </div>
        </div>
      )}

      {filtered.length === 0 ? (
        <EmptyState
          icon={<ShoppingBag className="h-10 w-10" />}
          title="No products in this category"
          description="Check back soon for new arrivals in this category."
          action={<Link href="/products" className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90">Browse All Products</Link>}
        />
      ) : (
        <motion.div
          variants={containerVariants}
          initial="hidden"
          animate="show"
          className="grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-3 xl:grid-cols-4"
        >
          {filtered.map((product) => (
            <motion.div key={product.id} variants={itemVariants}>
              <ProductCard product={product} />
            </motion.div>
          ))}
        </motion.div>
      )}
    </div>
  );
}
