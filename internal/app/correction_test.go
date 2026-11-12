package app_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// corriger prépare la correction d'un étudiant en ligne de commande.
func corriger(h *harnais, place, compte string) {
	h.Options.StudentsRequested = false
	h.Options.Manage = place
	h.Options.Student = compte
}

// Le « Renommer… » du navigateur, en ligne de commande : la fiche change, et
// les dépôts du groupe suivent — ceux des autres groupes, non.
func TestCorrigerUnEtudiantEnLigneDeCommande(t *testing.T) {
	h := college(t)
	corriger(h, "a26.5n6.01", "emilie-cote")
	h.Options.FullName = "Émilie Côté-Roy"
	h.Options.RenameRepos = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Fiche de @emilie-cote dans", "Émilie Côté → Émilie Côté-Roy",
		"a26.5n6.01.tp1.emilie-cote-roy", "Fiche de @emilie-cote corrigée",
		"1 dépôt renommé.")

	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.4w6.01.projet.emilie-cote,a26.5n6.01.tp1.emilie-cote-roy," +
		"a26.5n6.01.tp1.jean-luc-picard,h27.5n6.02.tp1.emilie-cote"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}
	contenu := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	set, _ := registry.Decode([]byte(contenu))
	if set.Name("emilie-cote") != "Émilie Côté-Roy" {
		t.Fatalf("registre = %+v", set.All())
	}

	// Le nom vaut partout, et les dépôts restés sous l'ancien sont toujours à
	// elle : sa fiche le montre.
	fiche := nouveauDansLeMemeDossier(t, h)
	fiche.Options.Student, fiche.Options.FullName = "", ""
	fiche.Options.RenameRepos = false
	fiche.Options.User = "emilie-cote"
	if code := fiche.muet(); code != app.ExitOK {
		t.Fatalf("fiche : code = %d\n%s", code, fiche.texte())
	}
	fiche.contient("Émilie Côté-Roy — @emilie-cote", "projet", "h27.5n6.02")
}

// Une simulation montre le plan et n'écrit rien : ni la fiche, ni les dépôts.
func TestCorrigerUnEtudiantEnSimulation(t *testing.T) {
	h := college(t)
	corriger(h, "a26.5n6.01", "emilie-cote")
	h.Options.FullName = "Émilie Côté-Roy"
	h.Options.RenameRepos = true
	h.Options.DryRun = true
	avant := h.depots()

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("a26.5n6.01.tp1.emilie-cote-roy", "Simulation")
	h.absent("corrigée")
	if apres := h.depots(); strings.Join(apres, ",") != strings.Join(avant, ",") {
		t.Fatalf("dépôts :\navant %v\naprès %v", avant, apres)
	}
	if contenu := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]; contenu != "" {
		t.Fatalf("le registre a été écrit :\n%s", contenu)
	}
}

// Sans « --yes », une commande scriptée ne décide pas à la place de qui la
// lance : elle refuse, et le dit.
func TestCorrigerUnEtudiantDemandeConfirmation(t *testing.T) {
	h := college(t)
	corriger(h, "a26.5n6.01", "emilie-cote")
	h.Options.FullName = "Émilie Côté-Roy"
	avant := h.depots()

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("--yes")
	if apres := h.depots(); strings.Join(apres, ",") != strings.Join(avant, ",") {
		t.Fatalf("dépôts :\navant %v\naprès %v", avant, apres)
	}
}

// Le travail géré désigne aussi bien son groupe ; un compte qui n'existe pas
// sur GitHub est refusé avant toute écriture.
func TestCorrigerUnEtudiantRefuseUnCompteInexistant(t *testing.T) {
	h := college(t)
	corriger(h, "a26.5n6.01.tp1", "emilie-cote")
	h.Options.StudentAccount = "fantome-introuvable"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("« fantome-introuvable » n'existe pas sur GitHub")
	h.absent("corrigée")
}

// Au terminal, la gestion d'un travail corrige un étudiant de son groupe comme
// le fait la liste du groupe au navigateur.
func TestCorrigerUnEtudiantDepuisLaGestion(t *testing.T) {
	h := gestion(t, cohorteNommee(t), "a26.5n6.01.tp1")
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{Username: "jlpicard"},
		},
	})

	// Quel étudiant ; son nom ; son compte (inchangé) ; ses dépôts aussi ;
	// confirmation ; et on s'en va.
	code, scripte := h.script("groupe", "etudiant", "emilie-cote", "Émilie Côté-Roy", "", "oui", "oui",
		"quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if question, posee := scripte.AskedFor("Nom complet"); !posee ||
		question.Default != "Émilie Côté" {
		t.Errorf("le nom courant doit être proposé : %+v", question)
	}
	h.contient("Fiche de @emilie-cote corrigée", "1 dépôt renommé.")
	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.emilie-cote-roy,a26.5n6.01.tp1.jlpicard,a26.5n6.01.tp2.jlpicard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}
}

// Nommer depuis la fiche ne renomme aucun dépôt ; la fiche dit donc, groupe par
// groupe, la commande qui le fait — et se tait quand ils portent déjà le nom.
func TestLaFicheDitCommentRenommerLesDepots(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "emilie-cote"
	h.Options.FullName = "Émilie Côté-Roy"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	for _, place := range []string{"a26.5n6.01", "a26.4w6.01", "h27.5n6.02"} {
		h.contient("gh milou --manage " + place + " --student emilie-cote --rename-repos")
	}

	meme := college(t)
	meme.Options.StudentsRequested = false
	meme.Options.User = "emilie-cote"
	meme.Options.FullName = "Émilie Côté"
	if code := meme.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, meme.texte())
	}
	meme.absent("--rename-repos")
}
