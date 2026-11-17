package cajupay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

type Client struct {
	baseURL   string
	apiKey    string
	apiSecret string
	http      *http.Client
}
type ListedPayment struct {
	PaymentID   string `json:"payment_id"`
	CustomerRef string `json:"customer_ref"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	Provider    string `json:"provider"`
	CreatedAt   string `json:"created_at"`
}

type Consumer struct {
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	Document string `json:"document,omitempty"`
	Phone    string `json:"phone,omitempty"`
}

type CreatePixRequest struct {
	AmountCents        int64     `json:"amount_cents"`
	Currency           string    `json:"currency"`
	Description        string    `json:"description"`
	ProductRef         string    `json:"product_ref,omitempty"`
	PartnerCheckoutURL string    `json:"partner_checkout_url"`
	Consumer           *Consumer `json:"consumer,omitempty"`
}

type CreatePixResponse struct {
	PaymentID    string `json:"payment_id"`
	Provider     string `json:"provider"`
	PixCopyPaste string `json:"pix_copy_paste"`
	PixQRCode    string `json:"pix_qr_code"`
	Status       string `json:"status"`
}

// PaymentDetails contém os campos que conhecemos.
//
// O JSON bruto é preservado porque ainda precisamos
// confirmar o formato completo da resposta de consulta.
type PaymentDetails struct {
	PaymentID   string          `json:"payment_id"`
	Status      string          `json:"status"`
	AmountCents int64           `json:"amount_cents"`
	Currency    string          `json:"currency"`
	ProductRef  string          `json:"product_ref"`
	Raw         json.RawMessage `json:"-"`
}

// PaymentListResponse mantém a resposta original.
//
// Não assumimos que a API retorna diretamente um array,
// pois o formato de paginação precisa ser confirmado.
type PaymentListResponse struct {
	Raw json.RawMessage
}

type APIError struct {
	StatusCode int
}

func (e *APIError) Error() string {
	return fmt.Sprintf(
		"CajuPay retornou HTTP %d",
		e.StatusCode,
	)
}

func NewClient(
	baseURL string,
	apiKey string,
	apiSecret string,
) (*Client, error) {
	u, err := url.Parse(baseURL)

	if err != nil ||
		u.Scheme != "https" ||
		u.Host != "api.cajupay.com.br" ||
		u.User != nil ||
		u.RawQuery != "" ||
		u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, errors.New("URL CajuPay inválida")
	}

	if strings.TrimSpace(apiKey) == "" ||
		strings.TrimSpace(apiSecret) == "" {
		return nil, errors.New("credenciais ausentes")
	}

	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		apiSecret: apiSecret,
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

// newRequest centraliza os cabeçalhos de autenticação.
func (c *Client) newRequest(
	ctx context.Context,
	method string,
	path string,
	body io.Reader,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		method,
		c.baseURL+path,
		body,
	)

	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("X-API-Secret", c.apiSecret)

	if body != nil {
		req.Header.Set(
			"Content-Type",
			"application/json",
		)
	}

	return req, nil
}

// do executa a requisição e limita o tamanho da resposta.
//
// Não registramos credenciais, dados bancários ou
// o corpo completo de erros nos logs.
func (c *Client) do(
	req *http.Request,
) ([]byte, error) {
	resp, err := c.http.Do(req)

	if err != nil {
		return nil, fmt.Errorf(
			"falha de comunicação com CajuPay: %w",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 ||
		resp.StatusCode >= 300 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
		}
	}

	limited := io.LimitReader(
		resp.Body,
		maxResponseBytes+1,
	)

	data, err := io.ReadAll(limited)

	if err != nil {
		return nil, fmt.Errorf(
			"falha ao ler resposta da CajuPay: %w",
			err,
		)
	}

	if len(data) > maxResponseBytes {
		return nil, errors.New(
			"resposta da CajuPay excedeu o limite permitido",
		)
	}

	if len(data) == 0 {
		return nil, errors.New(
			"CajuPay retornou resposta vazia",
		)
	}

	return data, nil
}

// CreatePix cria uma cobrança Pix.
//
// A chave de idempotência deve ser estável para
// cada pagamento interno.
func (c *Client) CreatePix(
	ctx context.Context,
	input CreatePixRequest,
	idempotencyKey string,
) (*CreatePixResponse, error) {
	if input.AmountCents < 200 {
		return nil, errors.New(
			"valor mínimo de R$ 2,00",
		)
	}

	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, errors.New(
			"idempotency key obrigatória",
		)
	}

	checkoutURL, err := url.Parse(
		input.PartnerCheckoutURL,
	)

	if err != nil ||
		checkoutURL.Scheme != "https" ||
		checkoutURL.Host == "" ||
		checkoutURL.User != nil {
		return nil, errors.New(
			"checkout HTTPS inválido",
		)
	}

	input.Currency = "BRL"

	body, err := json.Marshal(input)

	if err != nil {
		return nil, fmt.Errorf(
			"falha ao serializar Pix: %w",
			err,
		)
	}

	req, err := c.newRequest(
		ctx,
		http.MethodPost,
		"/api/payments/pix",
		bytes.NewReader(body),
	)

	if err != nil {
		return nil, err
	}

	req.Header.Set(
		"Idempotency-Key",
		idempotencyKey,
	)

	data, err := c.do(req)

	if err != nil {
		return nil, err
	}

	var result CreatePixResponse

	if err := json.Unmarshal(
		data,
		&result,
	); err != nil {
		return nil, fmt.Errorf(
			"resposta Pix inválida: %w",
			err,
		)
	}

	if result.PaymentID == "" {
		return nil, errors.New(
			"CajuPay não retornou payment_id",
		)
	}

	return &result, nil
}

// GetPayment consulta uma cobrança existente.
//
// Exemplo:
// GET /api/payments/{payment_id}
func (c *Client) GetPayment(
	ctx context.Context,
	paymentID string,
) (*PaymentDetails, error) {
	paymentID = strings.TrimSpace(paymentID)

	if paymentID == "" {
		return nil, errors.New(
			"payment_id obrigatório",
		)
	}

	path := "/api/payments/" +
		url.PathEscape(paymentID)

	req, err := c.newRequest(
		ctx,
		http.MethodGet,
		path,
		nil,
	)

	if err != nil {
		return nil, err
	}

	data, err := c.do(req)

	if err != nil {
		return nil, err
	}

	var result PaymentDetails

	if err := json.Unmarshal(
		data,
		&result,
	); err != nil {
		return nil, fmt.Errorf(
			"resposta de consulta inválida: %w",
			err,
		)
	}

	result.Raw = json.RawMessage(data)

	return &result, nil
}

func (c *Client) ListPaymentsTyped(
	ctx context.Context,
) ([]ListedPayment, error) {
	req, err := c.newRequest(
		ctx,
		http.MethodGet,
		"/api/payments",
		nil,
	)
	if err != nil {
		return nil, err
	}

	data, err := c.do(req)
	if err != nil {
		return nil, err
	}

	var payments []ListedPayment

	if err := json.Unmarshal(data, &payments); err != nil {
		return nil, fmt.Errorf(
			"resposta da listagem inválida: %w",
			err,
		)
	}

	return payments, nil
}

func (c *Client) ListPayments(
	ctx context.Context,
) (*PaymentListResponse, error) {
	req, err := c.newRequest(
		ctx,
		http.MethodGet,
		"/api/payments",
		nil,
	)

	if err != nil {
		return nil, err
	}

	data, err := c.do(req)

	if err != nil {
		return nil, err
	}

	if !json.Valid(data) {
		return nil, errors.New(
			"CajuPay retornou JSON inválido",
		)
	}

	return &PaymentListResponse{
		Raw: json.RawMessage(data),
	}, nil
}
