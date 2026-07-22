# finops skill

A Claude Code / Claude agent **Skill** that turns a telescope `report.json` into
prioritized FinOps recommendations (rightsizing, idle reclamation, consolidation)
with dollar-savings estimates.

telescope collects the facts (Stage A); this skill supplies the judgment
(Stage B). The recommendation logic lives entirely in [`SKILL.md`](./SKILL.md) —
telescope itself stays deterministic and data-only.

## Install

Copy this directory into your personal or project skills folder:

```bash
# personal (available in every project)
cp -r .claude/skills/finops ~/.claude/skills/

# or project-scoped (checked in for a team)
cp -r .claude/skills/finops <your-repo>/.claude/skills/
```

It is auto-active for anyone working inside the telescope repo. Verify with
`/finops` or by asking Claude to "review my cloud cost with telescope."

## Contents

- `SKILL.md` — trigger conditions, workflow, and recommendation heuristics.
- `references/sample-report.json` — a real `telescope scan` report for grounding.

## Use

Ask naturally — "find idle VMs", "rightsize my fleet", "review cloud spend" — or
point at an existing report: "run the finops skill on ./report.json". The skill
will run a scan (or read your report), reason over the utilization + cost, and
return a ranked table of actions.
