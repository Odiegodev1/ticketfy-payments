package payment

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// PendingConfirmations retorna pagamentos que possuem
// cobrança Pix criada e ainda aguardam confirmação.
//
// Esta consulta não modifica nenhum registro.
func (r Repository) PendingConfirmations(
	ctx context.Context,
	limit int,
) ([]string, error) {

	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := r.DB.Query(ctx, `
		SELECT id
		FROM payments
		WHERE status = 'PENDING'
		  AND pix_creation_status = 'CREATED'
		  AND cajupay_payment_id IS NOT NULL
		  AND cajupay_payment_id <> ''
		  AND cajupay_payment_id NOT LIKE 'mock_%'
		ORDER BY created_at ASC
		LIMIT $1
	`, limit)

	if err != nil {
		return nil, fmt.Errorf(
			"buscar pagamentos pendentes: %w",
			err,
		)
	}

	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id uuid.UUID

		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf(
				"ler pagamento pendente: %w",
				err,
			)
		}

		ids = append(ids, id.String())
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}
