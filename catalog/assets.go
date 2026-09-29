// Package catalog embeds the reviewed, versioned Anza catalog assets.
package catalog

import "embed"

// Assets contains only the catalog manifest and runtime content trees.
// Provenance sidecars are retained in the source repository and folded into
// manifest.json; they are not part of the runtime export.
//
//go:embed exercises manifest.json packs recipes skills
var Assets embed.FS
