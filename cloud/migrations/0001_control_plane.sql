CREATE TABLE users (
  id TEXT PRIMARY KEY, email TEXT NOT NULL, name TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id),
  expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE oauth_states (
  id TEXT PRIMARY KEY, browser_hash TEXT NOT NULL, verifier TEXT NOT NULL,
  nonce TEXT NOT NULL, return_to TEXT NOT NULL, expires_at INTEGER NOT NULL
);
CREATE TABLE pairings (
  id TEXT PRIMARY KEY, challenge TEXT NOT NULL, name TEXT NOT NULL, platform TEXT NOT NULL,
  code TEXT NOT NULL, user_id TEXT REFERENCES users(id), expires_at INTEGER NOT NULL,
  claimed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE devices (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id),
  token_hash TEXT NOT NULL UNIQUE, name TEXT NOT NULL, platform TEXT NOT NULL,
  created_at INTEGER NOT NULL, last_seen INTEGER NOT NULL DEFAULT 0,
  snapshot TEXT NOT NULL DEFAULT '{}', revoked_at INTEGER
);
CREATE INDEX devices_owner ON devices(user_id, revoked_at);
CREATE TABLE commands (
  id TEXT PRIMARY KEY, device_id TEXT NOT NULL REFERENCES devices(id),
  kind TEXT NOT NULL, payload TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
  created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL,
  delivered_at INTEGER, completed_at INTEGER, error TEXT,
  request_id TEXT NOT NULL,
  UNIQUE(device_id, request_id)
);
CREATE INDEX commands_delivery ON commands(device_id, status, created_at);
