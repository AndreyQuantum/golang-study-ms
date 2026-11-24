package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const ledgerModule = "github.com/AndreyQuantum/golang-study-ms/ledger"

// Gateway знает только публичный пакет ledger: ни БД, ни Redis, ни репозиториев.
func TestGatewayDependsOnlyOnLedgerPublicAPI(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go не найден в PATH")
	}
	cmd := exec.Command(goBin, "list", "-json", "./...")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	require.NoError(t, cmd.Run(), stderr.String())

	forbidden := []string{"database/sql", "github.com/jackc/pgx", "github.com/redis/"}
	dec := json.NewDecoder(&out)
	seen := 0
	for {
		var p struct {
			ImportPath string
			Imports    []string
		}
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else {
			require.NoError(t, err)
		}
		seen++
		for _, imp := range p.Imports {
			for _, f := range forbidden {
				if strings.HasPrefix(imp, f) {
					t.Errorf("%s импортирует %s", p.ImportPath, imp)
				}
			}
			if strings.HasPrefix(imp, ledgerModule) && imp != ledgerModule {
				t.Errorf("%s импортирует %s, разрешён только %s", p.ImportPath, imp, ledgerModule)
			}
		}
	}
	require.Positive(t, seen)
}
