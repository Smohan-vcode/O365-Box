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

## Build and run

Requires Go 1.24 or later and PowerShell 7.4 or later.

```bash
go build -o bin/box ./engine/cmd/box
```

Check prerequisites and install the pinned Microsoft modules:

```bash
bin/box doctor --install
```

Start or continue onboarding (opens the terminal UI):

```bash
bin/box
```

Headless commands, against a configuration file until the tenant store
exists (T0-06):

```bash
bin/box plan --config examples/acme/box-config.yaml
```

```bash
bin/box up --config <file> --yes
```

`box status`, `box drift`, `box apply <step>`, `box check <step>` and
`box gate <id> [--check|--confirm]` take the same flags. Exit codes: 0 ok,
1 failure, 2 validation error, 3 waiting on a gate.

## Develop

```bash
BOX_MODULE_PATH=<dir with the pinned modules and Pester> go test -race ./...
```

```bash
bin/box check-literals && bin/box check-connectors && bin/box backlog check
```

`box backlog index` regenerates `tasks/INDEX.md`; `box backlog done <id>`
marks a task done once its dependencies are done. CI (`.github/workflows/ci.yml`)
runs all of the above on every push.
