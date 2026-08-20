-- Initial schema for GoChat.
--
-- Mounted into the postgres container's docker-entrypoint-initdb.d, so it runs
-- once when the data volume is created. Apply manually with `make db-migrate`
-- when running against an existing database.

CREATE TABLE IF NOT EXISTS users (
    id          SERIAL PRIMARY KEY,
    user_name   VARCHAR(64)  NOT NULL,
    -- bcrypt hash, 60 characters today; the column leaves room for a future
    -- algorithm change without a migration.
    password    VARCHAR(255) NOT NULL,
    create_time TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Enforces name uniqueness in the database rather than in application code, so
-- two concurrent registrations of the same name cannot both succeed.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_user_name ON users (user_name);
