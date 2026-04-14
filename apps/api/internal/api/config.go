package api

import "os"

type Config struct {
	Addr        string
	DatabaseURL string

	AuthBaseURL string
	AdminEmail  string

	PolarAccessToken   string
	PolarSuccessURL    string
	PolarCancelURL     string
	PolarWebhookSecret string
	PolarCartProductID string
}

func LoadConfigFromEnv() Config {
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8788"
	}
	return Config{
		Addr:              addr,
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		AuthBaseURL:       os.Getenv("AUTH_BASE_URL"),
		AdminEmail:        os.Getenv("ADMIN_EMAIL"),
		PolarAccessToken:  os.Getenv("POLAR_ACCESS_TOKEN"),
		PolarSuccessURL:   os.Getenv("POLAR_SUCCESS_URL"),
		PolarCancelURL:    os.Getenv("POLAR_CANCEL_URL"),
		PolarWebhookSecret: os.Getenv("POLAR_WEBHOOK_SECRET"),
		PolarCartProductID: os.Getenv("POLAR_CART_PRODUCT_ID"),
	}
}

