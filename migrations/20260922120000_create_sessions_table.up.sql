CREATE TABLE IF NOT EXISTS sessions (
                                        id          BIGSERIAL PRIMARY KEY,
                                        jti         TEXT        NOT NULL UNIQUE,
                                        user_id     INTEGER     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                        issued_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
                                        expires_at  TIMESTAMPTZ NOT NULL,
                                        revoked_at  TIMESTAMPTZ,
                                        user_agent  TEXT,
                                        client_ip   TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_jti        ON sessions (jti);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id    ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at)
    WHERE revoked_at IS NULL;