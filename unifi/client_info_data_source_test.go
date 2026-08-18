package unifi

import (
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestNewClientInfoDataSource(t *testing.T) {
	d := NewClientInfoDataSource()
	if d == nil {
		t.Fatal("NewClientInfoDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}
