CREATE TABLE IF NOT EXISTS dating_product_purchases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_id TEXT NOT NULL,
    product_kind TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'manual',
    provider_transaction_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    purchased_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT dating_product_purchases_product_kind_chk CHECK (
        product_kind IN ('subscription', 'spotlight', 'super_spotlight', 'standout_like')
    ),
    CONSTRAINT dating_product_purchases_provider_chk CHECK (
        provider IN ('manual', 'apple', 'google', 'revenuecat')
    ),
    CONSTRAINT dating_product_purchases_status_chk CHECK (
        status IN ('pending', 'verified', 'failed', 'refunded')
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_dating_product_purchases_provider_tx
    ON dating_product_purchases(provider, provider_transaction_id)
    WHERE provider_transaction_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_dating_product_purchases_user_created
    ON dating_product_purchases(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS dating_spotlight_inventory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purchase_id UUID REFERENCES dating_product_purchases(id) ON DELETE SET NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'available',
    duration_minutes INT NOT NULL,
    source TEXT NOT NULL DEFAULT 'purchase',
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT dating_spotlight_inventory_kind_chk CHECK (
        kind IN ('spotlight', 'super_spotlight')
    ),
    CONSTRAINT dating_spotlight_inventory_status_chk CHECK (
        status IN ('available', 'consumed', 'expired')
    ),
    CONSTRAINT dating_spotlight_inventory_duration_chk CHECK (
        duration_minutes > 0 AND duration_minutes <= 1440
    ),
    CONSTRAINT dating_spotlight_inventory_source_chk CHECK (
        source IN ('purchase', 'plus_credit', 'manual', 'admin')
    )
);

CREATE INDEX IF NOT EXISTS idx_dating_spotlight_inventory_available
    ON dating_spotlight_inventory(user_id, kind, granted_at, id)
    WHERE status = 'available';

CREATE TABLE IF NOT EXISTS dating_spotlight_activations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    inventory_id UUID NOT NULL UNIQUE REFERENCES dating_spotlight_inventory(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT dating_spotlight_activations_kind_chk CHECK (
        kind IN ('spotlight', 'super_spotlight')
    ),
    CONSTRAINT dating_spotlight_activations_window_chk CHECK (ends_at > starts_at)
);

CREATE INDEX IF NOT EXISTS idx_dating_spotlight_activations_user_active
    ON dating_spotlight_activations(user_id, ends_at DESC);

CREATE INDEX IF NOT EXISTS idx_dating_spotlight_activations_candidate_windows
    ON dating_spotlight_activations(user_id, kind, ends_at DESC);
