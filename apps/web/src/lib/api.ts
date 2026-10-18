// Typed client for the gateway's admin API.
//
// Rules that keep this honest:
//   - every response is parsed into a declared shape; nothing is invented
//   - a failed request throws an ApiError carrying the server's code, so pages
//     render the real reason instead of a generic failure
//   - no secret is ever read into the console: the create-key response is the
//     only payload containing a token, and it is shown once, by the caller

export type Provider = {
  id: string;
  name: string;
  adapter: string;
  base_url: string;
  enabled: boolean;
  timeout_ms: number;
  credential_set: boolean;
  last_test_at?: string;
  last_test_state: string;
  last_test_error?: string;
  models: string[];
  created_at: string;
  updated_at: string;
};

export type RouteTarget = {
  position: number;
  provider_id: string;
  provider_name?: string;
  enabled: boolean;
  upstream_model: string;
  timeout_ms: number;
  retries: number;
};

export type Route = {
  id: string;
  alias: string;
  version: number;
  published: boolean;
  strategy: string;
  targets: RouteTarget[];
  created_at: string;
  published_at?: string;
};

export type ClientKey = {
  id: string;
  label: string;
  owner_label: string;
  prefix: string;
  enabled: boolean;
  revoked_at?: string;
  expires_at?: string;
  rpm: number;
  max_concurrency: number;
  token_quota: number;
  tokens_used: number;
  allowed_aliases: string[];
  created_at: string;
  last_used_at?: string;
};

export type RequestLog = {
  trace_id: string;
  key_label?: string;
  alias: string;
  provider_name?: string;
  upstream_model?: string;
  status: string;
  http_status: number;
  duration_ms: number;
  ttft_ms?: number;
  input_tokens: number;
  output_tokens: number;
  fallback_count: number;
  error_code?: string;
  streamed: boolean;
  created_at: string;
};

export type Attempt = {
  attempt_no: number;
  provider_name: string;
  upstream_model: string;
  status: string;
  http_status: number;
  duration_ms: number;
  error_code?: string;
  created_at: string;
};

export type TraceDetail = RequestLog & { attempts: Attempt[] };

export type SeriesPoint = { t: string; ok: number; failed: number };

export type Overview = {
  range: string;
  requests: number;
  success: number;
  errors: number;
  fallback_events: number;
  p95_latency_ms: number;
  tokens_in: number;
  tokens_out: number;
  series: SeriesPoint[];
  providers: Provider[];
  recent_failures: RequestLog[];
  published_routes: number;
  active_keys: number;
};

export type Settings = {
  site_name: string;
  public_api_url: string;
  timezone: string;
  default_timeout_ms: number;
  retention_days: number;
  motion: string;
};

export type AdminUser = {
  id: string;
  username: string;
  created_at: string;
  last_login_at?: string;
};

export type SetupState = { needs_bootstrap: boolean; bootstrap_configured: boolean };

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }

  /** A session that ended is not an error the operator should read as a bug. */
  get isAuthExpired(): boolean {
    return this.status === 401;
  }
}

type ErrorBody = { error?: { code?: string; message?: string } };

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`/api/admin${path}`, {
    ...init,
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...(init.headers ?? {}),
    },
  });

  if (response.status === 204) {
    return undefined as T;
  }

  const text = await response.text();
  let parsed: unknown = undefined;
  if (text.length > 0) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = undefined;
    }
  }

  if (!response.ok) {
    const body = (parsed ?? {}) as ErrorBody;
    throw new ApiError(
      response.status,
      body.error?.code ?? "unknown_error",
      body.error?.message ?? `The gateway answered ${response.status}.`,
    );
  }
  return parsed as T;
}

const json = (body: unknown) => ({ body: JSON.stringify(body) });

// ---------------------------------------------------------------- endpoints

export const api = {
  setupState: () => request<SetupState>("/setup-state"),

  bootstrap: (username: string, password: string, token: string) =>
    request<AdminUser>("/bootstrap", { method: "POST", ...json({ username, password, token }) }),

  login: (username: string, password: string) =>
    request<AdminUser>("/login", { method: "POST", ...json({ username, password }) }),

  logout: () => request<void>("/logout", { method: "POST" }),

  me: () => request<AdminUser>("/me"),

  changePassword: (current_password: string, new_password: string) =>
    request<void>("/password", { method: "POST", ...json({ current_password, new_password }) }),

  overview: (range: string) => request<Overview>(`/overview?range=${encodeURIComponent(range)}`),

  listProviders: () => request<{ providers: Provider[] }>("/providers").then((r) => r.providers),

  createProvider: (input: Record<string, unknown>) =>
    request<Provider>("/providers", { method: "POST", ...json(input) }),

  updateProvider: (id: string, input: Record<string, unknown>) =>
    request<Provider>(`/providers/${id}`, { method: "PATCH", ...json(input) }),

  deleteProvider: (id: string) => request<void>(`/providers/${id}`, { method: "DELETE" }),

  testProvider: (id: string) =>
    request<{ state: string; error: string; http_status?: number }>(`/providers/${id}/test`, {
      method: "POST",
    }),

  listRoutes: () => request<{ published: Route[]; drafts: Route[] }>("/routes"),

  getRoute: (id: string) => request<Route>(`/routes/${id}`),

  createRoute: (input: Record<string, unknown>) =>
    request<Route>("/routes", { method: "POST", ...json(input) }),

  publishRoute: (id: string) => request<Route>(`/routes/${id}/publish`, { method: "POST" }),

  deleteRoute: (id: string) => request<void>(`/routes/${id}`, { method: "DELETE" }),

  dryRun: (id: string) =>
    request<{
      alias: string;
      version: number;
      published: boolean;
      would_use: Array<{
        order: number;
        provider_name: string;
        upstream_model: string;
        timeout_s: number;
        retries: number;
      }>;
      skipped: Array<{ provider_id: string; provider_name?: string; reason: string }>;
      would_fail: boolean;
    }>(`/routes/${id}/dry-run`, { method: "POST" }),

  listKeys: () => request<{ keys: ClientKey[] }>("/keys").then((r) => r.keys),

  createKey: (input: Record<string, unknown>) =>
    request<{ key: ClientKey; token: string }>("/keys", { method: "POST", ...json(input) }),

  rotateKey: (id: string, grace_hours: number) =>
    request<{ key: ClientKey; token: string; replaced: string; grace_until: string }>(
      `/keys/${id}/rotate`,
      { method: "POST", ...json({ grace_hours }) },
    ),

  revokeKey: (id: string) => request<void>(`/keys/${id}/revoke`, { method: "POST" }),

  listRequests: (query: Record<string, string | number>) => {
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(query)) {
      if (v !== "" && v !== undefined && v !== null) params.set(k, String(v));
    }
    return request<{
      requests: RequestLog[];
      total: number;
      limit: number;
      offset: number;
      aliases: string[];
    }>(`/requests?${params.toString()}`);
  },

  traceDetail: (traceId: string) => request<TraceDetail>(`/requests/${encodeURIComponent(traceId)}`),

  getSettings: () => request<Settings>("/settings"),

  putSettings: (input: Partial<Settings>) =>
    request<Settings>("/settings", { method: "PUT", ...json(input) }),

  audit: (limit = 50) =>
    request<{ events: Array<{ actor: string; action: string; target: string; created_at: string }> }>(
      `/audit?limit=${limit}`,
    ).then((r) => r.events),
};
