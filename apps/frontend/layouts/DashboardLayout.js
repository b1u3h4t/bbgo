import React from 'react';

import { makeStyles } from '@mui/styles';
import AppBar from '@mui/material/AppBar';
import Toolbar from '@mui/material/Toolbar';
import Typography from '@mui/material/Typography';
import Container from '@mui/material/Container';
import IconButton from '@mui/material/IconButton';
import MenuIcon from '@mui/icons-material/Menu';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';
import { Box } from '@mui/material';

import SideBar, { DRAWER_WIDTH } from '../components/SideBar';
import SyncButton from '../components/SyncButton';
import ConnectWallet from '../components/ConnectWallet';

const useStyles = makeStyles((theme) => ({
  root: {
    flexGrow: 1,
    display: 'flex',
    minHeight: '100vh',
    '@supports (min-height: 100dvh)': {
      minHeight: '100dvh',
    },
  },
  content: {
    flexGrow: 1,
    height: '100vh',
    overflow: 'auto',
    width: '100%',
    WebkitOverflowScrolling: 'touch',
    '@supports (height: 100dvh)': {
      height: '100dvh',
    },
    [theme.breakpoints.up('md')]: {
      maxWidth: `calc(100% - ${DRAWER_WIDTH}px)`,
    },
  },
  appBar: {
    zIndex: theme.zIndex.drawer + 1,
    paddingTop: 'env(safe-area-inset-top, 0px)',
  },
  appBarSpacer: {
    ...theme.mixins.toolbar,
    // punch-hole / status bar on Xiaomi & similar
    minHeight: 'calc(56px + env(safe-area-inset-top, 0px))',
  },
  container: {
    paddingLeft: 'env(safe-area-inset-left, 0px)',
    paddingRight: 'env(safe-area-inset-right, 0px)',
    paddingBottom: 'env(safe-area-inset-bottom, 0px)',
  },
  toolbar: {
    justifyContent: 'space-between',
    minHeight: 48,
    [theme.breakpoints.up('sm')]: {
      minHeight: 56,
    },
  },
  title: {
    fontSize: '1.1rem',
    [theme.breakpoints.up('sm')]: {
      fontSize: '1.25rem',
    },
  },
  toolbarActions: {
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(0.5),
    flexShrink: 0,
  },
}));

export default function DashboardLayout({ children }) {
  const classes = useStyles();
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('md'), { noSsr: true });
  const [mobileOpen, setMobileOpen] = React.useState(false);

  const toggleDrawer = () => setMobileOpen((v) => !v);
  const closeDrawer = () => setMobileOpen(false);

  return (
    <div className={classes.root}>
      <AppBar className={classes.appBar} position="fixed" color="primary" enableColorOnDark>
        <Toolbar className={classes.toolbar}>
          {isMobile && (
            <IconButton
              color="inherit"
              edge="start"
              aria-label="open navigation"
              onClick={toggleDrawer}
              sx={{ mr: 1 }}
            >
              <MenuIcon />
            </IconButton>
          )}
          <Typography variant="h6" className={classes.title} noWrap>
            BBGO
          </Typography>
          <Box sx={{ flexGrow: 1 }} />
          <div className={classes.toolbarActions}>
            <SyncButton />
            <Box sx={{ display: { xs: 'none', md: 'block' } }}>
              <ConnectWallet />
            </Box>
          </div>
        </Toolbar>
      </AppBar>

      <SideBar mobileOpen={mobileOpen} onClose={closeDrawer} />

      <main className={classes.content}>
        <div className={classes.appBarSpacer} />
        <Container
          className={classes.container}
          maxWidth={false}
          disableGutters={true}
        >
          {children}
        </Container>
      </main>
    </div>
  );
}
