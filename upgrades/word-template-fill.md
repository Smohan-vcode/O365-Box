# Upgrade: fill templates including alternatives

Replaces child flow F03 `Fill`. Input and output stay the same:
in `document_id`; out `file_url`, `needs_manual_edit` (then always false).

- Needs: Power Automate Premium for the flow owner (premium connector).
- Change: F03 uses the Word template action with repeating and conditional
  sections; `choice` bindings in `brackets.csv` become conditional content.
- Template build adds a second output per template for this action.
- Remove the manual-edit gate in F01 for templates built this way.
- Exit: a document from a template with alternatives is produced with only
  the chosen wording and no manual step.
