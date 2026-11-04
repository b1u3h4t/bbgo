package bracket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c9s/bbgo/pkg/fixedpoint"
	"github.com/c9s/bbgo/pkg/types"
)

// Server exposes a localhost HTTP API for the bracket manager.
type Server struct {
	Manager *Manager
	Addr    string
	httpSrv *http.Server
}

type addJSON struct {
	Symbol   string `json:"symbol"`
	Side     string `json:"side"`
	Quantity string `json:"quantity"`
	Price    string `json:"price,omitempty"`
	Market   bool   `json:"market,omitempty"`
	Tp       string `json:"tp"`
	Sl       string `json:"sl,omitempty"`
}

// Handler returns the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/brackets", s.handleBrackets)
	mux.HandleFunc("/v1/brackets/", s.handleBracketByID)
	return mux
}

// ListenAndServe starts the HTTP server (blocking until Shutdown).
func (s *Server) ListenAndServe() error {
	s.httpSrv = &http.Server{
		Addr:              s.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) handleBrackets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Manager.List())
	case http.MethodPost:
		s.handleAdd(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var body addJSON
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	qty, err := fixedpoint.NewFromString(body.Quantity)
	if err != nil {
		http.Error(w, "invalid quantity", http.StatusBadRequest)
		return
	}
	tp, err := fixedpoint.NewFromString(body.Tp)
	if err != nil {
		http.Error(w, "invalid tp", http.StatusBadRequest)
		return
	}
	sl := fixedpoint.Zero
	if body.Sl != "" {
		sl, err = fixedpoint.NewFromString(body.Sl)
		if err != nil {
			http.Error(w, "invalid sl", http.StatusBadRequest)
			return
		}
	}
	price := fixedpoint.Zero
	if !body.Market {
		if body.Price == "" {
			http.Error(w, "price required for limit entry", http.StatusBadRequest)
			return
		}
		price, err = fixedpoint.NewFromString(body.Price)
		if err != nil {
			http.Error(w, "invalid price", http.StatusBadRequest)
			return
		}
	}

	job, err := s.Manager.Add(r.Context(), AddRequest{
		Symbol:   strings.ToUpper(body.Symbol),
		Side:     types.SideType(strings.ToUpper(body.Side)),
		Quantity: qty,
		Price:    price,
		Market:   body.Market,
		TpPrice:  tp,
		SlPrice:  sl,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) handleBracketByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/brackets/")
	id = strings.Trim(id, "/")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		job, ok := s.Manager.Get(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, job)
	case http.MethodDelete:
		job, err := s.Manager.Cancel(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, job)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		// header already written
		fmt.Fprintf(w, `{"error":%q}`, err.Error())
	}
}

// DefaultListenAddr is the default localhost bind for the daemon.
const DefaultListenAddr = "127.0.0.1:7947"
