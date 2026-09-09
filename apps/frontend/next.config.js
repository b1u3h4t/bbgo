/** @type {import('next').NextConfig} */
const nextConfig = {
  // Static export for nginx root at /home/admin/bbgo/web
  output: 'export',
  trailingSlash: false,
  images: { unoptimized: true },
};

module.exports = nextConfig;
