# Inventory

## ADDED Requirements

### Requirement: Products Must Expose Inventory State

The system SHALL store inventory counts for each product and expose a derived availability state in product responses.

#### Scenario: In-stock product appears available

- **GIVEN** a product has `stock_on_hand` greater than zero
- **WHEN** the storefront fetches product list or detail data
- **THEN** the response includes inventory quantity
- **AND** the response includes an availability state of `in_stock`

#### Scenario: Sold-out product appears unavailable

- **GIVEN** a product has no sellable units remaining
- **WHEN** the storefront fetches product list or detail data
- **THEN** the response includes an availability state of `sold_out`
- **AND** purchase controls can be disabled without extra client-side inference

### Requirement: Admins Must Be Able To Manage Inventory

The admin product management flow SHALL allow an authorized admin to set stock quantity and low-stock threshold for each product.

#### Scenario: Admin updates stock

- **GIVEN** an authenticated admin is editing a product
- **WHEN** they submit updated inventory values
- **THEN** the system persists the new stock quantity and threshold
- **AND** later product and admin responses reflect the updated values

### Requirement: Checkout Must Reserve Available Inventory

The system SHALL validate requested cart quantities against currently available stock and create temporary reservations before returning a checkout session.

#### Scenario: Checkout session is created for available stock

- **GIVEN** a cart requests quantities that do not exceed available stock
- **WHEN** the client requests a checkout session
- **THEN** the system creates the local checkout session
- **AND** creates matching inventory reservations with an expiry time
- **AND** returns success

#### Scenario: Checkout is rejected when stock is insufficient

- **GIVEN** a cart requests more units than are currently available after active reservations are considered
- **WHEN** the client requests a checkout session
- **THEN** the system rejects the request
- **AND** returns a machine-readable insufficient-stock error

### Requirement: Inventory Reservations Must Expire

The system SHALL ensure abandoned checkout attempts do not reserve inventory indefinitely.

#### Scenario: Expired reservation no longer reduces availability

- **GIVEN** a checkout session reservation has passed its expiry time without a completed payment
- **WHEN** availability is calculated for a later shopper
- **THEN** the expired reservation is ignored
- **AND** those units become sellable again

### Requirement: Paid Orders Must Commit Inventory Exactly Once

The system SHALL convert reserved inventory into committed stock deductions exactly once after Stripe confirms a paid checkout session.

#### Scenario: First successful webhook decrements stock

- **GIVEN** a paid Stripe checkout session maps to an existing local checkout session with reservations
- **WHEN** the first valid `checkout.session.completed` event is processed
- **THEN** product stock is decremented by the reserved quantities
- **AND** the reservations are marked finalized

#### Scenario: Duplicate webhook does not double-decrement stock

- **GIVEN** inventory was already finalized for a paid checkout session
- **WHEN** the same webhook event or a retried equivalent event is processed again
- **THEN** the system does not decrement stock a second time
- **AND** the order remains consistent
