import Fastify, { type FastifyReply, type FastifyRequest } from "fastify";
import { Pool } from "pg";
import Stripe from "stripe";
import { getCheckoutSuccessDetails } from "./checkoutSuccessDetails.js";
import { loadConfig } from "./config.js";
import { sendOrderConfirmationEmail } from "./email.js";
import { fetchShippingAddressFromStripe, handleStripeWebhook } from "./stripeWebhook.js";
import {
  escapeLike,
  isEmptyShippingAddr,
  nilIfBlankPtr,
  nullIfEmpty,
  sendJson,
  shippingCost,
  stripeCheckoutSuccessUrl,
} from "./util.js";

type ProductRow = {
  id: number;
  handle: string;
  name: string;
  description_md: string;
  price_cents: number;
  currency: string;
  image_url: string;
};

/** Browser `Origin` is always a scheme + host + port (no path). Normalize env values so trailing slashes / missing schemes don’t break CORS. */
function normalizeOrigin(input: string): string {
  const s = input.trim();
  if (!s) return s;
  try {
    let href = s;
    if (!/^https?:\/\//i.test(href)) {
      href = `https://${href}`;
    }
    const u = new URL(href);
    return u.origin;
  } catch {
    return s.replace(/\/+$/, "");
  }
}

/** Add storefront origin from a full URL (e.g. STRIPE_SUCCESS_URL) so CORS still works if APP_BASE_URL is mis-typed in the panel. */
function addOriginFromFullUrl(set: Set<string>, raw: string | undefined) {
  const s = raw?.trim();
  if (!s) return;
  try {
    set.add(normalizeOrigin(new URL(s).origin));
  } catch {
    /* ignore invalid URL */
  }
}

function computeAllowedOrigins(): Set<string> {
  const base = normalizeOrigin(process.env.APP_BASE_URL?.trim() || "http://localhost:4321");
  const out = new Set<string>([
    base,
    normalizeOrigin("http://localhost:4321"),
    normalizeOrigin("http://127.0.0.1:4321"),
  ]);
  addOriginFromFullUrl(out, process.env.STRIPE_SUCCESS_URL);
  addOriginFromFullUrl(out, process.env.STRIPE_CANCEL_URL);
  const raw = process.env.APP_ORIGIN_ALLOWLIST?.trim();
  if (raw) {
    for (const part of raw.split(",")) {
      const v = part.trim();
      if (v) out.add(normalizeOrigin(v));
    }
  }
  return out;
}

function buildApp(pool: Pool, cfg: ReturnType<typeof loadConfig>) {
  const corsAllow = computeAllowedOrigins();
  console.error(`api-node CORS allowed origins: ${[...corsAllow].sort().join(" | ")}`);

  const app = Fastify({ logger: false });

  app.addContentTypeParser("application/json", { parseAs: "buffer" }, (req, body, done) => {
    const r = req as { url?: string; method?: string; rawBody?: Buffer };
    const buf = Buffer.isBuffer(body) ? body : Buffer.from(String(body), "utf8");
    if (r.url === "/webhooks/stripe" && r.method === "POST") {
      r.rawBody = buf;
      done(null, null);
      return;
    }
    try {
      const json = buf.length === 0 ? {} : JSON.parse(buf.toString("utf8"));
      done(null, json);
    } catch (e) {
      done(e as Error, undefined);
    }
  });

  app.addHook("onRequest", async (req, reply) => {
    const origin = (req.headers.origin || "").trim();
    if (origin && corsAllow.has(origin)) {
      reply.header("Access-Control-Allow-Origin", origin);
      reply.header("Vary", "Origin");
      reply.header("Access-Control-Allow-Credentials", "true");
      reply.header("Access-Control-Allow-Headers", "Content-Type, Accept");
      reply.header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS");
    }
    if (req.method === "OPTIONS") {
      await reply.code(204).send();
    }
  });

  app.addHook("onRequest", async (req, reply) => {
    if (reply.sent) return;
    if (!cfg.authBaseUrl) return;
    const u = new URL(cfg.authBaseUrl.replace(/\/+$/, ""));
    u.pathname = "/internal/session";
    const headers: Record<string, string> = {};
    const cookie = req.headers.cookie;
    if (cookie) headers.cookie = cookie;
    try {
      const res = await fetch(u.toString(), { headers, signal: AbortSignal.timeout(2000) });
      if (res.ok) {
        const out = (await res.json()) as { session?: { user?: { id?: string; email?: string } } };
        const user = out.session?.user;
        if (user?.id) req.headers["x-user-id"] = user.id;
        if (user?.email) req.headers["x-user-email"] = user.email;
      }
    } catch {
      /* ignore */
    }
  });

  app.get("/healthz", async (_req, reply) => {
    reply.type("text/plain").send("ok");
  });

  app.get("/api/_meta/build", async (_req, reply) => {
    const brevoKey = process.env.BREVO_API_KEY?.trim() || "";
    const smtpHost = process.env.SMTP_HOST?.trim() || "";
    let backend = "none";
    if (brevoKey) backend = "brevo";
    else if (smtpHost) backend = "smtp";
    return sendJson(reply, 200, {
      build_id: process.env.SAS_BUILD_ID?.trim() || "",
      email_backend: backend,
      brevo_configured: !!brevoKey,
      smtp_configured: !!smtpHost,
      brevo_sender_email: process.env.BREVO_SENDER_EMAIL?.trim() || "",
      brevo_sender_name: process.env.BREVO_SENDER_NAME?.trim() || "",
    });
  });

  app.post("/webhooks/stripe", async (req, reply) => {
    const raw = (req as { rawBody?: Buffer }).rawBody;
    if (!raw) return sendJson(reply, 400, { error: "invalid_body" });
    const sig = req.headers["stripe-signature"] as string | undefined;
    const r = await handleStripeWebhook(pool, cfg, raw, sig || "");
    return sendJson(reply, r.status, r.body);
  });

  app.get("/api/products", async (req, reply) => {
    const qry = req.query as Record<string, string | string[] | undefined>;
    const one = (v: string | string[] | undefined) => (Array.isArray(v) ? v[0] : v) ?? "";
    const q = String(one(qry["q"])).trim();
    const collection = String(one(qry["collection"])).trim();
    let rows: ProductRow[];
    try {
      if (collection) rows = await queryProductsByCollection(pool, collection, q);
      else rows = await queryProducts(pool, q);
    } catch {
      return sendJson(reply, 500, { error: "db_error" });
    }
    return sendJson(reply, 200, { products: rows });
  });

  app.get<{ Params: { handle: string } }>("/api/products/:handle", async (req, reply) => {
    try {
      const p = await queryProductByHandle(pool, req.params.handle);
      return sendJson(reply, 200, { product: p });
    } catch {
      return sendJson(reply, 404, { error: "not_found" });
    }
  });

  app.get("/api/checkout/success-details", async (req, reply) => {
    const qry = req.query as Record<string, string | string[] | undefined>;
    const one = (v: string | string[] | undefined) => (Array.isArray(v) ? v[0] : v) ?? "";
    const stripeSid = String(one(qry["session_id"])).trim();
    const internalSid = String(one(qry["checkout_session_id"])).trim();
    const result = await getCheckoutSuccessDetails(
      pool,
      cfg.stripeSecretKey,
      stripeSid || undefined,
      internalSid || undefined
    );
    if (!result.ok) return sendJson(reply, result.status, { error: result.error });
    return sendJson(reply, 200, result.data);
  });

  app.post("/api/checkout/session", async (req, reply) => {
    if (!cfg.stripeSecretKey || !cfg.stripeSuccessUrl || !cfg.stripeCancelUrl) {
      return sendJson(reply, 412, { error: "stripe_not_configured" });
    }
    const body = req.body as {
      items?: Array<{ handle: string; quantity: number }>;
      shipping?: string;
    };
    if (!body?.items?.length) return sendJson(reply, 400, { error: "empty_cart" });
    const shipCents = shippingCost(body.shipping || "");
    if (shipCents < 0) return sendJson(reply, 400, { error: "invalid_shipping" });

    type Priced = { handle: string; name: string; unit_price_cents: number; quantity: number };
    const priced: Priced[] = [];
    let subtotal = 0;
    for (const it of body.items) {
      if (it.quantity <= 0 || it.quantity > 20) return sendJson(reply, 400, { error: "invalid_quantity" });
      let p: ProductRow;
      try {
        p = await queryProductByHandle(pool, it.handle);
      } catch {
        return sendJson(reply, 400, { error: "invalid_product" });
      }
      subtotal += p.price_cents * it.quantity;
      priced.push({
        handle: p.handle,
        name: p.name,
        unit_price_cents: p.price_cents,
        quantity: it.quantity,
      });
    }

    const userID = String(req.headers["x-user-id"] || "").trim();
    const email = String(req.headers["x-user-email"] || "").trim();

    const cartJson = JSON.stringify(priced);
    const ins = await pool.query(
      `INSERT INTO checkout_sessions (user_id, email, cart, shipping_method, shipping_cents, subtotal_cents, currency)
       VALUES ($1,$2,$3::jsonb,$4,$5,$6,'GBP') RETURNING id::text`,
      [nullIfEmpty(userID), nullIfEmpty(email), cartJson, body.shipping, shipCents, subtotal]
    );
    const checkoutSessionID = ins.rows[0]?.id as string;
    if (!checkoutSessionID) return sendJson(reply, 500, { error: "db_error" });

    const stripe = new Stripe(cfg.stripeSecretKey);
    const lineItems: Stripe.Checkout.SessionCreateParams.LineItem[] = priced.map((it) => ({
      quantity: it.quantity,
      price_data: {
        currency: "gbp",
        product_data: {
          name: it.name,
          metadata: { handle: it.handle },
        },
        unit_amount: it.unit_price_cents,
        tax_behavior: "inclusive",
      },
    }));

    let shipName = "Standard shipping";
    let minDays = 3;
    let maxDays = 5;
    if (body.shipping === "express") {
      shipName = "Express shipping";
      minDays = maxDays = 2;
    } else if (body.shipping === "next-day") {
      shipName = "Next day shipping";
      minDays = maxDays = 1;
    }

    try {
      const sess = await stripe.checkout.sessions.create({
        mode: "payment",
        success_url: stripeCheckoutSuccessUrl(cfg.stripeSuccessUrl),
        cancel_url: cfg.stripeCancelUrl,
        automatic_tax: { enabled: true },
        shipping_address_collection: { allowed_countries: ["GB"] },
        billing_address_collection: "required",
        metadata: {
          checkout_session_id: checkoutSessionID,
          shipping_method: body.shipping || "",
          user_id: userID,
        },
        customer_email: email || undefined,
        line_items: lineItems,
        shipping_options: [
          {
            shipping_rate_data: {
              display_name: shipName,
              type: "fixed_amount",
              fixed_amount: { amount: shipCents, currency: "gbp" },
              tax_behavior: "inclusive",
              delivery_estimate: {
                minimum: { unit: "business_day", value: minDays },
                maximum: { unit: "business_day", value: maxDays },
              },
            },
          },
        ],
      });
      if (!sess.url) return sendJson(reply, 502, { error: "stripe_error" });
      return sendJson(reply, 200, { checkout_url: sess.url });
    } catch {
      return sendJson(reply, 502, { error: "stripe_error" });
    }
  });

  app.get("/api/account/orders", async (req, reply) => {
    const userID = String(req.headers["x-user-id"] || "").trim();
    const email = String(req.headers["x-user-email"] || "").trim();
    if (!userID && !email) return sendJson(reply, 401, { error: "unauthorized" });

    type OrderRow = {
      id: number;
      email: string;
      status: string;
      total_cents: number;
      created_at: Date;
      items: Array<{ name: string; unit_price_cents: number; quantity: number }>;
    };
    // Match by user_id OR email: checkout often stores user_id as null when the session
    // cookie was not forwarded to POST /api/checkout/session, but the order email still matches.
    const or = await pool.query(
      `SELECT id, email, status, total_cents, created_at FROM orders
       WHERE ($1::text <> '' AND user_id = $1)
          OR ($2::text <> '' AND lower(trim(email)) = lower(trim($2::text)))
       ORDER BY created_at DESC LIMIT 50`,
      [userID, email]
    );
    const out: OrderRow[] = or.rows.map((r) => ({
      id: Number(r.id),
      email: r.email as string,
      status: r.status as string,
      total_cents: Number(r.total_cents),
      created_at: r.created_at as Date,
      items: [],
    }));
    const orderIDs = out.map((o) => o.id);
    if (orderIDs.length) {
      const ir = await pool.query(
        `SELECT order_id, product_name_snapshot, unit_price_cents, quantity FROM order_items
         WHERE order_id = ANY($1) ORDER BY order_id ASC, id ASC`,
        [orderIDs]
      );
      const by: Record<number, OrderRow["items"]> = {};
      for (const r of ir.rows) {
        const oid = Number(r.order_id);
        if (!by[oid]) by[oid] = [];
        by[oid].push({
          name: r.product_name_snapshot as string,
          unit_price_cents: Number(r.unit_price_cents),
          quantity: Number(r.quantity),
        });
      }
      for (const o of out) o.items = by[o.id] || [];
    }
    return sendJson(reply, 200, { orders: out });
  });

  async function requireAdmin(req: FastifyRequest, reply: FastifyReply): Promise<boolean> {
    if (!cfg.adminEmail) {
      sendJson(reply, 403, { error: "admin_disabled" });
      return false;
    }
    const em = String(req.headers["x-user-email"] || "").trim();
    if (em.toLowerCase() !== cfg.adminEmail.toLowerCase()) {
      sendJson(reply, 403, { error: "forbidden" });
      return false;
    }
    return true;
  }

  app.get("/api/admin/overview", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const r1 = await pool.query(`SELECT COALESCE(SUM(total_cents), 0)::bigint AS n FROM orders WHERE status = 'paid'`);
    const r2 = await pool.query(`SELECT COUNT(*)::bigint AS n FROM orders`);
    return sendJson(reply, 200, {
      revenue_cents: Number(r1.rows[0]?.n || 0),
      orders_count: Number(r2.rows[0]?.n || 0),
    });
  });

  app.get("/api/admin/products", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    try {
      const rows = await queryAllProducts(pool, "");
      return sendJson(reply, 200, { products: rows });
    } catch {
      return sendJson(reply, 500, { error: "db_error" });
    }
  });

  app.post("/api/admin/products", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const b = req.body as {
      handle?: string;
      name?: string;
      description_md?: string;
      price_cents?: number;
      currency?: string;
      image_url?: string;
      active?: boolean;
      featured_rank?: number | null;
    };
    const handle = (b.handle || "").trim();
    const name = (b.name || "").trim();
    if (!handle || !name || (b.price_cents ?? 0) < 0) return sendJson(reply, 400, { error: "invalid_input" });
    const currency = (b.currency || "").trim() || "GBP";
    const active = b.active !== undefined ? b.active : true;
    try {
      const r = await pool.query(
        `INSERT INTO products (handle, name, description_md, price_cents, currency, image_url, active, featured_rank)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
        [
          handle,
          name,
          (b.description_md || "").trim(),
          b.price_cents,
          currency,
          nullIfEmpty((b.image_url || "").trim()),
          active,
          b.featured_rank ?? null,
        ]
      );
      return sendJson(reply, 200, { id: Number(r.rows[0].id) });
    } catch {
      return sendJson(reply, 500, { error: "db_error" });
    }
  });

  app.put<{ Params: { id: string } }>("/api/admin/products/:id", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const id = Number(req.params.id);
    if (!id) return sendJson(reply, 400, { error: "invalid_id" });
    const b = req.body as {
      handle?: string;
      name?: string;
      description_md?: string | null;
      price_cents?: number;
      currency?: string;
      image_url?: string;
      active?: boolean;
      featured_rank?: number | null;
    };
    if (b.price_cents !== undefined && b.price_cents < 0) return sendJson(reply, 400, { error: "invalid_input" });
    const r = await pool.query(
      `UPDATE products SET
        handle = COALESCE($2, handle),
        name = COALESCE($3, name),
        description_md = COALESCE($4, description_md),
        price_cents = COALESCE($5, price_cents),
        currency = COALESCE($6, currency),
        image_url = COALESCE($7, image_url),
        active = COALESCE($8, active),
        featured_rank = $9,
        updated_at = now()
      WHERE id=$1`,
      [
        id,
        nilIfBlankPtr(b.handle),
        nilIfBlankPtr(b.name),
        b.description_md ?? null,
        b.price_cents ?? null,
        nilIfBlankPtr(b.currency),
        nilIfBlankPtr(b.image_url),
        b.active ?? null,
        b.featured_rank ?? null,
      ]
    );
    if (r.rowCount === 0) return sendJson(reply, 500, { error: "db_error" });
    return sendJson(reply, 200, { ok: true });
  });

  app.delete<{ Params: { id: string } }>("/api/admin/products/:id", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const id = Number(req.params.id);
    if (!id) return sendJson(reply, 400, { error: "invalid_id" });
    await pool.query(`DELETE FROM products WHERE id=$1`, [id]);
    return sendJson(reply, 200, { ok: true });
  });

  app.get("/api/admin/orders", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const or = await pool.query(
      `SELECT id, email, status, total_cents, created_at FROM orders ORDER BY created_at DESC LIMIT 100`
    );
    return sendJson(reply, 200, {
      orders: or.rows.map((r) => ({
        id: Number(r.id),
        email: r.email,
        status: r.status,
        total_cents: Number(r.total_cents),
        created_at: r.created_at,
      })),
    });
  });

  app.get<{ Params: { id: string } }>("/api/admin/orders/:id", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const id = Number(req.params.id);
    if (!id) return sendJson(reply, 400, { error: "invalid_id" });

    const row = await pool.query(
      `SELECT id, email, status, currency, stripe_checkout_session_id, subtotal_cents, shipping_cents, tax_cents, total_cents,
              COALESCE(shipping_method,'') AS shipping_method, COALESCE(shipping_address,'{}'::jsonb) AS shipping_address, created_at
       FROM orders WHERE id=$1`,
      [id]
    );
    if (!row.rows[0]) return sendJson(reply, 404, { error: "not_found" });
    const r = row.rows[0];
    const shippingAddr = r.shipping_address as Record<string, unknown>;
    const stripeSession = String(r.stripe_checkout_session_id || "").trim();
    const order: Record<string, unknown> = {
      id: Number(r.id),
      email: r.email,
      status: r.status,
      currency: r.currency,
      stripe_checkout_session_id: r.stripe_checkout_session_id,
      subtotal_cents: Number(r.subtotal_cents),
      shipping_cents: Number(r.shipping_cents),
      tax_cents: Number(r.tax_cents),
      total_cents: Number(r.total_cents),
      shipping_method: r.shipping_method,
      shipping_address: shippingAddr,
      created_at: r.created_at,
      items: [] as Array<{ name: string; unit_price_cents: number; quantity: number }>,
    };

    if (isEmptyShippingAddr(shippingAddr) && stripeSession && cfg.stripeSecretKey) {
      const stripe = new Stripe(cfg.stripeSecretKey);
      let dbg: Record<string, unknown> = {};
      let fetchErr = "";
      let addr: Record<string, unknown> = {};
      try {
        const r = await fetchShippingAddressFromStripe(stripe, stripeSession);
        dbg = r.dbg;
        addr = r.addr;
      } catch (e) {
        fetchErr = e instanceof Error ? e.message : String(e);
      }
      if (process.env.APP_ENV !== "production") {
        order._debug = {
          stripe_fetch_error: fetchErr,
          stripe_checkout_session_id_present: true,
          stripe_debug: dbg,
        };
      }
      if (!fetchErr && !isEmptyShippingAddr(addr)) {
        order.shipping_address = addr;
        await pool.query(`UPDATE orders SET shipping_address=$2::jsonb WHERE id=$1`, [id, JSON.stringify(addr)]);
      }
    } else if (process.env.APP_ENV !== "production") {
      order._debug = {
        stripe_checkout_session_id_present: !!stripeSession,
        stripe_secret_configured: !!cfg.stripeSecretKey,
        shipping_address_empty: isEmptyShippingAddr(shippingAddr),
      };
    }

    const ir = await pool.query(
      `SELECT product_name_snapshot, unit_price_cents, quantity FROM order_items WHERE order_id=$1 ORDER BY id ASC`,
      [id]
    );
    order.items = ir.rows.map((x) => ({
      name: x.product_name_snapshot as string,
      unit_price_cents: Number(x.unit_price_cents),
      quantity: Number(x.quantity),
    }));

    return sendJson(reply, 200, { order });
  });

  app.post<{ Params: { id: string } }>("/api/admin/orders/:id/resend-confirmation", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const id = Number(req.params.id);
    if (!id) return sendJson(reply, 400, { error: "invalid_id" });
    const r = await pool.query(`SELECT email, stripe_checkout_session_id FROM orders WHERE id=$1`, [id]);
    if (!r.rows[0]) return sendJson(reply, 404, { error: "not_found" });
    let email = String(r.rows[0].email || "").trim();
    if (!email) return sendJson(reply, 400, { error: "missing_email" });
    const ref = String(r.rows[0].stripe_checkout_session_id || "").trim() || `order-${id}`;
    try {
      await sendOrderConfirmationEmail(pool, email, id, ref);
    } catch {
      return sendJson(reply, 502, { error: "email_send_failed" });
    }
    await pool.query(`UPDATE orders SET confirmation_email_sent_at = now() WHERE id=$1`, [id]);
    return sendJson(reply, 200, { ok: true });
  });

  app.get("/api/admin/collections", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const cr = await pool.query(`SELECT id, type::text, slug, name FROM collections ORDER BY type, slug`);
    const collections = cr.rows.map((c) => ({
      id: Number(c.id),
      type: c.type,
      slug: c.slug,
      name: c.name,
    }));
    const pc = await pool.query(`SELECT collection_id, product_id FROM product_collections`);
    const product_ids_by_collection_id: Record<string, number[]> = {};
    for (const r of pc.rows) {
      const cid = String(r.collection_id);
      if (!product_ids_by_collection_id[cid]) product_ids_by_collection_id[cid] = [];
      product_ids_by_collection_id[cid].push(Number(r.product_id));
    }
    return sendJson(reply, 200, { collections, product_ids_by_collection_id });
  });

  app.post("/api/admin/collections", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const b = req.body as { type?: string; slug?: string; name?: string };
    const t = (b.type || "").trim();
    const slug = (b.slug || "").trim();
    const name = (b.name || "").trim();
    if (!t || !slug || !name) return sendJson(reply, 400, { error: "invalid_input" });
    try {
      const r = await pool.query(`INSERT INTO collections (type, slug, name) VALUES ($1,$2,$3) RETURNING id`, [
        t,
        slug,
        name,
      ]);
      return sendJson(reply, 200, { id: Number(r.rows[0].id) });
    } catch {
      return sendJson(reply, 500, { error: "db_error" });
    }
  });

  app.put<{ Params: { id: string } }>("/api/admin/collections/:id", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const id = Number(req.params.id);
    if (!id) return sendJson(reply, 400, { error: "invalid_id" });
    const b = req.body as { type?: string; slug?: string; name?: string };
    await pool.query(
      `UPDATE collections SET
        type = COALESCE($2, type),
        slug = COALESCE($3, slug),
        name = COALESCE($4, name)
      WHERE id=$1`,
      [id, nilIfBlankPtr(b.type), nilIfBlankPtr(b.slug), nilIfBlankPtr(b.name)]
    );
    return sendJson(reply, 200, { ok: true });
  });

  app.delete<{ Params: { id: string } }>("/api/admin/collections/:id", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const id = Number(req.params.id);
    if (!id) return sendJson(reply, 400, { error: "invalid_id" });
    await pool.query(`DELETE FROM collections WHERE id=$1`, [id]);
    return sendJson(reply, 200, { ok: true });
  });

  app.put<{ Params: { id: string } }>("/api/admin/collections/:id/products", async (req, reply) => {
    if (!(await requireAdmin(req, reply))) return;
    const id = Number(req.params.id);
    if (!id) return sendJson(reply, 400, { error: "invalid_id" });
    const b = req.body as { product_ids?: number[] };
    const c = await pool.connect();
    try {
      await c.query("BEGIN");
      await c.query(`DELETE FROM product_collections WHERE collection_id=$1`, [id]);
      for (const pid of b.product_ids || []) {
        if (pid > 0) await c.query(
          `INSERT INTO product_collections (product_id, collection_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
          [pid, id]
        );
      }
      await c.query("COMMIT");
    } catch {
      await c.query("ROLLBACK");
      return sendJson(reply, 500, { error: "db_error" });
    } finally {
      c.release();
    }
    return sendJson(reply, 200, { ok: true });
  });

  return app;
}

async function queryProducts(pool: Pool, q: string): Promise<ProductRow[]> {
  let sql = `SELECT id, handle, name, description_md, price_cents, currency, COALESCE(image_url,'') AS image_url FROM products WHERE active = true`;
  const args: unknown[] = [];
  if (q) {
    args.push(`%${escapeLike(q)}%`);
    sql += ` AND (name ILIKE $1 OR description_md ILIKE $1)`;
  }
  sql += ` ORDER BY featured_rank NULLS LAST, created_at DESC`;
  const r = await pool.query(sql, args);
  return r.rows.map(mapProduct);
}

async function queryProductsByCollection(pool: Pool, collectionSlug: string, q: string): Promise<ProductRow[]> {
  const args: unknown[] = [collectionSlug];
  let sql = `
SELECT p.id, p.handle, p.name, p.description_md, p.price_cents, p.currency, COALESCE(p.image_url,'') AS image_url
FROM products p
JOIN product_collections pc ON pc.product_id = p.id
JOIN collections c ON c.id = pc.collection_id
WHERE p.active = true AND c.slug = $1`;
  if (q) {
    args.push(`%${escapeLike(q)}%`);
    sql += ` AND (p.name ILIKE $2 OR p.description_md ILIKE $2)`;
  }
  sql += ` ORDER BY p.featured_rank NULLS LAST, p.created_at DESC`;
  const r = await pool.query(sql, args);
  return r.rows.map(mapProduct);
}

async function queryProductByHandle(pool: Pool, handle: string): Promise<ProductRow> {
  const r = await pool.query(
    `SELECT id, handle, name, description_md, price_cents, currency, COALESCE(image_url,'') AS image_url
     FROM products WHERE handle=$1 AND active=true`,
    [handle]
  );
  if (!r.rows[0]) throw new Error("not_found");
  return mapProduct(r.rows[0]);
}

function mapProduct(r: Record<string, unknown>): ProductRow {
  return {
    id: Number(r.id),
    handle: r.handle as string,
    name: r.name as string,
    description_md: r.description_md as string,
    price_cents: Number(r.price_cents),
    currency: r.currency as string,
    image_url: r.image_url as string,
  };
}

async function queryAllProducts(pool: Pool, q: string): Promise<ProductRow[]> {
  const args: unknown[] = [];
  let sql = `SELECT id, handle, name, description_md, price_cents, currency, COALESCE(image_url,'') AS image_url FROM products WHERE 1=1`;
  if (q) {
    args.push(`%${escapeLike(q)}%`);
    sql += ` AND (name ILIKE $1 OR description_md ILIKE $1)`;
  }
  sql += ` ORDER BY active DESC, featured_rank NULLS LAST, created_at DESC`;
  const r = await pool.query(sql, args);
  return r.rows.map(mapProduct);
}

function parseListenAddr(addr: string): { host: string; port: number } {
  const a = addr.trim();
  if (a.startsWith(":")) {
    return { host: "0.0.0.0", port: parseInt(a.slice(1), 10) || 8788 };
  }
  const i = a.lastIndexOf(":");
  if (i <= 0) return { host: "0.0.0.0", port: 8788 };
  const host = a.slice(0, i).replace(/^\[/, "").replace(/\]$/, "");
  const port = parseInt(a.slice(i + 1), 10) || 8788;
  return { host: host || "0.0.0.0", port };
}

async function main() {
  const cfg = loadConfig();
  if (!cfg.databaseUrl) {
    console.error("DATABASE_URL is required");
    process.exit(1);
  }
  const pool = new Pool({ connectionString: cfg.databaseUrl });
  const app = buildApp(pool, cfg);
  const { host, port } = parseListenAddr(cfg.addr);
  await app.listen({ host, port });
  console.error(`api-node listening on http://${host}:${port}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
