package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/domain"
)

var ErrDrift = errors.New("managed target has changed")
var ErrConfirmationRequired = errors.New("explicit removal confirmation required")

// FileSystem is deliberately injected so lifecycle operations can be exercised
// against synthetic fixtures without touching a user's home or workspace.
type FileSystem interface {
	ReadFile(context.Context, string) ([]byte, error)
	Remove(context.Context, string) error
	WriteFile(context.Context, string, []byte) error
}

type Removal struct {
	Path         string
	ExpectedHash string
	CurrentHash  string
	RestoreBytes []byte
	Restore      bool
	Remove       bool
	Conflict     string
}

type RemovalPlan struct{ Items []Removal }

// PreviewRemove selects only paths present in both the receipt ownership
// manifest and a succeeded, reversible operation. Native credentials are not
// represented as file operations and are never selected here.
func PreviewRemove(ctx context.Context, receipt domain.Receipt, plan domain.Plan, fs FileSystem) (RemovalPlan, error) {
	return PreviewRemoveWithEdits(ctx, receipt, plan, fs, nil)
}

// PreviewRemoveWithEdits plans drift-aware inverse config patches from the
// operation edits retained with the installation backup. Unrelated participant
// keys survive; changed Anza-owned keys are left as conflicts.
func PreviewRemoveWithEdits(ctx context.Context, receipt domain.Receipt, plan domain.Plan, fs FileSystem, edits map[string]configedit.Edit) (RemovalPlan, error) {
	owned := make(map[string]bool, len(receipt.OwnedPaths))
	for _, p := range receipt.OwnedPaths {
		owned[p] = true
	}
	states := make(map[string]string, len(receipt.Operations))
	for _, op := range receipt.Operations {
		states[op.ID] = op.State
	}
	items := make([]Removal, 0, len(owned))
	for _, op := range plan.Operations {
		path := filepath.Join(op.TargetRoot, op.RelativePath)
		if !owned[path] || states[op.ID] != "succeeded" || !op.Reversible {
			continue
		}
		if !safePath(path) {
			return RemovalPlan{}, fmt.Errorf("unsafe owned path %q", path)
		}
		b, err := fs.ReadFile(ctx, path)
		if errors.Is(err, iofs.ErrNotExist) {
			items = append(items, Removal{Path: path})
			continue
		}
		if err != nil {
			return RemovalPlan{}, fmt.Errorf("inspect %s: %w", path, err)
		}
		item := Removal{Path: path, ExpectedHash: op.ExpectedPostimageHash}
		if edit, ok := edits[op.ID]; ok {
			inverse, err := configedit.Inverse(b, edit)
			if err != nil || len(inverse.SkippedKeys) != 0 {
				item.Conflict = "Anza-owned configuration key changed"
			} else {
				item.CurrentHash = digest(b)
				item.RestoreBytes = inverse.Bytes
				item.Restore = true
			}
			items = append(items, item)
			continue
		}
		if digest(b) != op.ExpectedPostimageHash {
			item.Conflict = "current contents differ from installed postimage"
		} else {
			item.Remove = true
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return RemovalPlan{Items: items}, nil
}

// ApplyRemove rechecks every preimage immediately before removing it. Conflicts
// and missing paths are retained as valid partial outcomes; repeating the same
// removal is therefore safe.
func ApplyRemove(ctx context.Context, preview RemovalPlan, fs FileSystem, confirmed bool) error {
	if !confirmed {
		return ErrConfirmationRequired
	}
	var errs []error
	for _, item := range preview.Items {
		if item.Conflict != "" {
			errs = append(errs, fmt.Errorf("%w: %s", ErrDrift, item.Path))
			continue
		}
		if !item.Remove && !item.Restore {
			continue
		}
		b, err := fs.ReadFile(ctx, item.Path)
		if errors.Is(err, iofs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("inspect %s: %w", item.Path, err))
			continue
		}
		expected := item.ExpectedHash
		if item.Restore {
			expected = item.CurrentHash
		}
		if digest(b) != expected {
			errs = append(errs, fmt.Errorf("%w: %s", ErrDrift, item.Path))
			continue
		}
		if item.Restore {
			if err := fs.WriteFile(ctx, item.Path, item.RestoreBytes); err != nil {
				errs = append(errs, fmt.Errorf("restore %s: %w", item.Path, err))
			}
			continue
		}
		if err := fs.Remove(ctx, item.Path); err != nil && !errors.Is(err, iofs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", item.Path, err))
		}
	}
	return errors.Join(errs...)
}

func safePath(p string) bool {
	clean := filepath.Clean(p)
	return p != "" && clean != "." && !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
