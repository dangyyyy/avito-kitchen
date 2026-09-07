-- +goose Up

-- Trigram index support for dish search across all restaurants.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE restaurants (
    id                  UUID PRIMARY KEY,
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    city                TEXT NOT NULL,
    address             TEXT NOT NULL,
    cuisine_type        TEXT NOT NULL DEFAULT '',
    is_accepting_orders BOOLEAN NOT NULL DEFAULT FALSE,
    api_key_hash        TEXT NOT NULL UNIQUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE menu_categories (
    id            UUID PRIMARY KEY,
    restaurant_id UUID NOT NULL REFERENCES restaurants (id) ON DELETE CASCADE,
    external_id   TEXT NOT NULL,
    name          TEXT NOT NULL,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (restaurant_id, external_id)
);

CREATE TABLE menu_items (
    id            UUID PRIMARY KEY,
    restaurant_id UUID NOT NULL REFERENCES restaurants (id) ON DELETE CASCADE,
    category_id   UUID NOT NULL REFERENCES menu_categories (id) ON DELETE CASCADE,
    external_id   TEXT NOT NULL,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    -- Money is stored as integer kopecks to avoid floating point rounding.
    price_cents   BIGINT NOT NULL CHECK (price_cents > 0),
    -- is_available: temporary stop list. is_active: removed from the menu during sync.
    is_available  BOOLEAN NOT NULL DEFAULT TRUE,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (restaurant_id, external_id)
);

CREATE INDEX menu_items_restaurant_idx ON menu_items (restaurant_id) WHERE is_active;
CREATE INDEX menu_items_name_trgm_idx ON menu_items USING gin (name gin_trgm_ops);

CREATE TABLE users (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL,
    phone      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
    id                UUID PRIMARY KEY,
    restaurant_id     UUID NOT NULL REFERENCES restaurants (id),
    user_id           UUID NOT NULL REFERENCES users (id),
    status            TEXT NOT NULL CHECK (status IN (
        'created', 'accepted', 'cooking', 'ready', 'delivering', 'delivered', 'rejected', 'cancelled'
    )),
    total_price_cents BIGINT NOT NULL CHECK (total_price_cents >= 0),
    -- Contact details are snapshotted so a later profile change cannot alter a past order.
    delivery_address  TEXT NOT NULL,
    customer_phone    TEXT NOT NULL,
    comment           TEXT NOT NULL DEFAULT '',
    rejection_reason  TEXT NOT NULL DEFAULT '',
    idempotency_key   TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, idempotency_key)
);

CREATE INDEX orders_restaurant_status_idx ON orders (restaurant_id, status);
CREATE INDEX orders_user_created_idx ON orders (user_id, created_at DESC);

CREATE TABLE order_items (
    id               UUID PRIMARY KEY,
    order_id         UUID NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    -- Kept for analytics only; name and price below are the authoritative snapshot.
    menu_item_id     UUID REFERENCES menu_items (id) ON DELETE SET NULL,
    external_item_id TEXT NOT NULL,
    name             TEXT NOT NULL,
    price_cents      BIGINT NOT NULL CHECK (price_cents > 0),
    quantity         INTEGER NOT NULL CHECK (quantity > 0)
);

CREATE INDEX order_items_order_idx ON order_items (order_id);

CREATE TABLE order_status_history (
    id          BIGSERIAL PRIMARY KEY,
    order_id    UUID NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    from_status TEXT,
    to_status   TEXT NOT NULL,
    changed_by  TEXT NOT NULL CHECK (changed_by IN ('user', 'restaurant', 'system')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX order_status_history_order_idx ON order_status_history (order_id, created_at);

-- +goose Down

DROP TABLE order_status_history;
DROP TABLE order_items;
DROP TABLE orders;
DROP TABLE users;
DROP TABLE menu_items;
DROP TABLE menu_categories;
DROP TABLE restaurants;
