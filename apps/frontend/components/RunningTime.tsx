import { styled } from '@mui/styles';
import { Description } from './Description';

const RunningTimeSection = styled('div')(() => ({
  display: 'flex',
  alignItems: 'center',
}));

const StatusSign = styled('span')(() => ({
  width: '10px',
  height: '10px',
  display: 'block',
  backgroundColor: 'rgb(113, 218, 113)',
  borderRadius: '50%',
  marginRight: '5px',
}));

export default function RunningTime({ seconds }: { seconds: number }) {
  const safe = Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
  const day = Math.floor(safe / (60 * 60 * 24));
  const hour = Math.floor((safe % (60 * 60 * 24)) / 3600);
  const min = Math.floor(((safe % (60 * 60 * 24)) % 3600) / 60);

  return (
    <RunningTimeSection>
      <StatusSign />
      <Description>
        Running for
        <span className="duration">{day}</span>D
        <span className="duration">{hour}</span>H
        <span className="duration">{min}</span>M
      </Description>
    </RunningTimeSection>
  );
}
