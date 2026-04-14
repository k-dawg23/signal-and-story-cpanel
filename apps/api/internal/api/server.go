package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	cfg Config
	db  *pgxpool.Pool
	r   chi.Router
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	db, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg: cfg,
		db:  db,
		r:   chi.NewRouter(),
	}
	s.routes()
	return s, nil
}

func (s *Server) Router() http.Handler { return s.r }

func (s *Server) routes() {
	s.r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	s.r.Route("/api", func(r chi.Router) {
		r.Get("/products", s.handleProductsList)
		r.Get("/products/{handle}", s.handleProductDetail)
		r.Post("/checkout/session", s.handleCreateCheckoutSession) // Polar wiring in next todo
		r.Get("/account/orders", s.handleAccountOrders)

		r.Route("/admin", func(ar chi.Router) {
			ar.Use(s.requireAdmin)
			ar.Get("/overview", s.handleAdminOverview)
			ar.Get("/products", s.handleAdminProducts)
			ar.Get("/orders", s.handleAdminOrders)
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AdminEmail == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin_disabled"})
			return
		}
		email := strings.TrimSpace(r.Header.Get("X-User-Email"))
		if !strings.EqualFold(email, s.cfg.AdminEmail) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type productRow struct {
	ID            int64  `json:"id"`
	Handle        string `json:"handle"`
	Name          string `json:"name"`
	DescriptionMD string `json:"description_md"`
	PriceCents    int    `json:"price_cents"`
	Currency      string `json:"currency"`
	ImageURL      string `json:"image_url"`
}

func (s *Server) handleProductsList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	collection := strings.TrimSpace(r.URL.Query().Get("collection"))

	var rows []productRow
	var err error

	if collection != "" {
		rows, err = s.queryProductsByCollection(ctx, collection, q)
	} else {
		rows, err = s.queryProducts(ctx, q)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"products": rows})
}

func (s *Server) handleProductDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	handle := chi.URLParam(r, "handle")
	p, err := s.queryProductByHandle(ctx, handle)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"product": p})
}

func (s *Server) handleCreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	// Implemented in Polar todo: validate cart, price from DB, create Polar Checkout Session.
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not_implemented"})
}

func (s *Server) handleAccountOrders(w http.ResponseWriter, r *http.Request) {
	// Implemented after auth bridge: use X-User-Id / X-User-Email (verified by auth service)
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not_implemented"})
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var revenueCents int64
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(SUM(total_cents), 0) FROM orders WHERE status = 'paid'`).Scan(&revenueCents)
	var ordersCount int64
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM orders`).Scan(&ordersCount)

	writeJSON(w, http.StatusOK, map[string]any{
		"revenue_cents": revenueCents,
		"orders_count":  ordersCount,
	})
}

func (s *Server) handleAdminProducts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rows, err := s.queryProducts(ctx, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"products": rows})
}

func (s *Server) handleAdminOrders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	type orderRow struct {
		ID         int64     `json:"id"`
		Email      string    `json:"email"`
		Status     string    `json:"status"`
		TotalCents int64     `json:"total_cents"`
		CreatedAt  time.Time `json:"created_at"`
	}

	dbRows, err := s.db.Query(ctx, `SELECT id, email, status, total_cents, created_at FROM orders ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	defer dbRows.Close()

	var out []orderRow
	for dbRows.Next() {
		var o orderRow
		if err := dbRows.Scan(&o.ID, &o.Email, &o.Status, &o.TotalCents, &o.CreatedAt); err != nil {
			continue
		}
		out = append(out, o)
	}

	writeJSON(w, http.StatusOK, map[string]any{"orders": out})
}

func (s *Server) queryProducts(ctx context.Context, q string) ([]productRow, error) {
	var args []any
	sql := `SELECT id, handle, name, description_md, price_cents, currency, COALESCE(image_url,'') FROM products WHERE active = true`
	if q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		sql += ` AND (name ILIKE $1 OR description_md ILIKE $1)`
	}
	sql += ` ORDER BY featured_rank NULLS LAST, created_at DESC`

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []productRow
	for rows.Next() {
		var p productRow
		if err := rows.Scan(&p.ID, &p.Handle, &p.Name, &p.DescriptionMD, &p.PriceCents, &p.Currency, &p.ImageURL); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Server) queryProductsByCollection(ctx context.Context, collectionSlug, q string) ([]productRow, error) {
	var args []any
	args = append(args, collectionSlug)
	sql := `
SELECT p.id, p.handle, p.name, p.description_md, p.price_cents, p.currency, COALESCE(p.image_url,'')
FROM products p
JOIN product_collections pc ON pc.product_id = p.id
JOIN collections c ON c.id = pc.collection_id
WHERE p.active = true AND c.slug = $1
`
	if q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		sql += ` AND (p.name ILIKE $2 OR p.description_md ILIKE $2)`
	}
	sql += ` ORDER BY p.featured_rank NULLS LAST, p.created_at DESC`

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []productRow
	for rows.Next() {
		var p productRow
		if err := rows.Scan(&p.ID, &p.Handle, &p.Name, &p.DescriptionMD, &p.PriceCents, &p.Currency, &p.ImageURL); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Server) queryProductByHandle(ctx context.Context, handle string) (productRow, error) {
	var p productRow
	err := s.db.QueryRow(ctx,
		`SELECT id, handle, name, description_md, price_cents, currency, COALESCE(image_url,'') FROM products WHERE handle=$1 AND active=true`,
		handle,
	).Scan(&p.ID, &p.Handle, &p.Name, &p.DescriptionMD, &p.PriceCents, &p.Currency, &p.ImageURL)
	return p, err
}

func escapeLike(s string) string {
	// Basic escaping for LIKE wildcard characters.
	replacer := strings.NewReplacer(`%`, `\\%`, `_`, `\\_`)
	return replacer.Replace(s)
}

func mustParseURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

