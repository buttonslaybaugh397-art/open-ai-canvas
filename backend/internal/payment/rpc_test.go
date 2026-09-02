package payment

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRPCProviderUsesPaymentV1Contract(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "provider")
	if runtime.GOOS == "windows" {
		entry += ".exe"
		helper := filepath.Join(dir, "provider-helper.go")
		program := "package main\n\nimport (\n\t\"fmt\"\n\t\"io\"\n\t\"os\"\n)\n\nfunc main() {\n\t_, _ = io.ReadAll(os.Stdin)\n\tfmt.Println(\"{\\\"ok\\\":true,\\\"data\\\":{\\\"mode\\\":\\\"qr_code\\\",\\\"value\\\":\\\"https://pay.example/qr\\\"}}\")\n}\n"
		if err := os.WriteFile(helper, []byte(program), 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command("go", "build", "-o", entry, helper).CombinedOutput(); err != nil {
			t.Fatalf("build helper: %v\n%s", err, output)
		}
	} else {
		program := "#!/bin/sh\nread request\nprintf '%s\\n' '{\"ok\":true,\"data\":{\"mode\":\"qr_code\",\"value\":\"https://pay.example/qr\"}}'\n"
		if err := os.WriteFile(entry, []byte(program), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	provider, err := NewRPCProvider(Descriptor{ID: "test-provider", PluginID: "test-plugin", CheckoutMode: "qr_code"}, dir, "backend/provider")
	if err != nil {
		t.Fatal(err)
	}
	provider.command = entry
	checkout, err := provider.CreateOrder(context.Background(), Config{"merchantId": "m"}, CreateRequest{MerchantOrderNo: "order-1"})
	if err != nil {
		t.Fatal(err)
	}
	if checkout.Mode != "qr_code" || checkout.Value != "https://pay.example/qr" {
		t.Fatalf("checkout = %#v", checkout)
	}
}
