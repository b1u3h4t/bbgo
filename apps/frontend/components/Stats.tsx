import { styled } from '@mui/styles';
import { StatsTitle, StatsValue, Percentage, formatPnL, pnlColor } from './Summary';
import { GridStats } from '../api/bbgo';

const StatsSection = styled('div')(() => ({
  display: 'grid',
  gridTemplateColumns: '1fr 1fr 1fr',
  gap: '10px',
}));

export default function Stats({
  stats,
  gridProfitsPercentage,
  gridAprPercentage,
}: {
  stats: GridStats;
  gridProfitsPercentage: number;
  gridAprPercentage: number;
}) {
  const gridColor = pnlColor(stats.gridProfits);
  const floatColor = pnlColor(stats.floatingPNL);
  const aprColor = pnlColor(gridAprPercentage);
  return (
    <StatsSection>
      <div>
        <StatsTitle>Grid Profits</StatsTitle>
        <StatsValue style={{ color: gridColor }}>
          {formatPnL(stats.gridProfits)}
        </StatsValue>
        <Percentage style={{ color: gridColor }}>
          {Number.isFinite(gridProfitsPercentage)
            ? `${gridProfitsPercentage.toFixed(2)}%`
            : '0%'}
        </Percentage>
      </div>

      <div>
        <StatsTitle>Floating PNL</StatsTitle>
        <StatsValue style={{ color: floatColor }}>
          {formatPnL(stats.floatingPNL)}
        </StatsValue>
      </div>

      <div>
        <StatsTitle>Grid APR</StatsTitle>
        <Percentage style={{ color: aprColor }}>
          {Number.isFinite(gridAprPercentage)
            ? `${gridAprPercentage.toFixed(2)}%`
            : '0%'}
        </Percentage>
      </div>

      <div>
        <StatsTitle>Current Price</StatsTitle>
        <div>{stats.currentPrice}</div>
      </div>

      <div>
        <StatsTitle>Price Range</StatsTitle>
        <div>
          {stats.lowestPrice}~{stats.highestPrice}
        </div>
      </div>
    </StatsSection>
  );
}
