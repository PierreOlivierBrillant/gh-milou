package app_test

import (
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/users"
)

// college monte deux sessions et deux cours autour des mêmes personnes :
// Émilie les traverse tous, Picard n'a fait que l'automne, Aminata que l'hiver.
func college(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()
	envois := map[string]string{
		"a26.5n6.01.tp1.jean-luc-picard": "2026-09-01T10:00:00Z",
		"a26.5n6.01.tp1.emilie-cote":     "2026-09-20T10:00:00Z",
		"a26.4w6.01.projet.emilie-cote":  "2026-11-05T10:00:00Z",
		"h27.5n6.02.tp1.emilie-cote":     "2027-02-10T10:00:00Z",
	}
	for nom, envoi := range envois {
		state.AddRepo("acme", nom, true).PushedAt = envoi
	}

	h := nouveau(t, state)
	h.Options.StudentsRequested = true
	emilie := roster.Person{FullName: "Émilie Côté", Username: "emilie-cote"}
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Jean-Luc Picard", Username: "jlpicard"}, emilie,
		},
	})
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "4w6", Group: "01",
		Students: []roster.Person{emilie},
	})
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "h27", Course: "5n6", Group: "02",
		Students: []roster.Person{
			emilie, {FullName: "Aminata Diallo", Username: "aminata-d"},
		},
	})
	return h
}

// declarer retient un groupe dans le fichier de la session, comme le ferait
// une déclaration faite depuis l'une ou l'autre des interfaces.
func (h *harnais) declarer(cours classroom.Classroom) {
	h.t.Helper()
	cours.Defaults = classroom.DefaultsFrom(config.Default())
	store := classroom.Open(classroom.PathNextTo(h.Reglages))
	if _, err := store.Save(cours); err != nil {
		h.t.Fatalf("déclaration de « %s » : %v", cours.Scope(), err)
	}
}

// L'annuaire rassemble ce que la liste d'un groupe ne peut pas montrer : une
// personne, et tous les cours qu'elle a suivis.
func TestAnnuaireDuTerminal(t *testing.T) {
	h := college(t)
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Utilisateurs de « acme » — 3 personnes",
		"Émilie Côté", "Jean-Luc Picard", "Aminata Diallo",
		// Une même personne porte ses trois places sur une seule ligne, de la
		// session la plus récente à la plus ancienne.
		"h27.5n6.02  a26.4w6.01  a26.5n6.01",
		"2027-02-10")
}

// Les critères de la ligne de commande valent aussi pour l'annuaire : c'est le
// même paquet qui les applique dans les trois interfaces.
func TestAnnuaireFiltreParSessionEtParCours(t *testing.T) {
	h := college(t)
	h.Options.Filter = users.Filter{Session: "h27"}
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("2 affichées", "session Hiver 2027", "Aminata Diallo", "Émilie Côté")
	h.absent("Jean-Luc Picard")

	// Les deux critères portent sur la même inscription : Picard a bien fait
	// 5N6, mais pas à l'hiver.
	croise := college(t)
	croise.Options.Filter = users.Filter{Session: "a26", Course: "4w6"}
	if code := croise.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, croise.texte())
	}
	croise.contient("1 affichée", "Émilie Côté", "cours 4w6")
	croise.absent("Jean-Luc Picard", "Aminata Diallo")
}

// Le menu du terminal offre les mêmes critères que la barre du navigateur.
func TestAnnuaireFiltreDepuisLeMenu(t *testing.T) {
	h := college(t)
	code, scripte := h.script("session", "a26", "cours", "5n6", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if scripte.Remaining() != 0 {
		t.Errorf("%d réponse(s) inutilisée(s)", scripte.Remaining())
	}
	h.contient("session Automne 2026 · cours 5n6", "Jean-Luc Picard", "Émilie Côté")

	// Les sessions sont proposées nommées, de la plus récente à la plus ancienne.
	menu, trouve := h.dernierMenu("Session")
	if !trouve {
		t.Fatal("aucun menu de session")
	}
	if len(menu.Options) != 3 || menu.Options[1].Value != "h27" ||
		menu.Options[2].Value != "a26" {
		t.Fatalf("sessions proposées : %+v", menu.Options)
	}
}
