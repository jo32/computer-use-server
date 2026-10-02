CREATE TABLE mcp_clients (
  id TEXT PRIMARY KEY, user_id TEXT REFERENCES users(id),
  name TEXT NOT NULL, secret_hash TEXT, redirect_uris TEXT NOT NULL,
  auth_method TEXT NOT NULL DEFAULT 'client_secret_post',
  created_at INTEGER NOT NULL
);
CREATE INDEX mcp_clients_owner ON mcp_clients(user_id);
CREATE TABLE mcp_requests (
  id TEXT PRIMARY KEY, client_id TEXT NOT NULL REFERENCES mcp_clients(id) ON DELETE CASCADE,
  redirect_uri TEXT NOT NULL, state TEXT NOT NULL, challenge TEXT NOT NULL,
  expires_at INTEGER NOT NULL, csrf_hash TEXT, session_hash TEXT
);
CREATE TABLE mcp_codes (
  code_hash TEXT PRIMARY KEY, client_id TEXT NOT NULL REFERENCES mcp_clients(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id), redirect_uri TEXT NOT NULL,
  challenge TEXT NOT NULL, expires_at INTEGER NOT NULL
);
CREATE TABLE mcp_grants (
  id TEXT PRIMARY KEY, client_id TEXT NOT NULL REFERENCES mcp_clients(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id), access_hash TEXT NOT NULL UNIQUE,
  refresh_hash TEXT NOT NULL UNIQUE, access_expires_at INTEGER NOT NULL,
  refresh_expires_at INTEGER NOT NULL
);

CREATE INDEX mcp_grants_owner ON mcp_grants(user_id, client_id);
CREATE TABLE mcp_refresh_used (
  token_hash TEXT PRIMARY KEY,
  grant_id TEXT NOT NULL REFERENCES mcp_grants(id) ON DELETE CASCADE
);
CREATE TABLE mcp_registration_limits (
  key TEXT PRIMARY KEY, count INTEGER NOT NULL, expires_at INTEGER NOT NULL
);
