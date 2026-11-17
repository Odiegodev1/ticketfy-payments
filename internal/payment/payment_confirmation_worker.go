package payment

import (
	"context"
	"errors"
	"log"
	"time"
)

type PaymentConfirmationWorker struct {
	Repo      Repository
	Service   ConfirmationService
	Interval  time.Duration
	BatchSize int
}

func (w PaymentConfirmationWorker) Start(ctx context.Context) {

	interval := w.Interval

	if interval <= 0 {
		interval = 30 * time.Second
	}

	log.Println(
		"[CONFIRMATION WORKER] Iniciado",
	)

	// Executa uma primeira verificação ao iniciar.
	w.check(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {

		case <-ctx.Done():

			log.Println(
				"[CONFIRMATION WORKER] Encerrado",
			)

			return

		case <-ticker.C:

			w.check(ctx)
		}
	}
}

func (w PaymentConfirmationWorker) check(ctx context.Context) {

	batchSize := w.BatchSize

	if batchSize <= 0 || batchSize > 100 {
		batchSize = 50
	}

	// Limita o tempo da consulta ao NeonDB.
	queryCtx, cancel := context.WithTimeout(
		ctx,
		10*time.Second,
	)

	ids, err := w.Repo.PendingConfirmations(
		queryCtx,
		batchSize,
	)

	cancel()

	if err != nil {

		if ctx.Err() == nil {
			log.Printf(
				"[CONFIRMATION WORKER] Erro ao buscar pagamentos: %v",
				err,
			)
		}

		return
	}

	if len(ids) == 0 {
		return
	}

	log.Printf(
		"[CONFIRMATION WORKER] Verificando %d pagamentos",
		len(ids),
	)

	for _, paymentID := range ids {

		if ctx.Err() != nil {
			return
		}

		// Cada pagamento possui seu próprio timeout.
		paymentCtx, cancel := context.WithTimeout(
			ctx,
			20*time.Second,
		)

		result, err := w.Service.Confirm(
			paymentCtx,
			paymentID,
		)

		cancel()

		if err != nil {

			// Pagamento ainda não foi aprovado.
			// Não é erro operacional.
			if errors.Is(err, ErrPaymentNotPaid) {
				continue
			}

			// Pagamento não encontrado.
			if errors.Is(err, ErrNotFound) {

				log.Printf(
					"[CONFIRMATION WORKER] Pagamento não encontrado: %s",
					paymentID,
				)

				continue
			}

			// Divergência entre banco e gateway.
			// Exige investigação.
			if errors.Is(err, ErrProviderMismatch) {

				log.Printf(
					"[CONFIRMATION WORKER] ALERTA: dados divergentes no pagamento %s",
					paymentID,
				)

				continue
			}

			log.Printf(
				"[CONFIRMATION WORKER] Erro ao confirmar %s: %v",
				paymentID,
				err,
			)

			continue
		}

		if result == nil || !result.Confirmed {
			continue
		}

		if result.AlreadyPaid {

			log.Printf(
				"[CONFIRMATION WORKER] Pagamento já confirmado: %s",
				paymentID,
			)

			continue
		}

		log.Printf(
			"[CONFIRMATION WORKER] PIX CONFIRMADO | payment=%s order=%s",
			result.PaymentID,
			result.OrderID,
		)
	}
}
