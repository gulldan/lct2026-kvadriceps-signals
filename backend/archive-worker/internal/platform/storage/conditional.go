package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

var ErrConflict = errors.New("object version changed")

// GetVersion читает содержимое и ETag одним ответом; отсутствующий ключ даёт пустую версию и nil.
func (c *Client) GetVersion(ctx context.Context, key string) ([]byte, string, error) {
	out, err := c.api.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("get object version %s: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()
	size := int64(-1)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	raw, err := readObjectBytes(out.Body, size, c.maxReadBytes)
	if err != nil {
		return nil, "", fmt.Errorf("read object version %s: %w", key, err)
	}
	version := aws.ToString(out.ETag)
	if version == "" {
		return nil, "", errors.New("object store omitted ETag")
	}
	return raw, version, nil
}

// PutIfVersion: пустая версия требует отсутствия ключа; откатываться на Put вызывающим нельзя.
func (c *Client) PutIfVersion(ctx context.Context, key string, r io.Reader, size int64, contentType, version string) error {
	in := &s3.PutObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key), Body: r,
		ContentLength: aws.Int64(size), ContentType: aws.String(contentType)}
	if version == "" {
		in.IfNoneMatch = aws.String("*")
	} else {
		in.IfMatch = aws.String(version)
	}
	_, err := c.api.PutObject(ctx, in)
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok &&
		(apiErr.ErrorCode() == "PreconditionFailed" || apiErr.ErrorCode() == "ConditionalRequestConflict") {
		return fmt.Errorf("put object %s: %w", key, ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("conditional object write %s: %w", key, err)
	}
	return nil
}
