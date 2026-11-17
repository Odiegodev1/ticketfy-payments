package payment

import (
	"context"
	"log"
	"time"

	"github.com/odiegodev1/ticketfy-payments/internal/cajupay"
)

type Worker struct {
	Repo     Repository
	Client   *cajupay.Client
	Interval time.Duration
}

func (w Worker) Start(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 2 * time.Minute
	}

	log.Println("[WORKER] Reconciliação iniciada (somente leitura)")

	w.check(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[WORKER] Reconciliação encerrada")
			return

		case <-ticker.C:
			w.check(ctx)
		}
	}
}

func (w Worker) check(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(
		ctx,
		15*time.Second,
	)
	defer cancel()

	ids, err := w.Repo.PendingRecovery(checkCtx, 50)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[WORKER] Erro no NeonDB: %v", err)
		}
		return
	}

	log.Printf(
		"[WORKER] Pagamentos aguardando recuperação: %d",
		len(ids),
	)

	if w.Client == nil {
		log.Println("[WORKER] Gateway MOCK: consulta externa desativada")
		return
	}

	payments, err := w.Client.ListPaymentsTyped(checkCtx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[WORKER] Erro na consulta CajuPay: %v", err)
		}
		return
	}

	var pending, paid, other int

	for _, p := range payments {
		switch p.Status {
		case "pending":
			pending++
		case "paid":
			paid++
		default:
			other++
		}
	}

	log.Printf(
		"[WORKER] CajuPay: total=%d pending=%d paid=%d outros=%d",
		len(payments),
		pending,
		paid,
		other,
	)

	if len(ids) > 0 {
		log.Println(
			"[WORKER] Existem cobranças para investigação; recuperação automática desativada",
		)
	}
}
