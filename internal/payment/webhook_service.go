package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/odiegodev1/ticketfy-payments/internal/cajupay"
)

var (
	ErrWebhookNotConfigured = errors.New("cliente CajuPay não configurado")
	ErrProviderMismatch     = errors.New("dados da cobrança não correspondem ao pagamento interno")
	ErrProviderNotLinked    = errors.New("pagamento sem cobrança CajuPay vinculada")
)

type WebhookService struct {
	Repo   Repository
	Client *cajupay.Client
}

type PaymentVerification struct {
	PaymentID      string `json:"paymentId"`
	OrderID        string `json:"orderId"`
	ProviderID     string `json:"providerId"`
	InternalStatus string `json:"internalStatus"`
	ProviderStatus string `json:"providerStatus"`
	AmountCents    int64  `json:"amountCents"`
	Currency       string `json:"currency"`
	Verified       bool   `json:"verified"`
	Action         string `json:"action"`
}

func (s WebhookService) Verify(
	ctx context.Context,
	paymentID string,
) (*PaymentVerification, error) {
	if s.Client == nil {
		return nil, ErrWebhookNotConfigured
	}

	// 1. Consultar pagamento interno.
	p, err := s.Repo.ByID(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	// 2. Consultar cobrança vinculada no NeonDB.
	pix, err := s.Repo.GetPix(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	providerID := strings.TrimSpace(pix.ProviderID)

	if providerID == "" ||
		strings.HasPrefix(providerID, "mock_") ||
		pix.CreationStatus != "CREATED" {
		return nil, ErrProviderNotLinked
	}

	// 3. Consultar a CajuPay diretamente.
	providerCtx, cancel := context.WithTimeout(
		ctx,
		12*time.Second,
	)
	defer cancel()

	details, err := s.Client.GetPayment(
		providerCtx,
		providerID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"consulta à CajuPay falhou: %w",
			err,
		)
	}

	// 4. Conferir identidade e valor.
	if details.PaymentID != providerID ||
		details.AmountCents != p.AmountCents ||
		!strings.EqualFold(details.Currency, p.Currency) {
		return nil, ErrProviderMismatch
	}

	result := &PaymentVerification{
		PaymentID:      p.ID,
		OrderID:        p.OrderID,
		ProviderID:     providerID,
		InternalStatus: p.Status,
		ProviderStatus: details.Status,
		AmountCents:    p.AmountCents,
		Currency:       p.Currency,
		Verified:       true,
		Action:         "NO_CHANGE",
	}

	// Apenas sinaliza que o pagamento merece processamento.
	// Não altera o banco nem libera ingressos.
	if strings.EqualFold(details.Status, "paid") {
		result.Action = "READY_FOR_REVIEW"
	}

	return result, nil
}
