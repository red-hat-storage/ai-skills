# AI Skills

This repository is the shared home for team-authored agent guidance. It keeps material visible and reusable across contributors instead of maintaining separate copies in different locations.

## Content categories

- **Cases** capture reusable context from support and QE tickets. Add them under `cases/`.
- **Flows** give step-by-step instructions for completing a goal. Add them under `flows/`.
- **Lore** adds domain knowledge to the agent. Store each lore skill in `lore/<skill-name>/` with a `SKILL.md` and a `questions.yaml` file.

## Lore evaluations

Only lore skills are evaluated today. Each `questions.yaml` contains cases with an `id`, `polarity` (`positive` or `negative`), `question`, and `expected` answer. The runner compares answers with and without the skill, then grades accuracy, coverage, and unsupported claims. Questions live beside their skill so the evaluation is maintained once with the knowledge it checks.

Run the live evaluations with:

```sh
 OPENAI_API_KEY=... go test -v -run TestEval
```

The runner uses `gpt-6-luna` by default. Set `OPENAI_MODEL` to select another response model. Results are written as root-level timestamped YAML files and ignored by Git. A small runner and per-skill YAML questions keep the early evaluation process simple. Keeping skills and questions in this shared repository lets the team maintain each evaluation once instead of duplicating it across locations.

## Minimal Guidelines

- Avoid repeating general knowledge likely present in model training, such as what OCP and ODF are or basic `oc` and `kubectl` usage. Focus on information that changes the answer.
- State the skill's scope and activation conditions, including when it does not apply. Clear boundaries help the agent choose among many available skills.
- Lead with actionable, task-specific instructions and decision criteria. Remove general advice that does not change the next step.
- Ground project-specific claims in reliable sources. Note version-dependent behavior and what to verify when uncertain, so the agent does not guess between variants.
- Follow the [Agent Skills specification](https://agentskills.io/specification) when a skill includes supporting files or directories such as `scripts/`, `references/`, or `assets/` alongside `SKILL.md`.
