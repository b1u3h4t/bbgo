import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Typography from '@mui/material/Typography';
import { makeStyles } from '@mui/styles';

import { aggregateAssetsBy, fmtMoney, toNum } from '../utils/assets';

const useStyles = makeStyles((theme) => ({
  root: {
    margin: theme.spacing(1),
  },
  title: {
    fontSize: 14,
  },
  pos: {
    marginTop: 12,
  },
}));

export default function TotalAssetSummary({ assets }) {
  const classes = useStyles();
  const btc = toNum(aggregateAssetsBy(assets, 'inBTC'));
  const usd = toNum(aggregateAssetsBy(assets, 'inUSD'));

  return (
    <Card className={classes.root} variant="outlined">
      <CardContent>
        <Typography
          className={classes.title}
          color="textSecondary"
          gutterBottom
        >
          Total Account Balance
        </Typography>
        <Typography variant="h5" component="h2">
          {fmtMoney(btc, 8)} <span>BTC</span>
        </Typography>

        <Typography className={classes.pos} color="textSecondary">
          Estimated Value
        </Typography>

        <Typography variant="h5" component="h3">
          {fmtMoney(usd, 2)} <span>USD</span>
        </Typography>
      </CardContent>
    </Card>
  );
}
