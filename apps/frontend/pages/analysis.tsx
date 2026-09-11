import { useCallback, useEffect, useState } from 'react';
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
} from '@mui/material';
import {
  queryAnalysisGridCalc,
  queryAnalysisMargin,
  queryAnalysisMarket,
  queryAnalysisTodayPnL,
} from '../api/bbgo';

function pnlColor(v: number) {
  if (v > 0) return '#2e7d32';
  if (v < 0) return '#c62828';
  return undefined;
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
      };
      if (quantity) params.quantity = quantity;
      setGrid(await queryAnalysisGridCalc(params));
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || 'grid calc failed');
    } finally {
      setLoading(false);
    }
  }, [symbol, atrMult, gridNumber, quantity, leverage, targetUtil]);

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

        <Tabs value={tab} onChange={(_, v) => setTab(v)} sx={{ mb: 1 }}>
          <Tab label="行情分析" />
          <Tab label="保证金" />
          <Tab label="网格计算" />
          <Tab label="今日盈亏" />
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
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell align="right">Last</TableCell>
                <TableCell align="right">Bounce%</TableCell>
                <TableCell align="right">2h%</TableCell>
                <TableCell align="right">4h%</TableCell>
                <TableCell align="right">RSI</TableCell>
                <TableCell align="right">Score</TableCell>
                <TableCell>Verdict</TableCell>
                <TableCell>Notes</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(market?.symbols || []).map((row: any) => (
                <TableRow key={row.symbol}>
                  <TableCell>{row.symbol}</TableCell>
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
                  <TableCell align="right">{row.score}</TableCell>
                  <TableCell>
                    <Chip size="small" label={row.verdict} />
                  </TableCell>
                  <TableCell>
                    <Typography variant="caption">
                      {(row.notes || []).join(', ')}
                    </Typography>
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

          <Typography variant="subtitle1" gutterBottom>
            Positions
          </Typography>
          <Table size="small" sx={{ mb: 3 }}>
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell>Side</TableCell>
                <TableCell align="right">Amt</TableCell>
                <TableCell align="right">Entry</TableCell>
                <TableCell align="right">Mark</TableCell>
                <TableCell align="right">Notional</TableCell>
                <TableCell align="right">IM≈</TableCell>
                <TableCell align="right">uPnL</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(margin?.positions || []).map((p: any) => (
                <TableRow key={p.symbol}>
                  <TableCell>{p.symbol}</TableCell>
                  <TableCell>{p.side}</TableCell>
                  <TableCell align="right">{p.positionAmt}</TableCell>
                  <TableCell align="right">{p.entryPrice}</TableCell>
                  <TableCell align="right">{p.markPrice}</TableCell>
                  <TableCell align="right">{p.notional}</TableCell>
                  <TableCell align="right">{p.initialMarginEst}</TableCell>
                  <TableCell
                    align="right"
                    sx={{ color: pnlColor(p.unrealizedPnL) }}
                  >
                    {p.unrealizedPnL}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

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
                      Pins
                    </Typography>
                    {(grid.pins || []).map((p: number, i: number) => (
                      <Chip key={i} label={p} size="small" sx={{ m: 0.5 }} />
                    ))}
                  </CardContent>
                </Card>
              </Grid>
            </Grid>
          )}
        </TabPanel>

        <TabPanel value={tab} index={3}>
          <Box sx={{ display: 'flex', gap: 1, mb: 2 }}>
            <Button variant="contained" onClick={loadPnl}>
              刷新
            </Button>
            {pnl?.range && (
              <Typography variant="caption" color="text.secondary">
                {pnl.range.start} → {pnl.range.end} ({pnl.range.tz})
              </Typography>
            )}
          </Box>
          {pnl?.totals && (
            <Grid container spacing={2} sx={{ mb: 2 }}>
              {[
                ['Realized', pnl.totals.realized],
                ['Commission', pnl.totals.commission],
                ['Funding', pnl.totals.funding],
                ['Net', pnl.totals.net],
              ].map(([k, v]) => (
                <Grid item xs={6} md={3} key={String(k)}>
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
            </Grid>
          )}
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell align="right">Realized</TableCell>
                <TableCell align="right">Commission</TableCell>
                <TableCell align="right">Funding</TableCell>
                <TableCell align="right">Net</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(pnl?.symbols || [])
                .slice()
                .sort((a: any, b: any) => a.net - b.net)
                .map((row: any) => (
                  <TableRow key={row.symbol}>
                    <TableCell>{row.symbol}</TableCell>
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
      </Box>
    </DashboardLayout>
  );
}
