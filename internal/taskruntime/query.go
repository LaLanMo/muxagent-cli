package taskruntime

import (
	"context"
	"fmt"

	"github.com/LaLanMo/muxagent-cli/internal/taskconfig"
	"github.com/LaLanMo/muxagent-cli/internal/taskdomain"
	"github.com/LaLanMo/muxagent-cli/internal/taskengine"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
)

func (s *Service) ListTaskViews(ctx context.Context, workDir string) ([]taskdomain.TaskView, error) {
	workDir = taskstore.NormalizeWorkDir(workDir)
	tasks, err := s.store.ListTasksByWorkDir(ctx, workDir)
	if err != nil {
		return nil, err
	}
	views := make([]taskdomain.TaskView, 0, len(tasks))
	for _, task := range tasks {
		view, _, err := s.LoadTaskView(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) LoadTaskView(ctx context.Context, taskID string) (taskdomain.TaskView, *taskconfig.Config, error) {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return taskdomain.TaskView{}, nil, err
	}
	cfg, err := taskconfig.Load(taskstore.ConfigPath(task.WorkDir, task.ID))
	if err != nil {
		return taskdomain.TaskView{}, nil, err
	}
	runs, err := s.store.ListNodeRunsByTask(ctx, taskID)
	if err != nil {
		return taskdomain.TaskView{}, nil, err
	}
	blockedSteps, err := taskengine.DeriveBlockedSteps(cfg, runs)
	if err != nil {
		return taskdomain.TaskView{}, nil, err
	}
	return taskdomain.DeriveTaskView(task, cfg, runs, blockedSteps), cfg, nil
}

func (s *Service) BuildInputRequest(ctx context.Context, taskID, nodeRunID string) (*InputRequest, error) {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	cfg, err := taskconfig.Load(taskstore.ConfigPath(task.WorkDir, task.ID))
	if err != nil {
		return nil, err
	}
	run, err := s.store.GetNodeRun(ctx, nodeRunID)
	if err != nil {
		return nil, err
	}
	if run.TaskID != taskID {
		return nil, fmt.Errorf("node run %q does not belong to task %q", nodeRunID, taskID)
	}
	runs, err := s.store.ListNodeRunsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return s.buildInputRequest(ctx, task, cfg, runs, run)
}

func (s *Service) refreshTaskView(ctx context.Context, taskID string) (taskdomain.TaskView, error) {
	view, _, err := s.LoadTaskView(ctx, taskID)
	return view, err
}

func (s *Service) buildInputRequest(ctx context.Context, task taskdomain.Task, cfg *taskconfig.Config, runs []taskdomain.NodeRun, run taskdomain.NodeRun) (*InputRequest, error) {
	artifacts := completedArtifactPaths(runs)
	inheritedArtifacts, err := s.loadInheritedInputArtifacts(ctx, task)
	if err != nil {
		return nil, err
	}
	if len(inheritedArtifacts) > 0 {
		artifacts = mergeArtifactPaths(artifacts, inheritedArtifacts)
	}
	def := cfg.NodeDefinitions[run.NodeName]
	if def.Type == taskconfig.NodeTypeHuman {
		schema := def.ResultSchema
		return &InputRequest{
			Kind:          InputKindHumanNode,
			TaskID:        run.TaskID,
			NodeRunID:     run.ID,
			NodeName:      run.NodeName,
			Schema:        &schema,
			ArtifactPaths: artifacts,
		}, nil
	}
	if len(run.Clarifications) == 0 {
		return nil, nil
	}
	return &InputRequest{
		Kind:          InputKindClarification,
		TaskID:        run.TaskID,
		NodeRunID:     run.ID,
		NodeName:      run.NodeName,
		Questions:     run.Clarifications[len(run.Clarifications)-1].Request.Questions,
		ArtifactPaths: artifacts,
	}, nil
}

func viewNodeRuns(view taskdomain.TaskView) []taskdomain.NodeRun {
	runs := make([]taskdomain.NodeRun, 0, len(view.NodeRuns))
	for _, run := range view.NodeRuns {
		runs = append(runs, run.NodeRun)
	}
	return runs
}
