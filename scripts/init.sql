CREATE TYPE stock_tracking_status AS ENUM ('AUTO_ACTIVE', 'ACTIVE', 'INACTIVE', 'AUTO_INACTIVE');

CREATE TYPE order_status AS ENUM ('PENDING', 'COMPLETE', 'CANCELLED', 'REJECTED', 'OPEN');

CREATE TABLE IF NOT EXISTS tracking_stocks (
    id SERIAL PRIMARY KEY,
    trading_symbol VARCHAR(10) UNIQUE NOT NULL,
    exchange VARCHAR(10) DEFAULT 'NSE' NOT NULL,
    instrument_token BIGINT NOT NULL,
    target DECIMAL(10, 2) NOT NULL,
    stoploss DECIMAL(10, 2) NOT NULL,
    order_price_limit DECIMAL(10, 2) DEFAULT 0,
    quantity INT NOT NULL,
    allowed_trades INT DEFAULT 1,
    status stock_tracking_status NOT NULL DEFAULT 'AUTO_ACTIVE',
    is_deleted BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS orders (
    id SERIAL PRIMARY KEY,
    tracking_stock_id INT REFERENCES tracking_stocks(id) ON DELETE SET NULL,
    order_id VARCHAR(50) UNIQUE NOT NULL,
    exchange_order_id VARCHAR(50),
    parent_order_id VARCHAR(50),
    order_type VARCHAR(10) NOT NULL,
    event_type VARCHAR(20) NOT NULL,
    transaction_type VARCHAR(10),
    exchange VARCHAR(10) DEFAULT 'NSE',
    product VARCHAR(10),
    quantity DECIMAL(10, 2) NOT NULL,
    base_price DECIMAL(10, 2) NOT NULL,
    trigger_price DECIMAL(10, 2),
    purchase_price DECIMAL(10, 2),
    status_message VARCHAR(255),
    status order_status NOT NULL DEFAULT 'PENDING',
    placed_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    full_name VARCHAR(255) NOT NULL,
    phone VARCHAR(15) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS instruments (
    id SERIAL PRIMARY KEY,
    exchange VARCHAR(10) NOT NULL UNIQUE,
    instruments_data JSONB NOT NULL,
    stored_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_orders_tracking_stock_id
ON orders(tracking_stock_id);

CREATE INDEX idx_orders_imbalance_calc
ON orders (tracking_stock_id, placed_at)  -- keys for searching/sorting
INCLUDE (transaction_type, quantity)      -- payload for calculation
WHERE status = 'COMPLETE';                -- partial index to save space