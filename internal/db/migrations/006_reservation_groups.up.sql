CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS reservation_groups (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(128) UNIQUE NOT NULL,
    description TEXT DEFAULT '',
    options JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE reservations
    ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES reservation_groups(id) ON DELETE SET NULL;

ALTER TABLE v6_reservations
    ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES reservation_groups(id) ON DELETE SET NULL;
