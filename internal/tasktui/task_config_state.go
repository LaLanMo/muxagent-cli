package tasktui

type taskConfigSummary struct {
	Alias      string
	BundlePath string
	ConfigPath string
	IsDefault  bool
	Runtime    string
	NodeNames  []string
	LoadErr    string
}

type taskConfigFormMode int

const (
	taskConfigFormClone taskConfigFormMode = iota
	taskConfigFormRename
)

type taskConfigFormState struct {
	Mode        taskConfigFormMode
	SourceAlias string
	Title       string
	Label       string
	Placeholder string
	SubmitLabel string
	Slot        string
	SeedValue   string
	ErrorText   string
}

type taskConfigConfirmState struct {
	Alias        string
	Title        string
	Body         string
	ConfirmLabel string
}

type taskConfigManagerState struct {
	entries       []taskConfigSummary
	selectedAlias string
	errorText     string
	statusText    string
	pending       bool
	form          *taskConfigFormState
	confirm       *taskConfigConfirmState
}
