// Package jsonx — фасад JSON над bytedance/sonic, чтобы смена кодека была правкой одной строки.
package jsonx

import (
	"fmt"
	"io"

	"github.com/bytedance/sonic"
)

func Marshal(v any) ([]byte, error) {
	b, err := sonic.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("jsonx: marshal: %w", err)
	}
	return b, nil
}

func Unmarshal(data []byte, v any) error {
	if err := sonic.Unmarshal(data, v); err != nil {
		return fmt.Errorf("jsonx: unmarshal: %w", err)
	}
	return nil
}

func NewEncoder(w io.Writer) sonic.Encoder {
	return sonic.ConfigDefault.NewEncoder(w)
}

func NewDecoder(r io.Reader) sonic.Decoder {
	return sonic.ConfigDefault.NewDecoder(r)
}
