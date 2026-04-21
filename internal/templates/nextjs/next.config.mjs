/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  devIndicators: false,
  // E2B preview runs `next dev` at 3000-<sandboxId>.preview.wemob.app.
  // Next 16 blocks cross-origin requests to /_next/* dev resources by default,
  // which prevents the client bundle from loading and leaves the page stuck at
  // its SSR state (e.g. framer-motion `initial={{opacity:0}}` never animates).
  allowedDevOrigins: ["*.preview.wemob.app"],
  images: {
    unoptimized: true,
  },
  typescript: {
    ignoreBuildErrors: true,
  },
};

export default nextConfig;
