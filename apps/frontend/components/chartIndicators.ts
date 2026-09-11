import { Time } from 'lightweight-charts';

export type CandlePoint = {
  time: Time;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
};

export type LinePoint = { time: Time; value: number };
export type HistPoint = { time: Time; value: number; color?: string };

export type IndicatorKind =
  | 'ma'
  | 'ema'
  | 'boll'
  | 'rsi'
  | 'macd'
  | 'vwap'
  | 'udbox';

export type IndicatorPane = 'overlay' | 'oscillator';

export type IndicatorPreset = {
  kind: IndicatorKind;
  name: string;
  short: string;
  pane: IndicatorPane;
  keywords: string[];
  defaults: Record<string, number>;
  description: string;
};

export type ActiveIndicator = {
  id: string;
  kind: IndicatorKind;
  name: string;
  pane: IndicatorPane;
  params: Record<string, number>;
  color?: string;
};

export type IndicatorSeriesPayload =
  | {
      type: 'line';
      id: string;
      title: string;
      color: string;
      pane: IndicatorPane;
      data: LinePoint[];
      lineWidth?: number;
      lineStyle?: number;
    }
  | {
      type: 'hist';
      id: string;
      title: string;
      pane: IndicatorPane;
      data: HistPoint[];
    }
  | {
      type: 'priceline';
      id: string;
      title: string;
      color: string;
      price: number;
      lineWidth?: number;
      lineStyle?: number;
    };

const OVERLAY_COLORS = [
  '#2962ff',
  '#ff6d00',
  '#9c27b0',
  '#00897b',
  '#c62828',
  '#6d4c41',
];

export const INDICATOR_PRESETS: IndicatorPreset[] = [
  {
    kind: 'ma',
    name: 'Moving Average',
    short: 'MA',
    pane: 'overlay',
    keywords: ['ma', 'sma', '均线', 'moving'],
    defaults: { length: 7 },
    description: '简单移动平均线',
  },
  {
    kind: 'ma',
    name: 'MA 25',
    short: 'MA25',
    pane: 'overlay',
    keywords: ['ma25', 'sma'],
    defaults: { length: 25 },
    description: '25 周期均线',
  },
  {
    kind: 'ma',
    name: 'MA 99',
    short: 'MA99',
    pane: 'overlay',
    keywords: ['ma99', 'sma'],
    defaults: { length: 99 },
    description: '99 周期均线',
  },
  {
    kind: 'ema',
    name: 'EMA',
    short: 'EMA',
    pane: 'overlay',
    keywords: ['ema', '指数均线'],
    defaults: { length: 12 },
    description: '指数移动平均线',
  },
  {
    kind: 'ema',
    name: 'EMA 26',
    short: 'EMA26',
    pane: 'overlay',
    keywords: ['ema26'],
    defaults: { length: 26 },
    description: '26 周期 EMA',
  },
  {
    kind: 'boll',
    name: 'Bollinger Bands',
    short: 'BOLL',
    pane: 'overlay',
    keywords: ['boll', 'bb', '布林'],
    defaults: { length: 20, mult: 2 },
    description: '布林带 (中轨 / 上下轨)',
  },
  {
    kind: 'vwap',
    name: 'VWAP',
    short: 'VWAP',
    pane: 'overlay',
    keywords: ['vwap', '成交量加权'],
    defaults: {},
    description: '成交量加权平均价',
  },
  {
    kind: 'rsi',
    name: 'Relative Strength Index',
    short: 'RSI',
    pane: 'oscillator',
    keywords: ['rsi', '相对强弱'],
    defaults: { length: 14 },
    description: 'RSI 相对强弱指标',
  },
  {
    kind: 'macd',
    name: 'MACD',
    short: 'MACD',
    pane: 'oscillator',
    keywords: ['macd', 'macd'],
    defaults: { fast: 12, slow: 26, signal: 9 },
    description: 'MACD 柱 / DIF / DEA',
  },
  {
    kind: 'udbox',
    name: 'UD Box Support/Resistance',
    short: 'UDBox',
    pane: 'overlay',
    keywords: [
      'udbox',
      'ud',
      'box',
      '箱体',
      '支撑',
      '阻力',
      'support',
      'resistance',
      'darvas',
    ],
    // aligned with pkg/strategy/udbox defaults (4h)
    defaults: { window: 20, minWidthPct: 0.015, maxWidthPct: 0.05 },
    description: 'UD优道箱体：阻力(顶) / 支撑(底) / 中线',
  },
  {
    kind: 'udbox',
    name: 'UD Box 40',
    short: 'UDBox40',
    pane: 'overlay',
    keywords: ['udbox40', '箱体40', '支撑', '阻力'],
    defaults: { window: 40, minWidthPct: 0.02, maxWidthPct: 0.08 },
    description: 'UD箱体（短周期常用 window=40）',
  },
];

let idSeq = 0;
export function newIndicatorId(kind: string): string {
  idSeq += 1;
  return `${kind}-${Date.now().toString(36)}-${idSeq}`;
}

export function createActiveFromPreset(
  preset: IndicatorPreset,
  colorIndex = 0,
): ActiveIndicator {
  const length = preset.defaults.length;
  let label = preset.short;
  if (preset.kind === 'ma') label = `MA ${length}`;
  else if (preset.kind === 'ema') label = `EMA ${length}`;
  else if (preset.kind === 'boll') label = `BOLL ${length}`;
  else if (preset.kind === 'rsi') label = `RSI ${length}`;
  else if (preset.kind === 'macd') label = 'MACD';
  else if (preset.kind === 'vwap') label = 'VWAP';
  else if (preset.kind === 'udbox')
    label = `UDBox ${preset.defaults.window || 20}`;

  return {
    id: newIndicatorId(preset.kind),
    kind: preset.kind,
    name: label,
    pane: preset.pane,
    params: { ...preset.defaults },
    color: OVERLAY_COLORS[colorIndex % OVERLAY_COLORS.length],
  };
}

function sma(values: number[], length: number): (number | null)[] {
  const out: (number | null)[] = new Array(values.length).fill(null);
  if (length <= 0) return out;
  let sum = 0;
  for (let i = 0; i < values.length; i++) {
    sum += values[i];
    if (i >= length) sum -= values[i - length];
    if (i >= length - 1) out[i] = sum / length;
  }
  return out;
}

function ema(values: number[], length: number): (number | null)[] {
  const out: (number | null)[] = new Array(values.length).fill(null);
  if (length <= 0 || values.length === 0) return out;
  const k = 2 / (length + 1);
  let prev: number | null = null;
  let seed = 0;
  for (let i = 0; i < values.length; i++) {
    if (i < length - 1) {
      seed += values[i];
      continue;
    }
    if (i === length - 1) {
      seed += values[i];
      prev = seed / length;
      out[i] = prev;
      continue;
    }
    prev = values[i] * k + (prev as number) * (1 - k);
    out[i] = prev;
  }
  return out;
}

function stdev(values: number[], length: number, means: (number | null)[]): (number | null)[] {
  const out: (number | null)[] = new Array(values.length).fill(null);
  for (let i = length - 1; i < values.length; i++) {
    const mean = means[i];
    if (mean == null) continue;
    let sum = 0;
    for (let j = i - length + 1; j <= i; j++) {
      const d = values[j] - mean;
      sum += d * d;
    }
    out[i] = Math.sqrt(sum / length);
  }
  return out;
}

function toLine(
  candles: CandlePoint[],
  values: (number | null)[],
): LinePoint[] {
  const out: LinePoint[] = [];
  for (let i = 0; i < candles.length; i++) {
    const v = values[i];
    if (v == null || !Number.isFinite(v)) continue;
    out.push({ time: candles[i].time, value: v });
  }
  return out;
}

function rsi(values: number[], length: number): (number | null)[] {
  const out: (number | null)[] = new Array(values.length).fill(null);
  if (length <= 0 || values.length <= length) return out;
  let avgGain = 0;
  let avgLoss = 0;
  for (let i = 1; i <= length; i++) {
    const d = values[i] - values[i - 1];
    if (d >= 0) avgGain += d;
    else avgLoss -= d;
  }
  avgGain /= length;
  avgLoss /= length;
  out[length] = avgLoss === 0 ? 100 : 100 - 100 / (1 + avgGain / avgLoss);
  for (let i = length + 1; i < values.length; i++) {
    const d = values[i] - values[i - 1];
    const gain = d > 0 ? d : 0;
    const loss = d < 0 ? -d : 0;
    avgGain = (avgGain * (length - 1) + gain) / length;
    avgLoss = (avgLoss * (length - 1) + loss) / length;
    out[i] = avgLoss === 0 ? 100 : 100 - 100 / (1 + avgGain / avgLoss);
  }
  return out;
}

export function computeIndicator(
  ind: ActiveIndicator,
  candles: CandlePoint[],
): IndicatorSeriesPayload[] {
  if (!candles.length) return [];
  const closes = candles.map((c) => c.close);
  const color = ind.color || OVERLAY_COLORS[0];

  switch (ind.kind) {
    case 'ma': {
      const length = Math.max(1, Math.floor(ind.params.length || 7));
      return [
        {
          type: 'line',
          id: `${ind.id}-ma`,
          title: `MA ${length}`,
          color,
          pane: 'overlay',
          data: toLine(candles, sma(closes, length)),
          lineWidth: 1,
        },
      ];
    }
    case 'ema': {
      const length = Math.max(1, Math.floor(ind.params.length || 12));
      return [
        {
          type: 'line',
          id: `${ind.id}-ema`,
          title: `EMA ${length}`,
          color,
          pane: 'overlay',
          data: toLine(candles, ema(closes, length)),
          lineWidth: 1,
        },
      ];
    }
    case 'boll': {
      const length = Math.max(2, Math.floor(ind.params.length || 20));
      const mult = ind.params.mult || 2;
      const mid = sma(closes, length);
      const sd = stdev(closes, length, mid);
      const upper = mid.map((m, i) =>
        m == null || sd[i] == null ? null : m + mult * (sd[i] as number),
      );
      const lower = mid.map((m, i) =>
        m == null || sd[i] == null ? null : m - mult * (sd[i] as number),
      );
      return [
        {
          type: 'line',
          id: `${ind.id}-mid`,
          title: `BB mid`,
          color: '#787b86',
          pane: 'overlay',
          data: toLine(candles, mid),
          lineWidth: 1,
          lineStyle: 2,
        },
        {
          type: 'line',
          id: `${ind.id}-up`,
          title: `BB up`,
          color: '#2962ff',
          pane: 'overlay',
          data: toLine(candles, upper),
          lineWidth: 1,
        },
        {
          type: 'line',
          id: `${ind.id}-dn`,
          title: `BB dn`,
          color: '#2962ff',
          pane: 'overlay',
          data: toLine(candles, lower),
          lineWidth: 1,
        },
      ];
    }
    case 'vwap': {
      const values: (number | null)[] = new Array(candles.length).fill(null);
      let pv = 0;
      let vol = 0;
      for (let i = 0; i < candles.length; i++) {
        const c = candles[i];
        const typical = (c.high + c.low + c.close) / 3;
        const v = c.volume > 0 ? c.volume : 1;
        pv += typical * v;
        vol += v;
        values[i] = vol > 0 ? pv / vol : null;
      }
      return [
        {
          type: 'line',
          id: `${ind.id}-vwap`,
          title: 'VWAP',
          color: '#e65100',
          pane: 'overlay',
          data: toLine(candles, values),
          lineWidth: 2,
        },
      ];
    }
    case 'rsi': {
      const length = Math.max(2, Math.floor(ind.params.length || 14));
      return [
        {
          type: 'line',
          id: `${ind.id}-rsi`,
          title: `RSI ${length}`,
          color: '#7b1fa2',
          pane: 'oscillator',
          data: toLine(candles, rsi(closes, length)),
          lineWidth: 1,
        },
      ];
    }
    case 'macd': {
      const fast = Math.max(1, Math.floor(ind.params.fast || 12));
      const slow = Math.max(fast + 1, Math.floor(ind.params.slow || 26));
      const signal = Math.max(1, Math.floor(ind.params.signal || 9));
      const emaFast = ema(closes, fast);
      const emaSlow = ema(closes, slow);
      const dif: (number | null)[] = closes.map((_, i) =>
        emaFast[i] == null || emaSlow[i] == null
          ? null
          : (emaFast[i] as number) - (emaSlow[i] as number),
      );
      // EMA of DIF ignoring leading nulls
      const difNums = dif.map((v) => (v == null ? 0 : v));
      const firstValid = dif.findIndex((v) => v != null);
      const deaFull = ema(
        firstValid >= 0 ? difNums.slice(firstValid) : [],
        signal,
      );
      const dea: (number | null)[] = new Array(closes.length).fill(null);
      for (let i = 0; i < deaFull.length; i++) {
        dea[firstValid + i] = deaFull[i];
      }
      const hist: HistPoint[] = [];
      for (let i = 0; i < candles.length; i++) {
        if (dif[i] == null || dea[i] == null) continue;
        const h = (dif[i] as number) - (dea[i] as number);
        hist.push({
          time: candles[i].time,
          value: h,
          color:
            h >= 0 ? 'rgba(46, 125, 50, 0.55)' : 'rgba(198, 40, 40, 0.55)',
        });
      }
      return [
        {
          type: 'hist',
          id: `${ind.id}-hist`,
          title: 'MACD hist',
          pane: 'oscillator',
          data: hist,
        },
        {
          type: 'line',
          id: `${ind.id}-dif`,
          title: 'DIF',
          color: '#2962ff',
          pane: 'oscillator',
          data: toLine(candles, dif),
          lineWidth: 1,
        },
        {
          type: 'line',
          id: `${ind.id}-dea`,
          title: 'DEA',
          color: '#ff6d00',
          pane: 'oscillator',
          data: toLine(candles, dea),
          lineWidth: 1,
        },
      ];
    }
    case 'udbox': {
      const window = Math.max(3, Math.floor(ind.params.window || 20));
      const minW =
        ind.params.minWidthPct > 0 ? ind.params.minWidthPct : 0.015;
      const maxW =
        ind.params.maxWidthPct > 0 ? ind.params.maxWidthPct : 0.05;
      const tops: (number | null)[] = new Array(candles.length).fill(null);
      const bottoms: (number | null)[] = new Array(candles.length).fill(null);
      const mids: (number | null)[] = new Array(candles.length).fill(null);
      let curTop: number | null = null;
      let curBottom: number | null = null;

      for (let i = window - 1; i < candles.length; i++) {
        let hi = -Infinity;
        let lo = Infinity;
        for (let j = i - window + 1; j <= i; j++) {
          if (candles[j].high > hi) hi = candles[j].high;
          if (candles[j].low < lo) lo = candles[j].low;
        }
        if (hi > lo && lo > 0) {
          const widthPct = (hi - lo) / lo;
          if (widthPct >= minW && widthPct <= maxW) {
            curTop = hi;
            curBottom = lo;
          }
        }
        if (curTop != null && curBottom != null) {
          tops[i] = curTop;
          bottoms[i] = curBottom;
          mids[i] = (curTop + curBottom) / 2;
        }
      }

      const out: IndicatorSeriesPayload[] = [
        {
          type: 'line',
          id: `${ind.id}-res`,
          title: '阻力',
          color: '#c62828',
          pane: 'overlay',
          data: toLine(candles, tops),
          lineWidth: 2,
        },
        {
          type: 'line',
          id: `${ind.id}-sup`,
          title: '支撑',
          color: '#2e7d32',
          pane: 'overlay',
          data: toLine(candles, bottoms),
          lineWidth: 2,
        },
        {
          type: 'line',
          id: `${ind.id}-mid`,
          title: '箱中',
          color: '#787b86',
          pane: 'overlay',
          data: toLine(candles, mids),
          lineWidth: 1,
          lineStyle: 2,
        },
      ];
      if (curTop != null && curBottom != null) {
        const wPct = ((curTop - curBottom) / curBottom) * 100;
        out.push(
          {
            type: 'priceline',
            id: `${ind.id}-pl-res`,
            title: `阻力 ${wPct.toFixed(1)}%`,
            color: '#c62828',
            price: curTop,
            lineWidth: 2,
            lineStyle: 0,
          },
          {
            type: 'priceline',
            id: `${ind.id}-pl-sup`,
            title: '支撑',
            color: '#2e7d32',
            price: curBottom,
            lineWidth: 2,
            lineStyle: 0,
          },
        );
      }
      return out;
    }
    default:
      return [];
  }
}

export function filterPresets(query: string): IndicatorPreset[] {
  const q = query.trim().toLowerCase();
  if (!q) return INDICATOR_PRESETS;
  return INDICATOR_PRESETS.filter((p) => {
    const hay = [
      p.name,
      p.short,
      p.description,
      p.kind,
      ...p.keywords,
    ]
      .join(' ')
      .toLowerCase();
    return hay.includes(q) || q === '/';
  });
}
