# Universal Task Success Engine

The task success engine is the operational loop for every incoming or deduced
task. Its rule is completion first: the system optimizes cost, tokens, and
compute only after it has a path that can satisfy and verify the task.

## Engine Loop

1. Understand the request and infer the real goal.
2. Generate explicit success criteria when the caller did not provide them.
3. Retrieve relevant memory and project context only.
4. Classify risk, difficulty, reasoning need, and required capabilities.
5. Route to a capable model before choosing the lowest-cost option.
6. Route tools and block unsafe tools by default.
7. Build an ordered plan with approval gates.
8. Execute only allowed low-risk steps.
9. Validate the result against the success criteria.
10. Retry, escalate, or queue human review when validation fails.
11. Log every model, tool, risk, validation, and memory decision.
12. Store useful lessons in local memory when the task is validated.

## API Surface

- `POST /api/v1/task/plan`: creates a non-executing success plan.
- `POST /api/v1/task/run`: runs the allowed parts of the success engine and
  validates the result.
- `POST /api/v1/task/success`: alias for running the success engine.
- `GET /api/v1/task/logs?limit=10`: lists compact recent task-plan and run
  summaries. The default is 10 and the maximum is 50; full execution and
  context graphs are intentionally excluded from this list response.
- `GET /api/v1/task/review-queue`: lists blocked or unresolved tasks needing
  human review.

## Completion States

- `planned`: the system prepared a structured completion path.
- `validated`: the allowed run passed validation and can be treated as done.
- `retry_needed`: validation failed but another attempt is still available.
- `review_required`: approval is required or validation could not be resolved.

Unresolved tasks must never be treated as complete. They remain visible in the
review queue with the reason and priority.
