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

// groupeOuDeposer monte un travail de deux dépôts : Émilie a déjà remis un
// fichier du même nom, celui de Jean-Luc est encore vide.
func groupeOuDeposer(t *testing.T) (*harnais, string) {
	t.Helper()
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	state.SeedCommit("acme/a26.5n6.01.tp1.emilie-cote", map[string]string{
		"NOTE.md": "ma version\n", "main.py": "print(1)\n",
	}, "main")
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	state.SeedCommit("acme/a26.5n6.01.tp1.jean-luc-picard", map[string]string{
		"main.py": "print(2)\n",
	}, "main")

	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		},
	})
	h.Options.ManageRequested = true
	h.Options.Manage = "a26.5n6.01.tp1"

	fichier := filepath.Join(t.TempDir(), "NOTE.md")
	if err := os.WriteFile(fichier, []byte("Bonjour {prenom}, {cours} gr. {groupe}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return h, fichier
}

func TestLaLigneDeCommandeLitPushFile(t *testing.T) {
	options, err := app.Parse([]string{"--push-file", "a.md", "--push-path", "docs/a.md"}, nil)
	if err != nil {
		t.Fatalf("Parse : %v", err)
	}
	// Déposer un fichier touche les dépôts d'un travail : le drapeau ouvre la
	// gestion de lui-même.
	if options.PushFile != "a.md" || options.PushPath != "docs/a.md" || !options.ManageRequested {
		t.Errorf("options = %+v", options)
	}
	// Les drapeaux qui l'accompagnent ne vont pas sans lui.
	if _, err := app.Parse([]string{"--push-overwrite"}, nil); err == nil {
		t.Error("--push-overwrite accepté sans --push-file")
	}
}

func TestDrapeauPushFileDeposeSansEcraser(t *testing.T) {
	h, fichier := groupeOuDeposer(t)
	h.Options.PushFile = fichier

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if got := h.State.Files("acme/a26.5n6.01.tp1.jean-luc-picard", "main")["NOTE.md"]; got != "Bonjour Jean-Luc, 5n6 gr. 01\n" {
		t.Errorf("Jean-Luc a reçu %q", got)
	}
	if got := h.State.Files("acme/a26.5n6.01.tp1.emilie-cote", "main")["NOTE.md"]; got != "ma version\n" {
		t.Errorf("le fichier d'Émilie a été écrasé : %q", got)
	}
	h.contient("1 ajouté", "1 conservé", "--push-overwrite")
}

func TestDrapeauPushFileRemplaceSurDemande(t *testing.T) {
	h, fichier := groupeOuDeposer(t)
	h.Options.PushFile = fichier
	h.Options.PushOverwrite = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if got := h.State.Files("acme/a26.5n6.01.tp1.emilie-cote", "main")["NOTE.md"]; got != "Bonjour Émilie, 5n6 gr. 01\n" {
		t.Errorf("Émilie a %q", got)
	}
}

func TestDrapeauPushFileSimule(t *testing.T) {
	h, fichier := groupeOuDeposer(t)
	h.Options.PushFile = fichier
	h.Options.DryRun = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Bonjour Émilie, 5n6 gr. 01", "ajouté", "différent, conservé", "Simulation")
	if _, existe := h.State.Files("acme/a26.5n6.01.tp1.jean-luc-picard", "main")["NOTE.md"]; existe {
		t.Error("la simulation a écrit")
	}
}

func TestAssistantDeposeUnFichier(t *testing.T) {
	h, fichier := groupeOuDeposer(t)
	code, _ := h.script(
		"depots", "fichier", fichier,
		"docs/{compte}.md", // chemin dans le dépôt, lui aussi rempli
		"",                 // message par défaut
		"o",                // remplir les champs du contenu
		"o",                // déposer
		"quitter",
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Aperçu pour", "Bonjour Émilie", "2 ajoutés")
	if got := h.State.Files("acme/a26.5n6.01.tp1.jean-luc-picard", "main")["docs/jlpicard.md"]; got != "Bonjour Jean-Luc, 5n6 gr. 01\n" {
		t.Errorf("Jean-Luc a reçu %q", got)
	}
}

// Un fichier du même nom, déjà modifié par l'étudiant, n'est remplacé que si
// on le confirme — et la réponse proposée est non.
func TestAssistantDemandeAvantDeRemplacer(t *testing.T) {
	h, fichier := groupeOuDeposer(t)
	code, _ := h.script("depots", "fichier", fichier, "", "", "", "", "", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("différent, conservé", "1 ajouté")
	if got := h.State.Files("acme/a26.5n6.01.tp1.emilie-cote", "main")["NOTE.md"]; got != "ma version\n" {
		t.Errorf("le fichier d'Émilie a été écrasé : %q", got)
	}
}
