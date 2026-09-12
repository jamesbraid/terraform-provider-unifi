package unifi

import (
	"testing"
)

func TestNewClientInfoDataSource(t *testing.T) {
	d := NewClientInfoDataSource()
	if d == nil {
		t.Fatal("NewClientInfoDataSource() returned nil")
	}
}
