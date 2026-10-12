# Autonomy-First Product Review

Reviews the product through the lens of "how much can run safely without a human,
and are the humans-in-the-loop placed where they matter?"

## What runs autonomously today

- Source ingestion, memory dedup/retrieval, planning, and grounded verification
  run without human input.
- Scheduling (sources, workflows, ambient) runs on timers.

## Where humans are required (by design)

- **Approval gates** before real side effects.
- **Autonomy gate** (`internal/autonomygate`): risky/irreversible actions never
  auto-run; low-confidence routes to review.
- **Ambiguous actions** (`internal/actionresolver`): missing params → clarify;
  destructive + low confidence → block.
- **Paid/real providers** stay disabled until explicit approval.

## Assessment

| Dimension | Verdict |
| --- | --- |
| Safe defaults | Strong — fail toward review/block, production-strict. |
| Human decision minimization | Good — `autonomygate` auto-approves only safe, reversible, high-confidence cases. |
| Escape hatch | Strong — emergency stop blocks new execution immediately. HAI cancels an in-flight run only where the adapter retains a supported cancellation handle; otherwise the run remains bounded by its configured timeout and is recorded for review. |
| Transparency | Good — grounded answers, quality bands, audit trail. |

## Gaps

1. The autonomy gate and action resolver are **implemented and tested but not yet
   wired** into the live workflow execution path — wiring them is the highest-
   value autonomy improvement (turns policy into enforced behavior).
2. Exception-based dashboard (surfacing only what needs a human) is a frontend
   gap (109).

## Verdict

The product is designed autonomy-first with the right human checkpoints. The
remaining work is wiring the decision logic into execution and surfacing
exceptions in the UI — not rethinking the model.
