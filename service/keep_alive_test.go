package service

import (
	"context"
	"strings"
	"testing"
)

func TestKeepAliveRejectsMissingCheckTimeout(t *testing.T) {
	err := KeepAlive(context.Background(), nil, nil, "", KeepAliveOptions{})
	if err == nil || !strings.Contains(err.Error(), "timeout must be positive") {
		t.Fatalf("KeepAlive error = %v, want timeout validation error", err)
	}
}
