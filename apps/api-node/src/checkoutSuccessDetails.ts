import type { Pool } from "pg";
import Stripe from "stripe";

export type CheckoutSuccessLineItem = {
  name: string;
  quantity: number;
  unit_price_cents: number;
  line_total_cents: number;
};

export type CheckoutSuccessDetails = {
  source: "stripe" | "checkout_session";
  email: string;
  currency: string;
  total_cents: number;
  line_items: CheckoutSuccessLineItem[];
  shipping_cents: number;
};

function looksLikeStripeCheckoutSessionId(id: string): boolean {
  return /^cs_[a-zA-Z0-9_]+$/.test(id);
}

export async function getCheckoutSuccessDetails(
  pool: Pool,
  stripeSecretKey: string,
  stripeSessionId: string | undefined,
  internalCheckoutSessionId: string | undefined
): Promise<{ ok: true; data: CheckoutSuccessDetails } | { ok: false; status: number; error: string }> {
  const sk = stripeSecretKey.trim();

  if (stripeSessionId?.trim()) {
    const sid = stripeSessionId.trim();
    if (!looksLikeStripeCheckoutSessionId(sid)) {
      return { ok: false, status: 400, error: "invalid_session_id" };
    }
    if (!sk) {
      return { ok: false, status: 412, error: "stripe_not_configured" };
    }
    const stripe = new Stripe(sk);
    try {
      const session = await stripe.checkout.sessions.retrieve(sid, {
        expand: ["line_items.data.price.product"],
      });
      const email = (session.customer_details?.email || session.customer_email || "").trim();
      const currency = (session.currency || "gbp").toUpperCase();
      const totalCents = session.amount_total != null ? Number(session.amount_total) : 0;
      let shippingCents = 0;
      if (session.shipping_cost?.amount_subtotal != null) {
        shippingCents = Number(session.shipping_cost.amount_subtotal);
      } else if (session.shipping_cost?.amount_total != null) {
        shippingCents = Number(session.shipping_cost.amount_total);
      }

      const line_items: CheckoutSuccessLineItem[] = [];
      const lis = session.line_items?.data ?? [];
      for (const li of lis) {
        const qty = Number(li.quantity || 0);
        let name = (li.description || "").trim();
        const price = li.price;
        const product = price?.product;
        if (!name && product && typeof product !== "string" && !product.deleted && "name" in product) {
          name = ((product as Stripe.Product).name || "").trim();
        }
        if (!name) name = "Item";
        let lineTotal = li.amount_subtotal != null ? Number(li.amount_subtotal) : 0;
        if (lineTotal <= 0 && li.amount_total != null) lineTotal = Number(li.amount_total);
        let unit = 0;
        if (qty > 0 && lineTotal > 0) unit = Math.round(lineTotal / qty);
        else if (price?.unit_amount) unit = Number(price.unit_amount);
        line_items.push({
          name,
          quantity: qty,
          unit_price_cents: unit,
          line_total_cents: lineTotal,
        });
      }

      return {
        ok: true,
        data: {
          source: "stripe",
          email,
          currency,
          total_cents: totalCents,
          line_items,
          shipping_cents: shippingCents,
        },
      };
    } catch {
      return { ok: false, status: 502, error: "stripe_error" };
    }
  }

  if (internalCheckoutSessionId?.trim()) {
    const idRaw = internalCheckoutSessionId.trim();
    if (!/^\d+$/.test(idRaw)) {
      return { ok: false, status: 400, error: "invalid_checkout_session_id" };
    }
    const r = await pool.query(
      `SELECT email, cart, shipping_cents, subtotal_cents, currency FROM checkout_sessions WHERE id=$1`,
      [Number(idRaw)]
    );
    const row = r.rows[0];
    if (!row) return { ok: false, status: 404, error: "not_found" };
    const email = String(row.email || "").trim();
    const currency = String(row.currency || "GBP").toUpperCase();
    const shippingCents = Number(row.shipping_cents || 0);
    let subtotalCents = Number(row.subtotal_cents || 0);
    const line_items: CheckoutSuccessLineItem[] = [];
    try {
      const cart = row.cart as unknown;
      let raw: string;
      if (Buffer.isBuffer(cart)) raw = cart.toString("utf8");
      else if (typeof cart === "string") raw = cart;
      else raw = JSON.stringify(cart);
      const items = JSON.parse(raw) as Array<{
        name: string;
        unit_price_cents: number;
        quantity: number;
      }>;
      for (const it of items) {
        const qty = Number(it.quantity || 0);
        const unit = Number(it.unit_price_cents || 0);
        line_items.push({
          name: String(it.name || "Item"),
          quantity: qty,
          unit_price_cents: unit,
          line_total_cents: unit * qty,
        });
      }
      if (subtotalCents <= 0 && line_items.length) {
        subtotalCents = line_items.reduce((s, x) => s + x.line_total_cents, 0);
      }
    } catch {
      /* ignore */
    }
    const totalCents = subtotalCents + shippingCents;
    return {
      ok: true,
      data: {
        source: "checkout_session",
        email,
        currency,
        total_cents: totalCents,
        line_items,
        shipping_cents: shippingCents,
      },
    };
  }

  return { ok: false, status: 400, error: "missing_session" };
}
