"use client";

import React from "react";
import Image from "next/image";
import { ShoppingBag, ChevronLeft, ChevronRight } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

interface ProductGalleryProps {
  images: string[];
  productName: string;
}

export function ProductGallery({ images, productName }: ProductGalleryProps) {
  const [selectedIndex, setSelectedIndex] = React.useState(0);

  if (!images || images.length === 0) {
    return (
      <div className="flex aspect-square w-full items-center justify-center rounded-2xl bg-muted">
        <ShoppingBag className="h-16 w-16 text-muted-foreground/40" />
      </div>
    );
  }

  const handlePrev = () => setSelectedIndex((i) => (i > 0 ? i - 1 : images.length - 1));
  const handleNext = () => setSelectedIndex((i) => (i < images.length - 1 ? i + 1 : 0));

  return (
    <div className="space-y-3">
      <div className="group relative aspect-square overflow-hidden rounded-2xl bg-muted">
        <AnimatePresence mode="wait">
          <motion.div
            key={selectedIndex}
            initial={{ opacity: 0, scale: 1.02 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.98 }}
            transition={{ duration: 0.4 }}
            className="absolute inset-0"
          >
            <Image
              src={images[selectedIndex]}
              alt={productName + " - Image " + (selectedIndex + 1)}
              fill
              className="object-cover"
              sizes="(max-width: 768px) 100vw, 50vw"
              priority={selectedIndex === 0}
            />
          </motion.div>
        </AnimatePresence>
        {images.length > 1 && (
          <>
            <button
              onClick={handlePrev}
              className="absolute left-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-background/90 shadow-sm opacity-0 backdrop-blur-sm transition-all group-hover:opacity-100 hover:bg-background"
              aria-label="{{t.previousImage}}"
            >
              <ChevronLeft className="h-4 w-4" />
            </button>
            <button
              onClick={handleNext}
              className="absolute right-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-background/90 shadow-sm opacity-0 backdrop-blur-sm transition-all group-hover:opacity-100 hover:bg-background"
              aria-label="{{t.nextImage}}"
            >
              <ChevronRight className="h-4 w-4" />
            </button>
            <div className="absolute bottom-3 left-3 rounded-full bg-background/80 px-2.5 py-1 text-[11px] font-medium tabular-nums text-foreground/80 backdrop-blur-sm opacity-0 transition-opacity group-hover:opacity-100">
              {(selectedIndex + 1) + " / " + images.length}
            </div>
          </>
        )}
      </div>
      {/* Mobile dots */}
      {images.length > 1 && (
        <div className="flex justify-center gap-1.5 sm:hidden">
          {images.map((_, i) => (
            <button
              key={i}
              onClick={() => setSelectedIndex(i)}
              className={"h-1.5 rounded-full transition-all " + (i === selectedIndex ? "w-6 bg-foreground" : "w-1.5 bg-foreground/20")}
              aria-label={"Go to image " + (i + 1)}
            />
          ))}
        </div>
      )}
      {/* Desktop thumbnails */}
      {images.length > 1 && (
        <div className="hidden gap-2 overflow-x-auto pb-1 sm:flex">
          {images.map((img, i) => (
            <button
              key={i}
              onClick={() => setSelectedIndex(i)}
              className={"relative h-20 w-20 flex-shrink-0 overflow-hidden rounded-xl transition-all " +
                (i === selectedIndex ? "ring-2 ring-foreground ring-offset-2" : "opacity-60 hover:opacity-100")}
              aria-label={"View image " + (i + 1)}
            >
              <Image src={img} alt={productName + " thumbnail " + (i + 1)} fill className="object-cover" sizes="80px" />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
