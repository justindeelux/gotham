package files

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestDisabledFileServerOverridesEveryRPC(t *testing.T) {
	disabled := reflect.TypeOf(&DisabledFileServer{})
	unimplemented := reflect.TypeOf(agentv1.UnimplementedFileServiceServer{})
	for i := 0; i < unimplemented.NumMethod(); i++ {
		method := unimplemented.Method(i)
		overridden, ok := disabled.MethodByName(method.Name)
		if !ok || overridden.Func.Pointer() == method.Func.Pointer() {
			t.Errorf("%s still uses the Unimplemented answer", method.Name)
		}
	}
}

func TestDisabledFileServerAnswersFailedPrecondition(t *testing.T) {
	server := NewDisabledFileServer()
	_, err := server.ListRoots(context.Background(), &agentv1.ListRootsRequest{})
	assertDisabledError(t, err)
	assertDisabledError(t, server.Read(&agentv1.ReadRequest{}, nil))
	_, err = server.Mkdir(context.Background(), &agentv1.MkdirRequest{})
	assertDisabledError(t, err)
}

func assertDisabledError(t *testing.T, err error) {
	t.Helper()
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %s, want FailedPrecondition (%v)", status.Code(err), err)
	}
	if !strings.Contains(status.Convert(err).Message(), DisabledReason) {
		t.Fatalf("message = %q, want %q", status.Convert(err).Message(), DisabledReason)
	}
}
