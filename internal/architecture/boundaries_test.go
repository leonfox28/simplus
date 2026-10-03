package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionDependencyDirection(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			dependency, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(relative, "domain/") && (strings.Contains(dependency, "/internal/application/") || strings.Contains(dependency, "/internal/storage/") || strings.Contains(dependency, "/internal/api/") || strings.Contains(dependency, "/internal/agentapi")) {
				t.Errorf("domain imports outer layer: %s -> %s", relative, dependency)
			}
			if strings.HasPrefix(relative, "application/") {
				for _, forbidden := range []string{"net/http", "os", "os/exec", "/internal/storage/", "/internal/agentapi", "/internal/vowifisupervisor", "/internal/mihomosupervisor", "/internal/feishu", "/internal/mihomoassets"} {
					if dependency == forbidden || strings.Contains(dependency, forbidden) && strings.HasPrefix(forbidden, "/internal/") {
						t.Errorf("application imports adapter: %s -> %s", relative, dependency)
					}
				}
			}
			if strings.Contains(dependency, "/internal/vowifihil") && !strings.HasPrefix(relative, "vowifihil/") {
				t.Errorf("production depends on HIL: %s -> %s", relative, dependency)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
