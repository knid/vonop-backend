import AuthForm from "@/components/auth/AuthForm";

/**
 * Sample sign-in screen. The AI agent typically replaces this with a real
 * branded screen; the default is here so a fresh template builds + flows
 * end-to-end without manual scaffolding.
 *
 * Wires four sign-in paths so adding a third-party provider doesn't
 * inadvertently break Apple Guideline 4.8 (which requires SIWA whenever
 * any social login is offered):
 *   1. Sign in with Apple   (iOS only — system sheet)
 *   2. Continue with Google (browser-based OAuth via Better-Auth)
 *   3. Email magic-link     (one-time link delivered by the backend)
 *   4. Email + password     (Better-Auth's email provider)
 */
export default function SignIn() {
  return <AuthForm mode="sign-in" />;
}
