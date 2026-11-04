package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/c9s/bbgo/pkg/bbgo"
	"github.com/c9s/bbgo/pkg/bracket"
	"github.com/c9s/bbgo/pkg/cmd/cmdutil"
	"github.com/c9s/bbgo/pkg/types"
)

var bracketCmd = &cobra.Command{
	Use:   "bracket",
	Short: "Futures entry + deferred reduce-only TP/SL bracket daemon",
}

var bracketDaemonCmd = &cobra.Command{
	Use:          "daemon",
	Short:        "Run the bracket daemon (UserDataStream + local HTTP API)",
	SilenceUsage: true,
	PreRunE: cobraInitRequired([]string{
		"session",
	}),
	RunE: runBracketDaemon,
}

var bracketAddCmd = &cobra.Command{
	Use:          "add",
	Short:        "Submit an entry and register TP/SL with the running daemon",
	SilenceUsage: true,
	PreRunE: cobraInitRequired([]string{
		"symbol",
		"side",
		"quantity",
		"tp",
	}),
	RunE: runBracketAdd,
}

var bracketListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List bracket jobs from the running daemon",
	SilenceUsage: true,
	RunE:         runBracketList,
}

var bracketCancelCmd = &cobra.Command{
	Use:          "cancel",
	Short:        "Cancel a bracket job (open entry or exit orders)",
	SilenceUsage: true,
	PreRunE: cobraInitRequired([]string{
		"id",
	}),
	RunE: runBracketCancel,
}

func init() {
	defaultState := filepath.Join(os.Getenv("HOME"), ".bbgo", "brackets.json")

	bracketDaemonCmd.Flags().String("session", "", "exchange session name")
	bracketDaemonCmd.Flags().String("listen", bracket.DefaultListenAddr, "HTTP listen address")
	bracketDaemonCmd.Flags().String("state", defaultState, "path to persist bracket jobs")

	bracketAddCmd.Flags().String("listen", bracket.DefaultListenAddr, "daemon HTTP address")
	bracketAddCmd.Flags().String("symbol", "", "trading symbol, e.g. ZECUSDT")
	bracketAddCmd.Flags().String("side", "", "BUY or SELL")
	bracketAddCmd.Flags().String("quantity", "", "entry quantity")
	bracketAddCmd.Flags().String("price", "", "limit entry price (required unless --market)")
	bracketAddCmd.Flags().Bool("market", false, "submit entry as market order")
	bracketAddCmd.Flags().String("tp", "", "take-profit limit price")
	bracketAddCmd.Flags().String("sl", "0", "stop-loss trigger price; <=0 disables SL")

	bracketListCmd.Flags().String("listen", bracket.DefaultListenAddr, "daemon HTTP address")

	bracketCancelCmd.Flags().String("listen", bracket.DefaultListenAddr, "daemon HTTP address")
	bracketCancelCmd.Flags().String("id", "", "bracket job id")

	bracketCmd.AddCommand(bracketDaemonCmd)
	bracketCmd.AddCommand(bracketAddCmd)
	bracketCmd.AddCommand(bracketListCmd)
	bracketCmd.AddCommand(bracketCancelCmd)
	RootCmd.AddCommand(bracketCmd)
}

func runBracketDaemon(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sessionName, err := cmd.Flags().GetString("session")
	if err != nil {
		return err
	}
	listen, err := cmd.Flags().GetString("listen")
	if err != nil {
		return err
	}
	statePath, err := cmd.Flags().GetString("state")
	if err != nil {
		return err
	}

	environ := bbgo.NewEnvironment()
	if err := environ.ConfigureExchangeSessions(userConfig); err != nil {
		return err
	}
	if err := environ.Init(ctx); err != nil {
		return err
	}

	session, ok := environ.Session(sessionName)
	if !ok {
		return fmt.Errorf("session %s not found", sessionName)
	}

	mgr := bracket.NewManager(session.Exchange, session.Market, statePath)
	if err := mgr.Load(); err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	mgr.Reconcile(ctx)

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mgr.Reconcile(ctx)
			}
		}
	}()

	srv := &bracket.Server{Manager: mgr, Addr: listen}
	errCh := make(chan error, 1)
	go func() {
		log.Infof("bracket daemon starting HTTP on http://%s", listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	stream := session.Exchange.NewStream()
	stream.OnOrderUpdate(func(order types.Order) {
		mgr.OnOrderUpdate(ctx, order)
	})

	log.Infof("bracket daemon connecting user data stream (session=%s)...", sessionName)
	if err := stream.Connect(ctx); err != nil {
		_ = srv.Shutdown(context.Background())
		return fmt.Errorf("connect user data stream: %w", err)
	}
	log.Infof("bracket daemon user data stream connected; state=%s", statePath)

	select {
	case err := <-errCh:
		_ = stream.Close()
		return err
	default:
	}

	cmdutil.WaitForSignal(ctx, syscall.SIGINT, syscall.SIGTERM)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	if err := stream.Close(); err != nil {
		log.WithError(err).Warn("close user data stream")
	}
	if err := mgr.Persist(); err != nil {
		log.WithError(err).Warn("final persist")
	}
	return nil
}

func runBracketAdd(cmd *cobra.Command, args []string) error {
	listen, _ := cmd.Flags().GetString("listen")
	symbol, _ := cmd.Flags().GetString("symbol")
	side, _ := cmd.Flags().GetString("side")
	quantity, _ := cmd.Flags().GetString("quantity")
	price, _ := cmd.Flags().GetString("price")
	asMarket, _ := cmd.Flags().GetBool("market")
	tp, _ := cmd.Flags().GetString("tp")
	sl, _ := cmd.Flags().GetString("sl")

	if !asMarket && price == "" {
		return fmt.Errorf("--price is required unless --market")
	}

	body := map[string]interface{}{
		"symbol":   strings.ToUpper(symbol),
		"side":     strings.ToUpper(side),
		"quantity": quantity,
		"tp":       tp,
		"sl":       sl,
		"market":   asMarket,
	}
	if !asMarket {
		body["price"] = price
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s/v1/brackets", listen)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("daemon request failed (is bracket daemon running on %s?): %w", listen, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("daemon error %s: %s", resp.Status, string(respBody))
	}
	fmt.Println(string(respBody))
	return nil
}

func runBracketList(cmd *cobra.Command, args []string) error {
	listen, _ := cmd.Flags().GetString("listen")
	url := fmt.Sprintf("http://%s/v1/brackets", listen)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("daemon request failed (is bracket daemon running on %s?): %w", listen, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("daemon error %s: %s", resp.Status, string(respBody))
	}
	fmt.Println(string(respBody))
	return nil
}

func runBracketCancel(cmd *cobra.Command, args []string) error {
	listen, _ := cmd.Flags().GetString("listen")
	id, _ := cmd.Flags().GetString("id")
	url := fmt.Sprintf("http://%s/v1/brackets/%s", listen, id)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("daemon request failed (is bracket daemon running on %s?): %w", listen, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("daemon error %s: %s", resp.Status, string(respBody))
	}
	fmt.Println(string(respBody))
	return nil
}
