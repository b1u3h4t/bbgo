import { createTheme } from '@mui/material/styles';
import { red } from '@mui/material/colors';

// Create a theme instance.
const theme = createTheme({
  palette: {
    primary: {
      main: '#eb9534',
      contrastText: '#ffffff',
    },
    secondary: {
      main: '#ccc0b1',
      contrastText: '#eb9534',
    },
    error: {
      main: red.A400,
    },
    background: {
      default: '#fff',
    },
  },
  // Align with Xiaomi K80 Pro portrait (~400–440 CSS px) under MUI md breakpoint
  breakpoints: {
    values: {
      xs: 0,
      sm: 600,
      md: 900,
      lg: 1200,
      xl: 1536,
    },
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          paddingLeft: 'env(safe-area-inset-left, 0px)',
          paddingRight: 'env(safe-area-inset-right, 0px)',
        },
      },
    },
    MuiButton: {
      styleOverrides: {
        root: {
          '@media (max-width:899.95px)': {
            minHeight: 40,
          },
        },
      },
    },
    MuiToggleButton: {
      styleOverrides: {
        root: {
          '@media (max-width:899.95px)': {
            paddingLeft: 8,
            paddingRight: 8,
          },
        },
      },
    },
  },
});

export default theme;
