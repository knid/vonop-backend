// Web stub for lib/review. There's no equivalent to a native App Store
// rating prompt on the web, so every call is a no-op.

export function init(): void {
  // no-op
}

export async function available(): Promise<boolean> {
  return false;
}

export async function requestReview(): Promise<boolean> {
  return false;
}

export async function requestReviewForced(): Promise<boolean> {
  return false;
}
