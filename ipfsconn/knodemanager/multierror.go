package knodemanager

import (
	"errors"
	"fmt"
	"sync"
)

type MultiError struct {
	mux  sync.Mutex
	errs []error
}

func (m *MultiError) error() bool {
	return len(m.errs) > 0
}

func NewMultiError() MultiError {
	return MultiError{
		errs: make([]error, 0),
	}
}

func (m *MultiError) getError() error {
	if !m.error() {
		return nil
	}
	errStr := ""
	for _, e := range m.errs {
		errStr = fmt.Sprintf("%s%s \n", errStr, e.Error())
	}
	return errors.New(errStr)
}

func (m *MultiError) addError(e error) {

	m.mux.Lock()
	defer m.mux.Unlock()
	m.errs = append(m.errs, e)
}
