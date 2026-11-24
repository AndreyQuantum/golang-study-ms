package ledger_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const module = "github.com/AndreyQuantum/golang-study-ms/ledger"

type pkgInfo struct {
	ImportPath string
	Imports    []string
}

// listPackages возвращает не-тестовые импорты всех пакетов модуля: направление зависимостей проверяется по ним.
func listPackages(t *testing.T) map[string][]string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go не найден в PATH")
	}
	cmd := exec.Command(goBin, "list", "-json", "./...")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	require.NoError(t, cmd.Run(), stderr.String())

	result := map[string][]string{}
	dec := json.NewDecoder(&out)
	for {
		var p pkgInfo
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else {
			require.NoError(t, err)
		}
		result[strings.TrimPrefix(p.ImportPath, module)] = p.Imports
	}
	require.NotEmpty(t, result)
	return result
}

func hasImport(imports []string, forbidden string) bool {
	for _, imp := range imports {
		if imp == forbidden {
			return true
		}
		// корневой пакет модуля сравнивается только точно: иначе под запрет попали бы все его подпакеты
		if forbidden != module && strings.HasPrefix(imp, forbidden+"/") {
			return true
		}
	}
	return false
}

func requireNoImports(t *testing.T, pkgs map[string][]string, pkg string, forbidden ...string) {
	t.Helper()
	imports, ok := pkgs[pkg]
	require.True(t, ok, "пакет %q не найден", pkg)
	for _, f := range forbidden {
		if hasImport(imports, f) {
			t.Errorf("пакет %q не должен импортировать %q", module+pkg, f)
		}
	}
}

func TestDependencyDirection(t *testing.T) {
	pkgs := listPackages(t)

	infra := []string{
		"database/sql",
		"github.com/jackc/pgx",
		"github.com/redis/go-redis",
		module + "/internal/db",
		module + "/internal/cache",
	}
	repos := []string{module + "/internal/repository/memory", module + "/internal/repository/pg"}

	t.Run("domain depends on nothing of ledger", func(t *testing.T) {
		for _, imp := range pkgs["/internal/domain"] {
			if strings.HasPrefix(imp, module) {
				t.Errorf("domain импортирует %q", imp)
			}
		}
		requireNoImports(t, pkgs, "/internal/domain", infra...)
	})

	t.Run("repositories depend only on domain", func(t *testing.T) {
		for _, pkg := range []string{"/internal/repository/memory", "/internal/repository/pg"} {
			requireNoImports(t, pkgs, pkg,
				module, // корневой пакет ledger
				module+"/internal/service",
				module+"/internal/app",
				"github.com/AndreyQuantum/golang-study-ms/gateway",
			)
		}
	})

	t.Run("service knows no storage", func(t *testing.T) {
		requireNoImports(t, pkgs, "/internal/service", append(append([]string{}, infra...), repos...)...)
	})

	t.Run("public package is a thin entry point", func(t *testing.T) {
		requireNoImports(t, pkgs, "", append(append([]string{}, infra...), repos...)...)
	})

	t.Run("only app wires infrastructure", func(t *testing.T) {
		wired := []string{module + "/internal/repository/pg", module + "/internal/db", module + "/internal/cache"}
		for pkg, imports := range pkgs {
			if pkg == "/internal/app" || pkg == "/internal/testdb" {
				continue // testdb импортирует internal/db и используется только из _test.go
			}
			for _, w := range wired {
				if hasImport(imports, w) {
					t.Errorf("пакет %q не должен импортировать %q: подключать инфраструктуру может только app", module+pkg, w)
				}
			}
		}
	})
}
