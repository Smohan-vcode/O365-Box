# Working instructions for Claude Code

Read `docs/SPEC.md` first. It governs. Then work through `tasks/` and
`spikes/` in dependency order. `tasks/INDEX.md` lists them.

This repository is public and contains the engine only. It must never hold
a real company's data: names, addresses, domains, tenant IDs, people,
legal pack files or secrets. Use `examples/acme/` (fictional) for tests.

## Order of work

1. Pick the lowest-numbered task or spike whose `depends_on` entries all
   have `status: DONE`.
2. If the task lists `config_required`, run `box config validate --for
   <task>` against the test tenant's configuration. Stop if it fails and
   report the empty values.
3. Do the steps. Run the exit criteria. Write the evidence.
4. Set `status: DONE` in the task's front matter only when every exit
   criterion passes, and regenerate `tasks/INDEX.md`.
5. One task per commit, task ID in the message.

## Stop and report

- A step is listed under `Human steps`.
- A required configuration value is empty.
- A spike's answer is no, or differs from what a dependent task assumes.
- An action would touch a tenant whose `tenant.environment` is not
  `test`.
- An exit criterion fails twice.
- A flow would need a premium connector. Do not add it. Record the need in
  `upgrades/` and stop.

## Rules

- Design rules R1 to R12 apply to every change.
- Run `go test ./...`, `box check-literals` and `box check-connectors`
  before each commit.
- Never assign a paid licence that the configuration does not name.
- Never grant the pipeline identity a permission that is not listed in
  `expected-state/app-permissions.yaml`; propose the addition and stop.
- Never make a break-glass account subject to Conditional Access.
- Never store or log a field marked `never_store`, a secret, a first
  sign-in pass or a personal email address.
- `Events` items are created, never changed.
- Do not edit `docs/SPEC.md` to make a task pass. Propose the change in
  `docs/spec-changes.md` and stop.

## Environments

Build and verify each task on the test tenant. Apply to live only after the
task is `DONE` on test and the owner has approved the pull request.
