package oss

import (
	"bytes"
	"fmt"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/spf13/viper"
)

type OSSClient struct {
	bucket *oss.Bucket
}

type Config struct {
	Endpoint        string
	AccessKeyID     string
	AccessKeySecret string
	BucketName      string
}

var runtimeConfig Config

func SetConfig(config Config) {
	runtimeConfig = config
}

// NewOSSClient 创建OSS客户端
func NewOSSClient() (*OSSClient, error) {
	endpoint := runtimeConfig.Endpoint
	if endpoint == "" {
		endpoint = viper.GetString("oss.endpoint")
	}
	accessKeyID := runtimeConfig.AccessKeyID
	if accessKeyID == "" {
		accessKeyID = viper.GetString("oss.access_key_id")
	}
	accessKeySecret := runtimeConfig.AccessKeySecret
	if accessKeySecret == "" {
		accessKeySecret = viper.GetString("oss.access_key_secret")
	}
	bucketName := runtimeConfig.BucketName
	if bucketName == "" {
		bucketName = viper.GetString("oss.bucket_name")
	}

	if endpoint == "" || accessKeyID == "" || bucketName == "" {
		return nil, fmt.Errorf("OSS配置不完整")
	}

	client, err := oss.New(endpoint, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, err
	}

	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, err
	}

	return &OSSClient{bucket: bucket}, nil
}

// Upload 上传文件
func (o *OSSClient) Upload(path string, data []byte, contentType string) (string, error) {
	err := o.bucket.PutObject(path, bytes.NewReader(data), oss.ContentType(contentType))
	if err != nil {
		return "", err
	}

	bucketName := viper.GetString("oss.bucket_name")
	if runtimeConfig.BucketName != "" {
		bucketName = runtimeConfig.BucketName
	}
	endpoint := viper.GetString("oss.endpoint")
	if runtimeConfig.Endpoint != "" {
		endpoint = runtimeConfig.Endpoint
	}
	return fmt.Sprintf("https://%s.%s/%s", bucketName, endpoint, path), nil
}

// GetSignedURL 获取签名URL
func (o *OSSClient) GetSignedURL(path string, expire time.Duration) (string, error) {
	signedURL, err := o.bucket.SignURL(path, oss.HTTPGet, int64(expire.Seconds()))
	if err != nil {
		return "", err
	}
	return signedURL, nil
}
