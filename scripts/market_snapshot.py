#!/usr/bin/env python3
"""Account + multi-timeframe market snapshot for Binance USDT-M futures.

Usage: market_snapshot.py [--env /home/admin/bbgo/.env.local] [SYMBOL ...]

RSI uses Wilder smoothing (same as TradingView ta.rsi / Binance charts); EMA is seeded
with an SMA like TradingView. Each timeframe fetches WARMUP_BARS so both converge.
Only public endpoints plus four signed reads are used to stay well under the IP weight limit.
"""
import argparse
import hashlib
import hmac
import json
import time
import urllib.parse
import urllib.request
from collections import Counter
from pathlib import Path

BASE = "https://fapi.binance.com"
TIMEFRAMES = ["1h", "4h", "1d"]
WARMUP_BARS = 300
RSI_LEN = 14


def load_env(path):
    env = {}
    for line in Path(path).read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, v = line.split("=", 1)
        env[k] = v.strip().strip("\"'")
    return env


def public(path, **params):
    url = BASE + path + "?" + urllib.parse.urlencode(params)
    return json.load(urllib.request.urlopen(url, timeout=20))


def make_signed(key, secret):
    def signed(path, params=None):
        p = dict(params or {})
        p["timestamp"] = int(time.time() * 1000)
        p["recvWindow"] = 5000
        qs = urllib.parse.urlencode(p)
        sig = hmac.new(secret.encode(), qs.encode(), hashlib.sha256).hexdigest()
        req = urllib.request.Request(BASE + path + "?" + qs + "&signature=" + sig, headers={"X-MBX-APIKEY": key})
        return json.load(urllib.request.urlopen(req, timeout=20))

    return signed


def ema(xs, n):
    if len(xs) < n:
        return xs[-1]
    e = sum(xs[:n]) / n
    k = 2 / (n + 1)
    for x in xs[n:]:
        e = x * k + e * (1 - k)
    return e


def wilder_rsi(closes, n=RSI_LEN):
    if len(closes) < n + 1:
        return 50.0
    gains = losses = 0.0
    for i in range(1, n + 1):
        d = closes[i] - closes[i - 1]
        gains += max(d, 0)
        losses += max(-d, 0)
    avg_g, avg_l = gains / n, losses / n
    for i in range(n + 1, len(closes)):
        d = closes[i] - closes[i - 1]
        avg_g = (avg_g * (n - 1) + max(d, 0)) / n
        avg_l = (avg_l * (n - 1) + max(-d, 0)) / n
    if avg_l == 0:
        return 100.0
    return 100 - 100 / (1 + avg_g / avg_l)


def wilder_atr(highs, lows, closes, n=14):
    trs = [max(highs[i] - lows[i], abs(highs[i] - closes[i - 1]), abs(lows[i] - closes[i - 1])) for i in range(1, len(closes))]
    if len(trs) < n:
        return sum(trs) / max(len(trs), 1)
    atr = sum(trs[:n]) / n
    for tr in trs[n:]:
        atr = (atr * (n - 1) + tr) / n
    return atr


def print_account(signed):
    acc = signed("/fapi/v2/account")
    print("ACCOUNT wallet=%.1f margin=%.1f avail=%.1f uPnL=%.1f maint=%.1f" % tuple(
        float(acc[k]) for k in ("totalWalletBalance", "totalMarginBalance", "availableBalance", "totalUnrealizedProfit", "totalMaintMargin")))
    positions = [p for p in signed("/fapi/v2/positionRisk") if float(p["positionAmt"]) != 0]
    for p in positions:
        print("POS %-10s amt=%s entry=%.6g mark=%.6g uPnL=%.1f liq=%s" % (
            p["symbol"], p["positionAmt"], float(p["entryPrice"]), float(p["markPrice"]), float(p["unRealizedProfit"]), p["liquidationPrice"]))
    orders = signed("/fapi/v1/openOrders")
    for (sym, side, typ), n in sorted(Counter((o["symbol"], o["side"], o["type"]) for o in orders).items()):
        prices = sorted(float(o["price"]) or float(o["stopPrice"]) for o in orders if (o["symbol"], o["side"], o["type"]) == (sym, side, typ))
        print("ORD %s %s %s x%d %s-%s" % (sym, side, typ, n, prices[0], prices[-1]))
    for o in signed("/fapi/v1/openAlgoOrders"):
        print("ALGO %s %s %s trigger=%s qty=%s" % (o["symbol"], o["side"], o["orderType"], o["triggerPrice"], o["quantity"]))
    return [p["symbol"] for p in positions]


def print_market(symbol):
    lines = [symbol]
    for tf in TIMEFRAMES:
        k = public("/fapi/v1/klines", symbol=symbol, interval=tf, limit=WARMUP_BARS)
        c = [float(x[4]) for x in k]
        h = [float(x[2]) for x in k]
        l = [float(x[3]) for x in k]
        lines.append("  %-3s c=%.6g ema20=%.6g ema50=%.6g rsi14=%.1f atr14=%.4g hi20=%.6g lo20=%.6g chg24=%+.1f%%" % (
            tf, c[-1], ema(c, 20), ema(c, 50), wilder_rsi(c), wilder_atr(h, l, c), max(h[-20:]), min(l[-20:]), (c[-1] / c[-25] - 1) * 100))
    funding = float(public("/fapi/v1/premiumIndex", symbol=symbol)["lastFundingRate"]) * 100
    oi = public("/futures/data/openInterestHist", symbol=symbol, period="1h", limit=25)
    oi_chg = (float(oi[-1]["sumOpenInterest"]) / float(oi[0]["sumOpenInterest"]) - 1) * 100
    lines.append("  funding=%.4f%% OI24h=%+.1f%%" % (funding, oi_chg))
    print("\n".join(lines))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--env", default="/home/admin/bbgo/.env.local")
    ap.add_argument("symbols", nargs="*")
    args = ap.parse_args()

    env = load_env(args.env)
    held = print_account(make_signed(env["BINANCE_API_KEY"], env["BINANCE_API_SECRET"]))
    symbols = args.symbols or sorted(set(held + ["BTCUSDT", "ETHUSDT"]))
    for sym in symbols:
        print_market(sym.upper())


if __name__ == "__main__":
    main()
