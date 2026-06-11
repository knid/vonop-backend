import AuthForm from "@/components/auth/AuthForm";

/**
 * Sample sign-up screen. Pairs with sign-in.tsx — the AI agent usually
 * replaces both with branded copies. The shipped default uses Better-Auth's
 * email/password provider plus social helpers (Apple/Google/magic-link).
 *
 * SIWA is mandatory whenever any third-party social login is offered
 * (Apple Guideline 4.8); keep the SIWA button on this screen even when
 * customising the rest of the UX.
 */
export default function SignUp() {
  return <AuthForm mode="sign-up" />;
}
