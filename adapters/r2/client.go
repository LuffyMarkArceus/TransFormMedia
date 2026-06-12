package r2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
		u.PartSize = 100 * 1024 * 1024 // 100 MB per part
		u.Concurrency = 4
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

//nolint:unused
func splitHostPort(addr string) (string, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	return host, port, nil
}
