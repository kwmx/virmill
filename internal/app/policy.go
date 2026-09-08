package app

import (
	"context"
	"io"
	"math"
	"os"
	"time"

	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func readDeclaration(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := openDeclaration(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > wire.MaxFrame {
		return nil, domain.Fail("INVALID_INPUT", "declaration must be a bounded regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, wire.MaxFrame+1))
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || len(b) > wire.MaxFrame {
		return nil, domain.Fail("SOURCE_CHANGED", "declaration changed during reading or exceeds the document limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Service) backupPolicy(ctx context.Context, method string, r Request) (any, error) {
	if r.Path == "" || r.ID != "" || r.Action != "" || r.After != 0 || r.Apply != nil {
		return nil, domain.Fail("INVALID_INPUT", "backup policy requires a document path and optional preview parameters")
	}
	count, after := 5, time.Now().UTC()
	for key, value := range r.Input {
		if method != "backup.policy.preview" {
			return nil, domain.Fail("INVALID_INPUT", "policy validation takes no input parameters")
		}
		switch key {
		case "after":
			text, ok := value.(string)
			if !ok {
				return nil, domain.Fail("INVALID_INPUT", "after must be an RFC3339 timestamp")
			}
			var err error
			after, err = time.Parse(time.RFC3339Nano, text)
			if err != nil {
				return nil, domain.Fail("INVALID_INPUT", "after must be an RFC3339 timestamp")
			}
		case "count":
			n, ok := value.(float64)
			if !ok || math.IsNaN(n) || n < 1 || n > 32 || math.Trunc(n) != n {
				return nil, domain.Fail("INVALID_INPUT", "count must be an integer from 1 to 32")
			}
			count = int(n)
		default:
			return nil, domain.Fail("INVALID_INPUT", "unknown backup policy preview parameter; rejected values withheld")
		}
	}
	b, err := readDeclaration(ctx, r.Path)
	if err != nil {
		return nil, err
	}
	var report protection.PolicyReport
	if method == "backup.policy.preview" {
		report, err = protection.PreviewPolicyContext(ctx, b, after, count)
	} else {
		report, err = protection.ValidatePolicy(b)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return report, nil
}
