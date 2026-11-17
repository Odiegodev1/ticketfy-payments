package payment

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"
)

type WebhookHandler struct {
	Service WebhookService
}

func (h WebhookHandler) Verify(
	w http.ResponseWriter,
	r *http.Request,
) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)

	var input struct {
		PaymentID string `json:"paymentId"`
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		reply(w, http.StatusBadRequest, map[string]string{
			"error": "JSON inválido",
		})
		return
	}

	// Impede conteúdo JSON adicional.
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		reply(w, http.StatusBadRequest, map[string]string{
			"error": "Apenas um objeto JSON é permitido",
		})
		return
	} else if err.Error() != "EOF" {
		reply(w, http.StatusBadRequest, map[string]string{
			"error": "JSON inválido",
		})
		return
	}

	if _, err := uuid.Parse(input.PaymentID); err != nil {
		reply(w, http.StatusBadRequest, map[string]string{
			"error": "paymentId inválido",
		})
		return
	}

	result, err := h.Service.Verify(
		r.Context(),
		input.PaymentID,
	)

	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			reply(w, http.StatusNotFound, map[string]string{
				"error": "Pagamento não encontrado",
			})

		case errors.Is(err, ErrProviderNotLinked):
			reply(w, http.StatusConflict, map[string]string{
				"error": "Pagamento sem cobrança válida vinculada",
			})

		case errors.Is(err, ErrProviderMismatch):
			log.Printf(
				"[PAYMENT_VERIFY] Divergência detectada para pagamento %s",
				input.PaymentID,
			)

			reply(w, http.StatusConflict, map[string]string{
				"error": "Divergência nos dados da cobrança",
			})

		default:
			log.Printf("[PAYMENT_VERIFY] Falha: %v", err)

			reply(w, http.StatusBadGateway, map[string]string{
				"error": "Não foi possível verificar a cobrança",
			})
		}
		return
	}

	log.Printf(
		"[PAYMENT_VERIFY] payment=%s status=%s action=%s",
		result.PaymentID,
		result.ProviderStatus,
		result.Action,
	)

	reply(w, http.StatusOK, result)
}
