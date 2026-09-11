import { useCallback, useEffect, useState } from 'react';
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
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import {
  queryAnalysisAvgDown,
  queryAnalysisGridCalc,
  queryAnalysisKlines,
  queryAnalysisMargin,
  queryAnalysisMarket,
  queryAnalysisTodayPnL,
} from '../api/bbgo';
import { buildOrderBookPinLevels } from '../components/pinLevels';

// TradingView Lightweight Charts needs browser APIs
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

function IntervalPicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <ToggleButtonGroup
      exclusive
      size="small"
      value={value}
      onChange={(_, v) => {
        if (v) onChange(v);
      }}
      sx={{ flexWrap: 'wrap' }}
    >
      {CHART_INTERVALS.map((iv) => (
        <ToggleButton key={iv.value} value={iv.value} sx={{ px: 1.25 }}>
          {iv.label}
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  );
}

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
  const [avgSymbol, setAvgSymbol] = useState('DOGEUSDT');
  const [avgAddIm, setAvgAddIm] = useState('200');
  const [avgPrice, setAvgPrice] = useState('');
  const [avgLeverage, setAvgLeverage] = useState('3');
  const [gridInterval, setGridInterval] = useState('1h');
  const [pnlInterval, setPnlInterval] = useState('1h');
  const [marketInterval, setMarketInterval] = useState('15m');

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

  const loadPnl = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setPnl(await queryAnalysisTodayPnL());
    } catch (e: any) {
      setError(e?.message || 'failed to load today pnl');
    } finally {
      setLoading(false);
    }
  }, []);

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

  return (
    <DashboardLayout>
      <Box sx={{ p: 3, maxWidth: 1400, mx: 'auto' }}>
        <Typography variant="h5" gutterBottom>
          Analysis
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          行情结构、保证金利用率、ATR 网格试算、今日已实现盈亏（与运维聊天中的分析同口径）
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
          }}
          sx={{ mb: 1 }}
        >
          <Tab label="行情分析" />
          <Tab label="保证金" />
          <Tab label="网格计算" />
          <Tab label="今日盈亏" />
          <Tab label="慎重补仓" />
        </Tabs>

        <TabPanel value={tab} index={0}>
          <Box sx={{ display: 'flex', gap: 1, mb: 2, alignItems: 'center' }}>
            <Button variant="contained" onClick={loadMarket}>
              刷新
            </Button>
            {market?.stage && (
              <Chip
                label={`BTC阶段: ${market.stage}`}
                color={
                  market.stage.includes('偏强')
                    ? 'success'
                    : market.stage.includes('观望')
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

          <Accordion defaultExpanded sx={{ mb: 2 }}>
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
                      打分表
                    </Typography>
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
                  </Grid>
                  <Grid item xs={12} md={2}>
                    <Typography variant="subtitle2" gutterBottom>
                      结论映射
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
                    <Typography variant="caption" color="text.secondary">
                      人工核验：用 Notes 逐项对上打分表，加减应等于 Score。
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
                  <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
                    <IntervalPicker
                      value={marketChart.interval || marketInterval}
                      onChange={(iv) => loadMarketChart(marketChart.symbol, iv)}
                    />
                    <Button size="small" onClick={() => setMarketChart(null)}>
                      关闭
                    </Button>
                  </Box>
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
                  height={360}
                />
              </CardContent>
            </Card>
          )}

          <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
            点击 symbol 打开 TradingView Lightweight Charts（多周期；数据优先 MySQL）
          </Typography>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell align="right">Last</TableCell>
                <TableCell align="right">Bounce%</TableCell>
                <TableCell align="right">2h%</TableCell>
                <TableCell align="right">4h%</TableCell>
                <TableCell align="right">RSI</TableCell>
                <TableCell align="right">绿柱2h</TableCell>
                <TableCell align="center">HL</TableCell>
                <TableCell align="center">Mid↑</TableCell>
                <TableCell align="right">Score</TableCell>
                <TableCell>Verdict</TableCell>
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
                    {row.aboveMid ? 'Y' : '—'}
                  </TableCell>
                  <TableCell align="right" sx={{ fontWeight: 700 }}>
                    {row.score}
                  </TableCell>
                  <TableCell>
                    <Chip size="small" label={row.verdict} />
                  </TableCell>
                  <TableCell>
                    <Typography variant="caption">
                      {(row.notes || []).join(', ')}
                    </Typography>
                    {row.chunkLows?.length > 0 && (
                      <Typography
                        variant="caption"
                        display="block"
                        color="text.secondary"
                      >
                        chunkLows: {row.chunkLows.join(' → ')}
                      </Typography>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
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
                      <IntervalPicker
                        value={grid.interval || gridInterval}
                        onChange={reloadGridKlines}
                      />
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
                      height={380}
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
          <Box sx={{ display: 'flex', gap: 1, mb: 2 }}>
            <Button
              variant="contained"
              onClick={() => {
                loadPnl();
                loadMargin();
              }}
            >
              刷新
            </Button>
            {pnl?.range && (
              <Typography variant="caption" color="text.secondary">
                {pnl.range.start} → {pnl.range.end} ({pnl.range.tz})
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
                ['今日已实现', pnl.totals.realized],
                ['今日手续费', pnl.totals.commission],
                ['今日资金费', pnl.totals.funding],
                ['今日净流水', pnl.totals.net],
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
                    今日净流水对齐币安合约收入流水（REALIZED_PNL + COMMISSION +
                    FUNDING，CST 0:00 起）；手续费按 BNB≈
                    {pnl.feeNote.bnbPriceUSDT} 折合 USDT（原生{' '}
                    {pnl.totals.commissionBNB} BNB）。当前浮动盈亏是持仓累计浮盈亏，
                    不是「今日」增量。点击下方 symbol 可看 K 线+网格 pins。
                  </Typography>
                </Grid>
              )}
            </Grid>
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
                  <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
                    <IntervalPicker
                      value={pnlChart.interval || pnlInterval}
                      onChange={(iv) => loadPnlChart(pnlChart.symbol, iv)}
                    />
                    <Button size="small" onClick={() => setPnlChart(null)}>
                      关闭
                    </Button>
                  </Box>
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
                  height={320}
                />
              </CardContent>
            </Card>
          )}
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
      </Box>
    </DashboardLayout>
  );
}
