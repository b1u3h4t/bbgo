/** @type {import('next').NextConfig} */
const withTM = require('next-transpile-modules')(['lightweight-charts']);

const nextConfig = {
  // Static export for nginx root at /home/admin/bbgo/web
  output: 'export',
  trailingSlash: false,
  images: { unoptimized: true },
};

module.exports = withTM(nextConfig);
