import { styled } from '@mui/styles';
import type { GridStrategy } from '../api/bbgo';

import RunningTime from './RunningTime';
import Summary from './Summary';
import Stats from './Stats';
import { Description } from './Description';

const StrategyContainer = styled('section')(() => ({
  display: 'flex',
  flexDirection: 'column',
  justifyContent: 'space-around',
  width: '350px',
  border: '1px solid rgb(248, 149, 35)',
  borderRadius: '10px',
  padding: '10px',
}));

const Strategy = styled('div')(() => ({
  fontSize: '20px',
}));

function strategySymbol(data: GridStrategy): string {
  if (data?.grid?.symbol) {
    return data.grid.symbol;
  }
  const nested = (data as any)?.[data.strategy];
  if (nested?.symbol) {
    return nested.symbol;
  }
  return data?.id || '';
}

export default function Detail({ data }: { data: GridStrategy }) {
  if (!data?.stats) {
    return null;
  }

  const { strategy, stats, startTime } = data;
  const investment = stats.investment || 0;
  const totalProfitsPercentage =
    investment > 0 ? (stats.totalProfits / investment) * 100 : 0;
  const gridProfitsPercentage =
    investment > 0 ? (stats.gridProfits / investment) * 100 : 0;

  const now = Date.now();
  const durationMilliseconds = Math.max(now - (startTime || now), 1);
  const seconds = durationMilliseconds / 1000;
  const days = Math.max(seconds / 86400, 1 / 24);
  const gridAprPercentage =
    investment > 0 ? (stats.gridProfits / investment) * (365 / days) * 100 : 0;

  return (
    <StrategyContainer>
      <Strategy>{strategy}</Strategy>
      <div>{strategySymbol(data)}</div>
      <RunningTime seconds={seconds} />
      <Description>
        {stats.oneDayArbs ?? 0} arbs / 24h · Total {stats.totalArbs ?? 0} arbs
      </Description>
      <Summary stats={stats} totalProfitsPercentage={totalProfitsPercentage} />
      <Stats
        stats={stats}
        gridProfitsPercentage={gridProfitsPercentage}
        gridAprPercentage={gridAprPercentage}
      />
    </StrategyContainer>
  );
}
