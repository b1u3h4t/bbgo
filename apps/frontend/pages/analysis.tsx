import { useCallback, useEffect, useState, type ReactNode } from 'react';
import dynamic from 'next/dynamic';
import DashboardLayout from '../layouts/DashboardLayout';
import {
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  FormControl,
  Grid,
  InputLabel,
  MenuItem,
  Select,
  Tab,
  Tabs,
  TextField,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  LinearProgress,
  Accordion,
  AccordionSummary,
  AccordionDetails,
  List,
  ListItem,
  ListItemText,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  useMediaQuery,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import {
  queryAnalysisAvgDown,
  queryAnalysisGridCalc,
  queryAnalysisKlines,
  queryAnalysisMargin,
  queryAnalysisMarket,
  queryAnalysisProspec,
  queryAnalysisRegime,
  queryAnalysisTodayPnL,
  queryAnalysisTrend,
} from '../api/bbgo';
import { buildOrderBookPinLevels } from '../components/pinLevels';

// TradingView Lightweight Charts needs browser APIs
/** Horizontal swipe wrapper for wide tables on ~400px phones (e.g. Xiaomi K80 Pro). */
function ScrollTable({ children }: { children: ReactNode }) {
  return (
    <TableContainer
      className="bbgo-table-scroll"
      sx={{
        overflowX: 'auto',
        maxWidth: '100%',
        WebkitOverflowScrolling: 'touch',
      }}
    >
      {children}
    </TableContainer>
  );
}

const PinKlineChart = dynamic(() => import('../components/PinKlineChart'), {
  ssr: false,
  loading: () => (
    <Typography variant="body2" color="text.secondary">
      加载图表…
    </Typography>
  ),
});

const CHART_INTERVALS: { value: string; label: string }[] = [
  { value: '5m', label: '5m' },
  { value: '15m', label: '15m' },
  { value: '30m', label: '30m' },
  { value: '1h', label: '1h' },
  { value: '2h', label: '2h' },
  { value: '4h', label: '4h' },
  { value: '6h', label: '6h' },
  { value: '8h', label: '8h' },
  { value: '12h', label: '12h' },
  { value: '1d', label: '日线' },
];

const PROSPEC_INTERVALS: { value: string; label: string }[] = [
  { value: '15m', label: '15m' },
  { value: '30m', label: '30m' },
  { value: '1h', label: '1h' },
  { value: '4h', label: '4h' },
  { value: '1d', label: '日线' },
];

type PnlPeriod = 'today' | '7d' | '30d' | '90d';

const PNL_PERIOD_LABEL: Record<PnlPeriod, string> = {
  today: '今日',
  '7d': '7日',
  '30d': '30日',
  '90d': '90日',
};

function pnlColor(v: number) {
  if (v > 0) return '#2e7d32';
  if (v < 0) return '#c62828';
  return undefined;
}

function findPosition(
  positions: any[] | null | undefined,
  symbol: string,
): any | null {
  if (!positions?.length || !symbol) return null;
  const s = String(symbol).toUpperCase();
  return (
    positions.find((p) => String(p.symbol || '').toUpperCase() === s) || null
  );
}

function fmtNum(v: any, digits = 4): string {
  const n = Number(v);
  if (!Number.isFinite(n)) return '—';
  return n.toLocaleString(undefined, {
    maximumFractionDigits: digits,
    minimumFractionDigits: 0,
  });
}

function fmtSigned(v: any, digits = 4): string {
  const n = Number(v);
  if (!Number.isFinite(n)) return '—';
  const s = n.toLocaleString(undefined, {
    maximumFractionDigits: digits,
    minimumFractionDigits: 0,
  });
  return n > 0 ? `+${s}` : s;
}

/** Binance-style futures positions table */
function PositionsPanel({
  positions,
  highlightSymbol,
  title = '当前持仓',
  dense,
  onSelectSymbol,
}: {
  positions?: any[] | null;
  highlightSymbol?: string;
  title?: string;
  dense?: boolean;
  onSelectSymbol?: (symbol: string) => void;
}) {
  const rows = (positions || []).slice().sort(
    (a: any, b: any) => Number(a.unrealizedPnL) - Number(b.unrealizedPnL)
  );
  const uPnLSum = rows.reduce(
    (s: number, p: any) => s + Number(p.unrealizedPnL || 0),
    0
  );

  return (
    <Card variant="outlined" sx={{ mb: 2 }}>
      <CardContent sx={{ pb: dense ? 1 : undefined }}>
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 1,
            mb: 1,
          }}
        >
          <Typography variant="subtitle1">
            {title}
            <Typography
              component="span"
              variant="caption"
              color="text.secondary"
              sx={{ ml: 1 }}
            >
              {rows.length} 仓
            </Typography>
          </Typography>
          <Typography
            variant="body2"
            sx={{ color: pnlColor(uPnLSum), fontWeight: 600 }}
          >
            未实现盈亏合计 {fmtSigned(uPnLSum, 2)} USDT
          </Typography>
        </Box>
        {rows.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            无持仓
          </Typography>
        ) : (
          <ScrollTable>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>合约</TableCell>
                <TableCell>方向</TableCell>
                <TableCell align="right">数量</TableCell>
                <TableCell align="right">开仓均价</TableCell>
                <TableCell align="right">标记价格</TableCell>
                <TableCell align="right">强平价格</TableCell>
                <TableCell align="right">保证金</TableCell>
                <TableCell align="right">未实现盈亏 (ROE)</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((p: any) => {
                const long = String(p.side).toUpperCase() === 'LONG';
                const hi =
                  highlightSymbol &&
                  String(p.symbol).toUpperCase() ===
                    String(highlightSymbol).toUpperCase();
                const size = Math.abs(Number(p.positionAmt || 0));
                return (
                  <TableRow
                    key={p.symbol}
                    hover={!!onSelectSymbol}
                    selected={!!hi}
                    sx={{
                      cursor: onSelectSymbol ? 'pointer' : undefined,
                      bgcolor: hi ? 'action.selected' : undefined,
                    }}
                    onClick={() => onSelectSymbol?.(p.symbol)}
                  >
                    <TableCell>
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                        <Typography variant="body2" fontWeight={600}>
                          {p.symbol}
                        </Typography>
                        <Chip
                          size="small"
                          label={`${Number(p.leverage) || 1}x`}
                          sx={{ height: 20, fontSize: 11 }}
                        />
                        <Chip
                          size="small"
                          label={p.marginType === 'isolated' ? '逐仓' : '全仓'}
                          variant="outlined"
                          sx={{ height: 20, fontSize: 11 }}
                        />
                      </Box>
                    </TableCell>
                    <TableCell>
                      <Typography
                        variant="body2"
                        fontWeight={700}
                        sx={{ color: long ? '#2e7d32' : '#c62828' }}
                      >
                        {long ? '多' : '空'}
                      </Typography>
                    </TableCell>
                    <TableCell align="right">{fmtNum(size, 4)}</TableCell>
                    <TableCell align="right">{fmtNum(p.entryPrice, 6)}</TableCell>
                    <TableCell align="right">{fmtNum(p.markPrice, 6)}</TableCell>
                    <TableCell align="right">
                      {Number(p.liquidationPrice) > 0
                        ? fmtNum(p.liquidationPrice, 6)
                        : '—'}
                    </TableCell>
                    <TableCell align="right">
                      {fmtNum(p.initialMarginEst, 2)}
                    </TableCell>
                    <TableCell align="right">
                      <Typography
                        variant="body2"
                        fontWeight={600}
                        sx={{ color: pnlColor(Number(p.unrealizedPnL)) }}
                      >
                        {fmtSigned(p.unrealizedPnL, 2)}
                      </Typography>
                      <Typography
                        variant="caption"
                        sx={{ color: pnlColor(Number(p.roePct)) }}
                      >
                        ({fmtSigned(p.roePct, 2)}%)
                      </Typography>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
          </ScrollTable>
        )}
      </CardContent>
    </Card>
  );
}

function TabPanel({
  value,
  index,
  children,
}: {
  value: number;
  index: number;
  children: any;
}) {
  if (value !== index) return null;
  return <Box sx={{ pt: 2 }}>{children}</Box>;
}

export default function AnalysisPage() {
  const [tab, setTab] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const [margin, setMargin] = useState<any>(null);
  const [market, setMarket] = useState<any>(null);
  const [pnl, setPnl] = useState<any>(null);
  const [grid, setGrid] = useState<any>(null);
  const [pnlChart, setPnlChart] = useState<any>(null);
  const [marketChart, setMarketChart] = useState<any>(null);
  const [avgDown, setAvgDown] = useState<any>(null);
  const [trend, setTrend] = useState<any>(null);
  const [trendTop, setTrendTop] = useState('25');
  const [trendMinVol, setTrendMinVol] = useState('50000000');
  const [trendBars, setTrendBars] = useState('1920');
  const [regime, setRegime] = useState<any>(null);
  const [regimeSymbol, setRegimeSymbol] = useState('BTCUSDT');
  const [prospec, setProspec] = useState<any>(null);
  const [prospecSymbol, setProspecSymbol] = useState('BTCUSDT');
  const [prospecChart, setProspecChart] = useState<{
    klines: any[];
    klineSource?: string;
    interval: string;
  } | null>(null);
  const [prospecChartIv, setProspecChartIv] = useState('1d');
  const [avgSymbol, setAvgSymbol] = useState('DOGEUSDT');
  const [avgAddIm, setAvgAddIm] = useState('200');
  const [avgPrice, setAvgPrice] = useState('');
  const [avgLeverage, setAvgLeverage] = useState('3');
  const [gridInterval, setGridInterval] = useState('1h');
  const [pnlInterval, setPnlInterval] = useState('1h');
  const [marketInterval, setMarketInterval] = useState('15m');
  const [pnlPeriod, setPnlPeriod] = useState<PnlPeriod>('today');

  const [symbol, setSymbol] = useState('AVAXUSDT');
  const [atrMult, setAtrMult] = useState('1');
  const [gridNumber, setGridNumber] = useState('8');
  const [quantity, setQuantity] = useState('');
  const [leverage, setLeverage] = useState('3');
  const [targetUtil, setTargetUtil] = useState('0.65');

  const loadMargin = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setMargin(await queryAnalysisMargin());
    } catch (e: any) {
      setError(e?.message || 'failed to load margin');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadMarket = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setMarket(await queryAnalysisMarket());
    } catch (e: any) {
      setError(e?.message || 'failed to load market');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadTrend = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setTrend(
        await queryAnalysisTrend('binance', {
          top: Number(trendTop) || 25,
          minQuoteVol: Number(trendMinVol) || 50_000_000,
          bars: Number(trendBars) || 1920,
        }),
      );
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || 'failed to load trend');
    } finally {
      setLoading(false);
    }
  }, [trendTop, trendMinVol, trendBars]);

  const loadRegime = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setRegime(await queryAnalysisRegime('binance', { symbol: regimeSymbol || 'BTCUSDT' }));
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || 'failed to load regime');
    } finally {
      setLoading(false);
    }
  }, [regimeSymbol]);

  const loadProspec = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const sym = prospecSymbol || 'BTCUSDT';
      const [p, kl] = await Promise.all([
        queryAnalysisProspec('binance', { symbol: sym, scan: true }),
        queryAnalysisKlines({
          symbol: sym,
          interval: prospecChartIv || '1d',
          limit:
            prospecChartIv === '15m' ||
            prospecChartIv === '30m' ||
            prospecChartIv === '1h' ||
            prospecChartIv === '4h'
              ? 200
              : 180,
        }),
      ]);
      setProspec(p);
      setProspecChart({
        klines: kl?.klines || [],
        klineSource: kl?.klineSource,
        interval: prospecChartIv || '1d',
      });
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || 'failed to load prospec');
    } finally {
      setLoading(false);
    }
  }, [prospecSymbol, prospecChartIv]);

  const changeProspecChartIv = useCallback(
    async (v: string) => {
      setProspecChartIv(v);
      try {
        const lim =
          v === '15m' || v === '30m' || v === '1h' || v === '4h' ? 200 : 180;
        const kl = await queryAnalysisKlines({
          symbol: prospecSymbol || 'BTCUSDT',
          interval: v,
          limit: lim,
        });
        setProspecChart({
          klines: kl?.klines || [],
          klineSource: kl?.klineSource,
          interval: v,
        });
      } catch {
        /* keep previous chart */
      }
    },
    [prospecSymbol],
  );

  const loadPnl = useCallback(async (period: PnlPeriod = pnlPeriod) => {
    setLoading(true);
    setError('');
    try {
      setPnl(await queryAnalysisTodayPnL('binance', period));
    } catch (e: any) {
      setError(e?.message || 'failed to load pnl');
    } finally {
      setLoading(false);
    }
  }, [pnlPeriod]);

  const loadGrid = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const params: Record<string, string | number> = {
        session: 'binance',
        symbol,
        atrMult,
        gridNumber,
        leverage,
        targetUtil,
        interval: gridInterval,
      };
      if (quantity) params.quantity = quantity;
      setGrid(await queryAnalysisGridCalc(params));
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || 'grid calc failed');
    } finally {
      setLoading(false);
    }
  }, [symbol, atrMult, gridNumber, quantity, leverage, targetUtil, gridInterval]);

  const reloadGridKlines = useCallback(
    async (iv: string) => {
      if (!grid?.symbol) return;
      setGridInterval(iv);
      setLoading(true);
      setError('');
      try {
        const data = await queryAnalysisKlines({
          symbol: grid.symbol,
          interval: iv,
        });
        // keep calc pins/band; only swap candle series (+ recolor sides vs last)
        setGrid((prev: any) =>
          prev
            ? {
                ...prev,
                interval: data.interval || iv,
                last: data.last ?? prev.last,
                klines: data.klines || [],
                klines1h: data.klines || [],
                // prefer calc pins; recolor with latest last
                pinLevels: buildOrderBookPinLevels(
                  prev.pins || [],
                  data.last ?? prev.last,
                  data.quantity ?? prev.quantity,
                ),
                quantity: data.quantity ?? prev.quantity,
                klineSource: data.klineSource,
              }
            : prev,
        );
      } catch (e: any) {
        setError(e?.message || 'failed to load klines');
      } finally {
        setLoading(false);
      }
    },
    [grid?.symbol],
  );

  const loadPnlChart = useCallback(
    async (sym: string, iv = pnlInterval) => {
      if (!sym || sym === '(account)') return;
      setLoading(true);
      setError('');
      try {
        setPnlInterval(iv);
        setPnlChart(
          await queryAnalysisKlines({ symbol: sym, interval: iv }),
        );
      } catch (e: any) {
        setError(e?.message || 'failed to load klines');
      } finally {
        setLoading(false);
      }
    },
    [pnlInterval],
  );

  const loadMarketChart = useCallback(
    async (sym: string, iv = marketInterval) => {
      if (!sym) return;
      setLoading(true);
      setError('');
      try {
        setMarketInterval(iv);
        setMarketChart(
          await queryAnalysisKlines({ symbol: sym, interval: iv }),
        );
      } catch (e: any) {
        setError(e?.message || 'failed to load klines');
      } finally {
        setLoading(false);
      }
    },
    [marketInterval],
  );

  const loadAvgDown = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const params: Record<string, string | number> = {
        leverage: avgLeverage,
        targetUtil: 0.65,
        reserveAvail: 3000,
        maxUtil: 0.7,
      };
      if (avgSymbol) {
        params.symbol = avgSymbol;
        if (avgAddIm) params.addIm = avgAddIm;
        if (avgPrice) params.price = avgPrice;
      }
      const data = await queryAnalysisAvgDown(params);
      setAvgDown(data);
      if (!avgPrice && data?.scenario?.limitPrice) {
        // keep manual override empty; mark shown in scenario
      }
      if (
        avgSymbol &&
        data?.candidates?.length &&
        !data.candidates.find((c: any) => c.symbol === avgSymbol)
      ) {
        const first =
          data.candidates.find((c: any) => c.underwater) ||
          data.candidates[0];
        if (first?.symbol) setAvgSymbol(first.symbol);
      }
    } catch (e: any) {
      setError(e?.message || 'failed to load avg-down calc');
    } finally {
      setLoading(false);
    }
  }, [avgSymbol, avgAddIm, avgPrice, avgLeverage]);

  useEffect(() => {
    loadMargin();
    loadMarket();
    loadPnl();
  }, [loadMargin, loadMarket, loadPnl]);

  const acc = margin?.account;
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('md'), { noSsr: true });
  const chartH = isMobile ? 260 : 360;
  const chartHLg = isMobile ? 280 : 380;
  const chartHSm = isMobile ? 240 : 320;

  return (
    <DashboardLayout>
      <Box
        sx={{
          p: { xs: 1.5, sm: 2, md: 3 },
          maxWidth: 1400,
          mx: 'auto',
          width: '100%',
          overflowX: 'hidden',
        }}
      >
        <Typography variant={isMobile ? 'h6' : 'h5'} gutterBottom>
          Analysis
        </Typography>
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ mb: 2, display: { xs: 'none', sm: 'block' } }}
        >
          行情结构、保证金利用率、ATR 网格试算、区间/每日已实现盈亏（CST，自 2026-09-03 部署起）
        </Typography>

        {loading && <LinearProgress sx={{ mb: 2 }} />}
        {error && (
          <Typography color="error" sx={{ mb: 2 }}>
            {error}
          </Typography>
        )}

        <Tabs
          value={tab}
          onChange={(_, v) => {
            setTab(v);
            if (v === 1 || v === 2) loadMargin();
            if (v === 3) {
              loadPnl();
              loadMargin();
            }
            if (v === 4) loadAvgDown();
            if (v === 5) loadTrend();
            if (v === 6) loadRegime();
            if (v === 7) loadProspec();
          }}
          variant="scrollable"
          scrollButtons="auto"
          allowScrollButtonsMobile
          sx={{ mb: 1, borderBottom: 1, borderColor: 'divider' }}
        >
          <Tab label="行情分析" />
          <Tab label="保证金" />
          <Tab label="网格计算" />
          <Tab label="盈亏" />
          <Tab label="慎重补仓" />
          <Tab label="趋势选股" />
          <Tab label="优道嵌套" />
          <Tab label="专业投机" />
        </Tabs>

        <TabPanel value={tab} index={0}>
          <Box
            sx={{
              display: 'flex',
              gap: 1,
              mb: 2,
              alignItems: 'center',
              flexWrap: 'wrap',
            }}
          >
            <Button variant="contained" onClick={loadMarket} size={isMobile ? 'small' : 'medium'}>
              刷新
            </Button>
            {market?.stage && (
              <Chip
                label={`BTC止跌: ${market.stage}`}
                color={
                  market.stage.includes('偏强')
                    ? 'success'
                    : market.stage.includes('观望')
                    ? 'warning'
                    : 'default'
                }
              />
            )}
            {market?.stageTop && (
              <Chip
                label={`BTC止涨: ${market.stageTop}`}
                color={
                  market.stageTop.includes('偏强')
                    ? 'error'
                    : market.stageTop.includes('观望')
                    ? 'warning'
                    : 'default'
                }
              />
            )}
            {market?.asOf && (
              <Typography variant="caption" color="text.secondary">
                {market.asOf}
              </Typography>
            )}
          </Box>

          <Accordion defaultExpanded={false} sx={{ mb: 2 }}>
            <AccordionSummary expandIcon={<ExpandMoreIcon />}>
              <Typography variant="subtitle2">
                判断依据与打分规则（与后端 /api/analysis/market 同步）
              </Typography>
            </AccordionSummary>
            <AccordionDetails>
              {market?.rules ? (
                <Grid container spacing={2}>
                  <Grid item xs={12}>
                    <Typography variant="body2" sx={{ mb: 1 }}>
                      {market.rules.purpose}
                    </Typography>
                    {market.stageNote && (
                      <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
                        {market.stageNote}
                      </Typography>
                    )}
                    <Typography variant="caption" color="warning.main">
                      {market.rules.validation}
                    </Typography>
                  </Grid>
                  <Grid item xs={12} md={5}>
                    <Typography variant="subtitle2" gutterBottom>
                      数据口径
                    </Typography>
                    <List dense disablePadding>
                      {(market.rules.data || []).map((line: string, i: number) => (
                        <ListItem key={i} sx={{ py: 0.25, alignItems: 'flex-start' }}>
                          <ListItemText
                            primaryTypographyProps={{ variant: 'caption' }}
                            primary={`${i + 1}. ${line}`}
                          />
                        </ListItem>
                      ))}
                    </List>
                  </Grid>
                  <Grid item xs={12} md={5}>
                    <Typography variant="subtitle2" gutterBottom>
                      止跌打分表
                    </Typography>
                    <ScrollTable>
                    <Table size="small">
                      <TableHead>
                        <TableRow>
                          <TableCell>条件</TableCell>
                          <TableCell align="right">分</TableCell>
                          <TableCell>Notes 标记</TableCell>
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {(market.rules.scoring || []).map((r: any, i: number) => (
                          <TableRow key={i}>
                            <TableCell>
                              <Typography variant="caption">{r.when}</Typography>
                            </TableCell>
                            <TableCell
                              align="right"
                              sx={{
                                color: String(r.delta).startsWith('+')
                                  ? '#2e7d32'
                                  : String(r.delta).startsWith('-')
                                  ? '#c62828'
                                  : undefined,
                                fontWeight: 600,
                              }}
                            >
                              {r.delta}
                            </TableCell>
                            <TableCell>
                              <Typography variant="caption" color="text.secondary">
                                {r.note}
                              </Typography>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
          </ScrollTable>
                  </Grid>
                  <Grid item xs={12} md={5}>
                    <Typography variant="subtitle2" gutterBottom>
                      止涨打分表
                    </Typography>
                    <ScrollTable>
                    <Table size="small">
                      <TableHead>
                        <TableRow>
                          <TableCell>条件</TableCell>
                          <TableCell align="right">分</TableCell>
                          <TableCell>Notes 标记</TableCell>
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {(market.rules.scoringTop || []).map((r: any, i: number) => (
                          <TableRow key={`top-${i}`}>
                            <TableCell>
                              <Typography variant="caption">{r.when}</Typography>
                            </TableCell>
                            <TableCell
                              align="right"
                              sx={{
                                color: String(r.delta).startsWith('+')
                                  ? '#c62828'
                                  : String(r.delta).startsWith('-')
                                  ? '#2e7d32'
                                  : undefined,
                                fontWeight: 600,
                              }}
                            >
                              {r.delta}
                            </TableCell>
                            <TableCell>
                              <Typography variant="caption" color="text.secondary">
                                {r.note}
                              </Typography>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
          </ScrollTable>
                  </Grid>
                  <Grid item xs={12} md={2}>
                    <Typography variant="subtitle2" gutterBottom>
                      结论映射
                    </Typography>
                    <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 0.5 }}>
                      止跌 Score
                    </Typography>
                    {(market.rules.verdict || []).map((v: any, i: number) => (
                      <Box key={i} sx={{ mb: 1 }}>
                        <Chip
                          size="small"
                          label={
                            v.minScore != null
                              ? `score≥${v.minScore}`
                              : '其余'
                          }
                          sx={{ mr: 0.5 }}
                        />
                        <Typography variant="caption" display="block">
                          {v.label}
                        </Typography>
                      </Box>
                    ))}
                    <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1, mb: 0.5 }}>
                      止涨 TopScore
                    </Typography>
                    {(market.rules.verdictTop || []).map((v: any, i: number) => (
                      <Box key={`tv-${i}`} sx={{ mb: 1 }}>
                        <Chip
                          size="small"
                          color="error"
                          variant="outlined"
                          label={
                            v.minScore != null
                              ? `top≥${v.minScore}`
                              : '其余'
                          }
                          sx={{ mr: 0.5 }}
                        />
                        <Typography variant="caption" display="block">
                          {v.label}
                        </Typography>
                      </Box>
                    ))}
                    <Typography variant="caption" color="text.secondary">
                      人工核验：Notes→Score，TopNotes→TopScore。
                    </Typography>
                  </Grid>
                </Grid>
              ) : (
                <Typography variant="body2" color="text.secondary">
                  刷新后加载规则说明
                </Typography>
              )}
            </AccordionDetails>
          </Accordion>

          {marketChart && (
            <Card variant="outlined" sx={{ mb: 2 }}>
              <CardContent>
                <Box
                  sx={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 1,
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    mb: 1,
                  }}
                >
                  <Typography variant="subtitle2">
                    {marketChart.symbol} TradingView 图
                    {marketChart.klineSource
                      ? `（${marketChart.klineSource}）`
                      : ''}
                  </Typography>
                  <Button size="small" onClick={() => setMarketChart(null)}>
                    关闭
                  </Button>
                </Box>
                <PinKlineChart
                  symbol={marketChart.symbol}
                  klines={marketChart.klines || []}
                  pins={marketChart.pins || []}
                  pinLevels={marketChart.pinLevels}
                  last={marketChart.last}
                  quantity={marketChart.quantity}
                  lower={marketChart.band?.lower}
                  upper={marketChart.band?.upper}
                  interval={marketChart.interval || marketInterval}
                  klineSource={marketChart.klineSource}
                  position={
                    marketChart.position ||
                    findPosition(margin?.positions, marketChart.symbol)
                  }
                  height={chartH}
                  intervalOptions={CHART_INTERVALS}
                  onIntervalChange={(iv) =>
                    loadMarketChart(marketChart.symbol, iv)
                  }
                />
              </CardContent>
            </Card>
          )}

          <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
            点击 symbol 打开 TradingView Lightweight Charts（多周期；数据优先 MySQL）
          </Typography>
          <ScrollTable>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell align="right">Last</TableCell>
                <TableCell align="right">Bounce%</TableCell>
                <TableCell align="right">Drop%</TableCell>
                <TableCell align="right">2h%</TableCell>
                <TableCell align="right">4h%</TableCell>
                <TableCell align="right">RSI</TableCell>
                <TableCell align="right">绿柱2h</TableCell>
                <TableCell align="center">HL</TableCell>
                <TableCell align="center">LH</TableCell>
                <TableCell align="center">Mid↑</TableCell>
                <TableCell align="center">Mid↓</TableCell>
                <TableCell align="right">止跌</TableCell>
                <TableCell>止跌结论</TableCell>
                <TableCell align="right">止涨</TableCell>
                <TableCell>止涨结论</TableCell>
                <TableCell>Notes</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(market?.symbols || []).map((row: any) => (
                <TableRow
                  key={row.symbol}
                  hover
                  sx={{ cursor: 'pointer' }}
                  onClick={() => loadMarketChart(row.symbol)}
                >
                  <TableCell>
                    <Typography
                      component="span"
                      sx={{ color: 'primary.main', textDecoration: 'underline' }}
                    >
                      {row.symbol}
                    </Typography>
                  </TableCell>
                  <TableCell align="right">{row.last}</TableCell>
                  <TableCell
                    align="right"
                    sx={{ color: pnlColor(row.bouncePct) }}
                  >
                    {row.bouncePct}%
                  </TableCell>
                  <TableCell
                    align="right"
                    sx={{ color: pnlColor(-(row.dropPct || 0)) }}
                  >
                    {row.dropPct ?? '—'}
                    {row.dropPct != null ? '%' : ''}
                  </TableCell>
                  <TableCell
                    align="right"
                    sx={{ color: pnlColor(row.chg2hPct) }}
                  >
                    {row.chg2hPct}%
                  </TableCell>
                  <TableCell
                    align="right"
                    sx={{ color: pnlColor(row.chg4hPct) }}
                  >
                    {row.chg4hPct}%
                  </TableCell>
                  <TableCell align="right">{row.rsi15m}</TableCell>
                  <TableCell align="right">{row.greenBars2h}</TableCell>
                  <TableCell align="center">
                    {row.higherLows ? 'Y' : '—'}
                  </TableCell>
                  <TableCell align="center">
                    {row.lowerHighs ? 'Y' : '—'}
                  </TableCell>
                  <TableCell align="center">
                    {row.aboveMid ? 'Y' : '—'}
                  </TableCell>
                  <TableCell align="center">
                    {row.belowMid ? 'Y' : '—'}
                  </TableCell>
                  <TableCell align="right" sx={{ fontWeight: 700 }}>
                    {row.score}
                  </TableCell>
                  <TableCell>
                    <Chip
                      size="small"
                      color={
                        String(row.verdict || '').includes('偏强')
                          ? 'success'
                          : String(row.verdict || '').includes('观望')
                          ? 'warning'
                          : 'default'
                      }
                      label={row.verdict}
                    />
                  </TableCell>
                  <TableCell align="right" sx={{ fontWeight: 700 }}>
                    {row.topScore ?? '—'}
                  </TableCell>
                  <TableCell>
                    <Chip
                      size="small"
                      color={
                        String(row.topVerdict || '').includes('偏强')
                          ? 'error'
                          : String(row.topVerdict || '').includes('观望')
                          ? 'warning'
                          : 'default'
                      }
                      label={row.topVerdict || '—'}
                    />
                  </TableCell>
                  <TableCell>
                    <Typography variant="caption">
                      跌: {(row.notes || []).join(', ')}
                    </Typography>
                    {(row.topNotes || []).length > 0 && (
                      <Typography variant="caption" display="block" color="text.secondary">
                        涨: {(row.topNotes || []).join(', ')}
                      </Typography>
                    )}
                    {row.chunkLows?.length > 0 && (
                      <Typography
                        variant="caption"
                        display="block"
                        color="text.secondary"
                      >
                        chunkLows: {row.chunkLows.join(' → ')}
                      </Typography>
                    )}
                    {row.chunkHighs?.length > 0 && (
                      <Typography
                        variant="caption"
                        display="block"
                        color="text.secondary"
                      >
                        chunkHighs: {row.chunkHighs.join(' → ')}
                      </Typography>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          </ScrollTable>
        </TabPanel>

        <TabPanel value={tab} index={1}>
          <Box sx={{ display: 'flex', gap: 1, mb: 2 }}>
            <Button variant="contained" onClick={loadMargin}>
              刷新
            </Button>
          </Box>
          {acc && (
            <Grid container spacing={2} sx={{ mb: 3 }}>
              {[
                ['Wallet', acc.walletBalance],
                ['Available', acc.availableBalance],
                ['Margin Bal', acc.marginBalance],
                ['uPnL', acc.unrealizedPnL],
                ['Total IM', acc.totalInitialMargin],
                ['Pos IM', acc.positionInitialMargin],
                ['Order IM', acc.openOrderInitialMargin],
                ['IM Util %', acc.utilInitialMarginPct],
              ].map(([k, v]) => (
                <Grid item xs={6} md={3} key={String(k)}>
                  <Card variant="outlined">
                    <CardContent>
                      <Typography variant="caption" color="text.secondary">
                        {k}
                      </Typography>
                      <Typography
                        variant="h6"
                        sx={{
                          color:
                            k === 'uPnL' || k === 'IM Util %'
                              ? pnlColor(Number(v))
                              : undefined,
                        }}
                      >
                        {v}
                        {String(k).includes('%') ? '%' : ''}
                      </Typography>
                    </CardContent>
                  </Card>
                </Grid>
              ))}
              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">
                  目标利用率 {acc.targetUtilPct}%（口径: totalInitialMargin /
                  wallet）。(wallet−avail)/wallet ={' '}
                  {acc.utilWalletMinusAvailPct}%
                </Typography>
                <LinearProgress
                  variant="determinate"
                  value={Math.min(100, Number(acc.utilInitialMarginPct) || 0)}
                  sx={{ mt: 1, height: 10, borderRadius: 1 }}
                />
              </Grid>
            </Grid>
          )}

          <PositionsPanel positions={margin?.positions} title="当前持仓" />

          <Typography variant="subtitle1" gutterBottom>
            Open Orders
          </Typography>
          <ScrollTable>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell align="right">Count</TableCell>
                <TableCell align="right">Buy / Sell</TableCell>
                <TableCell align="right">Buy Notional</TableCell>
                <TableCell align="right">Sell Notional</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(margin?.openOrders || []).map((o: any) => (
                <TableRow key={o.symbol}>
                  <TableCell>{o.symbol}</TableCell>
                  <TableCell align="right">{o.count}</TableCell>
                  <TableCell align="right">
                    {o.buyCount}/{o.sellCount}
                  </TableCell>
                  <TableCell align="right">{o.buyNotional}</TableCell>
                  <TableCell align="right">{o.sellNotional}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          </ScrollTable>
        </TabPanel>

        <TabPanel value={tab} index={2}>
          <PositionsPanel
            positions={grid?.positions || margin?.positions}
            highlightSymbol={symbol}
            title="当前持仓"
            onSelectSymbol={(sym) => setSymbol(sym)}
          />
          <Grid container spacing={2} sx={{ mb: 2 }}>
            <Grid item xs={12} md={2}>
              <TextField
                fullWidth
                label="Symbol"
                value={symbol}
                onChange={(e) => setSymbol(e.target.value.toUpperCase())}
              />
            </Grid>
            <Grid item xs={6} md={2}>
              <TextField
                fullWidth
                label="ATR Mult"
                value={atrMult}
                onChange={(e) => setAtrMult(e.target.value)}
              />
            </Grid>
            <Grid item xs={6} md={2}>
              <TextField
                fullWidth
                label="Grid Number"
                value={gridNumber}
                onChange={(e) => setGridNumber(e.target.value)}
              />
            </Grid>
            <Grid item xs={6} md={2}>
              <TextField
                fullWidth
                label="Quantity (optional)"
                value={quantity}
                onChange={(e) => setQuantity(e.target.value)}
                helperText="空=按目标利用率估算"
              />
            </Grid>
            <Grid item xs={6} md={2}>
              <TextField
                fullWidth
                label="Leverage"
                value={leverage}
                onChange={(e) => setLeverage(e.target.value)}
              />
            </Grid>
            <Grid item xs={6} md={2}>
              <FormControl fullWidth>
                <InputLabel>Target Util</InputLabel>
                <Select
                  label="Target Util"
                  value={targetUtil}
                  onChange={(e) => setTargetUtil(String(e.target.value))}
                >
                  <MenuItem value="0.5">50%</MenuItem>
                  <MenuItem value="0.65">65%</MenuItem>
                  <MenuItem value="0.75">75%</MenuItem>
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={12}>
              <Button variant="contained" onClick={loadGrid}>
                计算 1×ATR 网格带
              </Button>
            </Grid>
          </Grid>

          {grid && (
            <Grid container spacing={2}>
              <Grid item xs={12}>
                <Card variant="outlined">
                  <CardContent>
                    <Box
                      sx={{
                        display: 'flex',
                        flexWrap: 'wrap',
                        gap: 1,
                        alignItems: 'center',
                        mb: 1,
                        justifyContent: 'space-between',
                      }}
                    >
                      <Typography variant="subtitle2">
                        {'K 线 + Pins（qty@price，买绿/卖红）'}
                      </Typography>
                    </Box>
                    <PinKlineChart
                      symbol={grid.symbol}
                      klines={grid.klines || grid.klines1h || []}
                      pins={grid.pins || []}
                      pinLevels={grid.pinLevels}
                      last={grid.last}
                      quantity={grid.quantity}
                      lower={grid.band?.lower}
                      upper={grid.band?.upper}
                      interval={grid.interval || gridInterval}
                      klineSource={grid.klineSource}
                      position={
                        grid.position ||
                        findPosition(
                          grid.positions || margin?.positions,
                          grid.symbol,
                        )
                      }
                      height={chartHLg}
                      intervalOptions={CHART_INTERVALS}
                      onIntervalChange={reloadGridKlines}
                    />
                  </CardContent>
                </Card>
              </Grid>
              <Grid item xs={12} md={6}>
                <Card variant="outlined">
                  <CardContent>
                    <Typography variant="h6" gutterBottom>
                      {grid.symbol} @ {grid.last}
                    </Typography>
                    <Typography>
                      日 ATR: {grid.dailyAtrPct}% × {grid.atrMult}
                    </Typography>
                    <Typography>
                      带: [{grid.band?.lower}, {grid.band?.upper}] 宽度{' '}
                      {grid.band?.widthPct}% / 档距 {grid.band?.stepPct}%
                    </Typography>
                    <Typography>
                      位置: {grid.band?.position} (
                      {grid.band?.fromLowPct}% from low)
                    </Typography>
                    <Typography>建议 takeProfit: {grid.band?.takeProfit}</Typography>
                    <Typography>
                      quantity: {grid.quantity} / gridNumber: {grid.gridNumber}
                    </Typography>
                    <Typography>
                      买侧 IM≈ {grid.buyInitialMarginEst} / 满仓 IM≈{' '}
                      {grid.maxInitialMarginEst}
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {grid.note}
                    </Typography>
                  </CardContent>
                </Card>
              </Grid>
              <Grid item xs={12} md={6}>
                <Card variant="outlined">
                  <CardContent>
                    <Typography variant="subtitle2" gutterBottom>
                      Pins（qty@price，盘口深度 B1&gt;… / S1&lt;…）
                    </Typography>
                    {(
                      grid.pinLevels ||
                      buildOrderBookPinLevels(
                        grid.pins || [],
                        grid.last,
                        grid.quantity,
                      )
                    ).map((lv: any) => (
                      <Chip
                        key={`${lv.side}-${lv.i}-${lv.price}`}
                        label={`${lv.depth ? `${lv.depth} ` : ''}${
                          lv.label || `${lv.quantity ?? '?'}@${lv.price}`
                        }`}
                        size="small"
                        sx={{
                          m: 0.5,
                          bgcolor: lv.side === 'sell' ? '#c62828' : '#2e7d32',
                          color: '#fff',
                        }}
                      />
                    ))}
                  </CardContent>
                </Card>
              </Grid>
            </Grid>
          )}
        </TabPanel>

        <TabPanel value={tab} index={3}>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, mb: 2, alignItems: 'center' }}>
            <ToggleButtonGroup
              exclusive
              size="small"
              value={pnlPeriod}
              onChange={(_, v) => {
                if (!v) return;
                setPnlPeriod(v);
                loadPnl(v);
              }}
            >
              <ToggleButton value="today">今日</ToggleButton>
              <ToggleButton value="7d">近7天</ToggleButton>
              <ToggleButton value="30d">近30天</ToggleButton>
              <ToggleButton value="90d">近90天</ToggleButton>
            </ToggleButtonGroup>
            <Button
              variant="contained"
              onClick={() => {
                loadPnl(pnlPeriod);
                loadMargin();
              }}
            >
              刷新
            </Button>
            {pnl?.range && (
              <Typography variant="caption" color="text.secondary">
                {pnl.range.start} → {pnl.range.end} ({pnl.range.tz})
                {pnl.range.deployStart
                  ? ` · 自部署 ${String(pnl.range.deployStart).slice(0, 10)}`
                  : ''}
              </Typography>
            )}
          </Box>
          <PositionsPanel
            positions={pnl?.positions || margin?.positions}
            title="当前持仓"
            onSelectSymbol={(sym) => {
              setSymbol(sym);
              loadPnlChart(sym, pnlInterval);
            }}
          />
          {pnl?.totals && (
            <Grid container spacing={2} sx={{ mb: 2 }}>
              {[
                [`${PNL_PERIOD_LABEL[pnlPeriod]}已实现`, pnl.totals.realized],
                [`${PNL_PERIOD_LABEL[pnlPeriod]}手续费`, pnl.totals.commission],
                [`${PNL_PERIOD_LABEL[pnlPeriod]}资金费`, pnl.totals.funding],
                [`${PNL_PERIOD_LABEL[pnlPeriod]}净流水`, pnl.totals.net],
                ['当前浮动盈亏', pnl.totals.unrealized],
              ].map(([k, v]) => (
                <Grid item xs={6} sm={4} md={2} key={String(k)}>
                  <Card variant="outlined">
                    <CardContent>
                      <Typography variant="caption">{k}</Typography>
                      <Typography
                        variant="h6"
                        sx={{ color: pnlColor(Number(v)) }}
                      >
                        {Number(v) > 0 ? '+' : ''}
                        {v}
                      </Typography>
                    </CardContent>
                  </Card>
                </Grid>
              ))}
              {pnl.feeNote && (
                <Grid item xs={12}>
                  <Typography variant="caption" color="text.secondary">
                    净流水对齐币安合约收入（REALIZED_PNL + COMMISSION +
                    FUNDING，CST）；区间最早从 2026-09-03 部署日起算。手续费按 BNB≈
                    {pnl.feeNote.bnbPriceUSDT} 折合 USDT（原生{' '}
                    {pnl.totals.commissionBNB} BNB）。当前浮动盈亏是持仓累计浮盈亏，
                    不是区间增量。点击下方 symbol 可看 K 线+网格 pins。
                  </Typography>
                </Grid>
              )}
            </Grid>
          )}
          {(pnl?.daily || []).length > 0 && (
            <Card variant="outlined" sx={{ mb: 2 }}>
              <CardContent>
                <Typography variant="subtitle2" sx={{ mb: 1 }}>
                  每日盈亏（CST）
                </Typography>
                <ScrollTable>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>日期</TableCell>
                      <TableCell align="right">Realized</TableCell>
                      <TableCell align="right">Commission</TableCell>
                      <TableCell align="right">Funding</TableCell>
                      <TableCell align="right">Net</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {pnl.daily.map((row: any) => (
                      <TableRow key={row.date}>
                        <TableCell>{row.date}</TableCell>
                        <TableCell
                          align="right"
                          sx={{ color: pnlColor(Number(row.realized)) }}
                        >
                          {row.realized}
                        </TableCell>
                        <TableCell align="right">{row.commission}</TableCell>
                        <TableCell
                          align="right"
                          sx={{ color: pnlColor(Number(row.funding)) }}
                        >
                          {row.funding}
                        </TableCell>
                        <TableCell
                          align="right"
                          sx={{ color: pnlColor(Number(row.net)), fontWeight: 600 }}
                        >
                          {Number(row.net) > 0 ? '+' : ''}
                          {row.net}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
          </ScrollTable>
              </CardContent>
            </Card>
          )}
          {pnlChart && (
            <Card variant="outlined" sx={{ mb: 2 }}>
              <CardContent>
                <Box
                  sx={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 1,
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    mb: 1,
                  }}
                >
                  <Typography variant="subtitle2">
                    {pnlChart.symbol}{' '}
                    {pnlChart.interval === '1d' ? '日线' : pnlChart.interval}
                    （买绿/卖红）
                  </Typography>
                  <Button size="small" onClick={() => setPnlChart(null)}>
                    关闭
                  </Button>
                </Box>
                <PinKlineChart
                  symbol={pnlChart.symbol}
                  klines={pnlChart.klines || []}
                  pins={pnlChart.pins || []}
                  pinLevels={pnlChart.pinLevels}
                  last={pnlChart.last}
                  quantity={pnlChart.quantity}
                  lower={pnlChart.band?.lower}
                  upper={pnlChart.band?.upper}
                  interval={pnlChart.interval || pnlInterval}
                  klineSource={pnlChart.klineSource}
                  position={
                    pnlChart.position ||
                    findPosition(
                      pnl?.positions || margin?.positions,
                      pnlChart.symbol,
                    )
                  }
                  height={chartHSm}
                  intervalOptions={CHART_INTERVALS}
                  onIntervalChange={(iv) => loadPnlChart(pnlChart.symbol, iv)}
                />
              </CardContent>
            </Card>
          )}
          <ScrollTable>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell align="right">Realized</TableCell>
                <TableCell align="right">Commission (USDT)</TableCell>
                <TableCell align="right">Funding</TableCell>
                <TableCell align="right">Net</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(pnl?.symbols || [])
                .slice()
                .sort((a: any, b: any) => a.net - b.net)
                .map((row: any) => (
                  <TableRow
                    key={row.symbol}
                    hover
                    sx={{ cursor: 'pointer' }}
                    onClick={() => loadPnlChart(row.symbol)}
                  >
                    <TableCell>
                      <Typography
                        component="span"
                        sx={{ color: 'primary.main', textDecoration: 'underline' }}
                      >
                        {row.symbol}
                      </Typography>
                    </TableCell>
                    <TableCell
                      align="right"
                      sx={{ color: pnlColor(row.realized) }}
                    >
                      {row.realized}
                    </TableCell>
                    <TableCell align="right">{row.commission}</TableCell>
                    <TableCell align="right">{row.funding}</TableCell>
                    <TableCell align="right" sx={{ color: pnlColor(row.net) }}>
                      {row.net}
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
          </ScrollTable>
        </TabPanel>

        <TabPanel value={tab} index={4}>
          <Typography variant="body2" color="warning.main" sx={{ mb: 2 }}>
            被套网格 / 裸多慎重补仓计算器：补仓不立刻减少浮亏，续跌会放大亏损。
            建议合计 IM 不超过 safeBudget，并保留 avail≥3000 给运行中网格。仅计算，不自动下单。
          </Typography>
          <Box sx={{ display: 'flex', gap: 1, mb: 2, flexWrap: 'wrap' }}>
            <Button variant="contained" onClick={loadAvgDown}>
              刷新 / 计算
            </Button>
          </Box>
          {avgDown?.account && (
            <Grid container spacing={2} sx={{ mb: 2 }}>
              {[
                ['Wallet', avgDown.account.walletBalance],
                ['Avail', avgDown.account.availableBalance],
                ['Total IM', avgDown.account.totalInitialMargin],
                ['IM%', avgDown.account.utilInitialMarginPct],
                ['Headroom→65%', avgDown.account.headroomTargetIM],
                ['SafeBudget IM', avgDown.account.safeBudgetIM],
                ['Safe Notional', avgDown.account.safeBudgetNotional],
                ['uPnL', avgDown.account.unrealizedPnL],
              ].map(([k, v]) => (
                <Grid item xs={6} md={3} key={String(k)}>
                  <Card variant="outlined">
                    <CardContent>
                      <Typography variant="caption" color="text.secondary">
                        {k}
                      </Typography>
                      <Typography
                        variant="h6"
                        sx={{
                          color:
                            k === 'uPnL' || String(k).includes('IM%')
                              ? pnlColor(Number(v))
                              : k === 'SafeBudget IM'
                              ? '#ed6c02'
                              : undefined,
                        }}
                      >
                        {v}
                        {String(k).includes('%') ? '%' : ''}
                      </Typography>
                    </CardContent>
                  </Card>
                </Grid>
              ))}
            </Grid>
          )}

          <Typography variant="subtitle2" gutterBottom>
            候选多单（浮亏优先）
          </Typography>
          <ScrollTable>
          <Table size="small" sx={{ mb: 3 }}>
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell align="right">Amt</TableCell>
                <TableCell align="right">Entry</TableCell>
                <TableCell align="right">Mark</TableCell>
                <TableCell align="right">距成本%</TableCell>
                <TableCell align="right">uPnL</TableCell>
                <TableCell align="right">IM≈</TableCell>
                <TableCell>Hint</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(avgDown?.candidates || [])
                .filter((c: any) => c.underwater)
                .map((c: any) => (
                  <TableRow
                    key={c.symbol}
                    hover
                    selected={c.symbol === avgSymbol}
                    sx={{ cursor: 'pointer' }}
                    onClick={() => {
                      setAvgSymbol(c.symbol);
                      setAvgPrice('');
                    }}
                  >
                    <TableCell>{c.symbol}</TableCell>
                    <TableCell align="right">{c.positionAmt}</TableCell>
                    <TableCell align="right">{c.entryPrice}</TableCell>
                    <TableCell align="right">{c.markPrice}</TableCell>
                    <TableCell
                      align="right"
                      sx={{ color: pnlColor(c.distFromEntryPct) }}
                    >
                      {c.distFromEntryPct}%
                    </TableCell>
                    <TableCell
                      align="right"
                      sx={{ color: pnlColor(c.unrealizedPnL) }}
                    >
                      {c.unrealizedPnL}
                    </TableCell>
                    <TableCell align="right">{c.initialMarginEst}</TableCell>
                    <TableCell>
                      {c.gridDisabledHint ? (
                        <Chip size="small" color="warning" label="网格已停" />
                      ) : (
                        <Chip size="small" label="运行中/其它" />
                      )}
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
          </ScrollTable>

          <Grid container spacing={2} sx={{ mb: 2 }}>
            <Grid item xs={12} md={3}>
              <FormControl fullWidth>
                <InputLabel>Symbol</InputLabel>
                <Select
                  label="Symbol"
                  value={avgSymbol}
                  onChange={(e) => setAvgSymbol(String(e.target.value))}
                >
                  {(avgDown?.candidates || [])
                    .filter((c: any) => c.underwater)
                    .map((c: any) => (
                      <MenuItem key={c.symbol} value={c.symbol}>
                        {c.symbol}
                      </MenuItem>
                    ))}
                  {['ENAUSDT', 'WLDUSDT', 'DOGEUSDT', 'BNBUSDT', 'SUIUSDT', 'SOLUSDT'].map(
                    (s) => (
                      <MenuItem key={s} value={s}>
                        {s}
                      </MenuItem>
                    ),
                  )}
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={6} md={2}>
              <TextField
                fullWidth
                label="加仓 IM (USDT)"
                value={avgAddIm}
                onChange={(e) => setAvgAddIm(e.target.value)}
                helperText="名义≈IM×杠杆"
              />
            </Grid>
            <Grid item xs={6} md={2}>
              <TextField
                fullWidth
                label="限价 (空=mark)"
                value={avgPrice}
                onChange={(e) => setAvgPrice(e.target.value)}
              />
            </Grid>
            <Grid item xs={6} md={2}>
              <TextField
                fullWidth
                label="Leverage"
                value={avgLeverage}
                onChange={(e) => setAvgLeverage(e.target.value)}
              />
            </Grid>
            <Grid item xs={12} md={3} sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
              <Button variant="contained" onClick={loadAvgDown}>
                计算摊派
              </Button>
              {(avgDown?.presets || []).map((p: any) => (
                <Button
                  key={p.id}
                  size="small"
                  variant="outlined"
                  onClick={() => {
                    setAvgAddIm(String(p.totalAddIm));
                  }}
                >
                  {p.label}
                </Button>
              ))}
            </Grid>
          </Grid>

          {avgDown?.scenario && (
            <Card
              variant="outlined"
              sx={{
                borderColor: avgDown.scenario.overSafeBudget
                  ? 'error.main'
                  : 'divider',
              }}
            >
              <CardContent>
                <Typography variant="h6" gutterBottom>
                  {avgDown.scenario.symbol} 模拟结果
                  {avgDown.scenario.overSafeBudget && (
                    <Chip
                      size="small"
                      color="error"
                      label="超过 SafeBudget"
                      sx={{ ml: 1 }}
                    />
                  )}
                </Typography>
                <Typography>
                  限价 {avgDown.scenario.limitPrice} · 买入 qty{' '}
                  {avgDown.scenario.addQty} · 名义{' '}
                  {avgDown.scenario.addNotional} · IM {avgDown.scenario.addIm}
                </Typography>
                <Typography>
                  均价 {avgDown.scenario.oldEntry} →{' '}
                  {avgDown.scenario.newEntry} (
                  {avgDown.scenario.entryImprovePct}%)
                </Typography>
                <Typography>
                  仓位 {avgDown.scenario.oldAmt} → {avgDown.scenario.newAmt}
                </Typography>
                <Typography sx={{ color: pnlColor(avgDown.scenario.uPnLNowAtMark) }}>
                  现价浮盈（成交后）≈ {avgDown.scenario.uPnLNowAtMark}
                </Typography>
                <Typography>
                  若再跌 5%: uPnL ≈ {avgDown.scenario.ifDrop5Pct?.uPnL} @{' '}
                  {avgDown.scenario.ifDrop5Pct?.price}
                </Typography>
                <Typography>
                  若再跌 10%: uPnL ≈ {avgDown.scenario.ifDrop10Pct?.uPnL} @{' '}
                  {avgDown.scenario.ifDrop10Pct?.price}
                </Typography>
                <Typography>
                  若回到旧成本: uPnL ≈{' '}
                  {avgDown.scenario.ifBackToOldEntry?.uPnL}
                </Typography>
                <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1 }}>
                  {avgDown.scenario.note} {avgDown.warning}
                </Typography>
              </CardContent>
            </Card>
          )}
        </TabPanel>

        <TabPanel value={tab} index={5}>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            按成交额筛活跃合约，对每标的用本地 15m K 线 walk-forward 回测（EMA+RSI 回踩）；仅期望&gt;0
            且样本足够才给多空与入场。
          </Typography>
          <Box sx={{ display: 'flex', gap: 1, mb: 2, flexWrap: 'wrap', alignItems: 'center' }}>
            <Button variant="contained" onClick={loadTrend} size={isMobile ? 'small' : 'medium'}>
              刷新回测选股
            </Button>
            <TextField
              size="small"
              label="Top N"
              value={trendTop}
              onChange={(e) => setTrendTop(e.target.value)}
              sx={{ width: 90 }}
            />
            <TextField
              size="small"
              label="Min QuoteVol"
              value={trendMinVol}
              onChange={(e) => setTrendMinVol(e.target.value)}
              sx={{ width: 160 }}
            />
            <TextField
              size="small"
              label="Bars(15m)"
              value={trendBars}
              onChange={(e) => setTrendBars(e.target.value)}
              sx={{ width: 110 }}
            />
            {trend?.asOf && (
              <Typography variant="caption" color="text.secondary">
                {trend.asOf} · {trend.bars} bars
              </Typography>
            )}
            {trend?.summary && (
              <>
                <Chip size="small" color="success" label={`现价多 ${trend.summary.longNow}`} />
                <Chip size="small" color="error" label={`现价空 ${trend.summary.shortNow}`} />
                <Chip size="small" color="warning" label={`等回踩/反抽 ${trend.summary.waitPullback}`} />
              </>
            )}
          </Box>
          <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1.5 }}>
            WR=胜率 · Exp=平均每笔净盈亏%（扣费后）· n=样本笔数 · PF=盈亏比。入场价锚定信号收盘价（不跟
            last 漂移）；到价→long/short，未到→wait，无信号不写假入场。
          </Typography>

          <Accordion defaultExpanded={false} sx={{ mb: 2 }}>
            <AccordionSummary expandIcon={<ExpandMoreIcon />}>
              <Typography variant="subtitle2">回测规则</Typography>
            </AccordionSummary>
            <AccordionDetails>
              {trend?.rules ? (
                <>
                  <Typography variant="body2" sx={{ mb: 1 }}>
                    {trend.rules.purpose}
                  </Typography>
                  <Typography variant="caption" display="block" color="text.secondary" sx={{ mb: 1 }}>
                    成交额: {trend.rules.volume}
                  </Typography>
                  {(trend.rules.metrics || []).length > 0 && (
                    <>
                      <Typography variant="subtitle2" sx={{ mt: 1, mb: 0.5 }}>
                        WR / Exp / n / PF
                      </Typography>
                      <List dense>
                        {(trend.rules.metrics || []).map((t: string) => (
                          <ListItem key={t} sx={{ py: 0 }}>
                            <ListItemText primary={t} primaryTypographyProps={{ variant: 'body2' }} />
                          </ListItem>
                        ))}
                      </List>
                    </>
                  )}
                  <List dense>
                    {(trend.rules.strategy || []).map((t: string) => (
                      <ListItem key={t} sx={{ py: 0 }}>
                        <ListItemText primary={t} primaryTypographyProps={{ variant: 'body2' }} />
                      </ListItem>
                    ))}
                  </List>
                  <List dense>
                    {(trend.rules.bias || []).map((t: string) => (
                      <ListItem key={t} sx={{ py: 0 }}>
                        <ListItemText primary={t} primaryTypographyProps={{ variant: 'body2' }} />
                      </ListItem>
                    ))}
                  </List>
                  <List dense>
                    {(trend.rules.entries || []).map((t: string) => (
                      <ListItem key={t} sx={{ py: 0 }}>
                        <ListItemText primary={t} primaryTypographyProps={{ variant: 'body2' }} />
                      </ListItem>
                    ))}
                  </List>
                </>
              ) : (
                <Typography variant="body2">刷新后显示规则</Typography>
              )}
            </AccordionDetails>
          </Accordion>

          <ScrollTable>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>#</TableCell>
                  <TableCell>Symbol</TableCell>
                  <TableCell align="right">Last</TableCell>
                  <TableCell align="right">24h额</TableCell>
                  <TableCell>偏向</TableCell>
                  <TableCell>动作</TableCell>
                  <TableCell align="right">
                    <Tooltip
                      title="多头回测：WR=胜率；Exp=平均每笔净盈亏%（扣 8bps）；下行 n=笔数，PF=总盈/总亏"
                      arrow
                    >
                      <Box component="span" sx={{ borderBottom: '1px dashed', cursor: 'help' }}>
                        多 WR/Exp/n
                      </Box>
                    </Tooltip>
                  </TableCell>
                  <TableCell align="right">
                    <Tooltip
                      title="空头回测：WR=胜率；Exp=平均每笔净盈亏%（扣 8bps）；下行 n=笔数，PF=总盈/总亏"
                      arrow
                    >
                      <Box component="span" sx={{ borderBottom: '1px dashed', cursor: 'help' }}>
                        空 WR/Exp/n
                      </Box>
                    </Tooltip>
                  </TableCell>
                  <TableCell align="right">多入场</TableCell>
                  <TableCell align="right">空入场</TableCell>
                  <TableCell align="right">多止损/盈</TableCell>
                  <TableCell align="right">空止损/盈</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {(trend?.symbols || []).map((row: any) => (
                  <TableRow key={row.symbol} hover>
                    <TableCell>{row.volumeRank}</TableCell>
                    <TableCell>
                      {row.symbol}
                      <Typography variant="caption" display="block" color="text.secondary">
                        {row.bars}b · {row.klineSource}
                        {row.signalClose
                          ? ` · 信号${fmtNum(row.signalClose, 6)} (${row.signalAgo}根前)`
                          : ''}
                      </Typography>
                    </TableCell>
                    <TableCell align="right">{fmtNum(row.last, 6)}</TableCell>
                    <TableCell align="right">
                      {row.quoteVolume24h
                        ? `${(Number(row.quoteVolume24h) / 1e6).toFixed(0)}M`
                        : '—'}
                    </TableCell>
                    <TableCell>
                      <Chip
                        size="small"
                        label={row.bias}
                        color={
                          row.bias === 'long'
                            ? 'success'
                            : row.bias === 'short'
                            ? 'error'
                            : row.bias?.includes('wait') || row.bias === 'range'
                            ? 'warning'
                            : 'default'
                        }
                      />
                    </TableCell>
                    <TableCell sx={{ maxWidth: 240 }}>
                      <Typography variant="caption">{row.action}</Typography>
                    </TableCell>
                    <TableCell align="right">
                      <Typography variant="caption" display="block" color={pnlColor(row.longBT?.expectancy)}>
                        {row.longBT?.winRate ?? '—'}% / {fmtSigned(row.longBT?.expectancy, 2)}%
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        n={row.longBT?.trades ?? 0} PF={row.longBT?.profitFactor ?? '—'}
                      </Typography>
                    </TableCell>
                    <TableCell align="right">
                      <Typography variant="caption" display="block" color={pnlColor(row.shortBT?.expectancy)}>
                        {row.shortBT?.winRate ?? '—'}% / {fmtSigned(row.shortBT?.expectancy, 2)}%
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        n={row.shortBT?.trades ?? 0} PF={row.shortBT?.profitFactor ?? '—'}
                      </Typography>
                    </TableCell>
                    <TableCell align="right">
                      {row.longEntry ? fmtNum(row.longEntry, 6) : '—'}
                    </TableCell>
                    <TableCell align="right">
                      {row.shortEntry ? fmtNum(row.shortEntry, 6) : '—'}
                    </TableCell>
                    <TableCell align="right">
                      {row.longStop ? (
                        <>
                          <Typography variant="caption" display="block">
                            {fmtNum(row.longStop, 6)}
                          </Typography>
                          <Typography variant="caption" display="block" color="success.main">
                            {fmtNum(row.longTP, 6)}
                          </Typography>
                        </>
                      ) : (
                        '—'
                      )}
                    </TableCell>
                    <TableCell align="right">
                      {row.shortStop ? (
                        <>
                          <Typography variant="caption" display="block">
                            {fmtNum(row.shortStop, 6)}
                          </Typography>
                          <Typography variant="caption" display="block" color="error.main">
                            {fmtNum(row.shortTP, 6)}
                          </Typography>
                        </>
                      ) : (
                        '—'
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </ScrollTable>
        </TabPanel>

        <TabPanel value={tab} index={6}>
          <Box
            sx={{
              display: 'flex',
              gap: 1,
              mb: 2,
              alignItems: 'center',
              flexWrap: 'wrap',
            }}
          >
            <TextField
              size="small"
              label="主观察币"
              value={regimeSymbol}
              onChange={(e) => setRegimeSymbol(e.target.value.toUpperCase())}
              sx={{ width: 140 }}
            />
            <Button variant="contained" onClick={loadRegime} size={isMobile ? 'small' : 'medium'}>
              刷新优道嵌套 + 破箱回测
            </Button>
            {regime?.tookMs != null && (
              <Typography variant="caption" color="text.secondary">
                {regime.symbolsUsed?.length || 0} 币种 · {regime.tookMs}ms · {regime.klineSource}
              </Typography>
            )}
          </Box>

          {regime && (
            <>
              {regime.ud?.cascade && (
                <Card variant="outlined" sx={{ mb: 2, borderColor: 'warning.main' }}>
                  <CardContent>
                    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, alignItems: 'center', mb: 1 }}>
                      <Chip
                        label={regime.ud.cascade.label}
                        color={
                          String(regime.ud.cascade.signal).includes('short')
                            ? 'error'
                            : String(regime.ud.cascade.signal).includes('long')
                              ? 'success'
                              : 'default'
                        }
                      />
                      <Chip
                        size="small"
                        label={`周线嵌套 ${
                          regime.ud.cascade.nestWeekly === 'bull'
                            ? '偏多'
                            : regime.ud.cascade.nestWeekly === 'bear'
                              ? '偏空'
                              : '震荡'
                        }`}
                      />
                    </Box>
                    <Typography variant="body2">{regime.ud.cascade.trigger}</Typography>
                    <Typography variant="body2" sx={{ mt: 1 }} color="warning.main">
                      {regime.ud.cascade.action}
                    </Typography>
                    <Typography variant="caption" display="block" sx={{ mt: 1 }}>
                      关键支撑 {fmtNum(regime.ud.cascade.keySupport, 2)} · 关键压力{' '}
                      {fmtNum(regime.ud.cascade.keyResist, 2)}
                    </Typography>
                  </CardContent>
                </Card>
              )}

              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                优道：锁箱 → 沿上做空/沿下做多 → 收盘出箱才变盘。4h 破支撑只出「日线出空预警」，日线再破才看周线。量能按币圈绿涨红跌：绿肥红瘦偏多，红肥绿瘦偏空。
                15m 只做扳机：跟 4h 同向突破或 2B。
              </Typography>

              {regime.ud?.intraday && (
                <Card
                  variant="outlined"
                  sx={{
                    mb: 2,
                    borderColor: regime.ud.intraday.aligned ? 'success.main' : 'divider',
                  }}
                >
                  <CardContent>
                    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, alignItems: 'center', mb: 1 }}>
                      <Typography variant="subtitle1">15m 日内扳机</Typography>
                      <Chip
                        size="small"
                        label={regime.ud.intraday.label}
                        color={
                          regime.ud.intraday.aligned
                            ? 'success'
                            : regime.ud.intraday.setup === 'ignore'
                              ? 'warning'
                              : 'default'
                        }
                      />
                      <Chip
                        size="small"
                        label={`4h嵌套 ${
                          regime.ud.intraday.nest4h === 'bull'
                            ? '偏多'
                            : regime.ud.intraday.nest4h === 'bear'
                              ? '偏空'
                              : '无主线'
                        }`}
                      />
                      {regime.ud.intraday.aligned && (
                        <Chip size="small" color="success" label="同向" />
                      )}
                    </Box>
                    <Typography variant="body2" color="warning.main">
                      {regime.ud.intraday.action}
                    </Typography>
                    {regime.ud.intraday.box && (
                      <Typography variant="body2" sx={{ mt: 1 }}>
                        15m 箱 {fmtNum(regime.ud.intraday.box.bottom, 2)} –{' '}
                        {fmtNum(regime.ud.intraday.box.top, 2)}
                        （{regime.ud.intraday.box.locked ? '已锁' : '不锁'} ·{' '}
                        {regime.ud.intraday.box.compressing ? '收敛' : '未收敛'} · 箱内{' '}
                        {fmtNum(regime.ud.intraday.box.posPct, 0)}% ·{' '}
                        {regime.ud.intraday.box.volumeBias}）
                      </Typography>
                    )}
                    {regime.ud.intraday.twoB && (
                      <Typography variant="body2" sx={{ mt: 0.5 }}>
                        2B：{regime.ud.intraday.twoB.side === 'long' ? '试多' : '试空'} · 刺穿{' '}
                        {fmtNum(regime.ud.intraday.twoB.pierced, 2)} · {regime.ud.intraday.twoB.barsAgo}{' '}
                        根前收回 · {regime.ud.intraday.twoB.note}
                      </Typography>
                    )}
                    {(regime.ud.intraday.stop > 0 || regime.ud.intraday.target > 0) && (
                      <Typography variant="caption" display="block" sx={{ mt: 0.5 }}>
                        停损 {fmtNum(regime.ud.intraday.stop, 2)} · 目标{' '}
                        {fmtNum(regime.ud.intraday.target, 2)}
                        {regime.ud.intraday.next15m
                          ? ` · 下一 15m 收盘 ${regime.ud.intraday.next15m.cst}`
                          : ''}
                      </Typography>
                    )}
                    {!regime.ud.intraday.stop && regime.ud.intraday.next15m && (
                      <Typography variant="caption" display="block" sx={{ mt: 0.5 }}>
                        下一 15m 收盘 {regime.ud.intraday.next15m.cst}
                      </Typography>
                    )}
                  </CardContent>
                </Card>
              )}

              <Grid container spacing={2} sx={{ mb: 2 }}>
                {(regime.ud?.boxes || []).map((b: any) => (
                  <Grid item xs={12} md={4} key={b.interval}>
                    <Card variant="outlined" sx={{ height: '100%' }}>
                      <CardContent>
                        <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', mb: 1, flexWrap: 'wrap' }}>
                          <Typography variant="subtitle1">{b.interval} 箱</Typography>
                          <Chip
                            size="small"
                            label={b.locked ? '已锁' : '不锁'}
                            color={b.locked ? 'success' : 'default'}
                          />
                          <Chip size="small" label={b.phase} />
                          <Chip
                            size="small"
                            label={
                              b.zone === 'lower'
                                ? '下沿'
                                : b.zone === 'upper'
                                  ? '上沿'
                                  : b.zone === 'mid'
                                    ? '中腰'
                                    : b.zone === 'below'
                                      ? '箱下'
                                      : b.zone === 'above'
                                        ? '箱上'
                                        : b.zone
                            }
                            color={
                              b.zone === 'lower' || b.zone === 'above'
                                ? 'success'
                                : b.zone === 'upper' || b.zone === 'below'
                                  ? 'error'
                                  : 'default'
                            }
                          />
                        </Box>
                        <Typography variant="body2">
                          {fmtNum(b.bottom, 2)} – {fmtNum(b.top, 2)}（宽 {fmtNum(b.widthPct, 2)}% · 窗
                          {b.window}）
                        </Typography>
                        <Typography variant="body2">
                          现价 {fmtNum(b.last, 2)} · 箱内 {fmtNum(b.posPct, 0)}% ·{' '}
                          {b.compressing ? '已收敛' : '未收敛'} · {b.volumeBias}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {b.note}
                        </Typography>
                      </CardContent>
                    </Card>
                  </Grid>
                ))}
              </Grid>

              <Grid container spacing={2} sx={{ mb: 2 }}>
                {(regime.clocks || []).map((ck: any) => (
                  <Grid item xs={6} md={3} key={ck.interval}>
                    <Card variant="outlined">
                      <CardContent>
                        <Typography variant="caption" color="text.secondary">
                          {ck.note}
                        </Typography>
                        <Typography variant="body2">{ck.cst}</Typography>
                        <Typography variant="caption">约 {fmtNum(ck.inHours, 1)}h</Typography>
                      </CardContent>
                    </Card>
                  </Grid>
                ))}
              </Grid>

              {regime.ud?.backtest && (
                <Card variant="outlined" sx={{ mb: 2 }}>
                  <CardContent>
                    <Typography variant="subtitle2" gutterBottom>
                      嵌套回测（{regime.ud.backtest.symbols} 币 · 4h 破合格箱后 {regime.ud.backtest.horizonBars}{' '}
                      根）
                    </Typography>
                    <Typography variant="body2">
                      破箱底 {regime.ud.backtest.breaksDown} 次 · 跟随{' '}
                      {regime.ud.backtest.followed} · 假突破收回 {regime.ud.backtest.failed} · 跟随率{' '}
                      {fmtNum(regime.ud.backtest.followRate, 1)}%
                    </Typography>
                    <Typography variant="body2">
                      破箱顶 {regime.ud.backtest.breaksUp} 次 · 跟随 {regime.ud.backtest.followedUp} · 跟随率{' '}
                      {fmtNum(regime.ud.backtest.followRateUp, 1)}%
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {regime.ud.backtest.note}
                    </Typography>
                  </CardContent>
                </Card>
              )}

              <Accordion>
                <AccordionSummary expandIcon={<ExpandMoreIcon />}>
                  <Typography variant="subtitle2">均线副屏（不作为优道变盘）</Typography>
                </AccordionSummary>
                <AccordionDetails>
                  <Grid container spacing={2} sx={{ mb: 2 }}>
                    {(regime.horizons || []).map((h: any) => (
                      <Grid item xs={12} md={4} key={h.name}>
                        <Typography variant="body2">
                          {h.label} · {h.summary}
                        </Typography>
                        {(h.timeframes || []).map((tf: any) => (
                          <Typography key={tf.interval} variant="caption" display="block">
                            {tf.interval}: RSI {fmtNum(tf.rsi14, 1)} · EMA20 {tf.aboveEMA20 ? '上' : '下'}
                          </Typography>
                        ))}
                      </Grid>
                    ))}
                  </Grid>
                  {regime.current && (
                    <Typography variant="caption" display="block">
                      反弹 EMA 窗口 P50 {fmtNum(regime.current.histP50Hours, 1)}h / P80{' '}
                      {fmtNum(regime.current.histP80Hours, 1)}h（仅参考）
                    </Typography>
                  )}
                </AccordionDetails>
              </Accordion>

              <Card variant="outlined" sx={{ mt: 2 }}>
                <CardContent>
                  <Typography variant="subtitle2" gutterBottom>
                    优道规则
                  </Typography>
                  <List dense>
                    {(regime.ud?.rules || regime.rules || []).map((r: string, i: number) => (
                      <ListItem key={i} disableGutters>
                        <ListItemText primary={`${i + 1}. ${r}`} />
                      </ListItem>
                    ))}
                  </List>
                </CardContent>
              </Card>
            </>
          )}
        </TabPanel>

        <TabPanel value={tab} index={7}>
          <Box
            sx={{
              display: 'flex',
              gap: 1,
              mb: 2,
              alignItems: 'center',
              flexWrap: 'wrap',
            }}
          >
            <TextField
              size="small"
              label="主观察币"
              value={prospecSymbol}
              onChange={(e) => setProspecSymbol(e.target.value.toUpperCase())}
              sx={{ width: 140 }}
            />
            <Button variant="contained" onClick={loadProspec} size={isMobile ? 'small' : 'medium'}>
              刷新 1-2-3 / 2B / 嵌套
            </Button>
            {prospec?.tookMs != null && (
              <Typography variant="caption" color="text.secondary">
                策略 prospec · {prospec.tookMs}ms · {prospec.klineSource}
              </Typography>
            )}
          </Box>

          {prospec && (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                《专业投机原理》结构看板：周/日定势，日线 1-2-3 为主；4h/15m 只作观察/扳机。目标约{' '}
                <strong>2R</strong>，须<strong>嵌套同向</strong>。策略 <code>prospec</code> —
                仅供人工参考，非自动下单信号。
              </Typography>
              <Typography variant="caption" color="warning.main" display="block" sx={{ mb: 2 }}>
                消融结论：裸 4h 123 期望为负；日线 + 嵌套 + 2R 相对最好。未对齐嵌套时不要当交易信号。
              </Typography>

              <Accordion sx={{ mb: 2 }}>
                <AccordionSummary expandIcon={<ExpandMoreIcon />}>
                  <Typography variant="subtitle2">1-2-3 与 2B 详解</Typography>
                </AccordionSummary>
                <AccordionDetails>
                  <Typography variant="body2" gutterBottom>
                    <strong>1-2-3 转空</strong>
                    ：①收盘跌破上升趋势线（RANSAC/OLS；旧版用破 HL）→ ②反抽不过前高 →
                    ③收盘破①→②之间拐点低点。进场看③收盘；停损在②上；目标约{' '}
                    <strong>2R</strong>（须低于进场）。
                  </Typography>
                  <Typography variant="body2" gutterBottom>
                    <strong>1-2-3 转多</strong>
                    ：镜像——①升破下降趋势线 → ②回踩不破前低 → ③破拐点高点。停损②下，目标进场上方 2R。
                  </Typography>
                  <Typography variant="body2">
                    <strong>2B</strong>
                    ：刺穿前高/前低后收盘收回。假上破→试空；假下破→试多。必须嵌套同向；逆嵌套当诱饵。
                  </Typography>
                </AccordionDetails>
              </Accordion>

              <Card variant="outlined" sx={{ mb: 2 }}>
                <CardContent>
                  <Typography variant="subtitle2" sx={{ mb: 1 }}>
                    {prospecSymbol} 趋势线（绿/红=RANSAC · 虚线=OLS · 灰=已破停画）
                    {prospecChart?.klineSource ? ` · ${prospecChart.klineSource}` : ''}
                  </Typography>
                  {prospecChart?.klines?.length ? (
                    <PinKlineChart
                      symbol={prospecSymbol}
                      klines={prospecChart.klines}
                      interval={prospecChart.interval || prospecChartIv}
                      klineSource={prospecChart.klineSource}
                      height={isMobile ? 280 : 380}
                      intervalOptions={PROSPEC_INTERVALS}
                      onIntervalChange={changeProspecChartIv}
                      trendSegments={(() => {
                        const iv = prospecChart.interval;
                        const tf =
                          iv === '15m'
                            ? prospec.m15
                            : iv === '30m'
                              ? prospec.m30
                              : iv === '1h'
                                ? prospec.h1
                                : iv === '4h'
                                  ? prospec.h4
                                  : prospec.daily;
                        const tl = tf?.trendLines || {};
                        const segs: any[] = [];
                        const push = (ln: any, color: string, title: string, dashed?: boolean) => {
                          if (!ln?.startTime || !ln?.y1) return;
                          const t2 = ln.nowTime || ln.endTime;
                          const p2 = ln.yNow > 0 ? ln.yNow : ln.y2;
                          if (!t2 || !(p2 > 0)) return;
                          const broken = !!ln.broken;
                          segs.push({
                            t1: ln.startTime,
                            p1: ln.y1,
                            t2,
                            p2,
                            color: broken ? '#9e9e9e' : color,
                            title: broken ? `${title}·破` : title,
                            dashed: broken || !!dashed,
                          });
                        };
                        const pushMany = (
                          arr: any[] | undefined,
                          fallback: any,
                          color: string,
                          base: string,
                          dashed?: boolean,
                        ) => {
                          const list =
                            Array.isArray(arr) && arr.length > 0
                              ? arr
                              : fallback
                                ? [fallback]
                                : [];
                          list.forEach((ln, i) => {
                            const title =
                              list.length > 1 ? `${base}${i + 1}` : base;
                            push(ln, color, title, dashed);
                          });
                        };
                        pushMany(
                          tl.ransacSupports,
                          tl.ransacSupport,
                          '#2e7d32',
                          'RANSAC支撑',
                        );
                        pushMany(
                          tl.ransacResistances,
                          tl.ransacResistance,
                          '#c62828',
                          'RANSAC阻力',
                        );
                        push(tl.olsSupport, '#00897b', 'OLS支撑', true);
                        push(tl.olsResistance, '#ef6c00', 'OLS阻力', true);
                        return segs;
                      })()}
                    />
                  ) : (
                    <Typography variant="body2" color="text.secondary">
                      暂无 K 线；点刷新加载（优先 MySQL，仅补最新 tip）。
                    </Typography>
                  )}
                </CardContent>
              </Card>

              {prospec.primary && (
                <Card variant="outlined" sx={{ mb: 2, borderColor: 'primary.main' }}>
                  <CardContent>
                    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, mb: 1, alignItems: 'center' }}>
                      <Chip
                        label={prospec.primary.label || '等待'}
                        color={
                          prospec.primary.side === 'long'
                            ? 'success'
                            : prospec.primary.side === 'short'
                              ? 'error'
                              : 'default'
                        }
                      />
                      {prospec.primary.aligned ? (
                        <Chip size="small" color="success" label="嵌套同向 · 可参考" />
                      ) : (
                        <Chip size="small" color="warning" label="嵌套未对齐 · 勿跟单" />
                      )}
                      <Chip size="small" label={prospec.primary.kind || 'wait'} />
                    </Box>
                    <Typography variant="body2" color="warning.main">
                      {prospec.primary.action}
                    </Typography>
                    {(prospec.primary.stop > 0 || prospec.primary.target > 0) && (
                      <Typography variant="caption" display="block" sx={{ mt: 1 }}>
                        参考进场 {fmtNum(prospec.primary.entry, 2)} · 停损{' '}
                        {fmtNum(prospec.primary.stop, 2)} · 目标 {fmtNum(prospec.primary.target, 2)}
                        {prospec.primary.entry > 0 &&
                        prospec.primary.stop > 0 &&
                        Math.abs(prospec.primary.entry - prospec.primary.stop) > 0
                          ? ` · ≈${fmtNum(
                              Math.abs(prospec.primary.target - prospec.primary.entry) /
                                Math.abs(prospec.primary.entry - prospec.primary.stop),
                              1
                            )}R`
                          : ''}
                        {prospec.primary.side === 'short' &&
                        prospec.primary.target >= prospec.primary.entry
                          ? ' ⚠ 目标应低于进场'
                          : ''}
                        {prospec.primary.side === 'long' &&
                        prospec.primary.target > 0 &&
                        prospec.primary.target <= prospec.primary.entry
                          ? ' ⚠ 目标应高于进场'
                          : ''}
                      </Typography>
                    )}
                  </CardContent>
                </Card>
              )}

              <Grid container spacing={2} sx={{ mb: 2 }}>
                {[
                  {
                    title: '日线多币回测（主看）',
                    bt: prospec.backtest1d,
                    hint: '消融：日线 + 嵌套 + 2R 相对最好',
                  },
                  {
                    title: '4h 多币回测（仅对照）',
                    bt: prospec.backtest4h,
                    hint: '消融：裸 4h 期望偏负，勿作下单依据',
                  },
                ].map(({ title, bt, hint }) => (
                  <Grid item xs={12} md={6} key={title}>
                    <Card variant="outlined">
                      <CardContent>
                        <Typography variant="subtitle2" gutterBottom>
                          {title}（{bt?.symbols || 0} 币 · 窗口 {bt?.horizonBars || 0} 根）
                        </Typography>
                        <Typography variant="body2">
                          1-2-3：n={bt?.trades123 || 0} · 胜率 {fmtNum(bt?.winRate123, 1)}% · 均 R{' '}
                          {fmtNum(bt?.avgR123, 2)}（胜 {bt?.wins123 || 0} / 负 {bt?.losses123 || 0}）
                        </Typography>
                        <Typography variant="body2">
                          2B：n={bt?.trades2B || 0} · 胜率 {fmtNum(bt?.winRate2B, 1)}% · 均 R{' '}
                          {fmtNum(bt?.avgR2B, 2)}（胜 {bt?.wins2B || 0} / 负 {bt?.losses2B || 0}）
                        </Typography>
                        <Typography variant="caption" color="text.secondary" display="block">
                          {hint}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {bt?.note}
                        </Typography>
                      </CardContent>
                    </Card>
                  </Grid>
                ))}
              </Grid>

              <Card variant="outlined" sx={{ mb: 2 }}>
                <CardContent>
                  <Typography variant="subtitle2" gutterBottom>
                    趋势线画法对比（hl / ols / ransac · 1-2-3）
                  </Typography>
                  <Grid container spacing={2}>
                    {[
                      { title: '日线', rows: prospec.methodCompare1d },
                      { title: '4h', rows: prospec.methodCompare4h },
                    ].map(({ title, rows }) => (
                      <Grid item xs={12} md={6} key={title}>
                        <Typography variant="caption" color="text.secondary">
                          {title}
                        </Typography>
                        {(rows || []).length === 0 ? (
                          <Typography variant="body2" color="text.secondary">
                            部署含 methodCompare 的 API 后显示
                          </Typography>
                        ) : (
                          (rows || []).map((r: any) => (
                            <Typography variant="body2" key={r.method}>
                              {r.method}：n={r.trades123 || 0} · 胜率 {fmtNum(r.winRate123, 1)}% · 均
                              R {fmtNum(r.avgR123, 2)}
                            </Typography>
                          ))
                        )}
                      </Grid>
                    ))}
                  </Grid>
                </CardContent>
              </Card>

              <Grid container spacing={2} sx={{ mb: 2 }}>
                {[
                  { key: 'weekly', title: '周线（大势）', tf: prospec.weekly, role: '定势' },
                  { key: 'daily', title: '日线（主结构）', tf: prospec.daily, role: '主看' },
                  { key: 'h4', title: '4h（波段·对照）', tf: prospec.h4, role: '对照' },
                  { key: 'm15', title: '15m（扳机·谨慎）', tf: prospec.m15, role: '扳机' },
                ].map(({ key, title, tf, role }) => (
                  <Grid item xs={12} md={6} key={key}>
                    <Card
                      variant="outlined"
                      sx={{
                        height: '100%',
                        borderColor: role === '主看' ? 'success.main' : undefined,
                      }}
                    >
                      <CardContent>
                        <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', mb: 1, alignItems: 'center' }}>
                          <Typography variant="subtitle2">{title}</Typography>
                          <Chip size="small" variant="outlined" label={role} />
                          <Chip
                            size="small"
                            label={
                              tf?.nest?.bias === 'bull'
                                ? '嵌套偏多'
                                : tf?.nest?.bias === 'bear'
                                  ? '嵌套偏空'
                                  : '嵌套震荡'
                            }
                            color={
                              tf?.nest?.bias === 'bull'
                                ? 'success'
                                : tf?.nest?.bias === 'bear'
                                  ? 'error'
                                  : 'default'
                            }
                          />
                          <Chip
                            size="small"
                            label={`1-2-3 阶段${tf?.oneTwoThree?.stage ?? 0}${
                              tf?.oneTwoThree?.confirmed ? ' 确认' : ''
                            }${tf?.oneTwoThree?.method ? ` ·${tf.oneTwoThree.method}` : ''}`}
                          />
                        </Box>
                        <Typography variant="caption" display="block">
                          {tf?.nest?.note}
                        </Typography>
                        <Typography variant="caption" display="block" sx={{ mt: 0.5 }}>
                          1-2-3：{tf?.oneTwoThree?.direction || 'none'} · {tf?.oneTwoThree?.note}
                        </Typography>
                        {(tf?.trendLines?.ransacSupport ||
                          tf?.trendLines?.ransacResistance ||
                          tf?.trendLines?.ransacSupports?.length ||
                          tf?.trendLines?.ransacResistances?.length) && (
                          <Typography variant="caption" display="block" color="text.secondary">
                            趋势线 RANSAC：支撑{' '}
                            {tf.trendLines.ransacSupports?.length ||
                              (tf.trendLines.ransacSupport ? 1 : 0)}{' '}
                            条
                            {tf.trendLines.ransacSupport
                              ? `（主触点 ${tf.trendLines.ransacSupport.touches}${
                                  tf.trendLines.ransacSupport.broken ? '·破' : ''
                                }）`
                              : ''}{' '}
                            / 阻力{' '}
                            {tf.trendLines.ransacResistances?.length ||
                              (tf.trendLines.ransacResistance ? 1 : 0)}{' '}
                            条
                            {tf.trendLines.ransacResistance
                              ? `（主触点 ${tf.trendLines.ransacResistance.touches}${
                                  tf.trendLines.ransacResistance.broken ? '·破' : ''
                                }）`
                              : ''}
                          </Typography>
                        )}
                        {tf?.twoB && (
                          <Typography variant="caption" display="block" color="text.secondary">
                            2B：{tf.twoB.side === 'long' ? '试多' : '试空'} · 刺穿{' '}
                            {fmtNum(tf.twoB.pierced, 2)} · {tf.twoB.barsAgo} 根前
                          </Typography>
                        )}
                        <Typography variant="body2" sx={{ mt: 1 }}>
                          现价 {fmtNum(tf?.last, 2)} · EMA50 {fmtNum(tf?.nest?.ema50, 2)}
                        </Typography>
                      </CardContent>
                    </Card>
                  </Grid>
                ))}
              </Grid>

              <Grid container spacing={2} sx={{ mb: 2 }}>
                {(prospec.clocks || []).map((ck: any) => (
                  <Grid item xs={6} md={3} key={ck.interval}>
                    <Card variant="outlined">
                      <CardContent>
                        <Typography variant="caption" color="text.secondary">
                          {ck.note}
                        </Typography>
                        <Typography variant="body2">{ck.cst}</Typography>
                      </CardContent>
                    </Card>
                  </Grid>
                ))}
              </Grid>

              {(prospec.scan || []).length > 0 && (
                <Card variant="outlined" sx={{ mb: 2 }}>
                  <CardContent>
                    <Typography variant="subtitle2" gutterBottom>
                      多币扫描（日线 1-2-3 / 15m 2B 有信号）
                    </Typography>
                    <ScrollTable>
                      <Table size="small">
                        <TableHead>
                          <TableRow>
                            <TableCell>币</TableCell>
                            <TableCell>日嵌套</TableCell>
                            <TableCell>1-2-3</TableCell>
                            <TableCell>2B</TableCell>
                            <TableCell>设置</TableCell>
                            <TableCell align="right">价格</TableCell>
                          </TableRow>
                        </TableHead>
                        <TableBody>
                          {prospec.scan.map((row: any) => (
                            <TableRow
                              key={row.symbol}
                              hover
                              sx={{ cursor: 'pointer' }}
                              onClick={() => setProspecSymbol(row.symbol)}
                            >
                              <TableCell>{row.symbol}</TableCell>
                              <TableCell>{row.nestBias}</TableCell>
                              <TableCell>
                                {row.dir123}/{row.stage123}
                                {row.confirmed ? '✓' : ''}
                              </TableCell>
                              <TableCell>{row.twoBSide || '—'}</TableCell>
                              <TableCell>
                                {row.label}
                                {row.aligned ? ' ·同向' : ''}
                              </TableCell>
                              <TableCell align="right">{fmtNum(row.last, 4)}</TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </ScrollTable>
                  </CardContent>
                </Card>
              )}

              <Card variant="outlined">
                <CardContent>
                  <Typography variant="subtitle2" gutterBottom>
                    规则（策略 prospec）
                  </Typography>
                  <List dense>
                    {(prospec.rules || []).map((r: string, i: number) => (
                      <ListItem key={i} disableGutters>
                        <ListItemText primary={`${i + 1}. ${r}`} />
                      </ListItem>
                    ))}
                  </List>
                </CardContent>
              </Card>
            </>
          )}
        </TabPanel>
      </Box>
    </DashboardLayout>
  );
}
