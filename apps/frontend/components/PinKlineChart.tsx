import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Box,
  Chip,
  ClickAwayListener,
  IconButton,
  Paper,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import ShowChartIcon from '@mui/icons-material/ShowChart';
import FullscreenIcon from '@mui/icons-material/Fullscreen';
import FullscreenExitIcon from '@mui/icons-material/FullscreenExit';
import {
  CandlestickData,
  ColorType,
  createChart,
  CrosshairMode,
  HistogramData,
  IChartApi,
  ISeriesApi,
  IPriceLine,
  MouseEventParams,
  Time,
} from 'lightweight-charts';
import {
  buildOrderBookPinLevels,
  PinLevel,
  qtyAtPrice,
} from './pinLevels';
import {
  ActiveIndicator,
  CandlePoint,
  computeIndicator,
  createActiveFromPreset,
  filterPresets,
  IndicatorPreset,
  IndicatorSeriesPayload,
} from './chartIndicators';

export type { PinLevel };
export type KlineBar = {
  t: string;
  o: number;
  h: number;
  l: number;
  c: number;
  v?: number;
};
export { buildOrderBookPinLevels };

export type ChartPosition = {
  symbol?: string;
  side?: string;
  positionAmt?: number;
  entryPrice?: number;
  markPrice?: number;
  unrealizedPnL?: number;
  roePct?: number;
  leverage?: number;
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
  klineSource?: string;
  height?: number;
  /** Current futures position — drawn at entry like Binance */
  position?: ChartPosition | null;
};

type Ohlcv = {
  time?: Time;
  o: number;
  h: number;
  l: number;
  c: number;
  v: number;
};

type SeriesHandle =
  | ISeriesApi<'Line'>
  | ISeriesApi<'Histogram'>
  | ISeriesApi<'Candlestick'>;

const BUY = '#2e7d32';
const SELL = '#c62828';
const ENTRY = '#f9a825';

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

function toChartTime(iso: string): Time {
  return Math.floor(new Date(iso).getTime() / 1000) as Time;
}

function fmtCompact(v: number, digits = 4): string {
  if (!Number.isFinite(v)) return '—';
  return Number(v.toPrecision(8)).toLocaleString(undefined, {
    maximumFractionDigits: digits,
  });
}

function fmtVol(v: number): string {
  if (!Number.isFinite(v) || v <= 0) return '—';
  if (v >= 1e6) return `${(v / 1e6).toFixed(2)}M`;
  if (v >= 1e3) return `${(v / 1e3).toFixed(2)}K`;
  return fmtCompact(v, 2);
}

function positionTitle(pos: ChartPosition): string {
  const amt = Math.abs(Number(pos.positionAmt || 0));
  const long = String(pos.side || '').toUpperCase() !== 'SHORT';
  const side = long ? '多' : '空';
  const upnl = Number(pos.unrealizedPnL);
  const upnlPart = Number.isFinite(upnl)
    ? ` ${upnl > 0 ? '+' : ''}${fmtCompact(upnl, 2)}`
    : '';
  return `${side} ${fmtCompact(amt, 4)}${upnlPart}`;
}

function barToOhlcv(k: KlineBar): Ohlcv {
  return {
    time: toChartTime(k.t),
    o: k.o,
    h: k.h,
    l: k.l,
    c: k.c,
    v: Number(k.v) || 0,
  };
}

function toCandles(klines: KlineBar[]): CandlePoint[] {
  return klines
    .map((k) => ({
      time: toChartTime(k.t),
      open: k.o,
      high: k.h,
      low: k.l,
      close: k.c,
      volume: Number(k.v) || 0,
    }))
    .sort((a, b) => (a.time as number) - (b.time as number));
}

/**
 * TradingView Lightweight Charts candlesticks with qty@price pin overlays,
 * volume, position, and searchable indicators (/).
 */
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
  klineSource,
  height = 360,
  position,
}: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const seriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null);
  const volumeRef = useRef<ISeriesApi<'Histogram'> | null>(null);
  const linesRef = useRef<IPriceLine[]>([]);
  const indicatorLinesRef = useRef<IPriceLine[]>([]);
  const indicatorSeriesRef = useRef<Map<string, SeriesHandle>>(new Map());
  const latestRef = useRef<Ohlcv | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);

  const [indicators, setIndicators] = useState<ActiveIndicator[]>([]);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerQuery, setPickerQuery] = useState('');
  const [fullscreen, setFullscreen] = useState(false);
  const [viewportH, setViewportH] = useState(0);

  const lastBar = useMemo(() => {
    if (!klines?.length) return null;
    const sorted = [...klines].sort(
      (a, b) => new Date(a.t).getTime() - new Date(b.t).getTime(),
    );
    return barToOhlcv(sorted[sorted.length - 1]);
  }, [klines]);

  const [ohlcv, setOhlcv] = useState<Ohlcv | null>(null);

  useEffect(() => {
    latestRef.current = lastBar;
    setOhlcv(lastBar);
  }, [lastBar]);

  useEffect(() => {
    if (!fullscreen) return;
    const onResize = () => setViewportH(window.innerHeight);
    onResize();
    window.addEventListener('resize', onResize);
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      window.removeEventListener('resize', onResize);
      document.body.style.overflow = prev;
    };
  }, [fullscreen]);

  const chartHeight = fullscreen
    ? Math.max((viewportH || (typeof window !== 'undefined' ? window.innerHeight : 800)) - 110, 420)
    : height;

  const presets = useMemo(() => filterPresets(pickerQuery), [pickerQuery]);

  const addIndicator = useCallback((preset: IndicatorPreset) => {
    setIndicators((prev) => {
      // only one oscillator pane at a time
      let next = prev;
      if (preset.pane === 'oscillator') {
        next = prev.filter((p) => p.pane !== 'oscillator');
      }
      return [...next, createActiveFromPreset(preset, next.length)];
    });
    setPickerOpen(false);
    setPickerQuery('');
  }, []);

  const removeIndicator = useCallback((id: string) => {
    setIndicators((prev) => prev.filter((p) => p.id !== id));
  }, []);

  const openPicker = useCallback(() => {
    setPickerOpen(true);
    // focus search after paint so "/" is not typed into the field
    requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select?.();
    });
  }, []);

  const toggleFullscreen = useCallback(() => {
    setFullscreen((v) => !v);
    setPickerOpen(false);
  }, []);

  // keep chart wrapper focusable in fullscreen so shortcuts work
  useEffect(() => {
    if (fullscreen) {
      wrapRef.current?.focus({ preventScroll: true });
    }
  }, [fullscreen]);

  // "/" opens indicator picker; Esc exits picker then fullscreen
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      const editing =
        tag === 'INPUT' ||
        tag === 'TEXTAREA' ||
        (e.target as HTMLElement)?.isContentEditable;

      if (e.key === 'Escape') {
        if (pickerOpen) {
          e.preventDefault();
          setPickerOpen(false);
          wrapRef.current?.focus({ preventScroll: true });
          return;
        }
        if (fullscreen) {
          e.preventDefault();
          setFullscreen(false);
          return;
        }
      }

      if (editing) return;

      if (e.key === '/' && !e.metaKey && !e.ctrlKey && !e.altKey) {
        const wrap = wrapRef.current;
        if (!wrap) return;
        // In fullscreen always allow; otherwise require hover/focus on chart
        if (!fullscreen) {
          const active = document.activeElement;
          if (
            !wrap.contains(active) &&
            active !== document.body &&
            !wrap.matches(':hover')
          ) {
            return;
          }
        }
        e.preventDefault();
        openPicker();
        return;
      }

      if (
        (e.key === 'f' || e.key === 'F') &&
        !e.metaKey &&
        !e.ctrlKey &&
        !e.altKey
      ) {
        const wrap = wrapRef.current;
        if (!wrap) return;
        if (
          !fullscreen &&
          !wrap.matches(':hover') &&
          !wrap.contains(document.activeElement)
        ) {
          return;
        }
        e.preventDefault();
        toggleFullscreen();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [openPicker, pickerOpen, fullscreen, toggleFullscreen]);

  useEffect(() => {
    if (!containerRef.current) return;

    const chart = createChart(containerRef.current, {
      height: chartHeight,
      layout: {
        background: { type: ColorType.Solid, color: '#fafafa' },
        textColor: '#424242',
      },
      grid: {
        vertLines: { color: '#eeeeee' },
        horzLines: { color: '#eeeeee' },
      },
      crosshair: { mode: CrosshairMode.Normal },
      rightPriceScale: { borderColor: '#e0e0e0' },
      timeScale: {
        borderColor: '#e0e0e0',
        timeVisible: true,
        secondsVisible: false,
      },
    });
    const series = chart.addCandlestickSeries({
      upColor: BUY,
      downColor: SELL,
      borderUpColor: BUY,
      borderDownColor: SELL,
      wickUpColor: BUY,
      wickDownColor: SELL,
    });
    series.priceScale().applyOptions({
      scaleMargins: { top: 0.08, bottom: 0.24 },
    });

    const volume = chart.addHistogramSeries({
      priceFormat: { type: 'volume' },
      priceScaleId: 'volume',
      lastValueVisible: false,
      priceLineVisible: false,
    });
    chart.priceScale('volume').applyOptions({
      scaleMargins: { top: 0.78, bottom: 0 },
      borderVisible: false,
    });

    chartRef.current = chart;
    seriesRef.current = series;
    volumeRef.current = volume;

    const onCrosshair = (param: MouseEventParams) => {
      if (!param.time || !param.seriesData) {
        setOhlcv(latestRef.current);
        return;
      }
      const candle = param.seriesData.get(series) as
        | CandlestickData
        | undefined;
      if (!candle || candle.open == null) {
        setOhlcv(latestRef.current);
        return;
      }
      const hist = param.seriesData.get(volume) as HistogramData | undefined;
      setOhlcv({
        time: param.time,
        o: candle.open,
        h: candle.high,
        l: candle.low,
        c: candle.close,
        v: hist?.value ?? 0,
      });
    };
    chart.subscribeCrosshairMove(onCrosshair);

    const onResize = () => {
      if (!containerRef.current || !chartRef.current) return;
      chartRef.current.applyOptions({
        width: containerRef.current.clientWidth,
      });
    };
    onResize();
    window.addEventListener('resize', onResize);

    return () => {
      window.removeEventListener('resize', onResize);
      chart.unsubscribeCrosshairMove(onCrosshair);
      chart.remove();
      chartRef.current = null;
      seriesRef.current = null;
      volumeRef.current = null;
      linesRef.current = [];
      indicatorSeriesRef.current.clear();
    };
    // intentionally mount once; height applied separately
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // resize chart when entering/exiting fullscreen or height prop changes
  useEffect(() => {
    const chart = chartRef.current;
    const el = containerRef.current;
    if (!chart || !el) return;
    chart.applyOptions({
      height: chartHeight,
      width: el.clientWidth,
    });
  }, [chartHeight, fullscreen]);

  // candles + pins + position
  useEffect(() => {
    const series = seriesRef.current;
    const volume = volumeRef.current;
    const chart = chartRef.current;
    if (!series || !chart || !klines?.length) return;

    const sorted = toCandles(klines);
    series.setData(
      sorted.map(({ time, open, high, low, close }) => ({
        time,
        open,
        high,
        low,
        close,
      })),
    );

    if (volume) {
      volume.setData(
        sorted.map(({ time, open, close, volume: vol }) => ({
          time,
          value: vol,
          color:
            close >= open
              ? 'rgba(46, 125, 50, 0.45)'
              : 'rgba(198, 40, 40, 0.45)',
        })),
      );
    }

    for (const line of linesRef.current) {
      try {
        series.removePriceLine(line);
      } catch {
        /* ignore */
      }
    }
    linesRef.current = [];

    const last =
      lastProp && lastProp > 0 ? lastProp : klines[klines.length - 1].c;
    const levels = resolvePinLevels(pinLevels, pins, last, quantity);

    if (lower && upper && upper > lower) {
      linesRef.current.push(
        series.createPriceLine({
          price: lower,
          color: '#90caf9',
          lineWidth: 1,
          lineStyle: 2,
          axisLabelVisible: true,
          title: 'L',
        }),
      );
      linesRef.current.push(
        series.createPriceLine({
          price: upper,
          color: '#90caf9',
          lineWidth: 1,
          lineStyle: 2,
          axisLabelVisible: true,
          title: 'U',
        }),
      );
    }

    for (const lv of levels) {
      if (!(lv.price > 0)) continue;
      const isBuy = lv.side !== 'sell';
      const title = lv.label || qtyAtPrice(lv.quantity ?? quantity, lv.price);
      linesRef.current.push(
        series.createPriceLine({
          price: lv.price,
          color: isBuy ? BUY : SELL,
          lineWidth: 1,
          lineStyle: 2,
          axisLabelVisible: true,
          title,
        }),
      );
    }

    const entry = Number(position?.entryPrice);
    const amt = Math.abs(Number(position?.positionAmt || 0));
    if (position && entry > 0 && amt > 0) {
      const long = String(position.side || '').toUpperCase() !== 'SHORT';
      linesRef.current.push(
        series.createPriceLine({
          price: entry,
          color: long ? BUY : SELL,
          lineWidth: 2,
          lineStyle: 0,
          axisLabelVisible: true,
          title: positionTitle(position),
        }),
      );
      const mark = Number(position.markPrice);
      if (mark > 0 && Math.abs(mark - entry) / entry > 1e-6) {
        linesRef.current.push(
          series.createPriceLine({
            price: mark,
            color: ENTRY,
            lineWidth: 1,
            lineStyle: 1,
            axisLabelVisible: true,
            title: '标记',
          }),
        );
      }
    }

    linesRef.current.push(
      series.createPriceLine({
        price: last,
        color: '#1565c0',
        lineWidth: 1,
        lineStyle: 1,
        axisLabelVisible: true,
        title: 'last',
      }),
    );

    chart.timeScale().fitContent();
  }, [
    klines,
    pins,
    pinLevels,
    lastProp,
    quantity,
    lower,
    upper,
    position,
  ]);

  // indicators
  useEffect(() => {
    const chart = chartRef.current;
    const candle = seriesRef.current;
    if (!chart || !candle || !klines?.length) return;

    const hasOsc = indicators.some((i) => i.pane === 'oscillator');
    candle.priceScale().applyOptions({
      scaleMargins: hasOsc
        ? { top: 0.06, bottom: 0.42 }
        : { top: 0.08, bottom: 0.24 },
    });
    chart.priceScale('volume').applyOptions({
      scaleMargins: { top: 0.82, bottom: 0 },
      borderVisible: false,
    });
    if (hasOsc) {
      chart.priceScale('osc').applyOptions({
        scaleMargins: { top: 0.58, bottom: 0.2 },
        borderVisible: false,
      });
    }

    // remove old indicator series / price lines
    Array.from(indicatorSeriesRef.current.values()).forEach((s) => {
      try {
        chart.removeSeries(s as any);
      } catch {
        /* ignore */
      }
    });
    indicatorSeriesRef.current.clear();
    for (const line of indicatorLinesRef.current) {
      try {
        candle.removePriceLine(line);
      } catch {
        /* ignore */
      }
    }
    indicatorLinesRef.current = [];

    const candles = toCandles(klines);
    const payloads: IndicatorSeriesPayload[] = [];
    for (const ind of indicators) {
      payloads.push(...computeIndicator(ind, candles));
    }

    for (const p of payloads) {
      if (p.type === 'line') {
        const s = chart.addLineSeries({
          color: p.color,
          lineWidth: (p.lineWidth || 1) as any,
          lineStyle: p.lineStyle ?? 0,
          priceScaleId: p.pane === 'oscillator' ? 'osc' : 'right',
          title: p.title,
          lastValueVisible: true,
          priceLineVisible: false,
          crosshairMarkerVisible: false,
        });
        s.setData(p.data);
        indicatorSeriesRef.current.set(p.id, s);
      } else if (p.type === 'hist') {
        const s = chart.addHistogramSeries({
          priceScaleId: 'osc',
          title: p.title,
          lastValueVisible: false,
          priceLineVisible: false,
        });
        s.setData(p.data);
        indicatorSeriesRef.current.set(p.id, s);
      } else if (p.type === 'priceline' && p.price > 0) {
        indicatorLinesRef.current.push(
          candle.createPriceLine({
            price: p.price,
            color: p.color,
            lineWidth: (p.lineWidth || 2) as any,
            lineStyle: p.lineStyle ?? 0,
            axisLabelVisible: true,
            title: p.title,
          }),
        );
      }
    }
  }, [indicators, klines]);

  if (!klines?.length) {
    return (
      <Typography variant="body2" color="text.secondary">
        暂无 K 线数据
      </Typography>
    );
  }

  const ivLabel = interval === '1d' ? '日线' : interval;
  const src = klineSource ? ` · ${klineSource}` : '';
  const entry = Number(position?.entryPrice);
  const amt = Math.abs(Number(position?.positionAmt || 0));
  const hasPos = !!(position && entry > 0 && amt > 0);
  const long = String(position?.side || '').toUpperCase() !== 'SHORT';
  const up = ohlcv ? ohlcv.c >= ohlcv.o : true;
  const pxColor = up ? BUY : SELL;

  return (
    <Box
      ref={wrapRef}
      tabIndex={0}
      sx={
        fullscreen
          ? {
              position: 'fixed',
              inset: 0,
              zIndex: 1400,
              bgcolor: '#fafafa',
              p: 1.5,
              display: 'flex',
              flexDirection: 'column',
              overflow: 'hidden', // avoid clipping picker; list uses disablePortal inside
            }
          : { width: '100%', position: 'relative' }
      }
    >
      <Box
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 1,
          mb: 0.5,
        }}
      >
        <Typography variant="caption" color="text.secondary">
          {symbol} {ivLabel}
          {src} · TradingView Lightweight Charts · qty@price · 成交量
          {hasPos && (
            <Box
              component="span"
              sx={{
                ml: 1,
                color: long ? BUY : SELL,
                fontWeight: 600,
              }}
            >
              · 持仓 {long ? '多' : '空'} {fmtCompact(amt, 4)} @{' '}
              {fmtCompact(entry, 6)}
              {Number.isFinite(Number(position?.unrealizedPnL)) && (
                <>
                  {' '}
                  (
                  {Number(position?.unrealizedPnL) > 0 ? '+' : ''}
                  {fmtCompact(Number(position?.unrealizedPnL), 2)})
                </>
              )}
            </Box>
          )}
        </Typography>
        <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Tooltip title="添加指标（快捷键 /）">
            <IconButton size="small" onClick={openPicker}>
              <ShowChartIcon fontSize="small" />
            </IconButton>
          </Tooltip>
          <Chip
            size="small"
            label="指标 /"
            onClick={openPicker}
            variant="outlined"
            sx={{ height: 24 }}
          />
          <Tooltip
            title={
              fullscreen
                ? '退出全屏（Esc / F）'
                : '全屏显示（快捷键 F）'
            }
          >
            <IconButton size="small" onClick={toggleFullscreen}>
              {fullscreen ? (
                <FullscreenExitIcon fontSize="small" />
              ) : (
                <FullscreenIcon fontSize="small" />
              )}
            </IconButton>
          </Tooltip>
        </Box>
      </Box>

      {indicators.length > 0 && (
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, mb: 0.75 }}>
          {indicators.map((ind) => (
            <Chip
              key={ind.id}
              size="small"
              label={ind.name}
              onDelete={() => removeIndicator(ind.id)}
              sx={{
                height: 24,
                bgcolor: ind.color ? `${ind.color}22` : undefined,
                borderColor: ind.color,
                borderStyle: 'solid',
                borderWidth: 1,
              }}
            />
          ))}
        </Box>
      )}

      {ohlcv && (
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: 1.5,
            mb: 0.75,
            fontFamily:
              'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
            fontSize: 12,
            lineHeight: 1.4,
          }}
        >
          {(
            [
              ['O', ohlcv.o],
              ['H', ohlcv.h],
              ['L', ohlcv.l],
              ['C', ohlcv.c],
            ] as const
          ).map(([k, v]) => (
            <Box key={k} component="span">
              <Box component="span" sx={{ color: 'text.secondary', mr: 0.5 }}>
                {k}
              </Box>
              <Box component="span" sx={{ color: pxColor, fontWeight: 600 }}>
                {fmtCompact(v, 6)}
              </Box>
            </Box>
          ))}
          <Box component="span">
            <Box component="span" sx={{ color: 'text.secondary', mr: 0.5 }}>
              V
            </Box>
            <Box component="span" sx={{ fontWeight: 600 }}>
              {fmtVol(ohlcv.v)}
            </Box>
          </Box>
        </Box>
      )}

      {pickerOpen && (
        <ClickAwayListener
          onClickAway={() => setPickerOpen(false)}
          mouseEvent="onMouseDown"
        >
          <Paper
            elevation={8}
            sx={{
              position: fullscreen ? 'fixed' : 'absolute',
              top: fullscreen ? 56 : 36,
              right: fullscreen ? 16 : 8,
              zIndex: 1600,
              width: 360,
              maxWidth: 'min(92vw, 360px)',
              p: 1,
              bgcolor: '#fff',
            }}
          >
            <Typography variant="caption" color="text.secondary" sx={{ px: 0.5, mb: 0.5, display: 'block' }}>
              搜索指标（/）{fullscreen ? ' · 全屏可用' : ''}
            </Typography>
            <TextField
              size="small"
              fullWidth
              autoFocus
              placeholder="MA / UDBox / RSI / MACD…"
              value={pickerQuery}
              inputRef={inputRef}
              onChange={(e) => setPickerQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Escape') {
                  e.stopPropagation();
                  setPickerOpen(false);
                  wrapRef.current?.focus({ preventScroll: true });
                  return;
                }
                if (e.key === 'Enter' && presets[0]) {
                  e.preventDefault();
                  addIndicator(presets[0]);
                }
              }}
            />
            <Box
              sx={{
                mt: 1,
                maxHeight: fullscreen ? '50vh' : 280,
                overflow: 'auto',
              }}
            >
              {presets.length === 0 ? (
                <Typography variant="body2" color="text.secondary" sx={{ px: 1, py: 1 }}>
                  无匹配指标
                </Typography>
              ) : (
                presets.map((option) => (
                  <Box
                    key={`${option.kind}-${option.short}-${option.defaults.length || 0}-${option.defaults.window || 0}`}
                    onClick={() => addIndicator(option)}
                    sx={{
                      px: 1,
                      py: 0.75,
                      cursor: 'pointer',
                      borderRadius: 1,
                      '&:hover': { bgcolor: 'action.hover' },
                    }}
                  >
                    <Typography variant="body2" fontWeight={600}>
                      {option.short}
                      <Typography
                        component="span"
                        variant="caption"
                        color="text.secondary"
                        sx={{ ml: 1 }}
                      >
                        {option.pane === 'overlay' ? '主图' : '副图'}
                      </Typography>
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {option.description}
                    </Typography>
                  </Box>
                ))
              )}
            </Box>
          </Paper>
        </ClickAwayListener>
      )}

      <Box
        ref={containerRef}
        sx={{
          width: '100%',
          height: chartHeight,
          bgcolor: '#fafafa',
          borderRadius: 1,
          flex: fullscreen ? 1 : undefined,
        }}
      />
    </Box>
  );
}
