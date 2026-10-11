# Completion-First Context and Routing Blueprint

This blueprint defines the implementation path for 018-HAI task handling. The
primary rule is completion first: the system may optimize for tokens, compute,
and cost only after it has selected a path that is likely to complete and verify
the task.

## Pipeline

1. Task intake
   - classify the request by type, risk, difficulty, reasoning need, project,
     context need, tool need, and approval need
   - require explicit success criteria; infer conservative defaults when the
     user does not provide them
   - produce a visible intake record before execution

2. Context planning
   - retrieve only relevant project context through `/api/v1/memory/retrieve`
   - rank context by keyword relevance, project match, recency, and confidence
   - return source references with memory records where available
   - exclude low-scoring unrelated memories from task prompts

3. Layered memory
   - working memory: active completion plan and current validation status
   - episodic memory: task plans, actions, results, and incidents
   - semantic memory: stable facts, user preferences, and domain rules
   - project memory: repository decisions, local setup notes, and service state
   - procedural memory: recurring workflows and validation patterns

4. Memory consolidation
   - store useful post-task facts only when they improve future work
   - deduplicate exact repeats by content hash
   - merge highly similar records into compact summaries
   - archive outdated or conflicting records for review instead of loading them

5. Model routing
   - route through `/api/v1/llm/route`
   - select the cheapest capable model, not the cheapest available model
   - skip models that are below task difficulty or reasoning requirements
   - escalate only after validation failure or capability mismatch
   - keep paid usage disabled by default

6. Validation
   - validate all explicit success criteria
   - validate required fields and structured outputs
   - run tests or build checks for code changes when possible
   - verify time-sensitive claims with current sources when needed
   - retry, escalate, or request review when validation fails

7. Controlled execution
   - separate planning from execution
   - require approval for high-risk actions
   - log every action, result, validation outcome, and memory proposal
   - mark a task complete only after validation passes

8. Dashboard visibility
   - the **Task Blueprint** page calls `/api/v1/task/plan`
   - it shows success criteria, selected context, selected model, skipped model
     reasons, validation steps, execution controls, completion state, and memory
     updates proposed for consolidation

## API Surface

- `POST /api/v1/task/plan`: build a completion-first task plan
- `GET /api/v1/task/logs?limit=10`: read bounded compact planning decisions
- `POST /api/v1/memory/retrieve`: retrieve relevant context only
- `POST /api/v1/llm/route`: choose and explain model selection

## Completion Gate

A task cannot be considered complete until:

- success criteria are explicit
- relevant context has been retrieved or intentionally skipped with a reason
- model selection is explained and logged
- validation has passed or a human review is requested
- memory updates are proposed rather than blindly stored
