package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrPaymentNotConfirmable = errors.New(
	"pagamento não está disponível para confirmação",
)

func (r Repository) ConfirmPaymentAndQueue(
	ctx context.Context,
	paymentID string,
	verifiedProviderID string,
) (bool, error) {

	// Valida os identificadores antes de iniciar a transação.
	paymentID = strings.TrimSpace(paymentID)
	verifiedProviderID = strings.TrimSpace(verifiedProviderID)

	if paymentID == "" ||
		verifiedProviderID == "" ||
		strings.HasPrefix(
			strings.ToLower(verifiedProviderID),
			"mock_",
		) {
		return false, ErrPaymentNotConfirmable
	}

	// 1. Inicia uma transação no NeonDB.
	tx, err := r.DB.BeginTx(
		ctx,
		pgx.TxOptions{},
	)

	if err != nil {
		return false, fmt.Errorf(
			"iniciar transação: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	// 2. Bloqueia o pagamento durante a transação.
	// Isso evita alterações concorrentes no mesmo registro.
	var orderID string
	var status string
	var creationStatus string
	var providerID string

	err = tx.QueryRow(
		ctx,
		`
		SELECT
			order_id,
			status,
			COALESCE(pix_creation_status, ''),
			COALESCE(cajupay_payment_id, '')
		FROM payments
		WHERE id = $1
		FOR UPDATE
		`,
		paymentID,
	).Scan(
		&orderID,
		&status,
		&creationStatus,
		&providerID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}

	if err != nil {
		return false, fmt.Errorf(
			"consultar pagamento para confirmação: %w",
			err,
		)
	}

	// 3. Verifica se o pagamento pode ser confirmado.
	if status != "PENDING" && status != "PAID" {
		return false, ErrPaymentNotConfirmable
	}

	// 4. Valida a cobrança vinculada.
	//
	// O identificador armazenado deve corresponder
	// exatamente ao identificador previamente
	// verificado na CajuPay.
	providerID = strings.TrimSpace(providerID)

	if creationStatus != "CREATED" ||
		providerID == "" ||
		strings.HasPrefix(
			strings.ToLower(providerID),
			"mock_",
		) ||
		providerID != verifiedProviderID {

		return false, ErrPaymentNotConfirmable
	}

	// 5. Confirma o pagamento apenas se estiver PENDING.
	//
	// Se já estiver PAID, não repetimos a atualização.
	updated := false

	if status == "PENDING" {

		tag, err := tx.Exec(
			ctx,
			`
			UPDATE payments
			SET
				status = 'PAID',
				updated_at = NOW()
			WHERE
				id = $1
				AND status = 'PENDING'
				AND pix_creation_status = 'CREATED'
				AND cajupay_payment_id = $2
			`,
			paymentID,
			verifiedProviderID,
		)

		if err != nil {
			return false, fmt.Errorf(
				"atualizar status do pagamento: %w",
				err,
			)
		}

		updated = tag.RowsAffected() == 1

		if !updated {
			return false, ErrPaymentNotConfirmable
		}
	}

	// 6. Registra o evento na outbox.
	//
	// O evento PAYMENT_PAID será processado
	// pelo OutboxWorker e enviado para a API Java.
	//
	// ON CONFLICT evita duplicação de eventos
	// para o mesmo pagamento.
	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO payment_outbox (
			payment_id,
			order_id,
			event_type,
			status
		)
		VALUES (
			$1,
			$2,
			'PAYMENT_PAID',
			'PENDING'
		)
		ON CONFLICT (payment_id) DO NOTHING
		`,
		paymentID,
		orderID,
	)

	if err != nil {
		return false, fmt.Errorf(
			"registrar evento na outbox: %w",
			err,
		)
	}

	// 7. Confirma a transação.
	//
	// A atualização do pagamento e o evento
	// são persistidos juntos.
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf(
			"confirmar transação: %w",
			err,
		)
	}

	// true: pagamento atualizado nesta execução.
	// false: pagamento já estava confirmado.
	return updated, nil
}
