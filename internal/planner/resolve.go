package planner

import (
	"fmt"

	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/domain"
)

func resolveSelection(recommendation domain.Recommendation, registry Registry) ([]domain.Recipe, []domain.Pack, error) {
	recipes := make(map[string]domain.Recipe)
	packs := make(map[string]domain.Pack)
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	orderedIDs := make([]string, 0)
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("%w: catalog dependency cycle at %q", ErrInvalidInput, id)
		}
		visiting[id] = true
		if recipe, ok := registry.Recipe(id); ok {
			recipes[id] = recipe
			for _, prerequisite := range recipe.Prerequisites {
				if err := visit(prerequisite); err != nil {
					return err
				}
			}
		} else if pack, ok := registry.Pack(id); ok {
			packs[id] = pack
			for _, prerequisite := range pack.Prerequisites {
				if err := visit(prerequisite); err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("%w: unknown recipe or pack %q", ErrInvalidInput, id)
		}
		visiting[id] = false
		visited[id] = true
		if _, recipe := recipes[id]; recipe {
			orderedIDs = append(orderedIDs, id)
		}
		return nil
	}
	for _, id := range sortedUnique(recommendation.SelectedPackIDs) {
		if _, ok := registry.Pack(id); !ok {
			return nil, nil, fmt.Errorf("%w: unknown pack %q", ErrInvalidInput, id)
		}
		if err := visit(id); err != nil {
			return nil, nil, err
		}
	}
	for _, id := range sortedUnique(recommendation.SelectedRecipeIDs) {
		if _, ok := registry.Recipe(id); !ok {
			return nil, nil, fmt.Errorf("%w: unknown recipe %q", ErrInvalidInput, id)
		}
		if err := visit(id); err != nil {
			return nil, nil, err
		}
	}
	ordered := make([]domain.Recipe, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		ordered = append(ordered, recipes[id])
	}
	selectedPacks := make([]domain.Pack, 0, len(packs))
	for id := range packs {
		selectedPacks = append(selectedPacks, packs[id])
	}
	// Packs are emitted deterministically while their prerequisite traversal
	// remains validated by the same cycle detector.
	sortPacks(selectedPacks)
	return ordered, selectedPacks, nil
}

func checkConfigEdit(edit configedit.Edit, observed ObservedFile) error {
	if edit.Format != configedit.TOML || edit.PostimageHash == "" || digestBytes(edit.Bytes) != edit.PostimageHash {
		return fmt.Errorf("%w: adapter postimage metadata does not match output bytes", ErrConflict)
	}
	if edit.InputMissing {
		if observed.Exists || edit.PreimageHash != digestBytes(nil) {
			return fmt.Errorf("%w: config appeared after adapter planning", ErrConflict)
		}
		return nil
	}
	if !observed.Exists || edit.PreimageHash == "" || digestBytes(observed.Bytes) != edit.PreimageHash {
		return fmt.Errorf("%w: config preimage changed after adapter planning", ErrConflict)
	}
	return nil
}

func sortPacks(packs []domain.Pack) {
	for i := 1; i < len(packs); i++ {
		for j := i; j > 0 && packs[j].ID < packs[j-1].ID; j-- {
			packs[j], packs[j-1] = packs[j-1], packs[j]
		}
	}
}
