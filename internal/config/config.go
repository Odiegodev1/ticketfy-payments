package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL        string
	InternalToken      string
	Addr               string
	PaymentGateway     string
	CajuPayBaseURL     string
	CajuPayAPIKey      string
	CajuPayAPISecret   string
	PartnerCheckoutURL string
}

func Load() (Config, error) {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8081"
	}

	c := Config{
		DatabaseURL:        strings.TrimSpace(os.Getenv("DATABASE_URL")),
		InternalToken:      strings.TrimSpace(os.Getenv("INTERNAL_API_TOKEN")),
		Addr:               net.JoinHostPort("127.0.0.1", port),
		PaymentGateway:     strings.ToLower(strings.TrimSpace(os.Getenv("PAYMENT_GATEWAY"))),
		CajuPayBaseURL:     strings.TrimSpace(os.Getenv("CAJUPAY_BASE_URL")),
		CajuPayAPIKey:      strings.TrimSpace(os.Getenv("CAJUPAY_API_KEY")),
		CajuPayAPISecret:   strings.TrimSpace(os.Getenv("CAJUPAY_API_SECRET")),
		PartnerCheckoutURL: strings.TrimSpace(os.Getenv("PARTNER_CHECKOUT_URL")),
	}

	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL é obrigatória")
	}

	if len(c.InternalToken) < 32 {
		return c, errors.New("INTERNAL_API_TOKEN deve ter no mínimo 32 caracteres")
	}

	if c.PaymentGateway == "" {
		c.PaymentGateway = "mock"
	}

	switch c.PaymentGateway {
	case "mock":
		// Nenhuma credencial CajuPay é necessária.

	case "cajupay":
		if c.CajuPayBaseURL == "" {
			return c, errors.New("CAJUPAY_BASE_URL é obrigatória")
		}

		if c.CajuPayAPIKey == "" || c.CajuPayAPISecret == "" {
			return c, errors.New("credenciais CajuPay são obrigatórias")
		}

		baseURL, err := url.Parse(c.CajuPayBaseURL)
		if err != nil ||
			baseURL.Scheme != "https" ||
			baseURL.Hostname() != "api.cajupay.com.br" ||
			baseURL.User != nil {
			return c, errors.New("CAJUPAY_BASE_URL inválida")
		}

		checkoutURL, err := url.Parse(c.PartnerCheckoutURL)
		if err != nil ||
			checkoutURL.Scheme != "https" ||
			checkoutURL.Host == "" ||
			checkoutURL.User != nil {
			return c, errors.New("PARTNER_CHECKOUT_URL deve ser uma URL HTTPS válida")
		}

		// Por enquanto, somente credenciais de teste.
		if !strings.HasPrefix(c.CajuPayAPIKey, "gpk_test_") ||
			!strings.HasPrefix(c.CajuPayAPISecret, "gsk_test_") {
			return c, errors.New(
				"integração CajuPay habilitada somente para credenciais de teste",
			)
		}

	default:
		return c, fmt.Errorf(
			"PAYMENT_GATEWAY inválido: %s (use mock ou cajupay)",
			c.PaymentGateway,
		)
	}

	return c, nil
}
