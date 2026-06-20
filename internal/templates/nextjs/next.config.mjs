/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  devIndicators: false,
  // Sandbox preview runs `next dev` behind a stable *.preview.wemob.app host.
  // Next 16 blocks cross-origin requests to /_next/* dev resources by default,
  // which prevents the client bundle from loading and leaves the page stuck at
  // its SSR state (e.g. framer-motion `initial={{opacity:0}}` never animates).
  allowedDevOrigins: ["*.preview.wemob.app"],
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
