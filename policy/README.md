# Policy sets

One catalogue of requirements, one catalogue of settings, and a resolver
that turns a company's selection into what gets applied. The catalogue is
engine data and holds no company values (R1).

## The four files that matter

| File | Holds |
| --- | --- |
| `requirements.yaml` | 90 requirements in 14 domains. Each has a plain statement, a tier, a type, the settings that enforce it, its evidence source, and references to five frameworks. |
| `settings/<centre>.yaml` | 334 settings across 12 admin centres. Each names the requirements it serves, the licence capability it needs, and whether it starts in audit. |
| `frameworks/<id>.yaml` | Each framework's controls and the requirements that answer them. |
| `capabilities.yaml` | What each capability means, which plans include it and which need an add-on. The plan lists must agree with `engine/capabilities.yaml`. |

## The selection

The selection is the `policy` section of the company's configuration
document (SPEC §9.1). It lives in the tenant with the rest of the
configuration, is changed in the TUI, and every save is a new version
with an `Events` item. It is never kept in this repository.

```yaml
policy:
  frameworks: [soc2, nist_csf_2]   # ids of files in frameworks/
  tier: core                       # core | enhanced
  rollout:
    stage: audit                   # audit | enforce; staged settings follow this
    audit_days: 14
    pilot_group: grp-admins        # an access group in groups
  exceptions:
  - id: TMS-005                    # a setting id, or a requirement id
    reason: Why the departure is needed
    reference: CHG-0001            # the change or risk record
    ends: '2099-12-31'             # YYYY-MM-DD; an expired exception fails validation
```

## How a selection becomes applied settings

1. `box policy validate` checks the catalogue's rules (below).
2. `box config validate` checks the selection: known frameworks, known
   exception ids, no expired exception, a defined pilot group.
3. `box policy resolve --config <file>` keeps a setting when the plan has
   its capability, its tier is selected and no exception removes it. A
   setting exception removes that setting; a requirement exception
   removes every setting that serves only excepted requirements. With
   `--out <dir>` it writes `<centre>.yaml` (to apply), `not-applied.yaml`
   (left out, with the reason) and `coverage.md` (what each selected
   framework gets) into a directory outside the repository.
4. The apply tasks push the resolved settings to the tenant. Staged
   settings go out in audit first and move to enforce when
   `rollout.stage` changes.
5. Collectors gather the evidence named on each requirement.

`box policy coverage --config <file>` prints the coverage report alone.

## Requirement status in the coverage report

| Status | Meaning |
| --- | --- |
| enforced | A technical requirement with at least one applied setting. |
| enforced with procedure | A mixed requirement with at least one applied setting; its procedure still applies. |
| procedure | A procedural requirement, or one with no applied setting that names a procedure. |
| needs add-on | An add-on requirement with no applied setting. |
| not selected | No applied setting and no procedure: the tier or plan leaves it out. |
| exception | Removed by an entry in `policy.exceptions`. |

## What ticking a framework changes

The technical baseline is the same whichever frameworks are ticked: sound
practice overlaps almost completely. Ticking a framework switches on:

- its coverage report, control by control;
- its procedural requirements as recurring tasks;
- its evidence collection schedule;
- its reporting deadlines on incidents.

## Tiers

| Tier | Meaning |
| --- | --- |
| core | Always applied. |
| enhanced | Stricter settings that cost users some convenience. Applied when `policy.tier` is `enhanced`. |
| addon | Needs a paid capability. Never applied; listed so the gap is visible. |

## Rules

`box policy validate` enforces these:

- Ids are unique: requirements, settings (across every centre) and the
  controls of each framework. A framework's id matches its file name.
- A setting serves at least one requirement, and every requirement it
  names exists and lists it back.
- A technical requirement has at least one setting. Every setting a
  requirement lists exists and names it back.
- Every control's requirements exist and refer back to the control, and
  every framework reference on a requirement is a control of that
  framework (a NIST CSF subcategory such as `PR.AA-03` belongs to its
  category `PR.AA`).
- Tiers, types and modes take the values above; every setting's
  capability is in `capabilities.yaml`; an addon setting needs a
  capability that no plan includes; `included_in` agrees with
  `engine/capabilities.yaml`.

And by design:

- Nothing that needs a paid capability is applied on a plan that lacks it.
- Every departure is an entry in `policy.exceptions` with a reference and
  an end date.
- Framework references are a working mapping to be validated against the
  source texts before an audit relies on them.
- Values are stated in words. The setting resolution task resolves each
  to the exact API setting on the tenant and records it.

## Adding a framework

Add `frameworks/<id>.yaml` listing its controls and the requirement ids
that answer each, and add references on the requirements. The schema
takes any framework id; `box config validate` checks it against this
directory. No setting changes unless the framework needs a new
requirement.
