package api

import "os"

type Config struct {
	Addr        string
	DatabaseURL string

	AuthBaseURL string
	AdminEmail  string

	StripeSecretKey     string
	StripeWebhookSecret string
	StripeSuccessURL    string
	StripeCancelURL     string
}

func LoadConfigFromEnv() Config {
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8788"
	}
	return Config{
		Addr:                addr,
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		AuthBaseURL:         os.Getenv("AUTH_BASE_URL"),
		AdminEmail:          os.Getenv("ADMIN_EMAIL"),
		StripeSecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		StripeSuccessURL:    os.Getenv("STRIPE_SUCCESS_URL"),
		StripeCancelURL:     os.Getenv("STRIPE_CANCEL_URL"),
	}
}
