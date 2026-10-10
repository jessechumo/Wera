-- +goose Up
-- Community blog: posts, comments and reactions. Every post and comment is
-- checked by the AI moderator before it is stored as published; rejected
-- ones are kept (status 'rejected') with the moderator's verdict for audit.
CREATE TABLE posts (
  id         BIGSERIAL PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,           -- a safe markdown subset, rendered as text
  tags       TEXT[] NOT NULL DEFAULT '{}',
  status     TEXT NOT NULL CHECK (status IN ('published','rejected')),
  moderation JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX posts_feed_idx ON posts (status, created_at DESC);

CREATE TABLE post_comments (
  id         BIGSERIAL PRIMARY KEY,
  post_id    BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  status     TEXT NOT NULL CHECK (status IN ('published','rejected')),
  moderation JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX post_comments_post_idx ON post_comments (post_id, created_at);

CREATE TABLE post_reactions (
  post_id    BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('like','insightful','celebrate')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (post_id, user_id, kind)
);

-- +goose Down
DROP TABLE post_reactions;
DROP TABLE post_comments;
DROP TABLE posts;
