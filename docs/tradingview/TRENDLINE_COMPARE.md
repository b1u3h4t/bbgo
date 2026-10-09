# 趋势线画法对比：Prospec vs TradingView 社区

## 本仓库实现

| 方法 | 输入点 | 拟合 | 触点门槛 | 用途 |
|------|--------|------|----------|------|
| `hl` | 最近摆动 HL/LH | 无斜线（水平突破） | — | 旧代理 |
| `ols` | 最近 ≤8 个 pivot | 全体最小二乘 | ≥2 | 平滑、易被尖刺拽歪 |
| `ransac` | 最近 ≤12 个 pivot | 穷举点对 + 内点再 OLS | ≥3（可回退 2） | **默认**；接近人手挑点 |

Pine 对照脚本（双线同屏）：[`prospec_trendlines.pine`](./prospec_trendlines.pine)

用法：TradingView → Pine Editor → 粘贴 → Add to chart。建议周期 4H/1D，品种 BTCUSDT.P / ETHUSDT.P 及持仓币。

## TradingView 他人常见画法

| 脚本 | 做法 | 与我们差异 |
|------|------|------------|
| [LuxAlgo ML Regression Trend](https://www.tradingview.com/script/58YCiPaa-Machine-Learning-Regression-Trend-LuxAlgo/) | **随机** RANSAC 拟合窗口内价格点（非仅 pivot） | 用全体 K 线回归；我们只用摆动点（更贴 Sperandeo） |
| [Metrify Automatic Trendline](https://www.tradingview.com/script/kiKDpU85-Automatic-Trendline-Metrify/) | pivot 两两点对穷举 + 触点/紧度/近因打分 | 最接近我们的 `ransac`；他们还有 relevance 距离过滤 |
| [Casoon Auto Trendlines](https://casoon.github.io/pine-scripts/docs/market-structure/auto-trendlines/) | Directional / Combinatorial + OLS/Outer，min touches=3 | Combinatorial ≈ 确定性 RANSAC；与我们同族 |
| 简易「连最近两 pivot」脚本 | 只连相邻高低点 | 无触点验证，假线多 |

## 目视对照建议

1. 同一品种、同一周期打开：本脚本 + Metrify 或 Casoon。  
2. 看支撑线是否贴多个更高低点、阻力是否贴多个更低高点。  
3. OLS（虚线）通常更「穿心」；RANSAC（粗实线）应更贴外沿 pivot，更像原书。  
4. LuxAlgo 回归通道偏「中轴趋势」，不要当成 Sperandeo 破线信号。

## 回测池（持仓 + 大币）

`BTCUSDT, ETHUSDT` + 当前持仓：`NEAR, BNB, AVAX, LINK, SOL, ENA, XRP, HYPE, ZEC`（分析用；NEAR 实盘不动）。

Dashboard：`/api/analysis/prospec` → `methodCompare4h` / `methodCompare1d`。
