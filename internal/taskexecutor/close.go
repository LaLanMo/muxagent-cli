package taskexecutor

import "errors"

type closer interface {
	Close() error
}

func Close(executor Executor) error {
	if executor == nil {
		return nil
	}
	c, ok := executor.(closer)
	if !ok {
		return nil
	}
	return c.Close()
}

func CloseAll(executors ...Executor) error {
	var errs []error
	for _, executor := range executors {
		if err := Close(executor); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
