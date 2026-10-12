package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestIdentityCommandFailuresDoNotLeakErrorPayloads(t *testing.T) {
	secret := "synthetic-sensitive-credential"
	for _, phase := range []string{"configuration", "runtime"} {
		for _, failure := range []error{
			errors.New("password=" + secret),
			errors.New("postgres://operator:" + secret + "@db/database"),
			&net.DNSError{Err: secret, Name: secret},
			errors.Join(context.DeadlineExceeded, errors.New("Bearer "+secret)),
		} {
			var output bytes.Buffer
			served := false
			setup := func() error {
				if phase == "configuration" {
					return failure
				}
				return nil
			}
			serve := func(context.Context) error { served = true; return failure }
			if code := runIdentity(context.Background(), setup, serve, &output); code != 1 {
				t.Fatalf("exit code %d", code)
			}
			if strings.Contains(output.String(), secret) || !strings.Contains(output.String(), "raw error details withheld") {
				t.Fatalf("unsafe/missing diagnostic: %q", output.String())
			}
			if served != (phase == "runtime") {
				t.Fatal("serve invoked after setup failure")
			}
		}
	}
}

func TestIdentityCommandExpectedCancellationAndJoinedFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		err  error
		code int
	}{
		{context.Canceled, 0},
		{errors.Join(context.Canceled, errors.New("cleanup failed")), 1},
		{context.DeadlineExceeded, 1},
		{nil, 0},
	} {
		var output bytes.Buffer
		code := runIdentity(ctx, func() error { return nil }, func(context.Context) error { return tc.err }, &output)
		if code != tc.code {
			t.Fatalf("code=%d want=%d err=%v", code, tc.code, tc.err)
		}
		if (output.Len() == 0) != (tc.code == 0) {
			t.Fatal("incorrect diagnostic presence")
		}
	}
}

func TestIdentityCommandUnexpectedCancellationIsFailure(t *testing.T) {
	var output bytes.Buffer
	if code := runIdentity(context.Background(), func() error { return nil }, func(context.Context) error { return context.Canceled }, &output); code != 1 {
		t.Fatalf("unexpected cancellation exit=%d", code)
	}
}
