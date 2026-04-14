(() => {
  const LS_KEY = "sas_cart_v1";

  const read = () => {
    try {
      return JSON.parse(localStorage.getItem(LS_KEY) || "[]");
    } catch {
      return [];
    }
  };

  const write = (items) => {
    localStorage.setItem(LS_KEY, JSON.stringify(items));
    renderDrawer();
  };

  const money = (cents) => `£${(cents / 100).toFixed(2)}`;

  const count = () => read().reduce((acc, it) => acc + (it.quantity || 0), 0);

  const add = (product) => {
    const items = read();
    const idx = items.findIndex((x) => x.handle === product.handle);
    if (idx >= 0) items[idx].quantity += 1;
    else items.push({ handle: product.handle, name: product.name, price_cents: product.price_cents, image_url: product.image_url, quantity: 1 });
    write(items);
  };

  const inc = (handle) => {
    const items = read();
    const it = items.find((x) => x.handle === handle);
    if (it) it.quantity += 1;
    write(items);
  };

  const dec = (handle) => {
    const items = read();
    const it = items.find((x) => x.handle === handle);
    if (!it) return;
    it.quantity -= 1;
    if (it.quantity <= 0) {
      write(items.filter((x) => x.handle !== handle));
      return;
    }
    write(items);
  };

  const remove = (handle) => write(read().filter((x) => x.handle !== handle));

  const subtotal = () => read().reduce((acc, it) => acc + (it.price_cents || 0) * (it.quantity || 0), 0);

  const openDrawer = () => {
    const el = document.getElementById("cart-drawer");
    if (el) el.style.right = "0px";
    renderDrawer();
  };
  const closeDrawer = () => {
    const el = document.getElementById("cart-drawer");
    if (el) el.style.right = "-420px";
  };

  const renderDrawer = () => {
    const root = document.getElementById("cart-drawer-items");
    if (!root) return;
    const items = read();
    if (!items.length) {
      root.innerHTML = `<div class="tag">Your cart is empty.</div>`;
      return;
    }
    root.innerHTML = `
      <div class="grid" style="gap:10px">
        ${items
          .map(
            (it) => `
          <div class="card" style="padding:12px;display:grid;gap:10px">
            <div style="display:flex;gap:10px;align-items:center">
              <div class="card" style="width:54px;height:54px;border-radius:14px;overflow:hidden;background:rgba(255,255,255,.03);border:1px solid var(--line);position:relative">
                ${
                  it.image_url
                    ? `<img src="${it.image_url}" alt="" onerror="this.remove()" style="position:absolute;inset:0;width:100%;height:100%;object-fit:cover;display:block" />`
                    : ""
                }
              </div>
              <div style="flex:1">
                <div style="font-weight:700">${it.name}</div>
                <div style="color:var(--muted);font-size:12px">${money(it.price_cents)} each</div>
              </div>
            </div>
            <div style="display:flex;align-items:center;justify-content:space-between;gap:10px">
              <div style="display:flex;gap:8px;align-items:center">
                <button class="btn secondary" type="button" onclick="window.cart.dec('${it.handle}')">-</button>
                <span class="tag">${it.quantity}</span>
                <button class="btn secondary" type="button" onclick="window.cart.inc('${it.handle}')">+</button>
              </div>
              <button class="btn secondary" type="button" onclick="window.cart.remove('${it.handle}')">Remove</button>
            </div>
          </div>
        `
          )
          .join("")}
        <div class="card" style="padding:12px;display:flex;justify-content:space-between">
          <span style="color:var(--muted)">Subtotal</span>
          <strong>${money(subtotal())}</strong>
        </div>
      </div>
    `;
  };

  window.cart = { read, write, add, inc, dec, remove, subtotal, count, openDrawer, closeDrawer };

  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeDrawer();
  });
})();

