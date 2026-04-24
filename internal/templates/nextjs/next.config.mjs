/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  devIndicators: false,
  // E2B preview runs `next dev` at 3000-<sandboxId>.preview.wemob.app.
  // Next 16 blocks cross-origin requests to /_next/* dev resources by default,
  // which prevents the client bundle from loading and leaves the page stuck at
  // its SSR state (e.g. framer-motion `initial={{opacity:0}}` never animates).
  allowedDevOrigins: ["*.preview.wemob.app"],
  experimental: {
    // Server Actions compare the browser Origin against X-Forwarded-Host and
    // abort on mismatch (CSRF guard). The preview tunnel terminates TLS at
    // preview.wemob.app but forwards to the E2B sandbox's internal
    // *.e2b.app host, so the two headers disagree by design. Whitelisting
    // both hostnames lets the action through without weakening CSRF for
    // any other caller.
    serverActions: {
      allowedOrigins: ["*.preview.wemob.app", "*.e2b.app"],
    },
  },
  // iyzipay loads its resource modules via a dynamic require that Turbopack
  // (the Next 16 default builder) can't statically resolve, so the build fails
  // with "server relative imports are not implemented yet". Externalizing the
  // package leaves the require to native Node at runtime; standalone output
  // still ships it under .next/standalone/node_modules.
  serverExternalPackages: ["iyzipay"],
  images: {
    unoptimized: true,
  },
  typescript: {
    ignoreBuildErrors: true,
  },
};

export default nextConfig;
