package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	awsclient "github.com/aws/aws-sdk-go/aws/client"
	awss3 "github.com/aws/aws-sdk-go/service/s3"
	qiniuAuth "github.com/qiniu/go-sdk/v7/auth"
	qiniuStorage "github.com/qiniu/go-sdk/v7/storage"
)

func statStoredCloudObject(ctx context.Context, setting ossSettingValue, key string) (storedObjectStat, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	switch setting.Provider {
	case s3Provider:
		client, err := newS3Client(setting, 10*time.Second)
		if err != nil {
			return storedObjectStat{}, err
		}
		client.Retryer = awsclient.DefaultRetryer{NumMaxRetries: 0}
		result, err := client.HeadObjectWithContext(ctx, &awss3.HeadObjectInput{Bucket: aws.String(setting.Bucket), Key: aws.String(key)})
		if err != nil {
			var failure awserr.RequestFailure
			if errors.As(err, &failure) {
				return storageStatHTTPResult(failure.StatusCode(), -1)
			}
			return storedObjectStat{}, errors.New("S3 文件元数据查询失败")
		}
		if result.ContentLength == nil {
			return storedObjectStat{}, errors.New("S3 未返回文件长度")
		}
		return storageStatHTTPResult(http.StatusOK, *result.ContentLength)
	case aliyunOSSProvider:
		request, err := newOSSRequest(http.MethodHead, setting, key, "", nil)
		if err != nil {
			return storedObjectStat{}, err
		}
		response, err := OutboundHTTPClient(10 * time.Second).Do(request.WithContext(ctx))
		if err != nil {
			return storedObjectStat{}, errors.New("OSS 文件元数据查询失败")
		}
		defer response.Body.Close()
		return storageStatHTTPResult(response.StatusCode, response.ContentLength)
	case tencentCOSProvider:
		client, err := newCOSClient(setting, 10*time.Second)
		if err != nil {
			return storedObjectStat{}, err
		}
		client.Conf.RetryOpt.Count = 1
		client.Conf.RetryOpt.AutoSwitchHost = false
		response, err := client.Object.Head(ctx, key, nil)
		if response != nil {
			defer response.Body.Close()
			if err != nil && response.StatusCode == http.StatusOK {
				return storedObjectStat{}, errors.New("COS 文件元数据查询失败")
			}
			return storageStatHTTPResult(response.StatusCode, response.ContentLength)
		}
		if err != nil {
			return storedObjectStat{}, errors.New("COS 文件元数据查询失败")
		}
		return storedObjectStat{}, errors.New("COS 未返回文件元数据")
	case qiniuKodoProvider:
		// Query Kodo's management API, never a CDN that may retain a deleted object.
		target := strings.TrimRight(qiniuRegion(setting.Region).GetRsHost(true), "/") + qiniuStorage.URIStat(setting.Bucket, key)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return storedObjectStat{}, err
		}
		if err := qiniuAuth.New(setting.AccessKeyID, setting.AccessKeySecret).AddToken(qiniuAuth.TokenQBox, request); err != nil {
			return storedObjectStat{}, errors.New("七牛文件元数据鉴权失败")
		}
		response, err := OutboundHTTPClient(10 * time.Second).Do(request)
		if err != nil {
			return storedObjectStat{}, errors.New("七牛文件元数据查询失败")
		}
		defer response.Body.Close()
		return decodeQiniuStorageStat(response.StatusCode, response.Body)
	default:
		return storedObjectStat{}, errors.New("当前存储类型不支持实际容量核验")
	}
}

func storageStatHTTPResult(status int, size int64) (storedObjectStat, error) {
	result := storedObjectStat{CheckedAt: time.Now().UTC()}
	if status == http.StatusNotFound {
		return result, nil
	}
	if status != http.StatusOK {
		return result, fmt.Errorf("对象存储元数据查询返回 HTTP %d，不能确认文件占用", status)
	}
	if size < 0 {
		return result, errors.New("对象存储未返回有效文件长度")
	}
	result.Bytes, result.Exists = size, true
	return result, nil
}

func decodeQiniuStorageStat(status int, body io.Reader) (storedObjectStat, error) {
	if status == 612 {
		return storedObjectStat{CheckedAt: time.Now().UTC()}, nil
	}
	if status != http.StatusOK {
		// A generic 404 is not Kodo's documented missing-object response.
		return storedObjectStat{}, fmt.Errorf("七牛文件元数据查询返回 HTTP %d，不能确认文件占用", status)
	}
	var result struct {
		Size *int64 `json:"fsize"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 64<<10)).Decode(&result); err != nil || result.Size == nil {
		return storedObjectStat{}, errors.New("七牛未返回有效文件长度")
	}
	return storageStatHTTPResult(http.StatusOK, *result.Size)
}
