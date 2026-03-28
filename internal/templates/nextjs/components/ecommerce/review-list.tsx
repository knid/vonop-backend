"use client";

import React from "react";
import { Star, Loader2 } from "lucide-react";
import { motion } from "framer-motion";
import { useReviews } from "@/lib/wemob-store";

interface ReviewListProps {
  productSlug: string;
}

function StarRating({ rating, size = "sm" }: { rating: number; size?: "sm" | "md" }) {
  const starSize = size === "sm" ? "h-3.5 w-3.5" : "h-5 w-5";
  return (
    <div className="flex gap-0.5">
      {[1, 2, 3, 4, 5].map((i) => (
        <Star
          key={i}
          className={starSize + " " + (i <= rating ? "fill-foreground text-foreground" : "fill-muted/40 text-muted/40")}
        />
      ))}
    </div>
  );
}

function StarInput({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  const [hover, setHover] = React.useState(0);
  return (
    <div className="flex gap-1">
      {[1, 2, 3, 4, 5].map((i) => (
        <button
          key={i}
          type="button"
          onClick={() => onChange(i)}
          onMouseEnter={() => setHover(i)}
          onMouseLeave={() => setHover(0)}
          className="p-0.5"
          aria-label={i + " star" + (i > 1 ? "s" : "")}
        >
          <Star className={"h-6 w-6 " + (i <= (hover || value) ? "fill-foreground text-foreground" : "fill-muted/40 text-muted/40")} />
        </button>
      ))}
    </div>
  );
}

export function ReviewList({ productSlug }: ReviewListProps) {
  const { reviews, averageRating, reviewCount, isLoading, submitReview } = useReviews(productSlug);
  const [showForm, setShowForm] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const [submitted, setSubmitted] = React.useState(false);
  const [form, setForm] = React.useState({ name: "", email: "", rating: 5, title: "", body: "" });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!form.rating || !form.name || !form.email) return;
    setSubmitting(true);
    try {
      await submitReview({ customer_name: form.name, customer_email: form.email, rating: form.rating, title: form.title, body: form.body });
      setSubmitted(true);
      setShowForm(false);
      setForm({ name: "", email: "", rating: 5, title: "", body: "" });
    } catch {
      // silently handle
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-6 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex-1">
          <h3 className="text-xl font-semibold tracking-tight">{{t.customerReviews}}</h3>
          {reviewCount > 0 && (
            <div className="mt-3 flex flex-col gap-4 sm:flex-row sm:items-center sm:gap-8">
              <div className="flex items-center gap-3">
                <span className="text-4xl font-bold tracking-tight text-foreground">{averageRating.toFixed(1)}</span>
                <div>
                  <StarRating rating={Math.round(averageRating)} size="md" />
                  <p className="mt-0.5 text-xs text-muted-foreground">{reviewCount} review{reviewCount !== 1 ? "s" : ""}</p>
                </div>
              </div>
              {/* Rating Distribution */}
              <div className="flex-1 space-y-1.5">
                {[5, 4, 3, 2, 1].map((star) => {
                  const count = reviews.filter((r) => r.rating === star).length;
                  const pct = reviewCount > 0 ? (count / reviewCount) * 100 : 0;
                  return (
                    <div key={star} className="flex items-center gap-2 text-xs">
                      <span className="w-3 text-right text-muted-foreground">{star}</span>
                      <Star className="h-3 w-3 fill-foreground text-foreground" />
                      <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted/50">
                        <div className="h-full rounded-full bg-foreground transition-all duration-500" style={{ width: pct + "%" }} />
                      </div>
                      <span className="w-8 text-right text-muted-foreground">{count}</span>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>
        {!showForm && !submitted && (
          <button onClick={() => setShowForm(true)} className="flex-shrink-0 rounded-full border border-foreground px-5 py-2.5 text-sm font-medium text-foreground transition-colors hover:bg-foreground hover:text-background">
            {{t.writeReview}}
          </button>
        )}
      </div>

      {submitted && (
        <div className="rounded-xl bg-green-50 px-4 py-3 text-sm text-green-800 dark:bg-green-950/30 dark:text-green-200">
          {{t.reviewThanks}}
        </div>
      )}

      {showForm && (
        <form onSubmit={handleSubmit} className="space-y-4 rounded-xl bg-muted/20 p-5">
          <div>
            <label className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">{{t.rating}}</label>
            <StarInput value={form.rating} onChange={(v) => setForm((f) => ({ ...f, rating: v }))} />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <label htmlFor="review-name" className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">{{t.name}}</label>
              <input id="review-name" required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} className="w-full border-b border-border/60 bg-transparent px-0 py-2 text-sm outline-none focus:border-foreground" />
            </div>
            <div>
              <label htmlFor="review-email" className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">{{t.email}}</label>
              <input id="review-email" type="email" required value={form.email} onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))} className="w-full border-b border-border/60 bg-transparent px-0 py-2 text-sm outline-none focus:border-foreground" />
            </div>
          </div>
          <div>
            <label htmlFor="review-title" className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">{{t.titleOptional}}</label>
            <input id="review-title" value={form.title} onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))} className="w-full border-b border-border/60 bg-transparent px-0 py-2 text-sm outline-none focus:border-foreground" />
          </div>
          <div>
            <label htmlFor="review-body" className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">{{t.review}}</label>
            <textarea id="review-body" rows={3} value={form.body} onChange={(e) => setForm((f) => ({ ...f, body: e.target.value }))} className="w-full border-b border-border/60 bg-transparent px-0 py-2 text-sm outline-none focus:border-foreground" />
          </div>
          <div className="flex gap-3 pt-2">
            <button type="submit" disabled={submitting} className="flex items-center gap-2 rounded-full bg-foreground px-5 py-2 text-sm font-medium text-background disabled:opacity-50">
              {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
              {{t.submitReview}}
            </button>
            <button type="button" onClick={() => setShowForm(false)} className="rounded-full px-5 py-2 text-sm font-medium text-muted-foreground hover:text-foreground">
              {{t.cancel}}
            </button>
          </div>
        </form>
      )}

      {isLoading ? (
        <div className="space-y-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-24 animate-pulse rounded-xl bg-muted" />
          ))}
        </div>
      ) : reviews.length === 0 && !submitted ? (
        <p className="py-8 text-center text-sm text-muted-foreground">{{t.noReviewsYet}}</p>
      ) : (
        <div className="space-y-4">
          {reviews.map((review, idx) => (
            <motion.div
              key={review.id}
              initial={{ opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: idx * 0.05 }}
              className="rounded-xl bg-muted/20 p-5"
            >
              <div className="mb-3 flex items-start justify-between">
                <div className="flex items-center gap-3">
                  <div className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full bg-muted text-sm font-semibold text-muted-foreground">
                    {review.customer_name.charAt(0).toUpperCase()}
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-foreground">{review.customer_name}</span>
                      {review.is_verified && (
                        <span className="rounded-full bg-green-100 px-2 py-0.5 text-[10px] font-medium text-green-700 dark:bg-green-500/10 dark:text-green-400">{{t.verified}}</span>
                      )}
                    </div>
                    <StarRating rating={review.rating} />
                  </div>
                </div>
                <span className="text-xs text-muted-foreground">
                  {new Date(review.created_at).toLocaleDateString()}
                </span>
              </div>
              {review.title && <h4 className="mb-1 font-medium">{review.title}</h4>}
              {review.body && <p className="text-sm leading-relaxed text-muted-foreground">{review.body}</p>}
            </motion.div>
          ))}
        </div>
      )}
    </div>
  );
}
