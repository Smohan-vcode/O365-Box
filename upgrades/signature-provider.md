# Upgrade: signature provider

Replaces child flow F05 `Signature` and trigger of F06.
In `document_id`; out `sent_at`, later `executed_file`.

- Choose a provider. If it has a standard connector, no licence is needed;
  otherwise Power Automate Premium for the flow owner.
- F05 sends the filled document to the signers and stores the provider's
  envelope id on the Documents item.
- A new flow on the provider's completion trigger saves the signed PDF and
  audit trail and calls the existing filing steps of F06.
- Declined and voided map to DECLINED and VOIDED.
- Exit: a READY document reaches EXECUTED with no manual send or upload.
