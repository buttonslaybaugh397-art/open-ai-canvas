package assets

import (
	"io"

	"infinite-canvas/backend/internal/model"
)

type ResourceStream struct {
	Resource      *model.Resource
	Body          io.ReadCloser
	StatusCode    int
	ContentLength int64
	ContentRange  string
	AcceptRanges  string
}

type ResourceDeliveryOptions = AccessOptions

type ResourceDelivery struct {
	Resource *model.Resource
	Stream   *ResourceStream
	Access   *ResourceAccess
	// RedirectURL 保留旧的应用层调用合同；新代码应读取 Access.URL。
	RedirectURL string
}
