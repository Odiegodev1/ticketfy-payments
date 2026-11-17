package payment

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	Repo Repository
	Pix  PixService
}

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		OrderID     string `json:"orderId"`
		AmountCents int64  `json:"amountCents"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		reply(w, 400, map[string]string{"error": "JSON inválido"})
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		reply(w, 400, map[string]string{"error": "JSON deve conter apenas um objeto"})
		return
	}
	input.OrderID = strings.TrimSpace(input.OrderID)
	if len(input.OrderID) < 1 || len(input.OrderID) > 120 || input.AmountCents < 1 || input.AmountCents > 100000000 {
		reply(w, 400, map[string]string{"error": "pedido ou valor inválido"})
		return
	}
	p, created, err := h.Repo.Create(r.Context(), input.OrderID, input.AmountCents)
	if err != nil {
		reply(w, 500, map[string]string{"error": "falha interna"})
		return
	}
	if p.AmountCents != input.AmountCents {
		reply(w, 409, map[string]string{"error": "pedido já existe com outro valor"})
		return
	}
	status := 200
	if created {
		status = 201
	}
	reply(w, status, p)
}
func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		reply(w, 400, map[string]string{"error": "id inválido"})
		return
	}
	p, err := h.Repo.ByID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		reply(w, 404, map[string]string{"error": "não encontrado"})
		return
	}
	if err != nil {
		reply(w, 500, map[string]string{"error": "falha interna"})
		return
	}
	reply(w, 200, p)
}

func (h *Handler) CreatePix(
	w http.ResponseWriter,
	r *http.Request,
) {
	id := chi.URLParam(r, "id")

	if _, err := uuid.Parse(id); err != nil {
		reply(w, http.StatusBadRequest, map[string]string{
			"error": "ID do pagamento inválido",
		})
		return
	}

	result, err := h.Pix.Create(r.Context(), id)

	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			reply(w, http.StatusNotFound, map[string]string{
				"error": "Pagamento não encontrado",
			})

		case errors.Is(err, ErrPixProcessing):
			reply(w, http.StatusConflict, map[string]string{
				"error": "Pix já solicitado ou em processamento",
			})

		default:
			reply(w, http.StatusInternalServerError, map[string]string{
				"error": "Não foi possível gerar o Pix",
			})
		}

		return
	}

	reply(w, http.StatusCreated, map[string]any{
		"paymentId":    result.PaymentID,
		"pixCopyPaste": result.PixCopyPaste,
		"pixQrCode":    result.PixQRCode,
		"status":       result.Status,
	})
}
