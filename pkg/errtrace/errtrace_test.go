package errtrace

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWrapPreservesChainAndLocations(t *testing.T) {
	sentinel := errors.New("database unavailable")
	err := Wrap(Wrap(sentinel, "repository.UpdateUser"), "SysUserService.Update")
	if !errors.Is(err, sentinel) {
		t.Fatal("wrapped error no longer matches sentinel")
	}
	detail := Format(err)
	for _, expected := range []string{"SysUserService.Update: repository.UpdateUser: database unavailable", "at SysUserService.Update", "at repository.UpdateUser", "errtrace_test.go:"} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("Format() missing %q:\n%s", expected, detail)
		}
	}
}

func TestWrapPreservesGRPCStatus(t *testing.T) {
	err := GRPC(errors.New("connection refused"), codes.Unavailable, "Server.Run", "model offline")
	if got := status.Code(err); got != codes.Unavailable {
		t.Fatalf("status code = %s", got)
	}
	if !strings.Contains(Format(err), "at Server.Run") {
		t.Fatalf("missing source frame: %s", Format(err))
	}
}

func TestWrapNil(t *testing.T) {
	if Wrap(nil, "operation") != nil {
		t.Fatal("Wrap(nil) must return nil")
	}
}
