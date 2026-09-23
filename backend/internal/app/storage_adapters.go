package app

import "infinite-canvas/backend/internal/storage"

type ossSettingValue = storage.Settings
type ossProviderCredentials = storage.Credentials
type ossObjectStream = storage.ObjectStream

var (
	errResourceObjectMissing   = storage.ErrObjectMissing
	errResourceCDNTemporary    = storage.ErrCDNTemporary
	cdnEnabled                 = storage.CDNEnabled
	signCDNURL                 = storage.SignCDNURL
	headS3Object               = storage.HeadS3Object
	signedOriginObjectURL      = storage.SignedOriginObjectURL
	publicHTTPSStorageEndpoint = storage.PublicHTTPSStorageEndpoint
	validateStorageEndpoint    = storage.ValidateStorageEndpoint
	standardAWSS3Endpoint      = storage.StandardAWSS3Endpoint
	newS3Client                = storage.NewS3Client
	putS3Object                = storage.PutS3Object
	getS3ObjectRange           = storage.GetS3ObjectRange
	signedS3ObjectURL          = storage.SignedS3ObjectURL
	deleteS3Object             = storage.DeleteS3Object
	deleteAliyunOSSObject      = storage.DeleteAliyunOSSObject
	deleteTencentCOSObject     = storage.DeleteTencentCOSObject
	deleteQiniuObject          = storage.DeleteQiniuObject
)
