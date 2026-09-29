package storage

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3LiveConditionalWritesAndReadBudgets(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set S3_TEST_ENDPOINT for the isolated object store")
	}
	c, err := New(t.Context(), Config{Endpoint: endpoint, Region: "us-east-1", AccessKey: "test", SecretKey: "test",
		Bucket: "quality-" + uuid.NewV4().String(), UsePathStyle: true, MaxReadBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Delete(context.Background(), "value")
		_ = c.Delete(context.Background(), "large")
		if _, deleteErr := c.api.DeleteBucket(
			context.Background(),
			&s3.DeleteBucketInput{
				Bucket: aws.String(
					c.bucket,
				),
			},
		); deleteErr != nil {
			t.Error(deleteErr)
		}
	})
	if err = c.PutIfVersion(t.Context(), "value", strings.NewReader("first"), 5, "text/plain", ""); err != nil {
		t.Fatal(err)
	}
	raw, version, err := c.GetVersion(t.Context(), "value")
	if err != nil || string(raw) != "first" || version == "" {
		t.Fatalf("read version: %q %q %v", raw, version, err)
	}
	if err = c.PutIfVersion(t.Context(), "value", strings.NewReader("new"), 3, "text/plain", version); err != nil {
		t.Fatal(err)
	}
	if err = c.PutIfVersion(t.Context(), "value", strings.NewReader("old"), 3, "text/plain", version); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale conditional writer accepted: %v", err)
	}
	if err = c.Put(t.Context(), "large", strings.NewReader("123456789"), 9, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.GetBytes(t.Context(), "large"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized read accepted: %v", err)
	}
	if _, _, err = c.GetVersion(t.Context(), "large"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized version read accepted: %v", err)
	}
}
