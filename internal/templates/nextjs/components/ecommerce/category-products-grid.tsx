"use client";

import React, { useMemo, useState } from "react";
import { ChevronDown } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { type StorefrontProduct } from "@/lib/wemob-store";
import { ProductCard } from "@/components/ecommerce/product-card";

type CatSort = "featured" | "price-asc" | "price-desc" | "name";

const containerVariants = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.06 } },
};
const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  show: { opacity: 1, y: 0 },
};

interface CategoryProductsGridProps {
  products: StorefrontProduct[];
}

export function CategoryProductsGrid({ products }: CategoryProductsGridProps) {
  const [sort, setSort] = useState<CatSort>("featured");
  const [showSort, setShowSort] = useState(false);

  const sortLabels: Record<CatSort, string> = {
    featured: "{{t.sortByFeatured}}",
    "price-asc": "{{t.sortByPriceLow}}",
    "price-desc": "{{t.sortByPriceHigh}}",
    name: "Name A\u2013Z",
  };

  const sorted = useMemo(() => {
    const result = [...products];
    switch (sort) {
      case "price-asc":
        return result.sort((a, b) => a.price_in_cents - b.price_in_cents);
      case "price-desc":
        return result.sort((a, b) => b.price_in_cents - a.price_in_cents);
      case "name":
        return result.sort((a, b) => a.name.localeCompare(b.name));
      default:
        return result;
    }
  }, [products, sort]);

  return (
    <>
      {/* Sort dropdown */}
      {sorted.length > 1 && (
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

      <motion.div
        variants={containerVariants}
        initial="hidden"
        animate="show"
        className="grid grid-cols-2 gap-6 sm:gap-8 lg:grid-cols-3 xl:grid-cols-4"
      >
        {sorted.map((product) => (
          <motion.div key={product.id} variants={itemVariants}>
            <ProductCard product={product} />
          </motion.div>
        ))}
      </motion.div>
    </>
  );
}
