package ghapi_test

import (
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
)

// Le premier commit est le dernier que GitHub rend : on saute à la dernière
// page plutôt que de dérouler tout l'historique.
func TestLePremierCommitEstAuBoutDeLHistorique(t *testing.T) {
	state := fakegh.NewState()
	repo := state.AddRepo("acme", "tp1-loiselle", true)
	repo.History = fakegh.Commits(
		"2026-11-02T09:15:00Z",
		"2026-10-18T14:00:00Z",
		"2026-09-01T12:30:00Z",
	)
	client, faux := client(t, state)
	defer faux.Close()

	moment, err := client.FirstCommit("acme", "tp1-loiselle")
	if err != nil {
		t.Fatalf("premier commit : %v", err)
	}
	if moment.UTC() != time.Date(2026, 9, 1, 12, 30, 0, 0, time.UTC) {
		t.Fatalf("moment = %s", moment)
	}
}

// Un dépôt vide n'a pas de premier commit, et ne pas en avoir n'est pas une
// erreur : c'est une devinette qui se tait.
func TestUnDepotVideNaPasDePremierCommit(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-neuf", true)
	client, faux := client(t, state)
	defer faux.Close()

	moment, err := client.FirstCommit("acme", "tp1-neuf")
	if err != nil {
		t.Fatalf("un dépôt vide a fait échouer la lecture : %v", err)
	}
	if !moment.IsZero() {
		t.Fatalf("moment = %s", moment)
	}
}
