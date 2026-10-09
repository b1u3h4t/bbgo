# prospec — 《专业投机原理》风格策略

Victor Sperandeo 框架的可执行子集：

| 组件 | 作用 |
|------|------|
| **Nest（嵌套）** | 更高周期偏多/偏空（摆动 HH/HL 或 LH/LL + EMA50） |
| **1-2-3** | 趋势变盘三步：破结构 → 失败反抽/回踩 → 收盘确认 |
| **2B** | 假突破：刺穿前高/前低后收盘收回 |

只认**收盘**；停损放在结构外（②点或刺穿极值）。

## 配置示例（15m 执行 + 4h 嵌套）

```yaml
exchangeStrategies:
  - on: binance
    prospec:
      symbol: BTCUSDT
      interval: 15m
      nestInterval: 4h
      useNestFilter: true
      requireNestAlign: true
      enable123: true
      enable2B: true
      swingLook: 3
      enableLong: true
      enableShort: true
      quantity: 0.01
      minRR: 1.2
```

回测：`config/prospec-backtest-15m.yaml`

## Dashboard

Analysis → **专业投机** → `/api/analysis/prospec`：周/日/4h/15m 嵌套、1-2-3 阶段、2B、主操作建议与多币扫描。
