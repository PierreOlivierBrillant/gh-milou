package app_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// groupeAuxDeuxTravaux monte un groupe dont les deux travaux ont chacun un
// dépôt pour Émilie, déjà garni, et qui la nomme dans sa liste. Rien ne l'a
// encore invitée.
func groupeAuxDeuxTravaux(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()
	for _, travail := range []string{"tp1", "tp2"} {
		nom := "a26.5n6.01." + travail + ".emilie-cote"
		state.AddRepo("acme", nom, true)
		state.SeedCommit("acme/"+nom, map[string]string{"main.py": "print(1)\n"}, "main")
	}
	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	h.Options.ManageRequested = true
	h.Options.Manage = "a26.5n6.01"
	return h
}

// Sur un groupe entier, chaque dépôt s'invite au nom de son groupe : qui n'a
// jamais été invité reçoit sa première invitation, comme depuis son travail.
// Avant, le groupe entier ne visait personne, et seules les invitations
// expirées pouvaient partir.
func TestUnGroupeEntierEnvoieLesPremieresInvitations(t *testing.T) {
	h := groupeAuxDeuxTravaux(t)
	h.Options.SendInvitations = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	for _, travail := range []string{"tp1", "tp2"} {
		invitations := h.State.Invitations["acme/a26.5n6.01."+travail+".emilie-cote"]
		if len(invitations) != 1 || invitations[0].Login != "emilie-cote" {
			t.Errorf("%s : invitations = %+v, attendu une première invitation d'Émilie",
				travail, invitations)
		}
	}
	h.absent("Aucune invitation ne manque")
}

// Sur un groupe entier, chaque dépôt reçoit les champs de son propre travail :
// le nom de l'étudiante, celui du travail, son échéance.
func TestUnGroupeEntierRemplitLeGabaritDeChaqueTravail(t *testing.T) {
	h := groupeAuxDeuxTravaux(t)
	fichier := filepath.Join(t.TempDir(), "NOTE.md")
	if err := os.WriteFile(fichier, []byte("{nom_etudiant} · {travail}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.Options.PushFile = fichier

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	for _, travail := range []string{"tp1", "tp2"} {
		depot := "acme/a26.5n6.01." + travail + ".emilie-cote"
		if obtenu := h.State.Files(depot, "main")["NOTE.md"]; obtenu != "Émilie Côté · "+travail+"\n" {
			t.Errorf("%s a reçu %q", depot, obtenu)
		}
	}
	h.absent("laisse un champ vide", "laissent un champ vide")
}
