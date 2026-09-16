package handler

import (
	"mime"
	"strings"
	"testing"
)

func TestAttachmentContentDispositionPreservesUnicodeFilename(t *testing.T) {
	value := attachmentContentDisposition("画布终稿 video.mp4")
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "attachment" {
		t.Fatalf("media type = %q, want attachment", mediaType)
	}
	if parameters["filename"] != "画布终稿 video.mp4" {
		t.Fatalf("filename = %q", parameters["filename"])
	}
}

func TestAttachmentContentDispositionRejectsHeaderInjection(t *testing.T) {
	value := attachmentContentDisposition("video.mp4\r\nX-Injection: true")
	if strings.ContainsAny(value, "\r\n") {
		t.Fatalf("content disposition contains a line break: %q", value)
	}
}
