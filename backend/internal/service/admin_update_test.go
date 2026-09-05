package service

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestUpdaterConnectionDetailIncludesCauseAndHint(t *testing.T) {
	detail := updaterConnectionDetail(&os.PathError{Op: "open", Path: "/run/token", Err: os.ErrNotExist})
	if !strings.Contains(detail, "Token 文件不存在") {
		t.Fatalf("connection detail omitted the cause: %s", detail)
	}
	if !strings.Contains(detail, "检查 systemd 服务") {
		t.Fatalf("connection detail omitted the remediation hint: %s", detail)
	}
}

func TestUpdaterConnectionDetailDoesNotExposeArbitraryError(t *testing.T) {
	detail := updaterConnectionDetail(errors.New("Authorization: Bearer private-secret " + strings.Repeat("中", 801)))
	if strings.Contains(detail, "private-secret") || strings.Contains(detail, "中") {
		t.Fatal("raw helper error must not reach the page")
	}
	if !strings.Contains(detail, "Token 配置") {
		t.Fatalf("connection detail omitted the remediation hint: %s", detail)
	}
	if got := len([]rune(detail)); got > 860 {
		t.Fatalf("connection detail was not bounded: %d runes", got)
	}
}
