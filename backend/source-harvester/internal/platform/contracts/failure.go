package contracts

import (
	"errors"
	"fmt"
)

// ErrUnrecoverable — отказ, который повтор не исправит (тело не декодируется, не сходятся схема
// или доказательства): повтор лишь занял бы воркер заведомо неверным ответом.
var ErrUnrecoverable = errors.New("unrecoverable")

func Unrecoverable(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrUnrecoverable, err)
}

func IsUnrecoverable(err error) bool { return errors.Is(err, ErrUnrecoverable) }

const (
	HeaderFailureClass  = "x-failure-class"
	HeaderFailureReason = "x-failure-reason"
	HeaderFailureQueue  = "x-failure-queue"

	FailureReview = "review"
	// FailureTransient исчерпал повторы; у сообщений, отправленных в DLX самим брокером, класса нет вовсе.
	FailureTransient = "transient"
)
