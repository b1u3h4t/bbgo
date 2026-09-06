# udbox — UD优道箱体：震荡 + 趋势混合

独立于 `grid2`。默认可只做突破；打开 `enableRange` 后：

| 阶段 | 行为 |
|------|------|
| **Range（箱内）** | 锁定箱体，下沿附近买入、上沿附近卖出（高抛低吸），中线或对侧区止盈 |
| **Trend（突破）** | 收盘突破箱顶/底 → 顺势；与震荡仓同向则晋升趋势停损，反向则平仓翻转 |

## 状态机

```
检测箱体且价格在内 → 锁定 lockedBox，phase=range
  ├─ 下区做多 / 上区做空（rangeQuantity）
  ├─ 到中线或对侧区 → 平仓止盈
  └─ 收盘突破 → phase=trend，对齐仓位，箱边停损 + 新箱移动停损
趋势停损触发 → phase=range，等待重新锁箱
```

## 配置示例（混合）

```yaml
exchangeStrategies:
  - on: binance
    udbox:
      symbol: BTCUSDT
      interval: 4h
      boxWindow: 20
      minBoxWidthPct: 0.015      # 过窄箱易被手续费吃掉
      maxBoxWidthPct: 0.05
      breakBufferPct: 0.001
      enableLong: true
      enableShort: true
      enableRange: true          # 箱内震荡
      rangeRequireCompression: true  # 仅波动收敛时锁箱/做震
      rangeBuyZonePct: 0.15      # 更贴底才进
      rangeSellZonePct: 0.15
      rangeQtyRatio: 0.5         # 震荡仓 = quantity * 0.5
      rangeTakeMid: false        # false=对侧区止盈（更大目标）
      useNestFilter: true        # 主要过滤趋势突破方向
      nestInterval: 1d
      nestEMAWindow: 20
      quantity: 0.05             # 趋势仓
      useBoxStop: true
      trailNewBox: true
```

纯趋势：设 `enableRange: false`（或省略）。

回测：
- 4h 混合：`config/udbox-backtest.yaml`
- 15m（默认纯趋势，更低 MDD）：`config/udbox-backtest-15m.yaml`
- 30m（收紧 Hybrid）：`config/udbox-backtest-30m.yaml`

## 短周期（15m / 30m）

4h 参数直接搬到分时会刷单、回撤变大。短周期建议：

| 参数 | 15m | 30m |
|------|-----|-----|
| `boxWindow` | 40 | 32 |
| `minBoxWidthPct` | 0.025 | 0.02 |
| `compressionLookback` | 20 | 20 |
| `rangeBuy/SellZonePct` | 0.10 | 0.10 |
| `rangeQtyRatio` | 0.35 | 0.35 |
| `enableRange` | **false（推荐）** | 可 true，但 MDD 通常仍高于纯趋势 |

## 注意

- 箱内震荡会增加交易次数与假突破前的磨损；突破时若持反向仓会先平再开。
- `rangeRequireCompression`：只在「起涨点」式波动收敛时锁箱做震，降低无压缩盘整的刷单。
- 锁定箱体避免滚动窗口把箱顶箱底不断拉开。
- 与 `grid2` 仍不同：单仓位、分区限价逻辑用市价收盘信号，不是挂满网格。
