import { styled } from '@mui/styles';
import { GridStats } from '../api/bbgo';

const SummarySection = styled('div')(() => ({
  width: '100%',
  display: 'flex',
  justifyContent: 'space-around',
  backgroundColor: 'rgb(255, 245, 232)',
  margin: '10px 0',
}));

const SummaryBlock = styled('div')(() => ({
  padding: '5px 0 5px 0',
}));

export const StatsTitle = styled('div')(() => ({
  margin: '0 0 10px 0',
}));

export const StatsValue = styled('div')(() => ({
  marginBottom: '10px',
}));

export const Percentage = styled('div')(() => ({}));

export const profitGreen = 'rgb(123, 169, 90)';
export const lossRed = 'rgb(200, 70, 70)';

export function pnlColor(value: number): string {
  return value < 0 ? lossRed : profitGreen;
}

export function formatPnL(value: number, digits = 2): string {
  if (!Number.isFinite(value)) {
    return '0';
  }
  const rounded = Number(value.toFixed(digits));
  return rounded > 0 ? `+${rounded}` : `${rounded}`;
}

export default function Summary({
  stats,
  totalProfitsPercentage,
}: {
  stats: GridStats;
  totalProfitsPercentage: number;
}) {
  const color = pnlColor(stats.totalProfits);
  return (
    <SummarySection>
      <SummaryBlock>
        <StatsTitle>Investment USDT</StatsTitle>
        <div>{Number(stats.investment).toFixed(2)}</div>
      </SummaryBlock>

      <SummaryBlock>
        <StatsTitle>Total Profit USDT</StatsTitle>
        <StatsValue style={{ color }}>{formatPnL(stats.totalProfits)}</StatsValue>
        <Percentage style={{ color }}>
          {Number.isFinite(totalProfitsPercentage)
            ? `${totalProfitsPercentage.toFixed(2)}%`
            : '0%'}
        </Percentage>
      </SummaryBlock>
    </SummarySection>
  );
}
