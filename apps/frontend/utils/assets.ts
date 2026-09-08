/** Normalize asset map from /api/assets to a stable shape for the dashboard. */
export function normalizeAssets(assets) {
  if (!assets || typeof assets !== 'object') {
    return {};
  }

  const out = {};
  for (const key of Object.keys(assets)) {
    const a = assets[key];
    if (!a) continue;

    const inUSD = toNum(a.netAssetInUSD ?? a.inUSD);
    const inBTC = toNum(a.netAssetInBTC ?? a.inBTC);
    const total = toNum(a.total ?? a.netAsset);

    out[key] = {
      ...a,
      currency: a.currency || key,
      total,
      inUSD,
      inBTC,
      netAssetInUSD: inUSD,
      netAssetInBTC: inBTC,
    };
  }
  return out;
}

export function toNum(v) {
  const n = typeof v === 'number' ? v : parseFloat(v);
  return Number.isFinite(n) ? n : 0;
}

export function aggregateAssetsBy(assets, field) {
  let total = 0;
  for (const key of Object.keys(assets || {})) {
    const a = assets[key];
    if (!a) continue;
    total += toNum(a[field]);
  }
  return total;
}

export function fmtMoney(n, digits = 2) {
  return toNum(n).toLocaleString('en-US', {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  });
}
