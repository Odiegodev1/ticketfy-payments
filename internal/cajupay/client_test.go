package cajupay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetPaymentPaid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("método inesperado: %s", r.Method)
			}

			if r.URL.Path != "/api/payments/test-payment-123" {
				t.Errorf("rota inesperada: %s", r.URL.Path)
			}

			w.Header().Set("Content-Type", "application/json")

			_, _ = w.Write([]byte(`{
				"payment_id": "test-payment-123",
				"status": "paid",
				"amount_cents": 28000,
				"currency": "BRL"
			}`))
		},
	))
	defer server.Close()

	client := &Client{
		baseURL:   server.URL,
		apiKey:    "test-key",
		apiSecret: "test-secret",
		http: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	result, err := client.GetPayment(
		context.Background(),
		"test-payment-123",
	)

	if err != nil {
		t.Fatalf("erro ao consultar pagamento: %v", err)
	}

	if result.PaymentID != "test-payment-123" {
		t.Errorf("payment_id incorreto: %s", result.PaymentID)
	}

	if result.Status != "paid" {
		t.Errorf("status esperado paid, recebido %s", result.Status)
	}

	if result.AmountCents != 28000 {
		t.Errorf("valor esperado 28000, recebido %d", result.AmountCents)
	}

	if result.Currency != "BRL" {
		t.Errorf("moeda esperada BRL, recebida %s", result.Currency)
	}
}
