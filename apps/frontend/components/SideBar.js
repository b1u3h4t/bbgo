import Drawer from '@mui/material/Drawer';
import Divider from '@mui/material/Divider';
import List from '@mui/material/List';
import Link from 'next/link';
import ListItem from '@mui/material/ListItem';
import ListItemIcon from '@mui/material/ListItemIcon';
import DashboardIcon from '@mui/icons-material/Dashboard';
import ListItemText from '@mui/material/ListItemText';
import ListIcon from '@mui/icons-material/List';
import TrendingUpIcon from '@mui/icons-material/TrendingUp';
import AssessmentIcon from '@mui/icons-material/Assessment';
import React from 'react';
import { makeStyles } from '@mui/styles';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';

export const DRAWER_WIDTH = 240;
export const DRAWER_WIDTH_MOBILE = 'min(86vw, 300px)';

const useStyles = makeStyles((theme) => ({
  drawerPaper: {
    width: DRAWER_WIDTH,
    boxSizing: 'border-box',
    whiteSpace: 'nowrap',
  },
  drawerPaperMobile: {
    width: DRAWER_WIDTH_MOBILE,
    boxSizing: 'border-box',
    whiteSpace: 'nowrap',
  },
  drawer: {
    width: DRAWER_WIDTH,
    flexShrink: 0,
  },
  appBarSpacer: theme.mixins.toolbar,
  drawerBody: {
    overflow: 'auto',
    paddingBottom: 'env(safe-area-inset-bottom, 0px)',
  },
}));

const navMain = [{ href: '/', label: 'Dashboard', icon: <DashboardIcon /> }];

const navSecondary = [
  { href: '/orders', label: 'Orders', icon: <ListIcon /> },
  { href: '/trades', label: 'Trades', icon: <ListIcon /> },
  { href: '/strategies', label: 'Strategies', icon: <TrendingUpIcon /> },
  { href: '/analysis', label: 'Analysis', icon: <AssessmentIcon /> },
];

export default function SideBar({ mobileOpen = false, onClose }) {
  const classes = useStyles();
  const theme = useTheme();
  // md = 900px; K80 Pro portrait CSS width ~400–440px → temporary drawer
  const isMobile = useMediaQuery(theme.breakpoints.down('md'), { noSsr: true });

  const handleNav = () => {
    if (isMobile && onClose) onClose();
  };

  const renderItems = (items) =>
    items.map((item) => (
      <Link href={item.href} key={item.href}>
        <ListItem button onClick={handleNav}>
          <ListItemIcon>{item.icon}</ListItemIcon>
          <ListItemText primary={item.label} />
        </ListItem>
      </Link>
    ));

  const content = (
    <div className={classes.drawerBody}>
      <div className={classes.appBarSpacer} />
      <List>{renderItems(navMain)}</List>
      <Divider />
      <List>{renderItems(navSecondary)}</List>
    </div>
  );

  if (isMobile) {
    return (
      <Drawer
        variant="temporary"
        open={mobileOpen}
        onClose={onClose}
        ModalProps={{ keepMounted: true }}
        classes={{ paper: classes.drawerPaperMobile }}
        sx={{
          zIndex: (t) => t.zIndex.drawer + 2,
          '& .MuiDrawer-paper': {
            width: DRAWER_WIDTH_MOBILE,
            paddingTop: 'env(safe-area-inset-top, 0px)',
          },
        }}
      >
        {content}
      </Drawer>
    );
  }

  return (
    <Drawer
      variant="permanent"
      className={classes.drawer}
      classes={{ paper: classes.drawerPaper }}
      anchor="left"
      open
    >
      {content}
    </Drawer>
  );
}
