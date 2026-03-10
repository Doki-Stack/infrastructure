package ui

import (
	"fmt"

	"github.com/charmbracelet/huh/spinner"
)

func RunWithSpinner(title string, action func() error) error {
	var actionErr error

	err := spinner.New().
		Title(title).
		Action(func() {
			actionErr = action()
		}).
		Run()

	if err != nil {
		return fmt.Errorf("spinner error: %w", err)
	}
	return actionErr
}
