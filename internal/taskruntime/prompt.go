package taskruntime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

func completedArtifactPaths(runs []taskdomain.NodeRun) []string {
	var artifacts []string
	for _, run := range runs {
		if run.Status != taskdomain.NodeRunDone {
			continue
		}
		artifacts = append(artifacts, taskdomain.ArtifactPaths(run.Result)...)
	}
	return artifacts
}

func buildPrompt(task taskdomain.Task, cfg *taskconfig.Config, configPath string, runs []taskdomain.NodeRun, run taskdomain.NodeRun, artifactDir string) (string, error) {
	iteration := runIteration(runs, run)
	if shouldResumeClarificationThread(run) {
		return buildClarificationResumePrompt(task, run, artifactDir, iteration)
	}
	def := cfg.NodeDefinitions[run.NodeName]
	template, err := taskconfig.ReadPromptText(configPath, def)
	if err != nil {
		return "", err
	}
	artifactPaths := completedArtifactPaths(runs)
	completedResults := summarizeCompletedResults(runs)
	workflowHistory := summarizeWorkflowHistory(runs)
	clarificationHistory := summarizeClarificationHistory(run.Clarifications)
	replacer := strings.NewReplacer(
		"{{NODE_NAME}}", run.NodeName,
		"{{CURRENT_ITERATION}}", fmt.Sprintf("%d", iteration),
		"{{TASK_DESCRIPTION}}", task.Description,
		"{{WORKFLOW_HISTORY}}", workflowHistory,
		"{{ARTIFACT_PATHS}}", joinLines(artifactPaths),
		"{{COMPLETED_RESULTS}}", completedResults,
		"{{CLARIFICATION_HISTORY}}", clarificationHistory,
		"{{ARTIFACT_DIR}}", artifactDir,
	)
	return replacer.Replace(template), nil
}

func shouldResumeClarificationThread(run taskdomain.NodeRun) bool {
	if strings.TrimSpace(run.SessionID) == "" {
		return false
	}
	if len(run.Clarifications) == 0 {
		return false
	}
	return run.Clarifications[len(run.Clarifications)-1].Response != nil
}

func buildClarificationResumePrompt(task taskdomain.Task, run taskdomain.NodeRun, artifactDir string, iteration int) (string, error) {
	latest := run.Clarifications[len(run.Clarifications)-1]
	lines := []string{buildPromptHeader(run.NodeName, artifactDir, iteration), "", "Mission", "Resume this same step after the user answered your clarification request.", "", "Task", "```", task.Description, "```", "", "Relevant History", "Continue in the existing session or thread for this node run.", "Workflow history for this follow-up lives in the thread context above.", "", "New clarification exchange"}
	for qi, question := range latest.Request.Questions {
		lines = append(lines, fmt.Sprintf("Q: %s", question.Question))
		if len(question.Options) > 0 {
			lines = append(lines, "Options offered:")
			for _, opt := range question.Options {
				if opt.Description != "" {
					lines = append(lines, fmt.Sprintf("  - %s: %s", opt.Label, opt.Description))
				} else {
					lines = append(lines, fmt.Sprintf("  - %s", opt.Label))
				}
			}
		}
		if latest.Response != nil && qi < len(latest.Response.Answers) {
			answerData, err := json.Marshal(latest.Response.Answers[qi].Selected)
			if err == nil {
				lines = append(lines, fmt.Sprintf("User selected: %s", string(answerData)))
			}
		}
		lines = append(lines, "")
	}
	lines = append(lines,
		"Pass Bar",
		"Stay in the same thread context and continue from where this node paused.",
		"Use the new clarification answers instead of restating the previous question.",
		"Return the next structured output for this same step.",
		"",
		"Evidence To Inspect",
		"Use the existing thread context, the current node artifacts, and the answers above before deciding on any next action.",
		"",
		"Produce",
		fmt.Sprintf("Continue writing this step's artifacts under %s.", artifactDir),
		"Return the next structured output for this same step.",
		"",
		"Ask For Clarification Only When",
		"The user's latest response still leaves a genuinely blocking decision unresolved.",
	)
	return strings.Join(lines, "\n"), nil
}

func buildPromptHeader(nodeName, artifactDir string, iteration int) string {
	return strings.Join([]string{
		fmt.Sprintf("Step: %s", nodeName),
		fmt.Sprintf("ArtifactDir: %s", artifactDir),
		fmt.Sprintf("Iteration: %d", iteration),
	}, "\n")
}

func summarizeWorkflowHistory(runs []taskdomain.NodeRun) string {
	entries := make([]string, 0, len(runs))
	ordinals := map[string]int{}
	step := 0
	for _, run := range runs {
		ordinals[run.NodeName]++
		if run.Status != taskdomain.NodeRunDone {
			continue
		}
		step++
		label := fmt.Sprintf("%s (#%d)", run.NodeName, ordinals[run.NodeName])
		entryLines := []string{fmt.Sprintf("%d. %s", step, label)}
		if len(run.Result) > 0 {
			if data, err := json.Marshal(run.Result); err == nil {
				entryLines = append(entryLines, fmt.Sprintf("   Result JSON: %s", string(data)))
			}
		} else {
			entryLines = append(entryLines, "   Result JSON: (none)")
		}
		entries = append(entries, strings.Join(entryLines, "\n"))
	}
	return joinLines(entries)
}

func runIteration(runs []taskdomain.NodeRun, current taskdomain.NodeRun) int {
	ordinal := 0
	for _, run := range runs {
		if run.NodeName == current.NodeName {
			ordinal++
		}
		if run.ID == current.ID {
			if ordinal < 1 {
				return 1
			}
			return ordinal
		}
	}
	ordinal++
	if ordinal < 1 {
		return 1
	}
	return ordinal
}

func summarizeCompletedResults(runs []taskdomain.NodeRun) string {
	entries := make([]string, 0, len(runs))
	for _, run := range runs {
		if run.Status != taskdomain.NodeRunDone || len(run.Result) == 0 {
			continue
		}
		data, err := json.Marshal(run.Result)
		if err != nil {
			continue
		}
		entries = append(entries, fmt.Sprintf("- %s: %s", run.NodeName, string(data)))
	}
	return joinLines(entries)
}

func summarizeClarificationHistory(exchanges []taskdomain.ClarificationExchange) string {
	entries := make([]string, 0, len(exchanges))
	for _, exchange := range exchanges {
		for qi, question := range exchange.Request.Questions {
			lines := []string{fmt.Sprintf("- Q: %s", question.Question)}
			if question.WhyItMatters != "" {
				lines = append(lines, fmt.Sprintf("  Context: %s", question.WhyItMatters))
			}
			if len(question.Options) > 0 {
				lines = append(lines, "  Options:")
				for _, opt := range question.Options {
					if opt.Description != "" {
						lines = append(lines, fmt.Sprintf("    - %s: %s", opt.Label, opt.Description))
					} else {
						lines = append(lines, fmt.Sprintf("    - %s", opt.Label))
					}
				}
			}
			if exchange.Response != nil && qi < len(exchange.Response.Answers) {
				answerData, err := json.Marshal(exchange.Response.Answers[qi].Selected)
				if err == nil {
					lines = append(lines, fmt.Sprintf("  Selected: %s", string(answerData)))
				}
			}
			entries = append(entries, strings.Join(lines, "\n"))
		}
	}
	return joinLines(entries)
}

func joinLines(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, "\n")
}
