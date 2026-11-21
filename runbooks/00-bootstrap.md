# Gates (manual steps the orchestrator checks)

Generated from the gate definitions in SPEC §22; T0-04 replaces this file
with output rendered by `box`. Placeholders in braces are filled from the
configuration when rendered.

| ID | Step | Expected value | Check |
| --- | --- | --- | --- |
| G-00 | Sign up for Microsoft 365 ({default_plan} recommended) | A tenant you can sign in to as Global Administrator | Successful sign-in |
| G-01 | Buy the plans chosen for your team | Seats cover every person in step 6 | Subscribed plans read from the tenant |
| G-02 | Register two FIDO2 keys for each break-glass account {break_glass_accounts}; seal one set offline | Each account lists a FIDO2 method | Authentication methods read from the tenant |
| G-03 | As the flow owner, create the connections the solution asks for | One connection per connector in the allow-list | Connection references connected |
| G-04 | Create the public forms (NDA request, demo request) from `flows/forms/` | Form links pasted into the TUI | Forms reachable; IDs stored in configuration `ids` |
| G-05 | Remove the founder's standing Global Administrator role after T1 passes | Founder holds no directory role | Directory roles read from the tenant |
| G-06 | Add the DNS records shown (only when DNS is managed by hand) | Records resolve to the expected values | DNS lookups |
