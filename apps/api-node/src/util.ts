import type { FastifyReply } from "fastify";

export function sendJson(reply: FastifyReply, status: number, payload: unknown) {
  return reply.code(status).header("Content-Type", "application/json; charset=utf-8").send(payload);
}

export function escapeLike(s: string): string {
  return s.replace(/%/g, "\\%").replace(/_/g, "\\_");
}

export function shippingCost(method: string): number {
  switch (method) {
    case "standard":
      return 0;
    case "express":
      return 299;
    case "next-day":
      return 599;
    default:
      return -1;
  }
}

export function nullIfEmpty(s: string): string | null {
  return s.trim() === "" ? null : s;
}

export function mustJSON(v: unknown): string {
  return JSON.stringify(v);
}

export function addQuery(rawURL: string, key: string, value: string): string {
  try {
    const u = new URL(rawURL);
    u.searchParams.set(key, value);
    return u.toString();
  } catch {
    return rawURL;
  }
}

export function nilIfBlankPtr(p: string | null | undefined): string | null {
  if (p === undefined || p === null) return null;
  const t = String(p).trim();
  return t === "" ? null : t;
}

export function moneyGBP(cents: number): string {
  const pounds = cents / 100;
  return `£${pounds.toFixed(2)}`;
}

export function isEmptyShippingAddr(addr: Record<string, unknown> | null | undefined): boolean {
  if (!addr) return true;
  const get = (k: string): string => {
    const v = addr[k];
    if (v == null) return "";
    return String(v).trim();
  };
  if (get("line1") !== "" || get("postal_code") !== "" || get("city") !== "") return false;
  return true;
}

export function htmlEscape(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}
