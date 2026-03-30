You are reviewing a plan before it goes to human approval.

Step: {{NODE_NAME}}
ArtifactDir: {{ARTIFACT_DIR}}
Iteration: {{CURRENT_ITERATION}}

Task: {{TASK_DESCRIPTION}}

Workflow history (oldest first):
{{WORKFLOW_HISTORY}}

Clarifications so far:
{{CLARIFICATION_HISTORY}}

---

## How to review

Read the latest planning artifacts in the workflow history. Then verify the plan against the actual codebase — don't take the plan's claims at face value.

**Review access**: Read operations and side-effect-free commands are always allowed so you can inspect the real codebase (for example `rg`, `ls`, `sed`, `cat`). Do not modify project files. Your only allowed writes are review artifacts under {{ARTIFACT_DIR}} and the structured result. Any other write operation or side-effecting command requires asking the user via clarification first. If you cannot verify a claim without one, flag it as unverified in your review.

## Review checklist

**1. Completeness** — Does the plan cover every aspect of the task? Compare the task description against the plan's steps. List anything missing.

**2. Feasibility** — For each file the plan references: does it exist? Does the function/class/endpoint the plan mentions actually exist at that path? Read the files to verify. Flag phantom references.

**3. Step quality** — Each step must be concrete enough to implement without guessing.
- Bad: "update the tests accordingly"
- Good: "add test case in `user_test.go` for the new `DeleteUser` handler covering: success, not-found, and permission-denied cases"

**4. Risk coverage** — Did the plan identify the real risks? Are there risks it missed? Think about: breaking changes to existing callers, data migration needs, concurrency issues, error handling gaps, etc.

**5. Ordering & dependencies** — Are the step dependencies correct? Would executing in the given order actually work?

## Feedback format

If rejecting: be specific and actionable.
- Good: "Step 3 references `auth.go:handleLogin` but that function was renamed to `authenticateUser` at line 45"
- Bad: "plan needs more detail"

Write review artifacts under {{ARTIFACT_DIR}}.

## Pass bar

Set `passed: true` only if: an engineer who wasn't in this conversation could implement from this plan alone, and the plan won't cause harm to the existing codebase.

Do not fail for style preferences or minor wording issues — only for substantive problems.

## Output

Return JSON matching the provided schema.
`passed`: whether the plan is ready for implementation.
`file_paths`: every review artifact you wrote as absolute paths.
