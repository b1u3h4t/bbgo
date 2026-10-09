# Prospec 消融回测结论（社区画法 + 持仓币）

脚本：`scripts/prospec_ablation_bt`（Casoon 式触点/违规打分、SFP/2B、嵌套、2R、费率）。

## 症结

| 问题 | 证据 |
|------|------|
| **周期错了** | 4h 全变体 avgR 为负；日线才有正期望 |
| **缺嵌套** | 日线 `123_ransac_1R`≈0 → 加 nest 后 avgR **+0.15** |
| **1R 太小** | 确认偏晚，日线 `2R+nest` avgR **+0.44** 明显好于 1R |
| **过交易** | 无过滤时笔数多、胜率被假破摊薄 |
| **费率** | 扣 ~0.15R 后仍勉强正，边缘不厚 |

## 日线聚合（持仓+BTC/ETH）摘要

| variant | trades | wr% | avgR |
|---------|--------|-----|------|
| 123_ransac_1R | 80 | 51 | ~0 |
| 123_ransac_1R_nest | 32 | 59 | **+0.15** |
| 123_ransac_2R_nest | 32 | 53 | **+0.44** |
| 123_casoon_2R_nest | 28 | 54 | +0.31 |
| 2b_2R_nest | 28 | 43 | +0.19 |

## 修复（已落地代码）

1. 默认目标 **2R**（`DefaultRewardRisk` / `rewardRisk`）
2. `MinRR` 默认 2；策略跳过 RR 不足
3. 默认 `trendFit=ransac`；日志警告 4h/短周期无嵌套
4. 生产配置应：`interval: 1d`，`nestInterval: 1w`，`useNestFilter: true`，`requireNestAlign: true`
5. **不要**用裸 4h 123 自动下单

## 仍不够生产的原因

样本仍短、币种少、未做 walk-forward 外推；日线 2R+nest 是「相对最好」，不是「足够上真钱全自动」。
