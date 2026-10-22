CREATE TABLE order_items (
    id           UUID PRIMARY KEY,
    order_id     UUID           NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    product_id   UUID           NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    product_name VARCHAR(200)   NOT NULL,
    quantity     INT            NOT NULL CHECK (quantity > 0),
    unit_price   NUMERIC(12, 2) NOT NULL CHECK (unit_price >= 0),
    subtotal     NUMERIC(12, 2) NOT NULL,
    CONSTRAINT order_items_subtotal_check CHECK (subtotal = unit_price * quantity)
);

CREATE INDEX order_items_order_id_idx ON order_items (order_id);
