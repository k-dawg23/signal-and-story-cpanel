export const apiBase = import.meta.env.PUBLIC_API_BASE || "http://127.0.0.1:8788";

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

