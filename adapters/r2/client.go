package r2

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	s3Client   *s3.Client
	bucket     string
	publicBase string
}

type Config struct {
	Bucket     string
	AccessKey  string
	SecretKey  string
	AccountID  string
	PublicBase string
}

// NewClient initializes R2 S3 client
func NewClient(cfg Config) (*Client, error) {
	if cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.AccountID == "" {
		return nil, fmt.Errorf("missing R2 configuration")
	}

	resolver := aws.EndpointResolverWithOptionsFunc(
		func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				URL: fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID),
			}, nil
		},
	)

	awsCfg, err := awsConfig.LoadDefaultConfig(
		context.Background(),
		awsConfig.WithEndpointResolverWithOptions(resolver),
		awsConfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
		awsConfig.WithRegion("auto"),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		s3Client:   s3.NewFromConfig(awsCfg),
		bucket:     cfg.Bucket,
		publicBase: cfg.PublicBase,
	}, nil
}

// Upload uploads a file to R2 and returns the public URL
func (c *Client) Upload(ctx context.Context, key string, file multipart.File, contentType string) (string, error) {
	defer file.Seek(0, io.SeekStart) // reset file for future reads

	_, err := c.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         &key,
		Body:        file,
		ContentType: &contentType,
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/%s", c.publicBase, key), nil
}
