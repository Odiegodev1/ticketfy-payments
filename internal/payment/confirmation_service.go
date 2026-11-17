package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrPaymentNotPaid = errors.New(
	"pagamento ainda não confirmado pela CajuPay",
)

type ConfirmationService struct {
	Webhook WebhookService
	Repo    Repository
}

type ConfirmationResult struct {
	PaymentID      string `json:"paymentId"`
	OrderID        string `json:"orderId"`
	ProviderStatus string `json:"providerStatus"`
	InternalStatus string `json:"internalStatus"`
	Confirmed      bool   `json:"confirmed"`
	AlreadyPaid    bool   `json:"alreadyPaid"`
}

func (s ConfirmationService) Confirm(
	ctx context.Context,
	paymentID string,
) (*ConfirmationResult, error) {

	// 1. Valida o identificador do pagamento.
	paymentID = strings.TrimSpace(paymentID)

	if paymentID == "" {
		return nil, errors.New(
			"identificador do pagamento obrigatório",
		)
	}

	// 2. Consulta a CajuPay e valida a cobrança.
	//
	// O WebhookService.Verify verifica:
	// - Identificador da cobrança
	// - Valor do pagamento
	// - Moeda
	// - Status retornado pela CajuPay
	verification, err := s.Webhook.Verify(
		ctx,
		paymentID,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"falha ao verificar pagamento: %w",
			err,
		)
	}

	if verification == nil || !verification.Verified {
		return nil, ErrProviderMismatch
	}

	// 3. Confere a identidade do pagamento.
	if verification.PaymentID != paymentID {
		return nil, ErrProviderMismatch
	}

	// 4. Exige que a CajuPay confirme o pagamento.
	//
	// Nenhum pagamento PENDING pode ser aprovado
	// sem o status paid do gateway.
	if !strings.EqualFold(
		strings.TrimSpace(verification.ProviderStatus),
		"paid",
	) {
		return nil, ErrPaymentNotPaid
	}

	// 5. Confere o status interno.
	//
	// PENDING: pode ser confirmado.
	// PAID: pode ser processado novamente
	// de forma idempotente.
	if verification.InternalStatus != "PENDING" &&
		verification.InternalStatus != "PAID" {

		return nil, fmt.Errorf(
			"status interno não permite confirmação: %s",
			verification.InternalStatus,
		)
	}

	// 6. Confirma o pagamento no NeonDB.
	//
	// O repositório verifica novamente:
	// - Status interno
	// - Estado da criação do Pix
	// - Identificador da cobrança CajuPay
	//
	// A atualização e a criação do evento
	// acontecem na mesma transação.
	updated, err := s.Repo.ConfirmPaymentAndQueue(
		ctx,
		paymentID,
		verification.ProviderID,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"falha ao confirmar pagamento: %w",
			err,
		)
	}

	// 7. Trata confirmações repetidas.
	//
	// Se updated=false, verificamos se o
	// pagamento já está confirmado.
	if !updated {

		current, err := s.Repo.ByID(
			ctx,
			paymentID,
		)

		if err != nil {
			return nil, fmt.Errorf(
				"falha ao consultar pagamento confirmado: %w",
				err,
			)
		}

		if current.Status != "PAID" {
			return nil, errors.New(
				"pagamento não pôde ser confirmado",
			)
		}
	}

	// 8. Retorna o resultado.
	return &ConfirmationResult{
		PaymentID:      paymentID,
		OrderID:        verification.OrderID,
		ProviderStatus: verification.ProviderStatus,
		InternalStatus: "PAID",
		Confirmed:      true,
		AlreadyPaid:    !updated,
	}, nil
}
