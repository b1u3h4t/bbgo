export type PinLevel = {
  i?: number;
  price: number;
  side: 'buy' | 'sell' | string;
  quantity?: number;
  label?: string;
  depth?: string;
};

function formatQty(qty?: number) {
  if (qty == null || !(qty > 0)) return '?';
  if (Number.isInteger(qty)) return String(qty);
  return String(qty);
}

function formatPrice(p: number) {
  if (p >= 100) return p.toFixed(2);
  if (p >= 1) return p.toFixed(4);
  return p.toFixed(6);
}

export function qtyAtPrice(qty: number | undefined, price: number) {
  return `${formatQty(qty)}@${formatPrice(price)}`;
}

/** Order-book style: B1>B2>… (B1 closest buy), S1<S2<… (S1 closest sell). */
export function buildOrderBookPinLevels(
  pins: number[],
  last: number,
  quantity?: number,
): PinLevel[] {
  const buys = pins
    .filter((p) => p > 0 && p <= last)
    .sort((a, b) => b - a)
    .map((price, idx) => ({
      i: idx + 1,
      price,
      side: 'buy' as const,
      quantity,
      depth: `B${idx + 1}`,
      label: qtyAtPrice(quantity, price),
    }));
  const sells = pins
    .filter((p) => p > last)
    .sort((a, b) => a - b)
    .map((price, idx) => ({
      i: idx + 1,
      price,
      side: 'sell' as const,
      quantity,
      depth: `S${idx + 1}`,
      label: qtyAtPrice(quantity, price),
    }));
  return [...buys, ...sells];
}
