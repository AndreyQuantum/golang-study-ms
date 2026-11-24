package memory

import (
	"testing"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/domain"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/repository/repotest"
)

func newRepos(t *testing.T) (domain.Repos, domain.TxManager) {
	t.Helper()
	s := New()
	return s.Repos(), s
}

func TestBudgetRepository(t *testing.T)  { repotest.RunBudgetSuite(t, newRepos) }
func TestExpenseRepository(t *testing.T) { repotest.RunExpenseSuite(t, newRepos) }
func TestTxManager(t *testing.T)         { repotest.RunTxSuite(t, newRepos) }
