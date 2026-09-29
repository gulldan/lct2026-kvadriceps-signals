package contracts_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/example/source-harvester/internal/platform/contracts"
)

func TestUnrecoverableSurvivesWrapping(t *testing.T) {
	schema := contracts.Unrecoverable(errors.New(`unknown object member name "language"`))
	deep := fmt.Errorf("begin source index: %w", fmt.Errorf("decode index request: %w", schema))
	if !contracts.IsUnrecoverable(deep) {
		t.Fatal("a wrapped schema failure lost its class and would be retried forever")
	}
	if !errors.Is(deep, contracts.ErrUnrecoverable) {
		t.Error("errors.Is does not see the class")
	}
	if got := deep.Error(); !strings.Contains(got, "language") {
		t.Errorf("the cause was swallowed: %s", got)
	}
	for _, transient := range []error{
		errors.New("dial tcp: connection refused"),
		fmt.Errorf("store chunk: %w", errors.New("context deadline exceeded")),
		nil,
	} {
		if contracts.IsUnrecoverable(transient) {
			t.Errorf("a transient failure was marked for review: %v", transient)
		}
	}
	if contracts.Unrecoverable(nil) != nil {
		t.Error("marking a nil error invented a failure")
	}
}
