Handle the current request.

Step: {{NODE_NAME}}
ArtifactDir: {{ARTIFACT_DIR}}
Iteration: {{CURRENT_ITERATION}}

Task
```
{{TASK_DESCRIPTION}}
```

Workflow history (oldest first):
{{WORKFLOW_HISTORY}}

Clarifications so far:
{{CLARIFICATION_HISTORY}}

---

Do the work the user asked for.

Use the workflow history and clarifications above when they are relevant.

Always write at least one result artifact under {{ARTIFACT_DIR}}. The artifact can be a short implementation summary, review note, analysis memo, or other result that matches the request.

Return only artifact paths under {{ARTIFACT_DIR}} in `file_paths`.
Before returning `kind="result"`, make sure `result.file_paths` lists every artifact you wrote as absolute paths.
