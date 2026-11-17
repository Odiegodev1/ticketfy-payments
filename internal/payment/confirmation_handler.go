package payment

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type ConfirmationHandler struct {
	Service ConfirmationService
}

func (h ConfirmationHandler) Confirm(
	w http.ResponseWriter,
	r *http.Request,
) {
	paymentID := chi.URLParam(r, "id")

	if _, err := uuid.Parse(paymentID); err != nil {
		reply(w, http.StatusBadRequest, map[string]string{
			"error": "ID inválido",
		})
		return
	}

	result, err := h.Service.Confirm(
		r.Context(),
		paymentID,
	)

	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			reply(w, http.StatusNotFound, map[string]string{
				"error": "Pagamento não encontrado",
			})

		case errors.Is(err, ErrPaymentNotPaid):
			reply(w, http.StatusConflict, map[string]string{
				"error": "Pagamento ainda não confirmado pela CajuPay",
			})

		case errors.Is(err, ErrProviderMismatch):
			reply(w, http.StatusConflict, map[string]string{
				"error": "Dados da cobrança divergentes",
			})

		default:
			reply(w, http.StatusBadGateway, map[string]string{
				"error": "Não foi possível confirmar o pagamento",
			})
		}
		return
	}

	reply(w, http.StatusOK, result)
}
