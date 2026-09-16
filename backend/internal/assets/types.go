package assets

import (
	"context"
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

type ResourceDeliveryOptions struct {
	Context     context.Context
	ForceDirect bool
	ForceProxy  bool
}

type ResourceDelivery struct {
	Resource    *model.Resource
	Stream      *ResourceStream
	RedirectURL string
}
