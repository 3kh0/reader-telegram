CREATE TABLE IF NOT EXISTS subscriptions (
    id SERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    feed_url TEXT NOT NULL,
    title TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    refresh_interval INTEGER DEFAULT 3600,
    last_refreshed TIMESTAMP,
    last_post_id TEXT,
    UNIQUE(user_id, feed_url)
);

CREATE INDEX idx_subscriptions_user_id ON subscriptions(user_id);
