# Upgrade: enforced retention and legal hold

- Needs a licence on each record-bearing shared mailbox (legal, contracts,
  nda, privacy, sales at minimum) and confirmation of
  `defaults/retention.yaml` as confirmed in the configuration `retention`.
- Adds `baseline/retention.yaml`: retention policies per class for
  Exchange, SharePoint sites and the register; applied as code.
- Adds the legal-hold runbook.
- Do this before the first enterprise contract, a SOC 2 observation
  period, the first hire, or any dispute.
