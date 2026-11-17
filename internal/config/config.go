package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
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
	// Porta definida pelo Render ou padrão local.
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8081"
	}

	// Validação da porta.
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return Config{}, errors.New("PORT deve ser um número entre 1 e 65535")
	}

	c := Config{
		DatabaseURL:   strings.TrimSpace(os.Getenv("DATABASE_URL")),
		InternalToken: strings.TrimSpace(os.Getenv("INTERNAL_API_TOKEN")),

		// Permite conexões externas no Render.
		Addr: net.JoinHostPort("0.0.0.0", port),

		PaymentGateway: strings.ToLower(
			strings.TrimSpace(os.Getenv("PAYMENT_GATEWAY")),
		),

		CajuPayBaseURL: strings.TrimSpace(
			os.Getenv("CAJUPAY_BASE_URL"),
		),

		CajuPayAPIKey: strings.TrimSpace(
			os.Getenv("CAJUPAY_API_KEY"),
		),

		CajuPayAPISecret: strings.TrimSpace(
			os.Getenv("CAJUPAY_API_SECRET"),
		),

		PartnerCheckoutURL: strings.TrimSpace(
			os.Getenv("PARTNER_CHECKOUT_URL"),
		),
	}

	// ==========================================
	// BANCO DE DADOS
	// ==========================================

	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL é obrigatória")
	}

	// ==========================================
	// SEGURANÇA DA API
	// ==========================================

	if len(c.InternalToken) < 32 {
		return c, errors.New(
			"INTERNAL_API_TOKEN deve ter no mínimo 32 caracteres",
		)
	}

	// ==========================================
	// GATEWAY DE PAGAMENTO
	// ==========================================

	if c.PaymentGateway == "" {
		c.PaymentGateway = "mock"
	}

	switch c.PaymentGateway {

	case "mock":
		// Nenhuma credencial CajuPay é necessária.

	case "cajupay":

		// ==========================================
		// CONFIGURAÇÃO CAJUPAY
		// ==========================================

		if c.CajuPayBaseURL == "" {
			return c, errors.New(
				"CAJUPAY_BASE_URL é obrigatória",
			)
		}

		if c.CajuPayAPIKey == "" || c.CajuPayAPISecret == "" {
			return c, errors.New(
				"credenciais CajuPay são obrigatórias",
			)
		}

		// ==========================================
		// VALIDAÇÃO DA URL CAJUPAY
		// ==========================================

		baseURL, err := url.Parse(c.CajuPayBaseURL)

		if err != nil ||
			baseURL.Scheme != "https" ||
			baseURL.Hostname() != "api.cajupay.com.br" ||
			baseURL.User != nil {

			return c, errors.New(
				"CAJUPAY_BASE_URL inválida",
			)
		}

		// ==========================================
		// VALIDAÇÃO DO CHECKOUT
		// ==========================================

		checkoutURL, err := url.Parse(c.PartnerCheckoutURL)

		if err != nil ||
			checkoutURL.Scheme != "https" ||
			checkoutURL.Host == "" ||
			checkoutURL.User != nil {

			return c, errors.New(
				"PARTNER_CHECKOUT_URL deve ser uma URL HTTPS válida",
			)
		}

		// ==========================================
		// SEGURANÇA - SOMENTE SANDBOX
		// ==========================================

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
