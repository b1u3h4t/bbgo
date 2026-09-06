# udbox — UD优道箱体突破（趋势策略）

独立于 `grid2` 的趋势策略，把 [UD优道-交易室](https://www.youtube.com/@UDTrading) 的箱体交易逻辑自动化为 **突破跟随**，不是箱内高抛低吸。

## 频道逻辑 → 代码映射

| UD / Darvas 要点 | 实现 |
|------------------|------|
| 先选主做周期 | `interval`（建议 `4h` / `1d`） |
| 横盘画出箱顶箱底 | `DetectBox`：最近 `boxWindow` 根高低点 |
| 箱内不做、等「出方向」 | 收盘价严格在箱内则忽略 |
| 向上突破箱顶做多 / 跌破箱底做空 | `BreakLong` / `BreakShort` |
| 起涨点：横盘 + 波动收敛 + 破压力 | `requireCompression` |
| 优道嵌套（大周期定方向） | `useNestFilter` + `nestInterval` EMA |
| 停损在箱另一侧 | `useBoxStop`，初始 stop=箱底(多)/箱顶(空) |
| 新箱形成后上移停损 | `trailNewBox` |
| 下跌趋势以空为主 | `enableShort: true`，嵌套空头过滤 |

参考视频（频道内）：
- 《箱体理论交易系统，10分钟学会！》
- 《箱体理论鼻祖：尼古拉斯·达瓦斯》
- 《优道嵌套策略》
- 《起涨点，有哪些特征？》
- 日常盘面：压力/支撑箱体，突破继续、跌破转向

## 与网格的区别

- **grid2**：箱内做市，吃震荡；趋势单边会堆仓浮亏。
- **udbox**：箱外突破才开仓；单边是顺势，假突破用箱边停损。

## 示例配置

```yaml
exchangeStrategies:
  - on: binance
    udbox:
      symbol: BTCUSDT
      interval: 4h
      boxWindow: 20
      minBoxWidthPct: 0.008   # 0.8%
      maxBoxWidthPct: 0.06    # 6%
      breakBufferPct: 0.001   # 收盘需越过边界 0.1%
      requireCompression: true
      compressionLookback: 10
      enableLong: true
      enableShort: true
      useNestFilter: true
      nestInterval: 1d
      nestEMAWindow: 20
      leverage: 2
      # quantity: 0.01       # 固定数量优先于 leverage；回测建议用 quantity
      useBoxStop: true       # 建议开启
      trailNewBox: true      # 建议开启
      roiTakeProfit: 0        # >0 时按 ROI 止盈，如 0.08
```

回测配置见 [`config/udbox-backtest.yaml`](../../../config/udbox-backtest.yaml)。
## 注意

- 无字幕可扒，逻辑来自公开视频标题/简介 + Darvas 经典规则 + UD 每日「箱体震荡→突破/跌破」话术归纳，**不是会员密训原文逐字复刻**。
- 假突破常见，期望是低胜率顺势；勿把 `roiTakeProfit` 设太小把利润砍死。
- 先回测/小仓位，再与现有 grid 并行。
