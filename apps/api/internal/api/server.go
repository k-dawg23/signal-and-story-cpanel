package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
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

func defaultAllowedOrigins() []string {
	base := strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	if base == "" {
		base = "http://localhost:4321"
	}
	out := []string{
		base,
		"http://localhost:4321",
		"http://127.0.0.1:4321",
	}
	// Optional allowlist for non-default dev hosts (e.g. http://192.168.x.x:4321).
	raw := strings.TrimSpace(os.Getenv("APP_ORIGIN_ALLOWLIST"))
	if raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if v := strings.TrimSpace(part); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	allow := map[string]struct{}{}
	for _, o := range defaultAllowedOrigins() {
		allow[o] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			if _, ok := allow[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
	s.r.Use(s.corsMiddleware)
	s.r.Use(s.attachUserFromAuthService)

	s.r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	s.r.Get("/api/_meta/build", func(w http.ResponseWriter, _ *http.Request) {
		brevoKey := strings.TrimSpace(os.Getenv("BREVO_API_KEY"))
		smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST"))
		backend := "none"
		if brevoKey != "" {
			backend = "brevo"
		} else if smtpHost != "" {
			backend = "smtp"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"build_id":       strings.TrimSpace(os.Getenv("SAS_BUILD_ID")),
			"email_backend":  backend,
			"brevo_configured": brevoKey != "",
			"smtp_configured":  smtpHost != "",
			"brevo_sender_email": strings.TrimSpace(os.Getenv("BREVO_SENDER_EMAIL")),
			"brevo_sender_name":  strings.TrimSpace(os.Getenv("BREVO_SENDER_NAME")),
		})
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
			// Products
			ar.Get("/products", s.handleAdminProducts)
			ar.Post("/products", s.handleAdminProductCreate)
			ar.Put("/products/{id}", s.handleAdminProductUpdate)
			ar.Delete("/products/{id}", s.handleAdminProductDelete)

			// Orders
			ar.Get("/orders", s.handleAdminOrders)
			ar.Get("/orders/{id}", s.handleAdminOrderDetail)
			ar.Post("/orders/{id}/resend-confirmation", s.handleAdminOrderResendConfirmation)

			// Collections + assignments
			ar.Get("/collections", s.handleAdminCollections)
			ar.Post("/collections", s.handleAdminCollectionCreate)
			ar.Put("/collections/{id}", s.handleAdminCollectionUpdate)
			ar.Delete("/collections/{id}", s.handleAdminCollectionDelete)
			ar.Put("/collections/{id}/products", s.handleAdminCollectionSetProducts)
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
		Items      []struct {
			Name          string `json:"name"`
			UnitPriceCents int   `json:"unit_price_cents"`
			Quantity      int    `json:"quantity"`
		} `json:"items"`
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
	var orderIDs []int64
	for rows.Next() {
		var o orderRow
		if err := rows.Scan(&o.ID, &o.Email, &o.Status, &o.TotalCents, &o.CreatedAt); err != nil {
			continue
		}
		out = append(out, o)
		orderIDs = append(orderIDs, o.ID)
	}

	// Load items for these orders in one query.
	if len(orderIDs) > 0 {
		type itemRow struct {
			OrderID        int64
			Name           string
			UnitPriceCents int
			Quantity       int
		}
		itemRows, err := s.db.Query(ctx, `
			SELECT order_id, product_name_snapshot, unit_price_cents, quantity
			FROM order_items
			WHERE order_id = ANY($1)
			ORDER BY order_id ASC, id ASC
		`, orderIDs)
		if err == nil {
			defer itemRows.Close()
			itemsByOrder := map[int64][]struct {
				Name           string `json:"name"`
				UnitPriceCents int    `json:"unit_price_cents"`
				Quantity       int    `json:"quantity"`
			}{}
			for itemRows.Next() {
				var it itemRow
				if err := itemRows.Scan(&it.OrderID, &it.Name, &it.UnitPriceCents, &it.Quantity); err != nil {
					continue
				}
				itemsByOrder[it.OrderID] = append(itemsByOrder[it.OrderID], struct {
					Name           string `json:"name"`
					UnitPriceCents int    `json:"unit_price_cents"`
					Quantity       int    `json:"quantity"`
				}{
					Name:           it.Name,
					UnitPriceCents: it.UnitPriceCents,
					Quantity:       it.Quantity,
				})
			}
			for i := range out {
				out[i].Items = itemsByOrder[out[i].ID]
			}
		}
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
	// Admin: include inactive products too.
	rows, err := s.queryAllProducts(ctx, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"products": rows})
}

func (s *Server) queryAllProducts(ctx context.Context, q string) ([]productRow, error) {
	var args []any
	sql := `SELECT id, handle, name, description_md, price_cents, currency, COALESCE(image_url,'') FROM products WHERE 1=1`
	if q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		sql += ` AND (name ILIKE $1 OR description_md ILIKE $1)`
	}
	sql += ` ORDER BY active DESC, featured_rank NULLS LAST, created_at DESC`
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

func parseIDParam(r *http.Request, key string) (int64, error) {
	raw := strings.TrimSpace(chi.URLParam(r, key))
	return strconv.ParseInt(raw, 10, 64)
}

func (s *Server) handleAdminProductCreate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	type req struct {
		Handle        string `json:"handle"`
		Name          string `json:"name"`
		DescriptionMD string `json:"description_md"`
		PriceCents    int    `json:"price_cents"`
		Currency      string `json:"currency"`
		ImageURL      string `json:"image_url"`
		Active        *bool  `json:"active"`
		FeaturedRank  *int   `json:"featured_rank"`
	}
	var body req
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	handle := strings.TrimSpace(body.Handle)
	name := strings.TrimSpace(body.Name)
	if handle == "" || name == "" || body.PriceCents < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_input"})
		return
	}
	currency := strings.TrimSpace(body.Currency)
	if currency == "" {
		currency = "GBP"
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}

	var id int64
	err := s.db.QueryRow(ctx, `
		INSERT INTO products (handle, name, description_md, price_cents, currency, image_url, active, featured_rank)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id
	`,
		handle, name, strings.TrimSpace(body.DescriptionMD), body.PriceCents, currency, nullIfEmpty(strings.TrimSpace(body.ImageURL)), active, body.FeaturedRank,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleAdminProductUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	id, err := parseIDParam(r, "id")
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}

	type req struct {
		Handle        *string `json:"handle"`
		Name          *string `json:"name"`
		DescriptionMD *string `json:"description_md"`
		PriceCents    *int    `json:"price_cents"`
		Currency      *string `json:"currency"`
		ImageURL      *string `json:"image_url"`
		Active        *bool   `json:"active"`
		FeaturedRank  *int    `json:"featured_rank"`
	}
	var body req
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if body.PriceCents != nil && *body.PriceCents < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_input"})
		return
	}

	ct, err := s.db.Exec(ctx, `
		UPDATE products SET
			handle = COALESCE($2, handle),
			name = COALESCE($3, name),
			description_md = COALESCE($4, description_md),
			price_cents = COALESCE($5, price_cents),
			currency = COALESCE($6, currency),
			image_url = COALESCE($7, image_url),
			active = COALESCE($8, active),
			featured_rank = $9,
			updated_at = now()
		WHERE id=$1
	`, id,
		nilIfBlankPtr(body.Handle),
		nilIfBlankPtr(body.Name),
		body.DescriptionMD,
		body.PriceCents,
		nilIfBlankPtr(body.Currency),
		nilIfBlankPtr(body.ImageURL),
		body.Active,
		body.FeaturedRank,
	)
	if err != nil || ct.RowsAffected() == 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminProductDelete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	id, err := parseIDParam(r, "id")
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	_, err = s.db.Exec(ctx, `DELETE FROM products WHERE id=$1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

func (s *Server) handleAdminOrderDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	id, err := parseIDParam(r, "id")
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}

	type orderDetail struct {
		ID             int64          `json:"id"`
		Email          string         `json:"email"`
		Status         string         `json:"status"`
		Currency       string         `json:"currency"`
		StripeCheckoutSessionID string `json:"stripe_checkout_session_id"`
		SubtotalCents  int64          `json:"subtotal_cents"`
		ShippingCents  int64          `json:"shipping_cents"`
		TaxCents       int64          `json:"tax_cents"`
		TotalCents     int64          `json:"total_cents"`
		ShippingMethod string         `json:"shipping_method"`
		ShippingAddr   map[string]any `json:"shipping_address"`
		CreatedAt      time.Time      `json:"created_at"`
		Items          []struct {
			Name           string `json:"name"`
			UnitPriceCents int    `json:"unit_price_cents"`
			Quantity       int    `json:"quantity"`
		} `json:"items"`
		Debug map[string]any `json:"_debug,omitempty"`
	}

	var o orderDetail
	var addrBytes []byte
	if err := s.db.QueryRow(ctx, `
		SELECT id, email, status, currency, stripe_checkout_session_id, subtotal_cents, shipping_cents, tax_cents, total_cents,
		       COALESCE(shipping_method,''), COALESCE(shipping_address,'{}'::jsonb), created_at
		FROM orders
		WHERE id=$1
	`, id).Scan(
		&o.ID, &o.Email, &o.Status, &o.Currency, &o.StripeCheckoutSessionID, &o.SubtotalCents, &o.ShippingCents, &o.TaxCents, &o.TotalCents,
		&o.ShippingMethod, &addrBytes, &o.CreatedAt,
	); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	_ = json.Unmarshal(addrBytes, &o.ShippingAddr)

	// If address is missing, try to fetch from Stripe and backfill.
	if isEmptyShippingAddr(o.ShippingAddr) && strings.TrimSpace(o.StripeCheckoutSessionID) != "" && strings.TrimSpace(s.cfg.StripeSecretKey) != "" {
		addr, dbg, err := s.fetchShippingAddressFromStripe(ctx, strings.TrimSpace(o.StripeCheckoutSessionID))
		if strings.TrimSpace(os.Getenv("APP_ENV")) != "production" {
			if o.Debug == nil {
				o.Debug = map[string]any{}
			}
			o.Debug["stripe_fetch_error"] = ""
			if err != nil {
				o.Debug["stripe_fetch_error"] = err.Error()
			}
			o.Debug["stripe_checkout_session_id_present"] = true
			o.Debug["stripe_debug"] = dbg
		}
		if err == nil && !isEmptyShippingAddr(addr) {
			o.ShippingAddr = addr
			_, _ = s.db.Exec(ctx, `UPDATE orders SET shipping_address=$2 WHERE id=$1`, o.ID, mustJSON(addr))
		}
	} else if strings.TrimSpace(os.Getenv("APP_ENV")) != "production" {
		// Dev-only: show why we skipped Stripe fetch.
		o.Debug = map[string]any{
			"stripe_checkout_session_id_present": strings.TrimSpace(o.StripeCheckoutSessionID) != "",
			"stripe_secret_configured":          strings.TrimSpace(s.cfg.StripeSecretKey) != "",
			"shipping_address_empty":            isEmptyShippingAddr(o.ShippingAddr),
		}
	}

	rows, err := s.db.Query(ctx, `
		SELECT product_name_snapshot, unit_price_cents, quantity
		FROM order_items
		WHERE order_id=$1
		ORDER BY id ASC
	`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var unit int
			var qty int
			if err := rows.Scan(&name, &unit, &qty); err != nil {
				continue
			}
			o.Items = append(o.Items, struct {
				Name           string `json:"name"`
				UnitPriceCents int    `json:"unit_price_cents"`
				Quantity       int    `json:"quantity"`
			}{Name: name, UnitPriceCents: unit, Quantity: qty})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": o})
}

func (s *Server) handleAdminOrderResendConfirmation(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	id, err := parseIDParam(r, "id")
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}

	var (
		email                 string
		stripeCheckoutSession string
	)
	if err := s.db.QueryRow(ctx, `SELECT email, stripe_checkout_session_id FROM orders WHERE id=$1`, id).Scan(&email, &stripeCheckoutSession); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	email = strings.TrimSpace(email)
	if email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_email"})
		return
	}

	ref := strings.TrimSpace(stripeCheckoutSession)
	if ref == "" {
		ref = "order-" + strconv.FormatInt(id, 10)
	}

	if err := s.sendOrderConfirmationEmail(ctx, email, id, ref); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "email_send_failed"})
		return
	}

	_, _ = s.db.Exec(ctx, `UPDATE orders SET confirmation_email_sent_at = now() WHERE id=$1`, id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func isEmptyShippingAddr(addr map[string]any) bool {
	if addr == nil {
		return true
	}
	get := func(k string) string {
		v, ok := addr[k]
		if !ok || v == nil {
			return ""
		}
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
		return ""
	}
	// Any real address line / postal code is enough.
	if get("line1") != "" || get("postal_code") != "" || get("city") != "" {
		return false
	}
	return true
}

func (s *Server) fetchShippingAddressFromStripe(ctx context.Context, checkoutSessionID string) (map[string]any, map[string]any, error) {
	stripe.Key = strings.TrimSpace(s.cfg.StripeSecretKey)
	// Note: `shipping_details` / `customer_details` are not expandable properties.
	// Stripe may reject requests that try to expand them.
	cs, err := checkoutsession.Get(checkoutSessionID, nil)
	if err != nil || cs == nil {
		dbg := map[string]any{"got_session": false}
		// Preserve useful Stripe error info in dev.
		if se, ok := err.(*stripe.Error); ok && se != nil {
			dbg["stripe_status"] = se.HTTPStatusCode
			dbg["stripe_type"] = se.Type
			dbg["stripe_code"] = se.Code
			dbg["stripe_msg"] = se.Msg
			dbg["stripe_param"] = se.Param
			dbg["stripe_request_id"] = se.RequestID
		} else if err != nil {
			dbg["stripe_err"] = err.Error()
		}
		return nil, dbg, errors.New("stripe_session_get_failed")
	}

	addr := map[string]any{}
	dbg := map[string]any{
		"got_session":           true,
		"has_shipping_details":  cs.ShippingDetails != nil,
		"has_customer_details":  cs.CustomerDetails != nil,
		"shipping_name":         "",
		"customer_name":         "",
		"shipping_has_address":  false,
		"customer_has_address":  false,
	}
	if cs.ShippingDetails != nil {
		if name := strings.TrimSpace(cs.ShippingDetails.Name); name != "" {
			addr["name"] = name
			dbg["shipping_name"] = name
		}
		if cs.ShippingDetails.Address != nil {
			a := cs.ShippingDetails.Address
			dbg["shipping_has_address"] = true
			addr["line1"] = a.Line1
			addr["line2"] = a.Line2
			addr["city"] = a.City
			addr["state"] = a.State
			addr["postal_code"] = a.PostalCode
			addr["country"] = a.Country
		}
	}
	if isEmptyShippingAddr(addr) && cs.CustomerDetails != nil && cs.CustomerDetails.Address != nil {
		a := cs.CustomerDetails.Address
		if name := strings.TrimSpace(cs.CustomerDetails.Name); name != "" {
			addr["name"] = name
			dbg["customer_name"] = name
		}
		dbg["customer_has_address"] = true
		addr["line1"] = a.Line1
		addr["line2"] = a.Line2
		addr["city"] = a.City
		addr["state"] = a.State
		addr["postal_code"] = a.PostalCode
		addr["country"] = a.Country
	}
	if isEmptyShippingAddr(addr) {
		return map[string]any{}, dbg, nil
	}
	return addr, dbg, nil
}

type adminCollection struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (s *Server) handleAdminCollections(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctx, `SELECT id, type::text, slug, name FROM collections ORDER BY type, slug`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	defer rows.Close()

	var out []adminCollection
	for rows.Next() {
		var c adminCollection
		if err := rows.Scan(&c.ID, &c.Type, &c.Slug, &c.Name); err != nil {
			continue
		}
		out = append(out, c)
	}

	// Also return product assignments for convenience.
	pcRows, err := s.db.Query(ctx, `SELECT collection_id, product_id FROM product_collections`)
	assignments := map[int64][]int64{}
	if err == nil {
		defer pcRows.Close()
		for pcRows.Next() {
			var cid, pid int64
			if err := pcRows.Scan(&cid, &pid); err != nil {
				continue
			}
			assignments[cid] = append(assignments[cid], pid)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"collections": out, "product_ids_by_collection_id": assignments})
}

func (s *Server) handleAdminCollectionCreate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	type req struct {
		Type string `json:"type"`
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	var body req
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	t := strings.TrimSpace(body.Type)
	slug := strings.TrimSpace(body.Slug)
	name := strings.TrimSpace(body.Name)
	if t == "" || slug == "" || name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_input"})
		return
	}
	var id int64
	if err := s.db.QueryRow(ctx, `INSERT INTO collections (type, slug, name) VALUES ($1,$2,$3) RETURNING id`, t, slug, name).Scan(&id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleAdminCollectionUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	id, err := parseIDParam(r, "id")
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	type req struct {
		Type *string `json:"type"`
		Slug *string `json:"slug"`
		Name *string `json:"name"`
	}
	var body req
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	_, err = s.db.Exec(ctx, `
		UPDATE collections SET
			type = COALESCE($2, type),
			slug = COALESCE($3, slug),
			name = COALESCE($4, name)
		WHERE id=$1
	`, id, nilIfBlankPtr(body.Type), nilIfBlankPtr(body.Slug), nilIfBlankPtr(body.Name))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminCollectionDelete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	id, err := parseIDParam(r, "id")
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	_, err = s.db.Exec(ctx, `DELETE FROM collections WHERE id=$1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminCollectionSetProducts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	id, err := parseIDParam(r, "id")
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	type req struct {
		ProductIDs []int64 `json:"product_ids"`
	}
	var body req
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM product_collections WHERE collection_id=$1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	for _, pid := range body.ProductIDs {
		if pid <= 0 {
			continue
		}
		_, _ = tx.Exec(ctx, `INSERT INTO product_collections (product_id, collection_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, pid, id)
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

func nilIfBlankPtr(p *string) any {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(*p) == "" {
		return nil
	}
	v := strings.TrimSpace(*p)
	return v
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
