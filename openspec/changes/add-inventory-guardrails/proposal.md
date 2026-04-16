# Change Proposal: Add Inventory Guardrails

## Summary

Add inventory tracking to Signal & Story so the storefront can show stock state, the admin can manage stock levels, and checkout can prevent overselling through stock-aware validation and temporary reservations.

## Problem

The current store supports products, collections, checkout sessions, Stripe checkout, and admin CRUD, but it has no concept of stock. That creates a few risks:

- Admins cannot record how many units are available for a product.
- Shoppers can add any quantity to cart as long as it is between 1 and 20.
- Checkout can succeed for items that should already be sold out.
- Concurrent shoppers can oversell the same product because there is no reservation or decrement flow.

## Goals

- Track sellable inventory per product.
- Show clear availability on product and catalog views.
- Let admins update inventory from the existing admin dashboard.
- Prevent checkout from creating sessions for unavailable quantities.
- Reserve inventory during checkout and finalize it exactly once when Stripe confirms payment.

## Non-Goals

- Multi-location inventory.
- Backorders or preorders.
- Supplier purchase orders or restock workflows.
- Variant-level inventory such as size or color combinations.

## Proposed Approach

1. Extend the product model with stock metadata and an availability status derived from inventory.
2. Add an inventory reservation table tied to local checkout sessions so concurrent shoppers cannot over-claim the same units.
3. Update checkout session creation to validate requested quantities against available inventory and create expiring reservations.
4. Update Stripe webhook fulfillment to convert reservations into committed stock reductions exactly once.
5. Add admin controls for stock quantity and low-stock threshold.
6. Surface availability badges and sold-out behavior in the storefront.

## Impact

### Backend

- New migration for inventory fields and reservation records.
- Product list/detail/admin endpoints return inventory state.
- Checkout session creation performs stock validation and writes reservations transactionally.
- Webhook processing finalizes inventory on successful payment and remains idempotent.

### Frontend

- Product cards and detail pages display availability labels.
- Cart and checkout prevent quantities above available stock.
- Admin product editor gains inventory controls and stock status feedback.

### Testing

- Add coverage for inventory math, reservation expiry, checkout rejection on insufficient stock, and webhook idempotency.

## Risks

- Reservation expiry needs a clear cleanup strategy so abandoned carts do not lock stock forever.
- Existing products need sensible defaults to avoid accidentally hiding all inventory after migration.
- Webhook retries must not decrement stock more than once.

## Rollout Notes

- Migrate existing products with a default stock policy agreed by the team.
- Prefer feature delivery in backend-first slices so storefront behavior can safely depend on the new fields.
