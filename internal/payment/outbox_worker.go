package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type OutboxWorker struct {
	Repo       Repository
	JavaURL    string
	Token      string
	Interval   time.Duration
	HTTPClient *http.Client
}

type outboxEvent struct {
	ID          string
	PaymentID   string
	OrderID     string
	AmountCents int64
}

type javaConfirmationRequest struct {
	PaymentID   string `json:"paymentId"`
	OrderID     string `json:"orderId"`
	AmountCents int64  `json:"amountCents"`
}

func (w OutboxWorker) Start(ctx context.Context) {
	if w.JavaURL == "" || w.Token == "" {
		log.Println("[OUTBOX] Desativado: configuração incompleta")
		return
	}

	parsed, err := url.Parse(w.JavaURL)
	if err != nil ||
		parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil {
		log.Println("[OUTBOX] URL da API Java inválida")
		return
	}

	interval := w.Interval
	if interval <= 0 {
		interval = 10 * time.Second
	}

	log.Println("[OUTBOX] Worker de entrega iniciado")

	w.processBatch(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[OUTBOX] Worker encerrado")
			return

		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w OutboxWorker) processBatch(ctx context.Context) {
	for i := 0; i < 20; i++ {
		if ctx.Err() != nil {
			return
		}

		processed, err := w.processOne(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("[OUTBOX] Erro: %v", err)
			}
			return
		}

		if !processed {
			return
		}
	}
}

func (w OutboxWorker) processOne(ctx context.Context) (bool, error) {
	workCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	tx, err := w.Repo.DB.BeginTx(workCtx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("iniciar transação: %w", err)
	}
	defer tx.Rollback(workCtx)

	var event outboxEvent
	var paymentStatus string
	var pixStatus string
	var providerID string

	err = tx.QueryRow(workCtx, `
		SELECT
			o.id::text,
			o.payment_id,
			o.order_id,
			p.amount_cents,
			p.status,
			COALESCE(p.pix_creation_status, ''),
			COALESCE(p.cajupay_payment_id, '')
		FROM payment_outbox o
		JOIN payments p
			ON p.id::text = o.payment_id
		WHERE o.status = 'PENDING'
		  AND o.event_type = 'PAYMENT_PAID'
		ORDER BY o.created_at
		LIMIT 1
		FOR UPDATE OF o SKIP LOCKED
	`).Scan(
		&event.ID,
		&event.PaymentID,
		&event.OrderID,
		&event.AmountCents,
		&paymentStatus,
		&pixStatus,
		&providerID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("buscar evento: %w", err)
	}

	// Defesa adicional: nunca entregar pagamento não confirmado.
	if paymentStatus != "PAID" ||
		pixStatus != "CREATED" ||
		providerID == "" ||
		event.AmountCents <= 0 {
		return false, fmt.Errorf(
			"evento %s possui pagamento inconsistente",
			event.ID,
		)
	}

	payload := javaConfirmationRequest{
		PaymentID:   event.PaymentID,
		OrderID:     event.OrderID,
		AmountCents: event.AmountCents,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}

	endpoint := strings.TrimRight(w.JavaURL, "/") +
		"/internal/payments/confirm"

	req, err := http.NewRequestWithContext(
		workCtx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return false, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.Token)

	client := w.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 8 * time.Second,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf(
			"comunicação com Java: %w",
			err,
		)
	}
	defer resp.Body.Close()

	// Resposta limitada para não consumir memória excessiva.
	responseBody, _ := io.ReadAll(
		io.LimitReader(resp.Body, 2048),
	)

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf(
			"Java retornou HTTP %d: %s",
			resp.StatusCode,
			string(responseBody),
		)
	}

	// Exigir confirmação explícita do Java.
	var confirmation struct {
		PaymentID string `json:"paymentId"`
		OrderID   string `json:"orderId"`
		Status    string `json:"status"`
	}

	if err := json.Unmarshal(responseBody, &confirmation); err != nil {
		return false, fmt.Errorf(
			"resposta Java inválida: %w",
			err,
		)
	}

	if confirmation.PaymentID != event.PaymentID ||
		confirmation.OrderID != event.OrderID ||
		confirmation.Status != "PAID" {
		return false, fmt.Errorf(
			"Java não confirmou o evento %s corretamente",
			event.ID,
		)
	}

	_, err = tx.Exec(workCtx, `
		UPDATE payment_outbox
		SET
			status = 'DELIVERED',
			attempts = attempts + 1,
			delivered_at = NOW(),
			updated_at = NOW()
		WHERE id = $1::uuid
		  AND status = 'PENDING'
	`, event.ID)

	if err != nil {
		return false, fmt.Errorf(
			"atualizar outbox: %w",
			err,
		)
	}

	if err := tx.Commit(workCtx); err != nil {
		return false, fmt.Errorf(
			"confirmar entrega: %w",
			err,
		)
	}

	log.Printf(
		"[OUTBOX] Entregue: payment=%s order=%s",
		event.PaymentID,
		event.OrderID,
	)

	return true, nil
}
