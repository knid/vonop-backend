// Wrapper over expo-store-review with sane "don't nag" defaults.
//
// Apple recommends prompting at most a few times per year; we throttle
// to once per 90 days using SecureStore as the durable cookie. iOS
// itself enforces an annual cap of three prompts regardless of what
// we ask for, so this throttle is mostly for the user's benefit.
//
// Typical use: call requestReview() after a user completes a
// success-flow milestone (e.g. saved their N-th note, finished onboarding).
//
// Web is a no-op via lib/review.web.ts.
import * as SecureStore from "expo-secure-store";
import * as StoreReview from "expo-store-review";

const LAST_PROMPT_KEY = "wemob.review.last_prompt_at";
const NINETY_DAYS_MS = 90 * 24 * 60 * 60 * 1000;

export function init(): void {
  // No initialization required.
}

export async function available(): Promise<boolean> {
  try {
    const [avail, action] = await Promise.all([
      StoreReview.isAvailableAsync(),
      StoreReview.hasAction(),
    ]);
    return avail && action;
  } catch {
    return false;
  }
}

/**
 * Prompts the user for a review if all of the following are true:
 *   1. The platform supports a native review flow (TestFlight excluded).
 *   2. We haven't prompted in the last 90 days.
 *
 * Returns true if a prompt was actually shown, false otherwise. The
 * caller doesn't need to handle the return value — it's there for tests
 * or for log-driven analytics.
 */
export async function requestReview(): Promise<boolean> {
  if (!(await available())) return false;
  try {
    const lastRaw = await SecureStore.getItemAsync(LAST_PROMPT_KEY);
    if (lastRaw) {
      const last = parseInt(lastRaw, 10);
      if (!Number.isNaN(last) && Date.now() - last < NINETY_DAYS_MS) {
        return false;
      }
    }
    await StoreReview.requestReview();
    await SecureStore.setItemAsync(LAST_PROMPT_KEY, String(Date.now()));
    return true;
  } catch {
    return false;
  }
}

/**
 * Force a review prompt regardless of throttling. Use sparingly — for
 * a "Rate this app" button in settings, not auto-triggered flows.
 */
export async function requestReviewForced(): Promise<boolean> {
  if (!(await available())) return false;
  try {
    await StoreReview.requestReview();
    await SecureStore.setItemAsync(LAST_PROMPT_KEY, String(Date.now()));
    return true;
  } catch {
    return false;
  }
}
