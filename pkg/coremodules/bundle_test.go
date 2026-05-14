package coremodules_test

import (
	"slices"
	"testing"

	"github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-modules/pkg/coremodules"
)

func TestBundleComposes(t *testing.T) {
	t.Parallel()

	catalog := module.NewCatalog().Add(coremodules.Bundle()).MustBuild()
	plan, err := module.Compose(catalog)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}

	got := make([]string, 0, len(plan.Modules))
	for _, module := range plan.Modules {
		got = append(got, module.Metadata().ID)
	}
	if !slices.Equal(got, []string{"tenant", "audit", "content"}) {
		t.Fatalf("module order = %v", got)
	}
}
