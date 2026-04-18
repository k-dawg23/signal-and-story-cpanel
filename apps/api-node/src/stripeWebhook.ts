import type { Pool } from "pg";
import Stripe from "stripe";
import type { AppConfig } from "./config.js";
import { sendOrderConfirmationEmail } from "./email.js";
import { isEmptyShippingAddr, mustJSON } from "./util.js";

function advisoryLockKeyStripeCheckout(sessionID: string): string {
  let h = 14695981039346656037n;
  for (let i = 0; i < sessionID.length; i++) {
    h ^= BigInt(sessionID.charCodeAt(i));
    h = (h * 1099511628211n) & 0xffffffffffffffffn;
  }
  let v = h & 0x7fffffffffffffffn;
  if (v === 0n) v = 1n;
  return v.toString();
}

export async function countOrderItems(pool: Pool, orderID: number): Promise<number> {
  const r = await pool.query(`SELECT COUNT(*)::bigint AS n FROM order_items WHERE order_id=$1`, [orderID]);
  return Number(r.rows[0]?.n || 0);
}

export async function backfillOrderItemsFromStripeCheckoutSession(
  pool: Pool,
  stripe: Stripe,
  orderID: number,
  checkoutSessionID: string
): Promise<void> {
  const lineItems = await stripe.checkout.sessions.listLineItems(checkoutSessionID, {
    expand: ["data.price.product"],
  });
  for (const li of lineItems.data) {
    const qty = Number(li.quantity || 0);
    if (qty <= 0) continue;
    let name = (li.description || "").trim();
    const price = li.price;
    const product = price?.product;
    if (!name && product && typeof product !== "string" && !product.deleted && "name" in product) {
      name = (product as Stripe.Product).name?.trim() || "";
    }
    if (!name) name = "Item";
    let unit = 0;
    if (price?.unit_amount && price.unit_amount > 0) unit = price.unit_amount;
    else if (li.amount_subtotal && li.amount_subtotal > 0) unit = Math.floor(li.amount_subtotal / qty);
    else if (li.amount_total && li.amount_total > 0) unit = Math.floor(li.amount_total / qty);
    if (unit < 0) unit = 0;
    await pool.query(
      `INSERT INTO order_items (order_id, product_name_snapshot, unit_price_cents, quantity) VALUES ($1,$2,$3,$4)`,
      [orderID, name, unit, qty]
    );
  }
}

export async function backfillShippingAddressFromStripeCheckoutSession(
  pool: Pool,
  stripe: Stripe,
  orderID: number,
  checkoutSessionID: string
): Promise<void> {
  const cs = await stripe.checkout.sessions.retrieve(checkoutSessionID);
  const addr: Record<string, unknown> = {};
  if (cs.shipping_details) {
    const name = (cs.shipping_details.name || "").trim();
    if (name) addr.name = name;
    const a = cs.shipping_details.address;
    if (a) {
      addr.line1 = a.line1;
      addr.line2 = a.line2;
      addr.city = a.city;
      addr.state = a.state;
      addr.postal_code = a.postal_code;
      addr.country = a.country;
    }
  }
  if (Object.keys(addr).length === 0 && cs.customer_details?.address) {
    const a = cs.customer_details.address;
    const name = (cs.customer_details.name || "").trim();
    if (name) addr.name = name;
    addr.line1 = a.line1;
    addr.line2 = a.line2;
    addr.city = a.city;
    addr.state = a.state;
    addr.postal_code = a.postal_code;
    addr.country = a.country;
  }
  if (Object.keys(addr).length === 0) return;
  await pool.query(`UPDATE orders SET shipping_address=$2::jsonb WHERE id=$1`, [orderID, mustJSON(addr)]);
}

export async function processStripeCheckoutSessionCompleted(
  pool: Pool,
  cfg: AppConfig,
  raw: unknown
): Promise<void> {
  const cs = raw as Stripe.Checkout.Session;
  if (!cs.id) throw new Error("missing_checkout_session_id");
  if (String(cs.payment_status || "").toLowerCase() !== "paid") return;

  const metadata = cs.metadata || {};
  const checkoutSessionID = (metadata.checkout_session_id || "").trim();
  let email = (cs.customer_details?.email || "").trim();
  if (!email) email = (cs.customer_email || "").trim();

  type LocalRow = {
    user_id: string | null;
    email: string | null;
    cart: unknown;
    shipping_method: string;
    shipping_cents: number;
    subtotal_cents: number;
    currency: string;
  };
  let local: LocalRow = {
    user_id: null,
    email: null,
    cart: null,
    shipping_method: "",
    shipping_cents: 0,
    subtotal_cents: 0,
    currency: "GBP",
  };
  if (checkoutSessionID) {
    const lr = await pool.query(
      `SELECT user_id, email, cart, shipping_method, shipping_cents, subtotal_cents, currency
       FROM checkout_sessions WHERE id::text=$1`,
      [checkoutSessionID]
    );
    if (lr.rows[0]) {
      const r = lr.rows[0];
      local = {
        user_id: r.user_id as string | null,
        email: r.email as string | null,
        cart: r.cart,
        shipping_method: (r.shipping_method as string) || "",
        shipping_cents: Number(r.shipping_cents),
        subtotal_cents: Number(r.subtotal_cents),
        currency: (r.currency as string) || "GBP",
      };
    }
  }

  if (!email && local.email) email = local.email;
  if (!email) throw new Error("missing_email");

  let userID: string | null = local.user_id;
  if (!userID && metadata.user_id) userID = (metadata.user_id as string).trim() || null;

  let currency = local.currency.trim() || "GBP";
  let subtotalCents = local.subtotal_cents;
  let shippingCents = local.shipping_cents;
  let taxCents = 0;
  let totalCents = 0;
  if (cs.amount_subtotal && cs.amount_subtotal > 0) subtotalCents = cs.amount_subtotal;
  if (cs.shipping_cost?.amount_subtotal && cs.shipping_cost.amount_subtotal > 0) {
    shippingCents = cs.shipping_cost.amount_subtotal;
  }
  if (cs.total_details?.amount_tax && cs.total_details.amount_tax > 0) taxCents = cs.total_details.amount_tax;
  if (cs.amount_total && cs.amount_total > 0) totalCents = cs.amount_total;
  else totalCents = subtotalCents + shippingCents + taxCents;

  let shippingMethod = local.shipping_method.trim();
  if (!shippingMethod && metadata.shipping_method) shippingMethod = String(metadata.shipping_method).trim();

  let shippingAddress: Record<string, unknown> | null = null;
  if (cs.shipping_details?.address) {
    const a = cs.shipping_details.address;
    shippingAddress = {
      name: (cs.shipping_details.name || "").trim(),
      line1: a.line1,
      line2: a.line2,
      city: a.city,
      state: a.state,
      postal_code: a.postal_code,
      country: a.country,
    };
  }

  let paymentIntentID =
    typeof cs.payment_intent === "string" ? cs.payment_intent : cs.payment_intent?.id || "";
  paymentIntentID = paymentIntentID.trim();
  const paymentIntentParam = paymentIntentID === "" ? null : paymentIntentID;

  const client = await pool.connect();
  try {
    await client.query("BEGIN");
    await client.query(`SELECT pg_advisory_xact_lock($1::bigint)`, [advisoryLockKeyStripeCheckout(cs.id)]);

    const ins = await client.query(
      `INSERT INTO orders (
          stripe_payment_intent_id, stripe_checkout_session_id,
          user_id, email, status, currency,
          subtotal_cents, shipping_cents, tax_cents, total_cents,
          shipping_method, shipping_address
        )
        VALUES ($1,$2,$3,$4,'paid',$5,$6,$7,$8,$9,$10,$11::jsonb)
        ON CONFLICT (stripe_checkout_session_id) DO NOTHING
        RETURNING id`,
      [
        paymentIntentParam,
        cs.id,
        userID,
        email,
        currency,
        subtotalCents,
        shippingCents,
        taxCents,
        totalCents,
        shippingMethod || null,
        mustJSON(shippingAddress),
      ]
    );

    let orderDBID: number;
    let isNew = true;
    if (ins.rows[0]) {
      orderDBID = Number(ins.rows[0].id);
    } else {
      isNew = false;
      const ex = await client.query(`SELECT id FROM orders WHERE stripe_checkout_session_id=$1`, [cs.id]);
      if (!ex.rows[0]) throw new Error("order_missing");
      orderDBID = Number(ex.rows[0].id);
    }

    if (isNew && local.cart != null) {
      let cartStr = "";
      if (Buffer.isBuffer(local.cart)) cartStr = local.cart.toString("utf8");
      else if (typeof local.cart === "string") cartStr = local.cart;
      else cartStr = JSON.stringify(local.cart);
      if (cartStr.length > 0) {
      try {
        const items = JSON.parse(cartStr) as Array<{
          handle: string;
          name: string;
          unit_price_cents: number;
          quantity: number;
        }>;
        for (const it of items) {
          await client.query(
            `INSERT INTO order_items (order_id, product_name_snapshot, unit_price_cents, quantity)
             VALUES ($1,$2,$3,$4)`,
            [orderDBID, it.name, it.unit_price_cents, it.quantity]
          );
        }
      } catch {
        /* ignore cart parse */
      }
      }
    }

    await client.query("COMMIT");

    if (!isNew) return;

    const stripe = new Stripe(cfg.stripeSecretKey);

    if (!shippingAddress || Object.keys(shippingAddress).length === 0) {
      await backfillShippingAddressFromStripeCheckoutSession(pool, stripe, orderDBID, cs.id).catch(() => {});
    }

    const n = await countOrderItems(pool, orderDBID);
    if (n === 0) {
      await backfillOrderItemsFromStripeCheckoutSession(pool, stripe, orderDBID, cs.id).catch(() => {});
    }

    // Exactly one email per Checkout Session (Stripe can hit multiple webhook URLs or deliver
    // related events with different ids). PK on session id is the authoritative guard.
    const gate = await pool.query(
      `INSERT INTO checkout_confirmation_email_sent (stripe_checkout_session_id) VALUES ($1) ON CONFLICT (stripe_checkout_session_id) DO NOTHING RETURNING stripe_checkout_session_id`,
      [cs.id]
    );
    if (!gate.rows[0]) {
      return;
    }
    try {
      await sendOrderConfirmationEmail(pool, email, orderDBID, cs.id);
      await pool.query(
        `UPDATE orders SET confirmation_email_sent_at = now() WHERE id=$1 AND confirmation_email_sent_at IS NULL`,
        [orderDBID]
      );
    } catch (e) {
      await pool.query(`DELETE FROM checkout_confirmation_email_sent WHERE stripe_checkout_session_id=$1`, [cs.id]);
      throw e;
    }
  } catch (e) {
    try {
      await client.query("ROLLBACK");
    } catch {
      /* */
    }
    throw e;
  } finally {
    client.release();
  }
}

export async function handleStripeWebhook(
  pool: Pool,
  cfg: AppConfig,
  rawBody: Buffer,
  sig: string
): Promise<{ status: number; body: unknown }> {
  if (!cfg.stripeWebhookSecret || !cfg.stripeSecretKey) {
    return { status: 412, body: { error: "stripe_webhook_not_configured" } };
  }
  if (!sig.trim()) {
    return { status: 400, body: { error: "missing_signature" } };
  }

  let evt: Stripe.Event;
  try {
    evt = Stripe.webhooks.constructEvent(rawBody, sig, cfg.stripeWebhookSecret);
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err);
    if (process.env.APP_ENV?.toLowerCase() === "development") {
      return { status: 401, body: { error: "invalid_signature", detail: msg } };
    }
    return { status: 401, body: { error: "invalid_signature" } };
  }

  const ins = await pool.query(`INSERT INTO processed_webhooks (webhook_id) VALUES ($1) ON CONFLICT DO NOTHING`, [
    evt.id,
  ]);
  if ((ins.rowCount ?? 0) === 0) {
    return { status: 200, body: { ok: true, duplicate_event: true } };
  }

  if (evt.type === "checkout.session.completed") {
    await processStripeCheckoutSessionCompleted(pool, cfg, evt.data.object).catch((e) => {
      console.error("checkout.session.completed handler:", e);
    });
  }

  return { status: 200, body: { ok: true } };
}

export async function fetchShippingAddressFromStripe(
  stripe: Stripe,
  checkoutSessionID: string
): Promise<{ addr: Record<string, unknown>; dbg: Record<string, unknown> }> {
  const dbg: Record<string, unknown> = {};
  let cs: Stripe.Checkout.Session;
  try {
    cs = await stripe.checkout.sessions.retrieve(checkoutSessionID);
  } catch (err: unknown) {
    dbg.got_session = false;
    if (err && typeof err === "object" && "statusCode" in err) {
      const e = err as Stripe.errors.StripeError;
      dbg.stripe_status = e.statusCode;
      dbg.stripe_type = e.type;
      dbg.stripe_code = e.code;
      dbg.stripe_msg = e.message;
    } else if (err instanceof Error) dbg.stripe_err = err.message;
    return { addr: {}, dbg };
  }

  const addr: Record<string, unknown> = {};
  dbg.got_session = true;
  dbg.has_shipping_details = !!cs.shipping_details;
  dbg.has_customer_details = !!cs.customer_details;
  if (cs.shipping_details) {
    const name = (cs.shipping_details.name || "").trim();
    if (name) {
      addr.name = name;
      dbg.shipping_name = name;
    }
    if (cs.shipping_details.address) {
      const a = cs.shipping_details.address;
      dbg.shipping_has_address = true;
      addr.line1 = a.line1;
      addr.line2 = a.line2;
      addr.city = a.city;
      addr.state = a.state;
      addr.postal_code = a.postal_code;
      addr.country = a.country;
    }
  }
  if (isEmptyShippingAddr(addr as Record<string, unknown>) && cs.customer_details?.address) {
    const a = cs.customer_details.address;
    const name = (cs.customer_details.name || "").trim();
    if (name) {
      addr.name = name;
      dbg.customer_name = name;
    }
    dbg.customer_has_address = true;
    addr.line1 = a.line1;
    addr.line2 = a.line2;
    addr.city = a.city;
    addr.state = a.state;
    addr.postal_code = a.postal_code;
    addr.country = a.country;
  }
  if (isEmptyShippingAddr(addr)) return { addr: {}, dbg };
  return { addr, dbg };
}
