package taskruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

func (s *Service) reconcileStaleRunning(ctx context.Context) error {
	runs, err := s.store.ListNodeRunsByStatus(ctx, taskdomain.NodeRunRunning)
	if err != nil {
		return err
	}
	for _, run := range runs {
		recovered, _ := s.recoverRunningNodeRun(ctx, run)
		if recovered {
			continue
		}
		if err := s.markRecoveredRunFailed(ctx, run, taskdomain.FailureReasonOrphanedAfterRestart); err != nil {
			return err
		}
	}
	awaitingRuns, err := s.store.ListNodeRunsByStatus(ctx, taskdomain.NodeRunAwaitingUser)
	if err != nil {
		return err
	}
	for _, run := range awaitingRuns {
		_, _ = s.recoverAwaitingUserNodeRun(ctx, run)
	}
	return nil
}

func (s *Service) failActiveRunningNodeRuns(ctx context.Context, reason string) error {
	runs, err := s.store.ListNodeRunsByStatus(ctx, taskdomain.NodeRunRunning)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, run := range runs {
		run.Status = taskdomain.NodeRunFailed
		run.FailureReason = reason
		run.CompletedAt = &now
		if err := s.store.SaveNodeRun(ctx, run); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recoverRunningNodeRun(ctx context.Context, run taskdomain.NodeRun) (bool, error) {
	task, err := s.store.GetTask(ctx, run.TaskID)
	if err != nil {
		return false, err
	}
	cfg, err := taskconfig.Load(taskstore.ConfigPath(task.WorkDir, task.ID))
	if err != nil {
		return false, err
	}
	outputPath := filepath.Join(taskstore.RunDir(task.WorkDir, task.ID, run.ID), outputArtifactName)
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	result, ok, err := recoverOutputEnvelope(task, cfg, run, outputBytes)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return false, err
	}
	recoveredAt := info.ModTime().UTC()
	switch result.Kind {
	case taskexecutor.ResultKindResult:
		run.Result = cloneMap(result.Result)
		run.Status = taskdomain.NodeRunDone
		run.FailureReason = ""
		run.CompletedAt = &recoveredAt
	case taskexecutor.ResultKindClarification:
		run.Status = taskdomain.NodeRunAwaitingUser
		run.FailureReason = ""
		run.CompletedAt = nil
		if result.Clarification != nil {
			run.Clarifications = recoverClarifications(run.Clarifications, *result.Clarification, recoveredAt)
		}
	default:
		return false, nil
	}
	if err := s.store.SaveNodeRun(ctx, run); err != nil {
		return false, err
	}
	runs, err := s.store.ListNodeRunsByTask(ctx, task.ID)
	if err != nil {
		return false, err
	}
	if err := persistRunManifest(task, runs, run); err != nil {
		return false, err
	}
	if run.Status == taskdomain.NodeRunDone {
		if err := s.afterNodeCompletedInternal(ctx, task, cfg, run, false); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) recoverAwaitingUserNodeRun(ctx context.Context, run taskdomain.NodeRun) (bool, error) {
	task, err := s.store.GetTask(ctx, run.TaskID)
	if err != nil {
		return false, err
	}
	cfg, err := taskconfig.Load(taskstore.ConfigPath(task.WorkDir, task.ID))
	if err != nil {
		return false, err
	}
	def, ok := cfg.NodeDefinitions[run.NodeName]
	if !ok || def.Type != taskconfig.NodeTypeHuman {
		return false, nil
	}
	outputPath := filepath.Join(taskstore.RunDir(task.WorkDir, task.ID, run.ID), outputArtifactName)
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	result, ok, err := recoverOutputEnvelope(task, cfg, run, outputBytes)
	if err != nil {
		return false, err
	}
	if !ok || result.Kind != taskexecutor.ResultKindResult {
		return false, nil
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return false, err
	}
	recoveredAt := info.ModTime().UTC()
	run.Result = cloneMap(result.Result)
	run.Status = taskdomain.NodeRunDone
	run.FailureReason = ""
	run.CompletedAt = &recoveredAt
	if err := s.store.SaveNodeRun(ctx, run); err != nil {
		return false, err
	}
	runs, err := s.store.ListNodeRunsByTask(ctx, task.ID)
	if err != nil {
		return false, err
	}
	if err := persistRunManifest(task, runs, run); err != nil {
		return false, err
	}
	if err := s.afterNodeCompletedInternal(ctx, task, cfg, run, false); err != nil {
		return false, err
	}
	return true, nil
}

func recoveryRequest(task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun) taskexecutor.Request {
	def := cfg.NodeDefinitions[run.NodeName]
	return taskexecutor.Request{
		Task:                task,
		NodeRun:             run,
		NodeDefinition:      def,
		ClarificationConfig: cfg.Clarification,
		ConfigPath:          taskstore.ConfigPath(task.WorkDir, task.ID),
		SchemaPath:          taskstore.SchemaPath(task.WorkDir, task.ID, run.NodeName),
		WorkDir:             task.ExecutionWorkDir(),
		ArtifactDir:         taskstore.RunDir(task.WorkDir, task.ID, run.ID),
		Runtime:             cfg.Runtime,
		ResultSchema:        def.ResultSchema,
	}
}

func recoverOutputEnvelope(task taskdomain.Task, cfg *taskconfig.Config, run taskdomain.NodeRun, outputBytes []byte) (taskexecutor.Result, bool, error) {
	def, ok := cfg.NodeDefinitions[run.NodeName]
	if !ok {
		return taskexecutor.Result{}, false, nil
	}
	if def.Type == taskconfig.NodeTypeHuman {
		payload, ok, err := parseHumanNodeRecoveryOutput(def.ResultSchema, outputBytes)
		if err != nil {
			return taskexecutor.Result{}, false, err
		}
		if !ok {
			return taskexecutor.Result{}, false, nil
		}
		return taskexecutor.Result{
			Kind:   taskexecutor.ResultKindResult,
			Result: payload,
		}, true, nil
	}
	result, err := taskexecutor.ParseOutputEnvelope(recoveryRequest(task, cfg, run), outputBytes)
	if err != nil {
		return taskexecutor.Result{}, false, nil
	}
	return result, true, nil
}

func parseHumanNodeRecoveryOutput(schema taskconfig.JSONSchema, outputBytes []byte) (map[string]interface{}, bool, error) {
	output, err := taskconfig.NormalizeJSONMap(outputBytes)
	if err != nil {
		return nil, false, nil
	}
	kind, _ := output["kind"].(string)
	if strings.TrimSpace(kind) != "human_node_result" {
		return nil, false, nil
	}
	resultPayload, ok := output["result"].(map[string]interface{})
	if !ok {
		return nil, false, errors.New("human node output is missing result object")
	}
	if err := taskconfig.ValidateValue(&schema, resultPayload); err != nil {
		return nil, false, err
	}
	return resultPayload, true, nil
}

func (s *Service) markRecoveredRunFailed(ctx context.Context, run taskdomain.NodeRun, reason string) error {
	now := time.Now().UTC()
	run.Status = taskdomain.NodeRunFailed
	run.FailureReason = reason
	run.CompletedAt = &now
	if err := s.store.SaveNodeRun(ctx, run); err != nil {
		return err
	}
	task, err := s.store.GetTask(ctx, run.TaskID)
	if err != nil {
		return nil
	}
	runs, err := s.store.ListNodeRunsByTask(ctx, task.ID)
	if err != nil {
		return nil
	}
	return persistRunManifest(task, runs, run)
}

func recoverClarifications(existing []taskdomain.ClarificationExchange, request taskdomain.ClarificationRequest, requestedAt time.Time) []taskdomain.ClarificationExchange {
	if len(existing) > 0 && existing[len(existing)-1].Response == nil {
		existing[len(existing)-1].Request = request
		existing[len(existing)-1].RequestedAt = requestedAt
		return existing
	}
	return append(existing, taskdomain.ClarificationExchange{
		Request:     request,
		RequestedAt: requestedAt,
	})
}
