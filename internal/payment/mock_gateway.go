package payment

import (
	"context"
	"fmt"
	"time"
)

// MockPixGateway simula a CajuPay.
// Não gera cobranças reais.
type MockPixGateway struct{}

func (m MockPixGateway) CreatePix(
	ctx context.Context,
	input PixRequest,
	idempotencyKey string,
) (PixResponse, error) {

	select {
	case <-ctx.Done():
		return PixResponse{}, ctx.Err()
	case <-time.After(300 * time.Millisecond):
	}

	return PixResponse{
		PaymentID:    fmt.Sprintf("mock_%s", idempotencyKey),
		PixCopyPaste: "PIX_SIMULADO_NAO_PAGAVEL",
		PixQRCode:    "",
		Status:       "PENDING",
	}, nil
}
