// Package executor applies only typed operations already present in an approved plan.
package executor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/domain"
)

var (
	ErrUnsupportedOperation = errors.New("unsupported approved operation")
	ErrPreimageConflict     = errors.New("operation preimage changed")
	ErrStaleApproval        = errors.New("approval is stale or does not match this plan")
	ErrInterrupted          = errors.New("operation interrupted at an injected boundary")
	ErrRecoveryManual       = errors.New("operation outcome requires manual reconciliation")
)

type Snapshot struct {
	Exists  bool
	Symlink bool
	Regular bool
	Bytes   []byte
	Mode    fs.FileMode
}
type ConfigPayload struct{ Edit configedit.Edit }
type ArtifactPayload struct{ Bytes []byte }
type Payload interface{ executorPayload() }

func (ConfigPayload) executorPayload()   {}
func (ArtifactPayload) executorPayload() {}

// Effects implementations resolve logical roots and must compare the expected
// preimage atomically at the mutation boundary to close the TOCTOU window.
type Effects interface {
	Read(context.Context, domain.Operation) (Snapshot, error)
	ApplyConfig(context.Context, domain.Operation, string, configedit.Edit) error
	PublishArtifact(context.Context, domain.Operation, string, ArtifactPayload) error
}

func validatePayload(p Payload, op domain.Operation) error {
	switch op.Kind {
	case "write_config":
		v, ok := p.(ConfigPayload)
		if !ok {
			return fmt.Errorf("%w: write_config requires ConfigPayload", ErrUnsupportedOperation)
		}
		if v.Edit.PostimageHash != digest(v.Edit.Bytes) || v.Edit.PostimageHash != op.ExpectedPostimageHash {
			return fmt.Errorf("%w: config postimage does not match approved hash", ErrPreimageConflict)
		}
		if op.PreimageHash != "" && v.Edit.PreimageHash != op.PreimageHash {
			return fmt.Errorf("%w: config preimage metadata differs from plan", ErrPreimageConflict)
		}
		if op.PreimageHash == "" && !v.Edit.InputMissing {
			return fmt.Errorf("%w: missing-file plan requires missing-file edit", ErrPreimageConflict)
		}
	case "install_artifact":
		v, ok := p.(ArtifactPayload)
		if !ok {
			return fmt.Errorf("%w: install_artifact requires ArtifactPayload", ErrUnsupportedOperation)
		}
		if digest(v.Bytes) != op.ExpectedPostimageHash {
			return fmt.Errorf("%w: artifact bytes differ from approved hash", ErrPreimageConflict)
		}
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedOperation, op.Kind)
	}
	return nil
}
func applyEffect(ctx context.Context, effects Effects, op domain.Operation, expected string, payload Payload) error {
	switch op.Kind {
	case "write_config":
		return effects.ApplyConfig(ctx, op, expected, payload.(ConfigPayload).Edit)
	case "install_artifact":
		return effects.PublishArtifact(ctx, op, expected, payload.(ArtifactPayload))
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedOperation, op.Kind)
	}
}
