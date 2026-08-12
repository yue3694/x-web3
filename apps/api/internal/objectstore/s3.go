package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

const defaultPresignTTL = 15 * time.Minute

type S3Store struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
	region  string
}

func NewS3Store(ctx context.Context, region, bucket string) (*S3Store, error) {
	if bucket == "" {
		return nil, errors.New("objectstore: S3 bucket is required")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("objectstore: load AWS config: %w", err)
	}
	client := s3.NewFromConfig(cfg)
	return &S3Store{client: client, presign: s3.NewPresignClient(client), bucket: bucket, region: region}, nil
}

func (s *S3Store) PresignPut(ctx context.Context, key, contentType string, size int64) (string, time.Time, error) {
	expiresAt := time.Now().Add(defaultPresignTTL)
	out, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(contentType), ContentLength: aws.Int64(size),
	}, s3.WithPresignExpires(defaultPresignTTL))
	if err != nil {
		return "", time.Time{}, err
	}
	return out.URL, expiresAt, nil
}

func (s *S3Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 || ttl > defaultPresignTTL {
		ttl = defaultPresignTTL
	}
	out, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", time.Time{}, err
	}
	return out.URL, time.Now().Add(ttl), nil
}

func (s *S3Store) Head(ctx context.Context, key string) (int64, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return 0, errors.New("objectstore: object missing")
		}
		return 0, err
	}
	return aws.ToInt64(out.ContentLength), nil
}

func (s *S3Store) PutBytes(ctx context.Context, key, contentType string, body []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(contentType), Body: bytes.NewReader(body),
	})
	return err
}

func (s *S3Store) PublicURL(key string) string {
	u := url.URL{Scheme: "https", Host: fmt.Sprintf("%s.s3.%s.amazonaws.com", s.bucket, s.region), Path: "/" + key}
	return u.String()
}

func (s *S3Store) Region() string { return s.region }
func (s *S3Store) Bucket() string { return s.bucket }

var _ Store = (*S3Store)(nil)
