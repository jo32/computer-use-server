-- Computer discovery and control credentials belong to a browser login session.
-- Logging out removes its credentials without changing public computer links.
CREATE TABLE discovery_tokens (
  token_hash TEXT PRIMARY KEY,
  session_hash TEXT NOT NULL REFERENCES sessions(token_hash) ON DELETE CASCADE
);
CREATE INDEX discovery_tokens_session ON discovery_tokens(session_hash);
