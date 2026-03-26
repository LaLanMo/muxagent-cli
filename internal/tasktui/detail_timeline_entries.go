package tasktui

import (
	"fmt"
	"sort"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
)

type detailTimelineEntry struct {
	run     *taskdomain.NodeRunView
	blocked *taskdomain.BlockedStep
}

func detailTimelineEntries(view taskdomain.TaskView) []detailTimelineEntry {
	entries := make([]detailTimelineEntry, 0, len(view.NodeRuns)+len(view.BlockedSteps))
	for i := range view.NodeRuns {
		run := view.NodeRuns[i]
		entries = append(entries, detailTimelineEntry{run: &run})
	}
	for i := range view.BlockedSteps {
		step := view.BlockedSteps[i]
		entries = append(entries, detailTimelineEntry{blocked: &step})
	}
	sort.Slice(entries, func(i, j int) bool {
		leftAt, leftID := detailTimelineEntrySortKey(entries[i])
		rightAt, rightID := detailTimelineEntrySortKey(entries[j])
		if leftAt.Equal(rightAt) {
			return leftID < rightID
		}
		return leftAt.Before(rightAt)
	})
	return entries
}

func detailTimelineEntrySortKey(entry detailTimelineEntry) (time.Time, string) {
	if entry.run != nil {
		return entry.run.StartedAt, entry.run.ID
	}
	source := ""
	if entry.blocked.TriggeredBy != nil {
		source = entry.blocked.TriggeredBy.NodeRunID
	}
	return entry.blocked.CreatedAt, source + ":" + entry.blocked.NodeName
}

func blockedStepLabel(step taskdomain.BlockedStep) string {
	if step.Iteration <= 1 {
		return step.NodeName
	}
	return fmt.Sprintf("%s (#%d)", step.NodeName, step.Iteration)
}
