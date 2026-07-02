CREATE TABLE IF NOT EXISTS review_history (
  id         text PRIMARY KEY,
  created_at timestamptz NOT NULL DEFAULT now(),
  title      text NOT NULL DEFAULT '',
  input      text NOT NULL,
  snippet    text NOT NULL DEFAULT '',
  source     text NOT NULL DEFAULT 'text',   -- 'text' | 'image' | 'generate'
  status     text NOT NULL DEFAULT 'reviewed',
  score      int  NOT NULL DEFAULT 0,
  result     jsonb NOT NULL,
  latency_ms int  NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_review_history_created_at ON review_history (created_at DESC);
