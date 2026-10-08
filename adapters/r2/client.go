package r2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"universal-media-service/core/upload"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type Client struct {
	s3Client   *s3.Client
	bucket     string
	PublicBase string
	uploader   *manager.Uploader
}

type Config struct {
	Bucket      string
	AccessKey   string
	SecretKey   string
	AccountID   string
	PublicBase  string
	PrivateBase string
}

// NewClient initializes R2 S3 client
func NewClient(cfg Config) (*Client, error) {
	if cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.AccountID == "" {
		return nil, fmt.Errorf("missing R2 configuration")
	}

	awsCfg, err := awsConfig.LoadDefaultConfig(context.TODO(),
		awsConfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
		awsConfig.WithHTTPClient(&http.Client{
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				TLSHandshakeTimeout: 30 * time.Second,
				IdleConnTimeout:     90 * time.Second,
			},
		}),
	)
	if err != nil {
		return nil, err
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.Region = "auto"
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID))
	})

	uploader := manager.NewUploader(s3Client, func(u *manager.Uploader) {
		// The manager buffers each in-flight part in memory. 16 MB parts with
		// 2-way concurrency keep the peak at ~32 MB so the worker can push
		// multi-hundred-MB outputs on a 512 Mi instance (the previous
		// 100 MB x 4 configuration could buffer 400 MB at once).
		u.PartSize = 16 * 1024 * 1024
		u.Concurrency = 2
	})

	return &Client{
		bucket:     cfg.Bucket,
		PublicBase: cfg.PublicBase,
		s3Client:   s3Client,
		uploader:   uploader,
	}, nil
}

// Upload uploads a file to R2 and returns the public URL
func (c *Client) Upload(ctx context.Context, key string, file io.Reader, contentType string) (string, error) {

	_, err := c.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         &key,
		Body:        file,
		ContentType: &contentType,
	})
	if err != nil {
		return "", err
	}

	// return the private URL if needed (not used by frontend)
	return fmt.Sprintf("https://%s.r2.cloudflarestorage.com/%s/%s", c.bucket, c.bucket, key), nil
}

func (c *Client) PublicBaseURL() string {
	return c.PublicBase
}

func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	return err
}

// Fetch a file from R2 as bytes
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := c.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, out.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DownloadTo streams an object into dst without buffering it in memory.
func (c *Client) DownloadTo(ctx context.Context, key string, dst io.Writer) (int64, error) {
	out, err := c.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		return 0, translateNotFound(err)
	}
	defer out.Body.Close()
	return io.Copy(dst, out.Body)
}

// Head returns the object's size without fetching its bytes.
func (c *Client) Head(ctx context.Context, key string) (upload.ObjectInfo, error) {
	out, err := c.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		return upload.ObjectInfo{}, translateNotFound(err)
	}
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return upload.ObjectInfo{Size: size}, nil
}

// GetRange reads at most length bytes starting at offset.
func (c *Client) GetRange(ctx context.Context, key string, offset, length int64) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	rng := fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)
	out, err := c.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
		Range:  &rng,
	})
	if err != nil {
		return nil, translateNotFound(err)
	}
	defer out.Body.Close()
	return io.ReadAll(io.LimitReader(out.Body, length))
}

// PresignPut returns a time-limited PUT URL for key that only accepts exactly
// contentType (the value is part of the signature, so a client sending any
// other Content-Type gets a signature mismatch).
func (c *Client) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	presigner := s3.NewPresignClient(c.s3Client)
	out, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         &key,
		ContentType: &contentType,
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

// translateNotFound maps S3 404 variants to upload.ErrObjectNotFound so the
// core package's sentinel works across storage implementations.
func translateNotFound(err error) error {
	if err == nil {
		return nil
	}
	var noSuchKey *types.NoSuchKey
	var notFound *types.NotFound
	var apiErr smithy.APIError
	if errors.As(err, &noSuchKey) || errors.As(err, &notFound) ||
		(errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey")) {
		return upload.ErrObjectNotFound
	}
	return err
}

//nolint:unused
func splitHostPort(addr string) (string, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	return host, port, nil
}
