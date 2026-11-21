# Company in a box: Microsoft 365 engine

Stands up and hardens any company's operating system on Microsoft 365,
free-first, with settings as code and business processes as Power Automate
flows. The owner runs one command, `box`. A terminal UI signs them in,
discovers what Microsoft already knows, asks for the rest in plain
language, shows the full plan and builds it.

- Specification: `docs/SPEC.md` (v1.1); change record: `docs/spec-changes.md`
- Working instructions for Claude Code: `CLAUDE.md`
- Development backlog: `tasks/INDEX.md`; spikes: `spikes/`
- Engine defaults: `defaults/`, `baseline/`, `flows/`, `expected-state/`
- Fictional example configuration: `examples/acme/`
- Paid capabilities deferred: `upgrades/`

This repository holds no company data. A company's configuration is
collected by the TUI and stored in that company's own tenant.

## Build

Requires Go 1.24 or later.

```bash
go build -o bin/box ./engine/cmd/box
```

```bash
bin/box config validate examples/acme/box-config.yaml
```

```bash
bin/box check-literals && bin/box check-connectors
```
