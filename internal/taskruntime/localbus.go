package taskruntime

type LocalBus struct {
	Commands chan RunCommand
	Events   chan RunEvent
}

func NewLocalBus(commandBuffer, eventBuffer int) *LocalBus {
	if commandBuffer <= 0 {
		commandBuffer = 16
	}
	if eventBuffer <= 0 {
		eventBuffer = 32
	}
	return &LocalBus{
		Commands: make(chan RunCommand, commandBuffer),
		Events:   make(chan RunEvent, eventBuffer),
	}
}
