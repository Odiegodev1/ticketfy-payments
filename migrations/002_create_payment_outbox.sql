CREATE TABLE IF NOT EXISTS payment_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payment_id TEXT NOT NULL UNIQUE,
    order_id TEXT NOT NULL,

    event_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',

    attempts INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_payment_outbox_pending
ON payment_outbox (created_at)
WHERE status = 'PENDING';
