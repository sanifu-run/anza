package catalog

import (
	"fmt"

	content "github.com/sanifu-run/anza/catalog"
)

// LoadBundled validates and loads the immutable catalog embedded in the Anza
// binary. It performs no network access and writes no files.
func LoadBundled() (*Catalog, error) {
	loaded, err := Load(content.Assets)
	if err != nil {
		return nil, fmt.Errorf("loading bundled catalog: %w", err)
	}
	return loaded, nil
}
