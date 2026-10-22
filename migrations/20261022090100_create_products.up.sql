CREATE TABLE products (
    id          UUID PRIMARY KEY,
    name        VARCHAR(200)   NOT NULL,
    description TEXT           NOT NULL DEFAULT '',
    price       NUMERIC(12, 2) NOT NULL CHECK (price >= 0),
    category    VARCHAR(100)   NOT NULL,
    stock       INT            NOT NULL CHECK (stock >= 0),
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ    NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ    NULL
);

CREATE INDEX products_category_lower_active_idx ON products (lower(category)) WHERE deleted_at IS NULL;
CREATE INDEX products_price_active_idx ON products (price) WHERE deleted_at IS NULL;
CREATE INDEX products_created_at_active_idx ON products (created_at) WHERE deleted_at IS NULL;
