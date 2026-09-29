package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/example/db-service/internal/platform/config"
)

type Client struct {
	api          *s3.Client
	bucket       string
	maxReadBytes int64
	// Только для подписи presigned-ссылок публичным адресом; межсервисный трафик идёт через api.
	presignAPI *s3.Client
}

// Delete: вызывающий обязан проверить владельца и сохранить ссылки-доказательства, прежде чем
// удалять артефакт source-index.
func (c *Client) Delete(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("empty object key")
	}
	_, err := c.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

type Config struct {
	MaxReadBytes int64
	Endpoint     string // пусто — настоящий AWS S3, иначе адрес S3-совместимого хранилища
	Region       string
	AccessKey    string
	SecretKey    string
	Bucket       string
	UsePathStyle bool
	// Адрес S3, доступный из браузера: им подписываются presigned-ссылки. Пусто — как Endpoint.
	PublicEndpoint string
}

func ConfigFromEnv() Config {
	return Config{
		MaxReadBytes: int64(config.GetInt("S3_MAX_READ_MB", 64)) << 20,
		Endpoint:     config.Get("S3_ENDPOINT", ""),
		Region:       config.Get("S3_REGION", "us-east-1"),
		AccessKey:    config.Get("S3_ACCESS_KEY", ""),
		SecretKey:    config.Get("S3_SECRET_KEY", ""),
		Bucket:       config.Get("S3_BUCKET", "documents"),
		UsePathStyle: config.GetBool("S3_USE_PATH_STYLE", false),

		PublicEndpoint: config.Get("S3_PUBLIC_ENDPOINT", ""),
	}
}

// Open не создаёт бакет и не меняет его настройки: так проверяют уже сохранённые артефакты
// контуры, которые только читают.
func Open(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.MaxReadBytes == 0 {
		cfg.MaxReadBytes = 64 << 20
	}
	if cfg.MaxReadBytes < 1 || cfg.MaxReadBytes > 512<<20 {
		return nil, errors.New("S3_MAX_READ_MB must be between 1 and 512")
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
	if cfg.AccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	api := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})
	c := &Client{api: api, bucket: cfg.Bucket, presignAPI: nil, maxReadBytes: cfg.MaxReadBytes}
	if cfg.PublicEndpoint != "" && cfg.PublicEndpoint != cfg.Endpoint {
		c.presignAPI = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.PublicEndpoint)
			o.UsePathStyle = cfg.UsePathStyle
		})
	}
	return c, nil
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	c, err := Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = c.ensureBucket(ctx); err != nil {
		return nil, err
	}
	if err = c.ensureCORS(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// ensureCORS разрешает браузеру PUT частей напрямую; ETag открыт, потому что без него не завершить
// загрузку. NotImplemented (старые SeaweedFS/MinIO) терпим: страдает только прямая загрузка.
func (c *Client) ensureCORS(ctx context.Context) error {
	_, err := c.api.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket: aws.String(c.bucket),
		CORSConfiguration: &s3types.CORSConfiguration{
			CORSRules: []s3types.CORSRule{{
				AllowedOrigins: []string{"*"},
				AllowedMethods: []string{"PUT", "GET", "HEAD"},
				AllowedHeaders: []string{"*"},
				ExposeHeaders:  []string{"ETag"},
				MaxAgeSeconds:  aws.Int32(3600),
			}},
		},
	})
	if err != nil {
		if apiErr, ok := errors.AsType[smithy.APIError](err); ok && apiErr.ErrorCode() == "NotImplemented" {
			return nil
		}
		return fmt.Errorf("configure bucket cors: %w", err)
	}
	return nil
}

func (c *Client) ensureBucket(ctx context.Context) error {
	_, err := c.api.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.bucket)})
	if err == nil {
		return nil
	}
	_, owned := errors.AsType[*s3types.BucketAlreadyOwnedByYou](err)
	_, exists := errors.AsType[*s3types.BucketAlreadyExists](err)
	if owned || exists {
		return nil
	}
	return fmt.Errorf("create bucket %s: %w", c.bucket, err)
}

// Put: size = -1 — длина неизвестна.
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	in := &s3.PutObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key), Body: r}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	if size >= 0 {
		in.ContentLength = aws.Int64(size)
	}
	if _, err := c.api.PutObject(ctx, in); err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}

var ErrNotFound = errors.New("object not found")

func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := c.api.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("get object %s: %w", key, ErrNotFound)
		}
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}
	return out.Body, nil
}

func isNotFound(err error) bool {
	apiErr, ok := errors.AsType[smithy.APIError](err)
	if !ok {
		return false
	}
	code := apiErr.ErrorCode()
	return code == "NoSuchKey" || code == "NotFound"
}

// GetBytes выделяет буфер по ContentLength: удваивающийся bytes.Buffer на больших объектах
// занял бы почти вдвое больше памяти.
func (c *Client) GetBytes(ctx context.Context, key string) ([]byte, error) {
	out, err := c.api.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()
	size := int64(-1)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	buf, err := readObjectBytes(out.Body, size, c.maxReadBytes)
	if err != nil {
		return nil, fmt.Errorf("read object %s: %w", key, err)
	}
	return buf, nil
}

var ErrTooLarge = errors.New("object exceeds in-memory read limit; use streaming access")

func readObjectBytes(r io.Reader, size, maximum int64) ([]byte, error) {
	if maximum <= 0 || size > maximum {
		return nil, ErrTooLarge
	}
	if size >= 0 {
		buf := make([]byte, size+1)
		n, err := io.ReadFull(r, buf)
		if int64(n) != size || (err != io.EOF && err != io.ErrUnexpectedEOF) {
			return nil, errors.New("object content length mismatch")
		}
		return buf[:n], nil
	}
	buf, err := io.ReadAll(io.LimitReader(r, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read bounded object: %w", err)
	}
	if int64(len(buf)) > maximum {
		return nil, ErrTooLarge
	}
	return buf, nil
}

func (c *Client) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	ps := s3.NewPresignClient(c.api)
	req, err := ps.PresignGetObject(ctx,
		&s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)},
		s3.WithPresignExpires(ttl),
	)
	if err != nil {
		return "", fmt.Errorf("presign %s: %w", key, err)
	}
	return req.URL, nil
}

func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.api.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)}); err != nil {
		return fmt.Errorf("ping object store: %w", err)
	}
	return nil
}

type MultipartPart struct {
	PartNumber int32
	ETag       string
}

func (c *Client) CreateMultipart(ctx context.Context, key, contentType string) (string, error) {
	in := &s3.CreateMultipartUploadInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	out, err := c.api.CreateMultipartUpload(ctx, in)
	if err != nil {
		return "", fmt.Errorf("create multipart %s: %w", key, err)
	}
	return aws.ToString(out.UploadId), nil
}

func (c *Client) PresignUploadPart(
	ctx context.Context, key, uploadID string, partNumber int32, ttl time.Duration,
) (string, error) {
	presigner := c.presigner()
	req, err := presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(c.bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(partNumber),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign part %d of %s: %w", partNumber, key, err)
	}
	return req.URL, nil
}

func (c *Client) CompleteMultipart(
	ctx context.Context, key, uploadID string, parts []MultipartPart,
) error {
	completed := make([]s3types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		completed = append(completed, s3types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		})
	}
	_, err := c.api.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(c.bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: completed},
	})
	if err != nil {
		return fmt.Errorf("complete multipart %s: %w", key, err)
	}
	return nil
}

func (c *Client) AbortMultipart(ctx context.Context, key, uploadID string) error {
	_, err := c.api.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(c.bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("abort multipart %s: %w", key, err)
	}
	return nil
}

// presigner подписывает публичным адресом: подпись S3 включает хост, куда пойдёт браузер.
func (c *Client) presigner() *s3.PresignClient {
	if c.presignAPI != nil {
		return s3.NewPresignClient(c.presignAPI)
	}
	return s3.NewPresignClient(c.api)
}
