-- +goose Up
-- KeyDB and Dragonfly are Redis-compatible servers; ClickHouse is a column
-- store. All three use the engine contract in internal/databases (image, port,
-- credentials, data directory, readiness probe), so the column accepts them.
ALTER TABLE databases DROP CONSTRAINT databases_engine_check;
ALTER TABLE databases ADD CONSTRAINT databases_engine_check CHECK (engine IN (
    'postgres', 'mysql', 'mariadb', 'mongodb', 'redis', 'keydb', 'dragonfly', 'clickhouse'
));

-- +goose Down
ALTER TABLE databases DROP CONSTRAINT databases_engine_check;
ALTER TABLE databases ADD CONSTRAINT databases_engine_check CHECK (engine IN (
    'postgres', 'mysql', 'mariadb', 'mongodb', 'redis'
));
