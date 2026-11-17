package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrPixProcessing = errors.New(
		"criação do Pix já iniciada ou aguardando recuperação",
	)

	ErrPixAlreadyCreated = errors.New(
		"Pix já criado",
	)
)

type PixGateway interface {
	CreatePix(
		ctx context.Context,
		input PixRequest,
		idempotencyKey string,
	) (PixResponse, error)
}

type PixRequest struct {
	AmountCents int64
	Currency    string
	Description string
	ProductRef  string
}

type PixResponse struct {
	PaymentID    string
	PixCopyPaste string
	PixQRCode    string
	Status       string
}

type PixService struct {
	Repo    Repository
	Gateway PixGateway
}

func (s PixService) Create(
	ctx context.Context,
	paymentID string,
) (*PixResponse, error) {
	if _, err := uuid.Parse(paymentID); err != nil {
		return nil, errors.New("ID inválido")
	}

	payment, err := s.Repo.ByID(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	if payment.Status != "PENDING" {
		return nil, errors.New("pagamento não está pendente")
	}

	// Primeiro verifica se o Pix já foi criado.
	existing, err := s.Repo.GetPix(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	if existing.CreationStatus == "CREATED" {
		if existing.ProviderID == "" {
			return nil, errors.New(
				"Pix criado sem identificador do provedor",
			)
		}

		return &PixResponse{
			PaymentID:    existing.ProviderID,
			PixCopyPaste: existing.CopyPaste,
			PixQRCode:    existing.QRCode,
			Status:       "PENDING",
		}, nil
	}

	// Se já estiver em criação, não inicia outra chamada.
	if existing.CreationStatus == "CREATING" {
		return nil, ErrPixProcessing
	}

	reserved, err := s.Repo.ReservePix(
		ctx,
		paymentID,
	)
	if err != nil {
		return nil, err
	}

	if !reserved {
		return nil, ErrPixProcessing
	}

	// A chave permanece estável para este pagamento.
	idempotencyKey := payment.ID

	// Contexto independente da conexão HTTP original.
	// Isso evita cancelar imediatamente a chamada ao
	// provedor quando o comprador fecha a página.
	providerCtx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	defer cancel()

	result, err := s.Gateway.CreatePix(
		providerCtx,
		PixRequest{
			AmountCents: payment.AmountCents,
			Currency:    "BRL",
			Description: fmt.Sprintf(
				"Pedido Ticketfy %s",
				payment.OrderID,
			),
			ProductRef: payment.OrderID,
		},
		idempotencyKey,
	)

	if err != nil {
		// Não remove a reserva.
		// O provedor pode ter criado a cobrança.
		return nil, fmt.Errorf(
			"falha ao criar Pix; reconciliação necessária: %w",
			err,
		)
	}

	if result.PaymentID == "" {
		return nil, errors.New(
			"CajuPay não retornou identificador; reconciliação necessária",
		)
	}

	// Salva mesmo se o cliente HTTP original desconectou.
	err = s.Repo.PersistPixAfterDisconnect(
		paymentID,
		result.PaymentID,
		result.PixCopyPaste,
		result.PixQRCode,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"Pix recebido, mas não persistido; reconciliação necessária: %w",
			err,
		)
	}

	return &result, nil
}
