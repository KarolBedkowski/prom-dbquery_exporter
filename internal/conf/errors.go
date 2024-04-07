package conf

import "fmt"

//
// errors.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

// ConfigurationError is error returned when load configuration failed.
type ConfigurationError struct {
	Message string
	Err     error
}

func newConfigurationError(msg string) ConfigurationError {
	return ConfigurationError{msg, nil}
}

// Wrap error with ConfigurationError.
func (c ConfigurationError) Wrap(err error) ConfigurationError {
	return ConfigurationError{c.Message, err}
}

func (c ConfigurationError) Error() string {
	if c.Err != nil {
		return fmt.Sprintf("%s: %v", c.Message, c.Err)
	}

	return c.Message
}

func (c *ConfigurationError) Unwrap() error {
	return c.Err
}

// MissingFieldError is error generated when `field` is missing in configuration.
type MissingFieldError struct {
	Field string
}

func (e MissingFieldError) Error() string {
	return "missing field " + e.Field
}

// InvalidFieldError is error generated when validation of `field` with `value` failed.
type InvalidFieldError struct {
	Field   string
	Value   any
	Message string
}

func (e InvalidFieldError) Error() string {
	res := "invalid " + e.Field

	if e.Value != nil {
		res += fmt.Sprintf(" (%v)", e.Value)
	}

	if e.Message != "" {
		res += ": " + e.Message
	}

	return res
}

// WithMsg add message to InvalidFieldError.
func (e InvalidFieldError) WithMsg(msg string) InvalidFieldError {
	return InvalidFieldError{e.Field, e.Value, msg}
}

// NewInvalidFieldError create InvalidFieldError.
func NewInvalidFieldError(field string, value any) InvalidFieldError {
	return InvalidFieldError{field, value, ""}
}
