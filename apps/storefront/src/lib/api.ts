/**
 * In `astro dev`, a repo-root `.env` often sets production `PUBLIC_API_BASE` / `PUBLIC_AUTH_BASE`.
 * Those hosts are not reachable from your laptop’s dev API, so we default to local URLs unless
 * `PUBLIC_SAS_REMOTE=1` (then your `PUBLIC_*` values are used as-is).
 */
function devUseRemoteUrls(): boolean {
  const v = import.meta.env.PUBLIC_SAS_REMOTE;
  return v === "1" || v === "true";
}

function isLocalHostname(hostname: string): boolean {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "[::1]";
}

function isLocalUrl(url: string): boolean {
  try {
    return isLocalHostname(new URL(url).hostname);
  } catch {
    return false;
  }
}

function resolveInDev(urlFromEnv: string | undefined, localFallback: string): string {
  const raw = (urlFromEnv || "").trim();
  if (!import.meta.env.DEV || devUseRemoteUrls()) {
    return raw || localFallback;
  }
  if (raw && isLocalUrl(raw)) return raw;
  return localFallback;
}

/** API origin (server-side fetch + `window.__SAS__.apiBase` via Base layout). */
export const apiBase = resolveInDev(import.meta.env.PUBLIC_API_BASE, "http://localhost:8788");

/** Auth origin for client scripts (Better Auth); keep in sync with Base layout. */
export const authBase = resolveInDev(import.meta.env.PUBLIC_AUTH_BASE, "http://localhost:8787");

export async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${apiBase}${path}`, { headers: { accept: "application/json" } });
  if (!res.ok) throw new Error(`API error ${res.status}`);
  return (await res.json()) as T;
}

export async function safeGetJSON<T>(path: string, fallback: T): Promise<T> {
  try {
    return await getJSON<T>(path);
  } catch {
    return fallback;
  }
}

/** Like `safeGetJSON`, but callers can tell **connection/API errors** from an empty successful response. */
export async function tryGetJSON<T>(path: string, fallback: T): Promise<{ data: T; ok: boolean }> {
  try {
    return { data: await getJSON<T>(path), ok: true };
  } catch {
    return { data: fallback, ok: false };
  }
}
