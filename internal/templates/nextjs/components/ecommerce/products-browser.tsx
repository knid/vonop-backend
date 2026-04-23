"use client";

import React, { useEffect, useMemo, useState } from "react";
import Image from "next/image";
import Link from "next/link";
import { Search, SlidersHorizontal, ChevronDown, ChevronRight, LayoutGrid, List, X, ShoppingBag } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import {
  formatPrice,
  type StorefrontCategory,
  type StorefrontProduct,
} from "@/lib/wemob-store";
import { ProductCard } from "@/components/ecommerce/product-card";
import { EmptyState } from "@/components/ecommerce/empty-state";

type SortOption = "featured" | "newest" | "price-asc" | "price-desc" | "name";
type ViewMode = "grid" | "list";

const containerVariants = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.06 } },
};
const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  show: { opacity: 1, y: 0 },
};

interface ProductsBrowserProps {
  initialProducts: StorefrontProduct[];
  categories: StorefrontCategory[];
  initialQuery?: string;
  initialCategorySlug?: string | null;
}

export function ProductsBrowser({
  initialProducts,
  categories,
  initialQuery = "",
  initialCategorySlug = null,
}: ProductsBrowserProps) {
  const products = initialProducts;
  const [search, setSearch] = useState(initialQuery);
  const [selectedCategory, setSelectedCategory] = useState<string | null>(initialCategorySlug);
  const [sort, setSort] = useState<SortOption>("featured");
  const [showSort, setShowSort] = useState(false);
  const [viewMode, setViewMode] = useState<ViewMode>("grid");
  const [showFilters, setShowFilters] = useState(false);
  const [priceMin, setPriceMin] = useState("");
  const [priceMax, setPriceMax] = useState("");
  const [onSaleOnly, setOnSaleOnly] = useState(false);
  const [inStockOnly, setInStockOnly] = useState(false);
  const [openSections, setOpenSections] = useState<Record<string, boolean>>({ categories: true, price: true, filters: true });

  useEffect(() => {
    setSearch(initialQuery);
  }, [initialQuery]);

  useEffect(() => {
    setSelectedCategory(initialCategorySlug);
  }, [initialCategorySlug]);

  const toggleSection = (key: string) => {
    setOpenSections((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const sortLabels: Record<SortOption, string> = {
    featured: "{{t.sortByFeatured}}",
    newest: "{{t.sortByNewest}}",
    "price-asc": "{{t.sortByPriceLow}}",
    "price-desc": "{{t.sortByPriceHigh}}",
    name: "Name A\u2013Z",
  };

  const filtered = useMemo(() => {
    let result = products ?? [];

    if (search.trim()) {
      const q = search.toLowerCase();
      result = result.filter(
        (p) =>
          p.name.toLowerCase().includes(q) ||
          (p.description && p.description.toLowerCase().includes(q))
      );
    }

    if (selectedCategory) {
      const catName = categories?.find((c) => c.slug === selectedCategory)?.name;
      result = result.filter((p) => p.category === catName);
    }

    if (priceMin) {
      const min = parseFloat(priceMin) * 100;
      result = result.filter((p) => p.price_in_cents >= min);
    }
    if (priceMax) {
      const max = parseFloat(priceMax) * 100;
      result = result.filter((p) => p.price_in_cents <= max);
    }

    if (onSaleOnly) {
      result = result.filter((p) => p.compare_at_price && p.compare_at_price > p.price_in_cents);
    }
    if (inStockOnly) {
      result = result.filter((p) => p.in_stock !== false);
    }

    switch (sort) {
      case "price-asc":
        result = [...result].sort((a, b) => a.price_in_cents - b.price_in_cents);
        break;
      case "price-desc":
        result = [...result].sort((a, b) => b.price_in_cents - a.price_in_cents);
        break;
      case "name":
        result = [...result].sort((a, b) => a.name.localeCompare(b.name));
        break;
      default:
        break;
    }

    return result;
  }, [products, categories, search, selectedCategory, sort, priceMin, priceMax, onSaleOnly, inStockOnly]);

  const activeFilterCount = [selectedCategory, priceMin, priceMax, onSaleOnly, inStockOnly].filter(Boolean).length;

  const clearFilters = () => {
    setSelectedCategory(null);
    setPriceMin("");
    setPriceMax("");
    setOnSaleOnly(false);
    setInStockOnly(false);
  };

  const filterContent = (
    <div className="space-y-8">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search products..."
          className="h-10 w-full rounded-lg border border-border bg-background pl-10 pr-9 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none"
        />
        {search && (
          <button onClick={() => setSearch("")} className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-full p-0.5 text-muted-foreground hover:text-foreground" aria-label="Clear search">
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      {categories && categories.length > 0 && (
        <div>
          <button onClick={() => toggleSection("categories")} className="flex w-full items-center justify-between text-sm font-semibold text-foreground">
            Categories
            <ChevronRight className={"h-4 w-4 text-muted-foreground transition-transform duration-200 " + (openSections.categories ? "rotate-90" : "")} />
          </button>
          <div className={"mt-3 space-y-0.5 " + (openSections.categories ? "" : "hidden")}>
            <button
              onClick={() => setSelectedCategory(null)}
              className={"flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-sm transition-colors " + (selectedCategory === null ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground")}
            >
              <span>All Categories</span>
              <span className="text-xs text-muted-foreground/50">{(products ?? []).length}</span>
            </button>
            {categories.map((cat) => {
              var catCount = (products ?? []).filter((p) => p.category === cat.name).length;
              return (
                <button
                  key={cat.id}
                  onClick={() => setSelectedCategory(selectedCategory === cat.slug ? null : cat.slug)}
                  className={"flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-sm transition-colors " + (selectedCategory === cat.slug ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground")}
                >
                  <span>{cat.name}</span>
                  <span className="text-xs text-muted-foreground/50">{catCount}</span>
                </button>
              );
            })}
          </div>
        </div>
      )}

      <div>
        <button onClick={() => toggleSection("price")} className="flex w-full items-center justify-between text-sm font-semibold text-foreground">
          Price Range
          <ChevronRight className={"h-4 w-4 text-muted-foreground transition-transform duration-200 " + (openSections.price ? "rotate-90" : "")} />
        </button>
        <div className={"mt-3 flex items-center gap-2 " + (openSections.price ? "" : "hidden")}>
          <input
            type="number"
            value={priceMin}
            onChange={(e) => setPriceMin(e.target.value)}
            placeholder="Min"
            min="0"
            className="h-10 w-full rounded-lg border border-border bg-background px-3 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
          />
          <span className="text-sm text-muted-foreground">\u2013</span>
          <input
            type="number"
            value={priceMax}
            onChange={(e) => setPriceMax(e.target.value)}
            placeholder="Max"
            min="0"
            className="h-10 w-full rounded-lg border border-border bg-background px-3 text-sm text-foreground placeholder:text-muted-foreground focus:border-foreground focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
          />
        </div>
      </div>

      <div>
        <button onClick={() => toggleSection("filters")} className="flex w-full items-center justify-between text-sm font-semibold text-foreground">
          Filters
          <ChevronRight className={"h-4 w-4 text-muted-foreground transition-transform duration-200 " + (openSections.filters ? "rotate-90" : "")} />
        </button>
        <div className={"mt-3 space-y-3 " + (openSections.filters ? "" : "hidden")}>
          <label className="flex cursor-pointer items-center gap-3">
            <input
              type="checkbox"
              checked={onSaleOnly}
              onChange={(e) => setOnSaleOnly(e.target.checked)}
              className="h-4 w-4 rounded border-border accent-foreground"
            />
            <span className="text-sm text-muted-foreground">On Sale</span>
          </label>
          <label className="flex cursor-pointer items-center gap-3">
            <input
              type="checkbox"
              checked={inStockOnly}
              onChange={(e) => setInStockOnly(e.target.checked)}
              className="h-4 w-4 rounded border-border accent-foreground"
            />
            <span className="text-sm text-muted-foreground">In Stock</span>
          </label>
        </div>
      </div>

      {activeFilterCount > 0 && (
        <button
          onClick={clearFilters}
          className="text-sm text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
        >
          Clear all filters
        </button>
      )}
    </div>
  );

  return (
    <>
      <p className="mb-6 -mt-6 text-sm text-muted-foreground">
        {"Showing " + filtered.length + " / " + (products?.length ?? 0) + " products"}
      </p>

      {/* Toolbar */}
      <div className="mb-6 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowFilters(true)}
            className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-background px-4 text-sm font-medium text-foreground transition-colors hover:bg-muted lg:hidden"
          >
            <SlidersHorizontal className="h-4 w-4" />
            Filters
            {activeFilterCount > 0 && (
              <span className="flex h-5 w-5 items-center justify-center rounded-full bg-foreground text-[10px] font-semibold text-background">
                {activeFilterCount}
              </span>
            )}
          </button>

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
                    className="absolute left-0 top-full z-20 mt-1 w-48 rounded-xl border border-border/40 bg-background py-1 shadow-lg"
                  >
                    {(Object.entries(sortLabels) as [SortOption, string][]).map(([key, label]) => (
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

        <div className="flex overflow-hidden rounded-lg border border-border">
          <button
            onClick={() => setViewMode("grid")}
            className={"flex h-9 w-9 items-center justify-center transition-colors " + (viewMode === "grid" ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground")}
            aria-label="Grid view"
          >
            <LayoutGrid className="h-4 w-4" />
          </button>
          <button
            onClick={() => setViewMode("list")}
            className={"flex h-9 w-9 items-center justify-center transition-colors " + (viewMode === "list" ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground")}
            aria-label="List view"
          >
            <List className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Active Filter Chips */}
      {activeFilterCount > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-2">
          {selectedCategory && (
            <button
              onClick={() => setSelectedCategory(null)}
              className="inline-flex items-center gap-1 rounded-full bg-foreground/10 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-foreground/20"
            >
              {categories?.find((c) => c.slug === selectedCategory)?.name || selectedCategory}
              <X className="h-3 w-3" />
            </button>
          )}
          {priceMin && (
            <button onClick={() => setPriceMin("")} className="inline-flex items-center gap-1 rounded-full bg-foreground/10 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-foreground/20">
              {"Min: $" + priceMin} <X className="h-3 w-3" />
            </button>
          )}
          {priceMax && (
            <button onClick={() => setPriceMax("")} className="inline-flex items-center gap-1 rounded-full bg-foreground/10 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-foreground/20">
              {"Max: $" + priceMax} <X className="h-3 w-3" />
            </button>
          )}
          {onSaleOnly && (
            <button onClick={() => setOnSaleOnly(false)} className="inline-flex items-center gap-1 rounded-full bg-rose-100 px-3 py-1.5 text-xs font-medium text-rose-700 transition-colors hover:bg-rose-200 dark:bg-rose-500/10 dark:text-rose-400">
              On Sale <X className="h-3 w-3" />
            </button>
          )}
          {inStockOnly && (
            <button onClick={() => setInStockOnly(false)} className="inline-flex items-center gap-1 rounded-full bg-foreground/10 px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-foreground/20">
              In Stock <X className="h-3 w-3" />
            </button>
          )}
          <button onClick={clearFilters} className="text-xs text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground">
            Clear all
          </button>
        </div>
      )}

      {/* Main: Sidebar + Products */}
      <div className="flex gap-8">
        <aside className="hidden w-60 shrink-0 lg:block">
          {filterContent}
        </aside>

        <div className="min-w-0 flex-1">
          {filtered.length === 0 ? (
            <EmptyState
              icon={<Search className="h-10 w-10" />}
              title="No products found"
              description={search || activeFilterCount > 0 ? "Try adjusting your search or filters." : "Check back soon for new arrivals."}
              action={(search || activeFilterCount > 0) ? (
                <button
                  onClick={() => { setSearch(""); clearFilters(); }}
                  className="inline-flex h-11 items-center rounded-full bg-foreground px-6 text-sm font-medium text-background transition-opacity hover:opacity-90"
                >
                  Clear Filters
                </button>
              ) : undefined}
            />
          ) : viewMode === "grid" ? (
            <motion.div
              variants={containerVariants}
              initial="hidden"
              animate="show"
              className="grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-3"
            >
              {filtered.map((product) => (
                <motion.div key={product.id} variants={itemVariants}>
                  <ProductCard product={product} />
                </motion.div>
              ))}
            </motion.div>
          ) : (
            <motion.div variants={containerVariants} initial="hidden" animate="show" className="space-y-3">
              {filtered.map((product) => {
                const onSale = product.compare_at_price > 0 && product.compare_at_price > product.price_in_cents;
                return (
                  <motion.div key={product.id} variants={itemVariants}>
                    <Link
                      href={"/products/" + product.slug}
                      className="group flex gap-5 rounded-2xl border border-border/40 bg-background p-3 transition-shadow hover:shadow-md"
                    >
                      <div className="relative h-36 w-36 shrink-0 overflow-hidden rounded-xl bg-muted sm:h-44 sm:w-44">
                        {product.images && product.images.length > 0 ? (
                          <Image
                            src={product.images[0]}
                            alt={product.name}
                            fill
                            className="object-cover transition-transform duration-300 group-hover:scale-105"
                            sizes="176px"
                          />
                        ) : (
                          <div className="flex h-full w-full items-center justify-center">
                            <ShoppingBag className="h-8 w-8 text-muted-foreground/40" />
                          </div>
                        )}
                        {onSale && (
                          <div className="absolute left-2 top-2 rounded-full bg-foreground px-2 py-0.5 text-[10px] font-medium text-background">
                            {{t.sale}}
                          </div>
                        )}
                      </div>
                      <div className="flex min-w-0 flex-1 flex-col justify-between py-1">
                        <div>
                          {product.category && (
                            <p className="text-[11px] font-medium uppercase tracking-[0.15em] text-muted-foreground/60">
                              {product.category}
                            </p>
                          )}
                          <h3 className="mt-1 text-base font-semibold text-foreground group-hover:underline">
                            {product.name}
                          </h3>
                          {product.description && (
                            <p className="mt-1.5 line-clamp-2 text-sm text-muted-foreground">
                              {product.description}
                            </p>
                          )}
                        </div>
                        <div className="mt-3 flex items-center gap-3">
                          <span className="text-base font-semibold text-foreground">{formatPrice(product.price_in_cents)}</span>
                          {onSale && (
                            <span className="text-sm text-muted-foreground line-through">{formatPrice(product.compare_at_price)}</span>
                          )}
                          {product.in_stock === false && (
                            <span className="rounded-full bg-muted px-2.5 py-0.5 text-xs font-medium text-muted-foreground">Out of Stock</span>
                          )}
                        </div>
                      </div>
                    </Link>
                  </motion.div>
                );
              })}
            </motion.div>
          )}
        </div>
      </div>

      {/* Mobile Filter Drawer */}
      <AnimatePresence>
        {showFilters && (
          <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="fixed inset-0 z-40 bg-black/40"
              onClick={() => setShowFilters(false)}
            />
            <motion.div
              initial={{ x: "-100%" }}
              animate={{ x: 0 }}
              exit={{ x: "-100%" }}
              transition={{ type: "spring", damping: 25, stiffness: 300 }}
              className="fixed inset-y-0 left-0 z-50 w-80 max-w-[85vw] overflow-y-auto bg-background p-6 shadow-xl"
            >
              <div className="mb-6 flex items-center justify-between">
                <h2 className="text-lg font-semibold text-foreground">Filters</h2>
                <button
                  onClick={() => setShowFilters(false)}
                  className="flex h-8 w-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
                >
                  <X className="h-5 w-5" />
                </button>
              </div>
              {filterContent}
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </>
  );
}
