// Web stub for lib/haptics. Browsers don't have a haptics engine — we
// just no-op every call so screens importing { light, success, ... }
// still compile and run in the studio preview without checking
// Platform.OS at every call site.

export function available(): boolean {
  return false;
}

export function init(): void {
  // no-op
}

export async function light(): Promise<void> {
  // no-op
}

export async function medium(): Promise<void> {
  // no-op
}

export async function heavy(): Promise<void> {
  // no-op
}

export async function selection(): Promise<void> {
  // no-op
}

export async function success(): Promise<void> {
  // no-op
}

export async function warning(): Promise<void> {
  // no-op
}

export async function error(): Promise<void> {
  // no-op
}
