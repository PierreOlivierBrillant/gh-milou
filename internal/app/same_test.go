package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// commetuveux monte le cas de l'issue : la même personne, @Mr-Commetuveux à
// l'automne et @commetuveuxx à l'hiver, chacun inscrit dans son groupe.
func commetuveux(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-commetuveux", true).PushedAt = "2026-09-20T10:00:00Z"
	state.AddRepo("acme", "h27.4w6.01.tp1.jean-commetuveux", true).PushedAt = "2027-02-10T10:00:00Z"
	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Jean Commetuveux", Username: "Mr-Commetuveux"}},
	})
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "h27", Course: "4w6", Group: "01",
		Students: []roster.Person{{FullName: "Jean Commetuveux", Username: "commetuveuxx"}},
	})
	return h
}

// comptesReunis relit au registre les comptes de la personne derrière un compte.
func (h *harnais) comptesReunis(compte string) string {
	h.t.Helper()
	contenu := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	set, _ := registry.Decode([]byte(contenu))
	return strings.Join(set.Accounts(compte), ",")
}

// Par les drapeaux : « --dry-run » montre ce qui l'emporte sans rien écrire,
// puis la réunion s'écrit, et « --separate » la défait.
func TestReunirEtSeparerParLesDrapeaux(t *testing.T) {
	h := commetuveux(t)
	h.Options.User, h.Options.SameAs, h.Options.DryRun = "Mr-Commetuveux", "commetuveuxx", true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Réunir @Mr-Commetuveux à @commetuveuxx", "@commetuveuxx  @Mr-Commetuveux",
		"Rien n'a été écrit")
	if comptes := h.comptesReunis("commetuveuxx"); comptes != "" {
		t.Fatalf("l'aperçu ne doit rien écrire : %s", comptes)
	}

	h = nouveauDansLeMemeDossier(t, h)
	h.Options.User, h.Options.SameAs = "Mr-Commetuveux", "commetuveuxx"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("sont désormais une même personne", "--user Mr-Commetuveux --separate")
	if comptes := h.comptesReunis("Mr-Commetuveux"); comptes != "commetuveuxx,Mr-Commetuveux" {
		t.Fatalf("registre : %s", comptes)
	}
	// La fiche qui suit montre la personne entière : ses deux cours.
	h.contient("Hiver 2027", "Automne 2026")
	// Aucune liste n'est touchée.
	if locaux := h.groupesLocaux(); strings.Contains(locaux, `"also"`) {
		t.Errorf("listes locales : %s", locaux)
	}

	h = nouveauDansLeMemeDossier(t, h)
	h.Options.User, h.Options.Separate = "Mr-Commetuveux", true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("de nouveau une personne à lui seul")
	if comptes := h.comptesReunis("Mr-Commetuveux"); comptes != "Mr-Commetuveux" {
		t.Fatalf("registre : %s", comptes)
	}
}

// Une faute de frappe ne crée personne au registre.
func TestReunirAUnCompteInconnuSeRefuse(t *testing.T) {
	h := commetuveux(t)
	h.Options.User, h.Options.SameAs = "Mr-Commetuveux", "commetuvex"
	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("n'apparaît nulle part")
}

// Depuis le menu de l'annuaire : choisir la personne, l'autre compte, celui
// qui la désigne, puis confirmer.
func TestReunirDepuisLAnnuaire(t *testing.T) {
	h := commetuveux(t)
	h.Options.StudentsRequested = true
	code, scripte := h.script("reunir", "Mr-Commetuveux", "commetuveuxx", "commetuveuxx", "o",
		"quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	// L'homonyme est proposé, et dit comme tel.
	if menu, propose := scripte.MenuFor("Autre compte"); !propose ||
		!strings.Contains(menu.Options[0].Label, "même nom") {
		t.Fatalf("menu = %+v", menu)
	}
	if comptes := h.comptesReunis("Mr-Commetuveux"); comptes != "commetuveuxx,Mr-Commetuveux" {
		t.Fatalf("registre : %s\n%s", comptes, h.texte())
	}
	// L'annuaire redressé n'a plus qu'une ligne.
	h.contient("— 1 personne")
}

// Les drapeaux qui ne peuvent rien vouloir dire seuls sont refusés.
func TestLesDrapeauxDeReunionAccompagnentUser(t *testing.T) {
	for _, args := range [][]string{
		{"--same-as", "commetuveuxx"},
		{"--separate"},
		{"--user", "a", "--same-as", "b", "--separate"},
	} {
		if _, err := app.Parse(args, &bytes.Buffer{}); err == nil {
			t.Errorf("%v : accepté", args)
		}
	}
	options := analyser(t, "--user", "Mr-Commetuveux", "--same-as", "commetuveuxx")
	if options.SameAs != "commetuveuxx" || options.Separate {
		t.Errorf("options = %+v", options)
	}
}
