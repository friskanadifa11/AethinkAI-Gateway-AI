-- 0001_init.sql — AethinkAI Gateway AI initial schema.
-- Applied once, tracked in schema_migrations. Idempotent by construction so a
-- partially applied file can be re-run during development.

create extension if not exists pgcrypto;

create table if not exists schema_migrations (
    version    text primary key,
    applied_at timestamptz not null default now()
);

-- ---------------------------------------------------------------- operators
create table if not exists admin_users (
    id            uuid primary key default gen_random_uuid(),
    username      text not null unique,
    password_hash text not null,
    created_at    timestamptz not null default now(),
    last_login_at timestamptz
);

create table if not exists admin_sessions (
    token_hash text primary key,
    user_id    uuid not null references admin_users(id) on delete cascade,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null
);
create index if not exists admin_sessions_expiry_idx on admin_sessions(expires_at);

-- ---------------------------------------------------------------- providers
create table if not exists providers (
    id              uuid primary key default gen_random_uuid(),
    name            text not null unique,
    adapter         text not null default 'openai-compatible',
    base_url        text not null,
    credential_enc  text,
    credential_fp   text,
    enabled         boolean not null default true,
    timeout_ms      int not null default 60000,
    last_test_at    timestamptz,
    last_test_state text not null default 'untested',
    last_test_error text not null default '',
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now()
);

create table if not exists provider_models (
    provider_id uuid not null references providers(id) on delete cascade,
    model_id    text not null,
    primary key (provider_id, model_id)
);

-- ------------------------------------------------------------------- routes
create table if not exists routes (
    id           uuid primary key default gen_random_uuid(),
    alias        text not null,
    version      int  not null,
    published    boolean not null default false,
    strategy     text not null default 'priority',
    created_at   timestamptz not null default now(),
    published_at timestamptz,
    unique (alias, version)
);
-- At most one live version per alias; this is what makes publish atomic.
create unique index if not exists routes_one_published_per_alias
    on routes(alias) where published;

create table if not exists route_targets (
    id             uuid primary key default gen_random_uuid(),
    route_id       uuid not null references routes(id) on delete cascade,
    position       int  not null,
    provider_id    uuid not null references providers(id),
    upstream_model text not null,
    timeout_ms     int  not null default 60000,
    retries        int  not null default 0,
    enabled        boolean not null default true,
    unique (route_id, position),
    -- Rejects the duplicate-target case the PRD calls out.
    unique (route_id, provider_id, upstream_model)
);

-- --------------------------------------------------------------- client keys
create table if not exists client_keys (
    id              uuid primary key default gen_random_uuid(),
    label           text not null,
    owner_label     text not null default '',
    key_hash        text not null unique,
    prefix          text not null,
    enabled         boolean not null default true,
    revoked_at      timestamptz,
    expires_at      timestamptz,
    rpm             int not null default 0,
    max_concurrency int not null default 0,
    token_quota     bigint not null default 0,
    tokens_used     bigint not null default 0,
    allowed_aliases text[] not null default '{}',
    created_at      timestamptz not null default now(),
    last_used_at    timestamptz
);
create index if not exists client_keys_prefix_idx on client_keys(prefix);

-- ----------------------------------------------------------------- requests
-- Confidential prompt/response bodies are deliberately absent: this schema
-- stores metadata only.
create table if not exists requests (
    id             uuid primary key default gen_random_uuid(),
    trace_id       text not null unique,
    key_id         uuid references client_keys(id) on delete set null,
    key_label      text not null default '',
    alias          text not null,
    provider_name  text not null default '',
    upstream_model text not null default '',
    status         text not null,
    http_status    int  not null default 0,
    duration_ms    int  not null default 0,
    ttft_ms        int,
    input_tokens   int  not null default 0,
    output_tokens  int  not null default 0,
    fallback_count int  not null default 0,
    error_code     text not null default '',
    streamed       boolean not null default false,
    created_at     timestamptz not null default now()
);
create index if not exists requests_created_idx on requests(created_at desc);
create index if not exists requests_alias_idx   on requests(alias);
create index if not exists requests_status_idx  on requests(status);

create table if not exists request_attempts (
    id             uuid primary key default gen_random_uuid(),
    request_id     uuid not null references requests(id) on delete cascade,
    attempt_no     int  not null,
    provider_name  text not null default '',
    upstream_model text not null default '',
    status         text not null,
    http_status    int  not null default 0,
    duration_ms    int  not null default 0,
    error_code     text not null default '',
    created_at     timestamptz not null default now(),
    unique (request_id, attempt_no)
);

-- ------------------------------------------------------- settings / auditing
create table if not exists settings (
    key        text primary key,
    value      text not null,
    updated_at timestamptz not null default now()
);

create table if not exists audit_events (
    id         uuid primary key default gen_random_uuid(),
    actor      text not null default '',
    action     text not null,
    target     text not null default '',
    meta       jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now()
);
create index if not exists audit_events_created_idx on audit_events(created_at desc);
