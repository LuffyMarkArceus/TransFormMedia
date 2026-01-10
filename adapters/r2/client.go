package r2

import (
	"context"
	"fmt"
	"io"

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
	)
	if err != nil {
		return nil, err
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true // required for R2
		o.EndpointResolver = s3.EndpointResolverFromURL(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID))
	})

	uploader := manager.NewUploader(s3Client)

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
