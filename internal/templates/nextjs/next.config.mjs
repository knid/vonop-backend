/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  devIndicators: false,
  images: {
    unoptimized: true,
  },
  typescript: {
    ignoreBuildErrors: true,
  },
  eslint: {
    ignoreDuringBuilds: true,
  },
};

export default nextConfig;
