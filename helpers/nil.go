package helpers

import "fmt"

func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func Check(err error, msg string) {
	if err != nil {
		panic(fmt.Errorf("%s: %w", msg, err))
	}
}
