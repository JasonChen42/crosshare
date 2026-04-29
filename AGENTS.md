# AGENTS.md

## Working Agreements

When multiple solutions exist, evaluate whether the optimal solution is feasible before considering inferior alternatives. Prefer the optimal solution by default. If it is materially more expensive in time, complexity, or risk, ask for confirmation before proceeding. Never silently downgrade to a suboptimal solution.

When elevated access is required, prefer a graphical authentication prompt so the user can enter credentials directly. If graphical authentication is unavailable, say so plainly instead of silently falling back to blocked terminal `sudo` attempts or sandbox-only workarounds.

## Project Structure And Progress

If you need to understand the current CrossHare structure, project progress, runtime setup, recent implementation details, or the speech-service integration plan, read:

```text
docs/progress/2026-04-25-daily-update.md
```

Keep that document updated with new daily progress records or add a newer dated file under `docs/progress/` when the project state changes materially.
