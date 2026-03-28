"use client";

import React, { useState } from "react";
import { Minus, Plus } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

interface QuantitySelectorProps {
  value?: number;
  onChange?: (value: number) => void;
  min?: number;
  max?: number;
  size?: "sm" | "md";
  disabled?: boolean;
}

export function QuantitySelector({
  value: controlledValue,
  onChange,
  min = 1,
  max = 99,
  size = "md",
  disabled = false,
}: QuantitySelectorProps) {
  const [internalValue, setInternalValue] = useState(controlledValue ?? min);
  const value = controlledValue ?? internalValue;

  const handleChange = (next: number) => {
    setInternalValue(next);
    onChange?.(next);
  };

  const btnSize = size === "sm" ? "h-7 w-7" : "h-9 w-9";
  const textSize = size === "sm" ? "text-sm w-8" : "text-base w-10";
  const iconSize = size === "sm" ? "h-3 w-3" : "h-4 w-4";

  return (
    <div className="inline-flex items-center rounded-xl border border-border/60">
      <button
        onClick={() => handleChange(Math.max(min, value - 1))}
        disabled={disabled || value <= min}
        className={"flex items-center justify-center rounded-l-xl transition-colors hover:bg-muted active:scale-95 disabled:opacity-40 " + btnSize}
        aria-label="{{t.decreaseQuantity}}"
      >
        <Minus className={iconSize} />
      </button>
      <div className={"relative overflow-hidden text-center font-medium tabular-nums " + textSize}>
        <AnimatePresence mode="popLayout">
          <motion.span
            key={value}
            initial={{ y: -12, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: 12, opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="inline-block"
          >
            {value}
          </motion.span>
        </AnimatePresence>
      </div>
      <button
        onClick={() => handleChange(Math.min(max, value + 1))}
        disabled={disabled || value >= max}
        className={"flex items-center justify-center rounded-r-xl transition-colors hover:bg-muted active:scale-95 disabled:opacity-40 " + btnSize}
        aria-label="{{t.increaseQuantity}}"
      >
        <Plus className={iconSize} />
      </button>
    </div>
  );
}
