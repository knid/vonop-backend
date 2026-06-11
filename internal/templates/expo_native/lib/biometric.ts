// Wrapper over expo-local-authentication. Centralises the
// hardware-check + enrolment-check + authenticate flow so screens
// can call a single `authenticate({ prompt })` and either succeed
// or get a typed reason back.
//
// FaceID requires `NSFaceIDUsageDescription` in Info.plist — Wemob's
// Codemagic step (P0 #6) writes the per-permission descriptions, so
// you don't need to add anything to app.json here.
//
// Web is a no-op via lib/biometric.web.ts.
import * as LocalAuthentication from "expo-local-authentication";

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
  // No initialization required.
}

export async function available(): Promise<boolean> {
  try {
    return await LocalAuthentication.hasHardwareAsync();
  } catch {
    return false;
  }
}

export async function isSupported(): Promise<boolean> {
  // Combined hardware-present + at-least-one-credential-enrolled check.
  // This is what most callers actually want before showing a "Sign in
  // with FaceID" affordance.
  try {
    const [hw, enrolled] = await Promise.all([
      LocalAuthentication.hasHardwareAsync(),
      LocalAuthentication.isEnrolledAsync(),
    ]);
    return hw && enrolled;
  } catch {
    return false;
  }
}

export async function getEnrolledTypes(): Promise<BiometricKind[]> {
  try {
    const types = await LocalAuthentication.supportedAuthenticationTypesAsync();
    return types.map((t) => {
      if (t === LocalAuthentication.AuthenticationType.FINGERPRINT) return "fingerprint";
      if (t === LocalAuthentication.AuthenticationType.FACIAL_RECOGNITION) return "face";
      if (t === LocalAuthentication.AuthenticationType.IRIS) return "iris";
      return "unknown";
    });
  } catch {
    return [];
  }
}

export async function authenticate(
  options: AuthenticateOptions = {},
): Promise<AuthenticateResult> {
  try {
    const result = await LocalAuthentication.authenticateAsync({
      promptMessage: options.prompt ?? "Confirm your identity",
      fallbackLabel: options.fallbackLabel,
      disableDeviceFallback: options.disableDeviceFallback ?? false,
    });
    if (result.success) return { success: true };
    return { success: false, error: result.error ?? "authentication_failed" };
  } catch (e) {
    return { success: false, error: e instanceof Error ? e.message : "unknown_error" };
  }
}
