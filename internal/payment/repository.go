package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("pagamento não encontrado")

type Repository struct {
	DB *pgxpool.Pool
}

type StoredPix struct {
	PaymentID      string
	ProviderID     string
	CopyPaste      string
	QRCode         string
	Status         string
	CreationStatus string
}

// Create cria um pagamento interno vinculado a um pedido.
//
// O order_id é único, impedindo que o mesmo pedido
// gere múltiplos pagamentos internos.
//
// Retorna:
// - Payment: pagamento encontrado ou criado.
// - bool: true se foi criado agora.
// - error: erro durante a operação.
func (r Repository) Create(
	ctx context.Context,
	orderID string,
	cents int64,
) (Payment, bool, error) {

	var p Payment

	id := uuid.NewString()

	err := r.DB.QueryRow(
		ctx,
		`
		INSERT INTO payments (
			id,
			order_id,
			amount_cents
		)
		VALUES ($1, $2, $3)
		ON CONFLICT (order_id) DO NOTHING
		RETURNING
			id,
			order_id,
			amount_cents,
			currency,
			status,
			created_at
		`,
		id,
		orderID,
		cents,
	).Scan(
		&p.ID,
		&p.OrderID,
		&p.AmountCents,
		&p.Currency,
		&p.Status,
		&p.CreatedAt,
	)

	if err == nil {
		return p, true, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return p, false, fmt.Errorf(
			"erro ao criar pagamento: %w",
			err,
		)
	}

	// Se o pedido já possui pagamento,
	// retorna o registro existente.
	p, err = r.ByOrder(ctx, orderID)

	if err != nil {
		return p, false, err
	}

	return p, false, nil
}

// ByOrder consulta um pagamento pelo identificador
// do pedido na API Java.
func (r Repository) ByOrder(
	ctx context.Context,
	orderID string,
) (Payment, error) {

	var p Payment

	err := r.DB.QueryRow(
		ctx,
		`
		SELECT
			id,
			order_id,
			amount_cents,
			currency,
			status,
			created_at
		FROM payments
		WHERE order_id = $1
		`,
		orderID,
	).Scan(
		&p.ID,
		&p.OrderID,
		&p.AmountCents,
		&p.Currency,
		&p.Status,
		&p.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}

	if err != nil {
		return p, fmt.Errorf(
			"erro ao consultar pagamento pelo pedido: %w",
			err,
		)
	}

	return p, nil
}

// ByID consulta um pagamento pelo identificador
// interno do microserviço Go.
func (r Repository) ByID(
	ctx context.Context,
	id string,
) (Payment, error) {

	var p Payment

	err := r.DB.QueryRow(
		ctx,
		`
		SELECT
			id,
			order_id,
			amount_cents,
			currency,
			status,
			created_at
		FROM payments
		WHERE id = $1
		`,
		id,
	).Scan(
		&p.ID,
		&p.OrderID,
		&p.AmountCents,
		&p.Currency,
		&p.Status,
		&p.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}

	if err != nil {
		return p, fmt.Errorf(
			"erro ao consultar pagamento: %w",
			err,
		)
	}

	return p, nil
}

// ReservePix reserva a criação de uma cobrança Pix.
//
// Somente pagamentos PENDING sem cobrança vinculada
// podem iniciar a criação de um Pix.
//
// A operação é atômica, evitando que duas requisições
// reservem simultaneamente a mesma cobrança.
func (r Repository) ReservePix(
	ctx context.Context,
	paymentID string,
) (bool, error) {

	tag, err := r.DB.Exec(
		ctx,
		`
		UPDATE payments
		SET
			pix_creation_status = 'CREATING',
			pix_idempotency_key = id::text,
			pix_requested_at = NOW(),
			updated_at = NOW()
		WHERE
			id = $1
			AND status = 'PENDING'
			AND pix_creation_status IS NULL
			AND cajupay_payment_id IS NULL
		`,
		paymentID,
	)

	if err != nil {
		return false, fmt.Errorf(
			"erro ao reservar criação do Pix: %w",
			err,
		)
	}

	return tag.RowsAffected() == 1, nil
}

// GetPix consulta os dados da cobrança Pix
// vinculada ao pagamento interno.
func (r Repository) GetPix(
	ctx context.Context,
	paymentID string,
) (StoredPix, error) {

	var p StoredPix

	err := r.DB.QueryRow(
		ctx,
		`
		SELECT
			id,
			COALESCE(cajupay_payment_id, ''),
			COALESCE(pix_copy_paste, ''),
			COALESCE(pix_qr_code, ''),
			status,
			COALESCE(pix_creation_status, '')
		FROM payments
		WHERE id = $1
		`,
		paymentID,
	).Scan(
		&p.PaymentID,
		&p.ProviderID,
		&p.CopyPaste,
		&p.QRCode,
		&p.Status,
		&p.CreationStatus,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}

	if err != nil {
		return p, fmt.Errorf(
			"erro ao consultar cobrança Pix: %w",
			err,
		)
	}

	return p, nil
}

// SavePix salva a cobrança criada pela CajuPay.
//
// A cobrança somente será vinculada se:
// - O pagamento estiver PENDING.
// - A criação estiver reservada como CREATING.
// - Ainda não existir uma cobrança vinculada.
//
// Isso impede a substituição acidental de uma
// cobrança Pix já registrada.
func (r Repository) SavePix(
	ctx context.Context,
	paymentID string,
	providerID string,
	copyPaste string,
	qrCode string,
) error {

	tag, err := r.DB.Exec(
		ctx,
		`
		UPDATE payments
		SET
			cajupay_payment_id = $2,
			pix_copy_paste = $3,
			pix_qr_code = $4,
			pix_creation_status = 'CREATED',
			updated_at = NOW()
		WHERE
			id = $1
			AND status = 'PENDING'
			AND pix_creation_status = 'CREATING'
			AND cajupay_payment_id IS NULL
		`,
		paymentID,
		providerID,
		copyPaste,
		qrCode,
	)

	if err != nil {
		return fmt.Errorf(
			"falha ao salvar Pix: %w",
			err,
		)
	}

	if tag.RowsAffected() != 1 {
		return errors.New(
			"pagamento não disponível para atualização",
		)
	}

	return nil
}

// PendingRecovery retorna cobranças cuja criação
// foi iniciada há mais de dois minutos.
//
// IMPORTANTE:
//
// Esta função apenas identifica pagamentos
// que precisam de investigação.
//
// Ela NÃO autoriza gerar automaticamente
// uma segunda cobrança Pix.
func (r Repository) PendingRecovery(
	ctx context.Context,
	limit int,
) ([]string, error) {

	if limit < 1 || limit > 100 {
		limit = 50
	}

	rows, err := r.DB.Query(
		ctx,
		`
		SELECT id
		FROM payments
		WHERE
			status = 'PENDING'
			AND pix_creation_status = 'CREATING'
			AND pix_requested_at < NOW() - INTERVAL '2 minutes'
		ORDER BY pix_requested_at ASC
		LIMIT $1
		`,
		limit,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"erro ao buscar pagamentos para recuperação: %w",
			err,
		)
	}

	defer rows.Close()

	ids := make([]string, 0)

	for rows.Next() {

		var id string

		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf(
				"erro ao ler pagamento para recuperação: %w",
				err,
			)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"erro ao percorrer pagamentos para recuperação: %w",
			err,
		)
	}

	return ids, nil
}

// PersistPixAfterDisconnect tenta persistir a cobrança
// mesmo quando o cliente HTTP original desconecta.
//
// Utiliza um contexto independente com timeout
// para reduzir o risco de perder uma cobrança
// que já foi criada pela CajuPay.
func (r Repository) PersistPixAfterDisconnect(
	paymentID string,
	providerID string,
	copyPaste string,
	qrCode string,
) error {

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	return r.SavePix(
		ctx,
		paymentID,
		providerID,
		copyPaste,
		qrCode,
	)
}
