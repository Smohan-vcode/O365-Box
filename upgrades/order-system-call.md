# Upgrade: order system call

Replaces child flow F08 `Order hand-off`.
In `document_id`; out `external_licence_id`, `external_invoice_id`.

- Needs Power Automate Premium for the flow owner (web request), or a
  standard connector if the order system exposes one.
- Idempotent on the order id; three retries; failure raises the owner task
  that version 1 uses.
