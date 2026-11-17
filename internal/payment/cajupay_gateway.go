package payment

import (
	"context"

	"github.com/odiegodev1/ticketfy-payments/internal/cajupay"
)

type CajuPayGateway struct {
	Client      *cajupay.Client
	CheckoutURL string
}

func (g CajuPayGateway) CreatePix(
	ctx context.Context,
	input PixRequest,
	idempotencyKey string,
) (PixResponse, error) {

	result, err := g.Client.CreatePix(
		ctx,
		cajupay.CreatePixRequest{
			AmountCents:        input.AmountCents,
			Currency:           input.Currency,
			Description:        input.Description,
			ProductRef:         input.ProductRef,
			PartnerCheckoutURL: g.CheckoutURL,
		},
		idempotencyKey,
	)

	if err != nil {
		return PixResponse{}, err
	}

	return PixResponse{
		PaymentID:    result.PaymentID,
		PixCopyPaste: result.PixCopyPaste,
		PixQRCode:    result.PixQRCode,
		Status:       result.Status,
	}, nil
}
