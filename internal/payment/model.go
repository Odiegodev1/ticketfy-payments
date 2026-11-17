package payment

import "time"

type Payment struct {
	ID          string    `json:"id"`
	OrderID     string    `json:"orderId"`
	AmountCents int64     `json:"amountCents"`
	Currency    string    `json:"currency"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}
