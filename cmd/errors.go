package cmd

import "errors"

// ErrAlreadyPresented signals to run() that the command has already written
// its own formatted output and the presenter should not add a second footer.
// Exported so main.go's WrapWithPresenter and run() can match it via errors.Is.
var ErrAlreadyPresented = errors.New("already presented")
