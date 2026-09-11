import { useEffect, useRef } from 'react';
import { Box, Typography } from '@mui/material';

export type KlineBar = {
  t: string;
  o: number;
  h: number;
  l: number;
  c: number;
  v?: number;
};

export type PinLevel = {
  i?: number;
  price: number;
  side: 'buy' | 'sell' | string;
  quantity?: number;
  label?: string;
  depth?: string;
};

type Props = {
  symbol: string;
  klines: KlineBar[];
  pins?: number[];
  pinLevels?: PinLevel[];
  last?: number;
  quantity?: number;
  lower?: number;
  upper?: number;
  interval?: string;
  height?: number;
};

const BUY = '#2e7d32';
const SELL = '#c62828';

function formatQty(qty?: number) {
  if (qty == null || !(qty > 0)) return '?';
  if (Number.isInteger(qty)) return String(qty);
  return String(qty);
}

function qtyAtPrice(qty: number | undefined, price: number) {
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

function resolvePinLevels(
  pinLevels: PinLevel[] | undefined,
  pins: number[],
  last: number,
  quantity?: number,
): PinLevel[] {
  if (pinLevels?.length) {
    return pinLevels.map((p, idx) => {
      const side = p.side || (p.price > last ? 'sell' : 'buy');
      const i = p.i ?? idx + 1;
      const qty = p.quantity ?? quantity;
      return {
        i,
        price: p.price,
        side,
        quantity: qty,
        depth: p.depth || `${side === 'sell' ? 'S' : 'B'}${i}`,
        label: p.label || qtyAtPrice(qty, p.price),
      };
    });
  }
  return buildOrderBookPinLevels(pins, last, quantity);
}

function formatAxisTime(iso: string, interval?: string) {
  const d = new Date(iso);
  const md = `${d.getMonth() + 1}/${d.getDate()}`;
  if (interval === '1d') return md;
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(
    d.getMinutes(),
  ).padStart(2, '0')}`;
  if (interval === '5m' || interval === '15m' || interval === '30m') {
    return `${md} ${hm}`;
  }
  return `${md} ${String(d.getHours()).padStart(2, '0')}:00`;
}

/** Canvas candlestick chart with buy/sell pin overlays. */
export default function PinKlineChart({
  symbol,
  klines,
  pins = [],
  pinLevels,
  last: lastProp,
  quantity,
  lower,
  upper,
  interval = '1h',
  height = 360,
}: Props) {
  const ref = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas || !klines?.length) return;

    const dpr = window.devicePixelRatio || 1;
    const width = canvas.parentElement?.clientWidth || 800;
    canvas.width = Math.floor(width * dpr);
    canvas.height = Math.floor(height * dpr);
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    const pad = { l: 72, r: 16, t: 24, b: 36 };
    const w = width - pad.l - pad.r;
    const h = height - pad.t - pad.b;

    const last =
      lastProp && lastProp > 0
        ? lastProp
        : klines[klines.length - 1].c;
    const levels = resolvePinLevels(pinLevels, pins, last, quantity);

    let minP = Math.min(...klines.map((k) => k.l));
    let maxP = Math.max(...klines.map((k) => k.h));
    for (const p of levels) {
      if (p.price > 0) {
        minP = Math.min(minP, p.price);
        maxP = Math.max(maxP, p.price);
      }
    }
    if (lower && lower > 0) minP = Math.min(minP, lower);
    if (upper && upper > 0) maxP = Math.max(maxP, upper);
    const span = maxP - minP || 1;
    minP -= span * 0.05;
    maxP += span * 0.05;

    const yOf = (p: number) => pad.t + ((maxP - p) / (maxP - minP)) * h;
    const xOf = (i: number) => pad.l + ((i + 0.5) / klines.length) * w;
    const candleW = Math.max(2, (w / klines.length) * 0.6);

    ctx.fillStyle = '#fafafa';
    ctx.fillRect(0, 0, width, height);
    ctx.strokeStyle = '#e0e0e0';
    ctx.strokeRect(pad.l, pad.t, w, h);

    ctx.font = '11px sans-serif';
    for (let i = 0; i <= 4; i++) {
      const p = minP + ((maxP - minP) * i) / 4;
      const y = yOf(p);
      ctx.strokeStyle = '#eeeeee';
      ctx.beginPath();
      ctx.moveTo(pad.l, y);
      ctx.lineTo(pad.l + w, y);
      ctx.stroke();
      ctx.fillStyle = '#757575';
      ctx.textAlign = 'right';
      ctx.fillText(formatPrice(p), pad.l - 6, y + 3);
    }

    if (lower && upper && upper > lower) {
      const y1 = yOf(upper);
      const y2 = yOf(lower);
      ctx.fillStyle = 'rgba(33, 150, 243, 0.06)';
      ctx.fillRect(pad.l, y1, w, y2 - y1);
    }

    klines.forEach((k, i) => {
      const x = xOf(i);
      const yO = yOf(k.o);
      const yC = yOf(k.c);
      const yH = yOf(k.h);
      const yL = yOf(k.l);
      const up = k.c >= k.o;
      ctx.strokeStyle = up ? BUY : SELL;
      ctx.fillStyle = up ? BUY : SELL;
      ctx.beginPath();
      ctx.moveTo(x, yH);
      ctx.lineTo(x, yL);
      ctx.stroke();
      const top = Math.min(yO, yC);
      const body = Math.max(1, Math.abs(yC - yO));
      ctx.fillRect(x - candleW / 2, top, candleW, body);
    });

    levels.forEach((lv) => {
      if (!(lv.price > 0)) return;
      const y = yOf(lv.price);
      const isBuy = lv.side !== 'sell';
      const color = isBuy ? BUY : SELL;
      ctx.strokeStyle = color;
      ctx.lineWidth = 1.5;
      ctx.setLineDash([5, 3]);
      ctx.beginPath();
      ctx.moveTo(pad.l, y);
      ctx.lineTo(pad.l + w, y);
      ctx.stroke();
      ctx.setLineDash([]);
      ctx.lineWidth = 1;
      ctx.fillStyle = color;
      ctx.textAlign = 'left';
      ctx.font = '10px sans-serif';
      const tag =
        lv.label ||
        qtyAtPrice(lv.quantity ?? quantity, lv.price);
      ctx.fillText(tag, pad.l + 4, y - 3);
    });

    const yLast = yOf(last);
    ctx.strokeStyle = '#1565c0';
    ctx.setLineDash([2, 2]);
    ctx.beginPath();
    ctx.moveTo(pad.l, yLast);
    ctx.lineTo(pad.l + w, yLast);
    ctx.stroke();
    ctx.setLineDash([]);
    ctx.fillStyle = '#1565c0';
    ctx.textAlign = 'left';
    ctx.fillText(`last ${formatPrice(last)}`, pad.l + w - 110, yLast - 4);

    ctx.fillStyle = '#757575';
    ctx.textAlign = 'center';
    ctx.font = '10px sans-serif';
    const step = Math.max(1, Math.floor(klines.length / 6));
    for (let i = 0; i < klines.length; i += step) {
      ctx.fillText(formatAxisTime(klines[i].t, interval), xOf(i), height - 12);
    }

    ctx.fillStyle = '#424242';
    ctx.font = '12px sans-serif';
    ctx.textAlign = 'left';
    const ivLabel = interval === '1d' ? '日线' : interval;
    ctx.fillText(
      `${symbol} ${ivLabel} · qty@price 买绿/卖红`,
      pad.l,
      16,
    );
  }, [symbol, klines, pins, pinLevels, lastProp, quantity, lower, upper, interval, height]);

  if (!klines?.length) {
    return (
      <Typography variant="body2" color="text.secondary">
        暂无 K 线数据
      </Typography>
    );
  }

  return (
    <Box sx={{ width: '100%', bgcolor: '#fafafa', borderRadius: 1 }}>
      <canvas ref={ref} />
    </Box>
  );
}

function formatPrice(p: number) {
  if (p >= 100) return p.toFixed(2);
  if (p >= 1) return p.toFixed(4);
  return p.toFixed(6);
}
