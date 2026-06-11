// Web stub for lib/biometric. Browsers expose WebAuthn for biometric-style
// flows but it's a different API and not a drop-in replacement for FaceID
// / fingerprint. We just say "not supported" so screens fall back to
// password / OTP on web. Native implementation lives in lib/biometric.ts.

export type BiometricKind = "fingerprint" | "face" | "iris" | "unknown";

export type AuthenticateOptions = {
  prompt?: string;
  fallbackLabel?: string;
  disableDeviceFallback?: boolean;
};

export type AuthenticateResult =
  | { success: true }
  | { success: false; error: string };

export function init(): void {
  // no-op
}

export async function available(): Promise<boolean> {
  return false;
}

export async function isSupported(): Promise<boolean> {
  return false;
}

export async function getEnrolledTypes(): Promise<BiometricKind[]> {
  return [];
}

export async function authenticate(
  _options: AuthenticateOptions = {},
): Promise<AuthenticateResult> {
  return { success: false, error: "biometric_not_supported_on_web" };
}
