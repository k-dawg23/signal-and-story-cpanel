## 1. Data model

- [ ] Add product inventory fields such as `stock_on_hand` and `low_stock_threshold`.
- [ ] Add an `inventory_reservations` table keyed to checkout session and product.
- [ ] Define cleanup rules for expired reservations.

## 2. Backend API

- [ ] Return derived availability data from product list/detail endpoints.
- [ ] Update admin product create/update handlers to accept inventory fields.
- [ ] Validate stock and create reservations transactionally during checkout session creation.
- [ ] Finalize stock deductions during successful Stripe webhook processing with idempotent behavior.

## 3. Storefront

- [ ] Show stock badges on collection cards and product detail pages.
- [ ] Disable purchase actions for sold-out products.
- [ ] Prevent checkout from requesting more units than are available.
- [ ] Show clear error messaging when stock changes before checkout begins.

## 4. Admin UX

- [ ] Add stock quantity and low-stock inputs to the admin product editor.
- [ ] Highlight low-stock and sold-out products in the dashboard.

## 5. Verification

- [ ] Add tests for stock calculations and reservation expiry behavior.
- [ ] Add tests for insufficient-stock checkout rejection.
- [ ] Add tests proving webhook retries do not double-decrement inventory.
