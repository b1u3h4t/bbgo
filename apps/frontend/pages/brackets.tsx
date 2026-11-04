import { useCallback, useEffect, useState, type FormEvent } from 'react';
import DashboardLayout from '../layouts/DashboardLayout';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Checkbox,
  Chip,
  FormControl,
  FormControlLabel,
  Grid,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from '@mui/material';
import {
  BracketJob,
  cancelBracket,
  createBracket,
  queryBracketHealth,
  queryBrackets,
} from '../api/bbgo';

function stateColor(state: string): 'default' | 'info' | 'success' | 'warning' | 'error' {
  switch (state) {
    case 'pending_entry':
      return 'info';
    case 'pending_exit':
      return 'warning';
    case 'done':
      return 'success';
    case 'canceled':
      return 'default';
    default:
      return 'default';
  }
}

function fmt(v: unknown): string {
  if (v === undefined || v === null || v === '') return '—';
  return String(v);
}

function orderId(v: unknown): string {
  if (v === undefined || v === null || v === '' || v === 0 || v === '0') return '';
  return String(v);
}

export default function BracketsPage() {
  const [jobs, setJobs] = useState<BracketJob[]>([]);
  const [health, setHealth] = useState<{ ok: boolean; error?: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [okMsg, setOkMsg] = useState('');

  const [symbol, setSymbol] = useState('ZECUSDT');
  const [side, setSide] = useState('BUY');
  const [quantity, setQuantity] = useState('');
  const [price, setPrice] = useState('');
  const [tp, setTp] = useState('');
  const [sl, setSl] = useState('0');
  const [asMarket, setAsMarket] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const h = await queryBracketHealth();
      setHealth(h);
      if (!h.ok) {
        setJobs([]);
        setError(h.error || 'bracket daemon 不可用');
        return;
      }
      const list = await queryBrackets();
      setJobs(list);
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 8000);
    return () => clearInterval(t);
  }, [refresh]);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError('');
    setOkMsg('');
    try {
      const job = await createBracket({
        symbol: symbol.trim().toUpperCase(),
        side: side.toUpperCase(),
        quantity: quantity.trim(),
        price: asMarket ? undefined : price.trim(),
        market: asMarket,
        tp: tp.trim(),
        sl: sl.trim() || '0',
      });
      setOkMsg(`已创建 ${job.id.slice(0, 8)}… 进场单 ${job.entryOrderID || ''}`);
      setQuantity('');
      await refresh();
    } catch (err: any) {
      const msg =
        err?.response?.data?.error ||
        (typeof err?.response?.data === 'string' ? err.response.data : null) ||
        err?.message ||
        String(err);
      setError(msg);
    } finally {
      setSubmitting(false);
    }
  };

  const onCancel = async (id: string) => {
    setError('');
    setOkMsg('');
    try {
      await cancelBracket(id);
      setOkMsg(`已取消 ${id.slice(0, 8)}…`);
      await refresh();
    } catch (err: any) {
      setError(err?.response?.data?.error || err?.message || String(err));
    }
  };

  return (
    <DashboardLayout>
      <Box sx={{ p: { xs: 1.5, md: 3 }, maxWidth: 1200, mx: 'auto' }}>
        <Stack
          direction={{ xs: 'column', sm: 'row' }}
          alignItems={{ sm: 'center' }}
          justifyContent="space-between"
          spacing={1}
          sx={{ mb: 2 }}
        >
          <Box>
            <Typography variant="h5" component="h1">
              Bracket 挂单
            </Typography>
            <Typography variant="body2" color="text.secondary">
              进场成交后自动挂 reduceOnly 止盈；止损价 ≤0 不挂止损
            </Typography>
          </Box>
          <Stack direction="row" spacing={1} alignItems="center">
            {health && (
              <Chip
                size="small"
                label={health.ok ? 'daemon 在线' : 'daemon 离线'}
                color={health.ok ? 'success' : 'error'}
              />
            )}
            <Button variant="outlined" size="small" onClick={refresh} disabled={loading}>
              刷新
            </Button>
          </Stack>
        </Stack>

        {error && (
          <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError('')}>
            {error}
          </Alert>
        )}
        {okMsg && (
          <Alert severity="success" sx={{ mb: 2 }} onClose={() => setOkMsg('')}>
            {okMsg}
          </Alert>
        )}

        <Card sx={{ mb: 3 }}>
          <CardContent>
            <Typography variant="subtitle1" gutterBottom>
              新建
            </Typography>
            <Box component="form" onSubmit={onSubmit}>
              <Grid container spacing={2}>
                <Grid item xs={12} sm={4} md={3}>
                  <TextField
                    label="Symbol"
                    value={symbol}
                    onChange={(e) => setSymbol(e.target.value)}
                    fullWidth
                    required
                    size="small"
                  />
                </Grid>
                <Grid item xs={6} sm={4} md={2}>
                  <FormControl fullWidth size="small">
                    <InputLabel>Side</InputLabel>
                    <Select
                      label="Side"
                      value={side}
                      onChange={(e) => setSide(String(e.target.value))}
                    >
                      <MenuItem value="BUY">BUY</MenuItem>
                      <MenuItem value="SELL">SELL</MenuItem>
                    </Select>
                  </FormControl>
                </Grid>
                <Grid item xs={6} sm={4} md={2}>
                  <TextField
                    label="数量"
                    value={quantity}
                    onChange={(e) => setQuantity(e.target.value)}
                    fullWidth
                    required
                    size="small"
                  />
                </Grid>
                <Grid item xs={12} sm={4} md={2}>
                  <TextField
                    label="进场价"
                    value={price}
                    onChange={(e) => setPrice(e.target.value)}
                    fullWidth
                    required={!asMarket}
                    disabled={asMarket}
                    size="small"
                  />
                </Grid>
                <Grid item xs={6} sm={4} md={2}>
                  <TextField
                    label="止盈 TP"
                    value={tp}
                    onChange={(e) => setTp(e.target.value)}
                    fullWidth
                    required
                    size="small"
                  />
                </Grid>
                <Grid item xs={6} sm={4} md={2}>
                  <TextField
                    label="止损 SL"
                    value={sl}
                    onChange={(e) => setSl(e.target.value)}
                    fullWidth
                    size="small"
                    helperText="0=不挂"
                  />
                </Grid>
                <Grid item xs={12} sm={6} md={3}>
                  <FormControlLabel
                    control={
                      <Checkbox
                        checked={asMarket}
                        onChange={(e) => setAsMarket(e.target.checked)}
                      />
                    }
                    label="市价进场"
                  />
                </Grid>
                <Grid item xs={12} sm={6} md={3}>
                  <Button
                    type="submit"
                    variant="contained"
                    fullWidth
                    disabled={submitting || health?.ok === false}
                  >
                    {submitting ? '提交中…' : '下单并挂 TP/SL'}
                  </Button>
                </Grid>
              </Grid>
            </Box>
          </CardContent>
        </Card>

        <Card>
          <CardContent>
            <Typography variant="subtitle1" gutterBottom>
              任务列表 {loading ? '…' : `(${jobs.length})`}
            </Typography>
            <TableContainer sx={{ overflowX: 'auto' }}>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>状态</TableCell>
                    <TableCell>Symbol</TableCell>
                    <TableCell>方向</TableCell>
                    <TableCell align="right">数量</TableCell>
                    <TableCell align="right">进场价</TableCell>
                    <TableCell>进场单</TableCell>
                    <TableCell align="right">止盈价</TableCell>
                    <TableCell>止盈单</TableCell>
                    <TableCell align="right">止损价</TableCell>
                    <TableCell>止损单</TableCell>
                    <TableCell>操作</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {jobs.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={11}>
                        <Typography variant="body2" color="text.secondary">
                          暂无任务
                        </Typography>
                      </TableCell>
                    </TableRow>
                  )}
                  {jobs
                    .slice()
                    .reverse()
                    .map((j) => {
                      const active =
                        j.state === 'pending_entry' || j.state === 'pending_exit';
                      const tpId = orderId(j.tpOrderID);
                      const slId = orderId(j.slOrderID);
                      const slEnabled = Number(j.slPrice) > 0;
                      return (
                        <TableRow key={j.id} hover>
                          <TableCell>
                            <Chip size="small" label={j.state} color={stateColor(j.state)} />
                            {j.lastError ? (
                              <Typography variant="caption" color="error" display="block">
                                {j.lastError}
                              </Typography>
                            ) : null}
                          </TableCell>
                          <TableCell>{j.symbol}</TableCell>
                          <TableCell>{j.side}</TableCell>
                          <TableCell align="right">{fmt(j.quantity)}</TableCell>
                          <TableCell align="right">{fmt(j.entryPrice)}</TableCell>
                          <TableCell>
                            <Typography variant="caption">{orderId(j.entryOrderID) || '—'}</Typography>
                          </TableCell>
                          <TableCell align="right">{fmt(j.tpPrice)}</TableCell>
                          <TableCell>
                            {tpId ? (
                              <Typography variant="caption">{tpId}</Typography>
                            ) : (
                              <Typography
                                variant="caption"
                                color={j.state === 'pending_exit' ? 'error' : 'text.secondary'}
                              >
                                {j.state === 'pending_entry' ? '待进场成交' : '未挂'}
                              </Typography>
                            )}
                          </TableCell>
                          <TableCell align="right">
                            {slEnabled ? fmt(j.slPrice) : '—'}
                          </TableCell>
                          <TableCell>
                            {!slEnabled ? (
                              <Typography variant="caption" color="text.secondary">
                                未启用
                              </Typography>
                            ) : slId ? (
                              <Typography variant="caption">{slId}</Typography>
                            ) : (
                              <Typography
                                variant="caption"
                                color={j.state === 'pending_exit' ? 'error' : 'text.secondary'}
                              >
                                {j.state === 'pending_entry' ? '待进场成交' : '未挂'}
                              </Typography>
                            )}
                          </TableCell>
                          <TableCell>
                            {active ? (
                              <Button
                                size="small"
                                color="warning"
                                onClick={() => onCancel(j.id)}
                              >
                                取消
                              </Button>
                            ) : (
                              <Typography variant="caption" color="text.secondary">
                                {j.id.slice(0, 8)}
                              </Typography>
                            )}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                </TableBody>
              </Table>
            </TableContainer>
          </CardContent>
        </Card>
      </Box>
    </DashboardLayout>
  );
}
