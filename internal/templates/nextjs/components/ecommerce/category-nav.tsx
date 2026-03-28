"use client";

import React from "react";
import { motion } from "framer-motion";
import { useCategories, type StorefrontCategory } from "@/lib/wemob-store";

interface CategoryNavProps {
  selectedCategory?: string;
  onSelect: (slug: string | undefined) => void;
}

export function CategoryNav({ selectedCategory, onSelect }: CategoryNavProps) {
  const { categories, isLoading } = useCategories();

  if (isLoading) {
    return (
      <div className="flex gap-2 overflow-x-auto pb-2">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="h-9 w-24 animate-pulse rounded-full bg-muted" />
        ))}
      </div>
    );
  }

  if (!categories || categories.length === 0) return null;

  const allItems = [
    { id: "all", slug: undefined as string | undefined, name: "{{t.all}}" },
    ...categories.map((cat) => ({ id: cat.id, slug: cat.slug as string | undefined, name: cat.name })),
  ];

  return (
    <div className="flex flex-wrap gap-2">
      {allItems.map((item) => {
        const isActive = item.slug === undefined ? !selectedCategory : selectedCategory === item.slug;
        return (
          <button
            key={item.id}
            onClick={() => onSelect(item.slug)}
            className={"relative rounded-full px-4 py-2 text-sm font-medium transition-colors " +
              (isActive ? "bg-foreground text-background" : "border border-border/60 text-muted-foreground hover:text-foreground")}
          >
            {isActive && (
              <motion.div
                layoutId="category-pill"
                className="absolute inset-0 rounded-full bg-foreground"
                transition={{ type: "spring", bounce: 0.2, duration: 0.5 }}
              />
            )}
            <span className="relative z-10">{item.name}</span>
          </button>
        );
      })}
    </div>
  );
}
