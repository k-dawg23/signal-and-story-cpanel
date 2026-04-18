// Default to `localhost` so Better Auth cookies are sent cross-service in dev.
export const apiBase = import.meta.env.PUBLIC_API_BASE || "http://localhost:8788";

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

