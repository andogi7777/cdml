package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"cdml/node"
)

// Server: REST API 서버.
type Server struct {
	addr string
	srv  *http.Server
}

// NewServer: API 서버 생성 및 라우팅.
func NewServer(addr string, n *node.Node) *Server {
	h := NewHandler(n)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz",                    h.Healthz)
	mux.HandleFunc("GET /v1/status",                  h.Status)
	mux.HandleFunc("GET /v1/balance/{pubkey}",        h.Balance)
	mux.HandleFunc("POST /v1/tx",                     h.SendTx)
	mux.HandleFunc("GET /v1/tx/{pubkey}/{seq}",       h.GetTx)
	mux.HandleFunc("GET /v1/peers",                   h.Peers)
	mux.HandleFunc("GET /v1/witness/active",          h.ActiveWitnesses)
	mux.HandleFunc("POST /v1/witness/register",       h.RegisterWitnesses)
	mux.HandleFunc("POST /v1/snapshot",               h.CreateSnapshot)
	mux.HandleFunc("POST /v1/faucet",                 h.Faucet)

	srv := &http.Server{
		Addr:         addr,
		Handler:      logging(cors(mux)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	return &Server{addr: addr, srv: srv}
}

// Start: API 서버 시작.
func (s *Server) Start() error {
	log.Printf("[cdml] API listening on %s", s.addr)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("api server: %w", err)
	}
	return nil
}

// Stop: API 서버 정지.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[api] %s %s %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
