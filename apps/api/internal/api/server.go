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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stripe/stripe-go/v78"
	checkoutsession "github.com/stripe/stripe-go/v78/checkout/session"
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
	s.r.Use(s.attachUserFromAuthService)

	s.r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	s.r.Post("/webhooks/stripe", s.handleStripeWebhook)

	s.r.Route("/api", func(r chi.Router) {
		r.Get("/products", s.handleProductsList)
		r.Get("/products/{handle}", s.handleProductDetail)
		r.Post("/checkout/session", s.handleCreateCheckoutSession)
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
	if strings.TrimSpace(s.cfg.StripeSecretKey) == "" || strings.TrimSpace(s.cfg.StripeSuccessURL) == "" || strings.TrimSpace(s.cfg.StripeCancelURL) == "" {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "stripe_not_configured"})
		return
	}

	type cartItem struct {
		Handle   string `json:"handle"`
		Quantity int    `json:"quantity"`
	}
	type reqBody struct {
		Items    []cartItem `json:"items"`
		Shipping string     `json:"shipping"`
	}
	var body reqBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if len(body.Items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty_cart"})
		return
	}

	shippingCents := shippingCost(body.Shipping)
	if shippingCents < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_shipping"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Price authority: load all products by handle from DB.
	subtotal := 0
	type pricedItem struct {
		Handle   string `json:"handle"`
		Name     string `json:"name"`
		Price    int    `json:"unit_price_cents"`
		Quantity int    `json:"quantity"`
	}
	var priced []pricedItem
	for _, it := range body.Items {
		if it.Quantity <= 0 || it.Quantity > 20 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_quantity"})
			return
		}
		p, err := s.queryProductByHandle(ctx, it.Handle)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_product"})
			return
		}
		subtotal += p.PriceCents * it.Quantity
		priced = append(priced, pricedItem{
			Handle:   p.Handle,
			Name:     p.Name,
			Price:    p.PriceCents,
			Quantity: it.Quantity,
		})
	}

	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	email := strings.TrimSpace(r.Header.Get("X-User-Email"))

	var checkoutSessionID string
	if err := s.db.QueryRow(ctx,
		`INSERT INTO checkout_sessions (user_id, email, cart, shipping_method, shipping_cents, subtotal_cents, currency)
		 VALUES ($1, $2, $3, $4, $5, $6, 'GBP')
		 RETURNING id::text`,
		nullIfEmpty(userID), nullIfEmpty(email), mustJSON(priced), body.Shipping, shippingCents, subtotal,
	).Scan(&checkoutSessionID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}

	stripe.Key = strings.TrimSpace(s.cfg.StripeSecretKey)

	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(addQuery(s.cfg.StripeSuccessURL, "checkout_session_id", checkoutSessionID)),
		CancelURL:  stripe.String(s.cfg.StripeCancelURL),
		AutomaticTax: &stripe.CheckoutSessionAutomaticTaxParams{
			Enabled: stripe.Bool(true),
		},
		ShippingAddressCollection: &stripe.CheckoutSessionShippingAddressCollectionParams{
			AllowedCountries: stripe.StringSlice([]string{"GB"}),
		},
		BillingAddressCollection: stripe.String(string(stripe.CheckoutSessionBillingAddressCollectionRequired)),
		Metadata: map[string]string{
			"checkout_session_id": checkoutSessionID,
			"shipping_method":     body.Shipping,
			"user_id":             strings.TrimSpace(userID),
		},
	}
	if email != "" {
		params.CustomerEmail = stripe.String(email)
	}

	// Convert DB-priced items into inline PriceData so we don't need Stripe Products/Prices.
	for _, it := range priced {
		params.LineItems = append(params.LineItems, &stripe.CheckoutSessionLineItemParams{
			Quantity: stripe.Int64(int64(it.Quantity)),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String("gbp"),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String(it.Name),
					Metadata: map[string]string{
						"handle": it.Handle,
					},
				},
				UnitAmount:  stripe.Int64(int64(it.Price)),
				TaxBehavior: stripe.String(string(stripe.PriceTaxBehaviorInclusive)),
			},
		})
	}

	// Shipping: only include the user's selected option.
	shipName := "Standard shipping"
	minDays, maxDays := int64(3), int64(5)
	switch body.Shipping {
	case "express":
		shipName = "Express shipping"
		minDays, maxDays = 2, 2
	case "next-day":
		shipName = "Next day shipping"
		minDays, maxDays = 1, 1
	}
	params.ShippingOptions = []*stripe.CheckoutSessionShippingOptionParams{
		{
			ShippingRateData: &stripe.CheckoutSessionShippingOptionShippingRateDataParams{
				DisplayName: stripe.String(shipName),
				Type:        stripe.String(string(stripe.ShippingRateTypeFixedAmount)),
				FixedAmount: &stripe.CheckoutSessionShippingOptionShippingRateDataFixedAmountParams{
					Amount:   stripe.Int64(int64(shippingCents)),
					Currency: stripe.String("gbp"),
				},
				TaxBehavior: stripe.String(string(stripe.ShippingRateTaxBehaviorInclusive)),
				DeliveryEstimate: &stripe.CheckoutSessionShippingOptionShippingRateDataDeliveryEstimateParams{
					Minimum: &stripe.CheckoutSessionShippingOptionShippingRateDataDeliveryEstimateMinimumParams{Unit: stripe.String("business_day"), Value: stripe.Int64(minDays)},
					Maximum: &stripe.CheckoutSessionShippingOptionShippingRateDataDeliveryEstimateMaximumParams{Unit: stripe.String("business_day"), Value: stripe.Int64(maxDays)},
				},
			},
		},
	}

	sess, err := checkoutsession.New(params)
	if err != nil || sess == nil || sess.URL == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "stripe_error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"checkout_url": sess.URL})
}

func (s *Server) handleAccountOrders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	email := strings.TrimSpace(r.Header.Get("X-User-Email"))
	if userID == "" && email == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	type orderRow struct {
		ID         int64     `json:"id"`
		Email      string    `json:"email"`
		Status     string    `json:"status"`
		TotalCents int64     `json:"total_cents"`
		CreatedAt  time.Time `json:"created_at"`
	}

	var rows pgx.Rows
	var err error
	if userID != "" {
		rows, err = s.db.Query(ctx, `SELECT id, email, status, total_cents, created_at FROM orders WHERE user_id=$1 ORDER BY created_at DESC LIMIT 50`, userID)
	} else {
		rows, err = s.db.Query(ctx, `SELECT id, email, status, total_cents, created_at FROM orders WHERE email=$1 ORDER BY created_at DESC LIMIT 50`, email)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	defer rows.Close()

	var out []orderRow
	for rows.Next() {
		var o orderRow
		if err := rows.Scan(&o.ID, &o.Email, &o.Status, &o.TotalCents, &o.CreatedAt); err != nil {
			continue
		}
		out = append(out, o)
	}

	writeJSON(w, http.StatusOK, map[string]any{"orders": out})
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

func shippingCost(method string) int {
	switch method {
	case "standard":
		return 0
	case "express":
		return 299
	case "next-day":
		return 599
	default:
		return -1
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func addQuery(rawURL, key, value string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}
