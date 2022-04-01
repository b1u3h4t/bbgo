## bbgo backtest

backtest your strategies

```
bbgo backtest [flags]
```

### Options

```
      --base-asset-baseline   use base asset performance as the competitive baseline performance
      --force                 force execution without confirm
  -h, --help                  help for backtest
      --output string         the report output directory
      --sync                  sync backtest data
      --sync-from string      sync backtest data from the given time, which will override the time range in the backtest config
      --sync-only             sync backtest data only, do not run backtest
  -v, --verbose count         verbose level
      --verify                verify the kline back-test data
```

### Options inherited from parent commands

```
      --binance-api-key string           binance api key
      --binance-api-secret string        binance api secret
      --config string                    config file (default "bbgo.yaml")
      --debug                            debug mode
      --dotenv string                    the dotenv file you want to load (default ".env.local")
      --ftx-api-key string               ftx api key
      --ftx-api-secret string            ftx api secret
      --ftx-subaccount string            subaccount name. Specify it if the credential is for subaccount.
      --max-api-key string               max api key
      --max-api-secret string            max api secret
      --metrics                          enable prometheus metrics
      --metrics-port string              prometheus http server port (default "9090")
      --no-dotenv                        disable built-in dotenv
      --slack-channel string             slack trading channel (default "dev-bbgo")
      --slack-error-channel string       slack error channel (default "bbgo-error")
      --slack-token string               slack token
      --telegram-bot-auth-token string   telegram auth token
      --telegram-bot-token string        telegram bot token from bot father
```

### SEE ALSO

* [bbgo](bbgo.md)	 - bbgo is a crypto trading bot

###### Auto generated by spf13/cobra on 1-Apr-2022