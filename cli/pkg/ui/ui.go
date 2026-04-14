package ui

import (
	"fmt"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
)

var (
	successPrefix = color.GreenString("✓ ")
	errorPrefix   = color.RedString("✗ ")
	infoPrefix    = color.CyanString("→ ")
	warnPrefix    = color.YellowString("⚠ ")
	stepPrefix    = "  "
)

// Success prints a success message in green
func Success(msg string) {
	fmt.Println(successPrefix + msg)
}

// Error prints an error message in red
func Error(msg string) {
	fmt.Println(errorPrefix + msg)
}

// Info prints an info message in cyan
func Info(msg string) {
	fmt.Println(infoPrefix + msg)
}

// Warn prints a warning message in yellow
func Warn(msg string) {
	fmt.Println(warnPrefix + msg)
}

// Step prints a step message in white
func Step(msg string) {
	fmt.Println(stepPrefix + msg)
}

// NewSpinner creates a new spinner with the given message
func NewSpinner(msg string) *spinner.Spinner {
	s := spinner.New(spinner.CharSets[14], 100)
	s.Color("cyan")
	s.Suffix = " " + msg
	return s
}

// StartSpinner starts the given spinner
func StartSpinner(s *spinner.Spinner) {
	s.Start()
}

// StopSpinner stops the spinner and prints a success message
func StopSpinner(s *spinner.Spinner, successMsg string) {
	s.Stop()
	Success(successMsg)
}

// FailSpinner stops the spinner and prints an error message
func FailSpinner(s *spinner.Spinner, errorMsg string) {
	s.Stop()
	Error(errorMsg)
}