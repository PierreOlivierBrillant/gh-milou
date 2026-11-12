package app_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
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

// omnivox déclare un groupe tel qu'une liste du collège l'inscrit : Naomi Chéry
// n'y a que son matricule, et son dépôt de « tp1 » a été créé sans elle. Son
// compte existe sur GitHub ; il reste à le lui rattacher.
func omnivox(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()
	state.Users["nchery"] = "Naomi Chéry"
	state.AddRepo("acme", "a26.5n6.01.tp1.naomi-chery", true)
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
			{FullName: "Naomi Chéry", StudentID: "2100456"},
		},
	})
	return h
}

// rattacheeANchery vérifie ce que le rattachement laisse derrière lui : une
// seule ligne pour Naomi, sous son compte et avec son matricule, et un registre
// qui a appris l'un avec l'autre — c'est ce qui rapporte à ce compte ce qui
// avait été délivré au matricule. Son nom, que le registre porte désormais, n'a
// plus à être redit par la liste du poste.
func rattacheeANchery(t *testing.T, h *harnais) {
	t.Helper()
	cours, existe := classroom.Open(classroom.PathNextTo(h.Reglages)).Find("acme", "a26.5n6.01")
	if !existe {
		t.Fatal("le groupe a disparu du poste")
	}
	if len(cours.Students) != 2 {
		t.Fatalf("liste du groupe : %d lignes\n%s", len(cours.Students), h.groupesLocaux())
	}
	if naomi, inscrite := cours.Find("nchery"); !inscrite || naomi.StudentID != "2100456" {
		t.Fatalf("Naomi doit garder son matricule sous son compte :\n%s", h.groupesLocaux())
	}
	contenu := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	set, _ := registry.Decode([]byte(contenu))
	fiche, connue := set.PersonOf("nchery")
	if !connue || fiche.StudentID != "2100456" || fiche.FullName != "Naomi Chéry" {
		t.Fatalf("le registre doit apprendre le matricule avec le compte : %+v\n%s",
			fiche, contenu)
	}
}

// Au terminal comme au navigateur, une inscrite que seul son matricule désigne
// reçoit son premier compte par une correction de sa fiche. Elle était refusée
// — « @ n'est pas dans « Groupe 01 ». » — faute d'un compte pour la désigner.
func TestRattacherUnPremierCompteParLeMatricule(t *testing.T) {
	h := omnivox(t)
	corriger(h, "a26.5n6.01", "2100456")
	h.Options.StudentAccount = "nchery"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Fiche de Naomi Chéry dans « a26.5n6.01 »", "à rattacher → @nchery",
		"Fiche de @nchery corrigée")
	h.absent("@ ", "n'est pas dans")
	rattacheeANchery(t, h)
}

// L'assistant la propose aussi, désignée par son matricule : il l'écartait,
// faute d'un compte pour la désigner, si bien qu'aucun geste du terminal ne
// pouvait lui en rattacher un. Ses dépôts portent déjà son nom ; le dépôt de
// Jean-Luc, lui, n'a rien à voir avec elle.
func TestRattacherUnPremierCompteDepuisLaGestion(t *testing.T) {
	h := omnivox(t)
	h.Options.ManageRequested = true
	h.Options.Manage = "a26.5n6.01.tp1"

	// Quel étudiant ; son nom (inchangé) ; son premier compte ; ses dépôts
	// aussi ; confirmation ; et on s'en va.
	code, scripte := h.script("groupe", "etudiant", "2100456", "", "nchery", "oui", "oui",
		"quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	menu, propose := scripte.MenuFor("Quel étudiant")
	if !propose || !strings.Contains(strings.Join(menu.Labels(), "\n"), "Naomi Chéry (2100456)") {
		t.Fatalf("Naomi doit être proposée par son matricule : %+v", menu)
	}
	if posees := strings.Join(scripte.Asked, "\n"); !strings.Contains(posees,
		"Corriger la fiche de Naomi Chéry ?") {
		t.Errorf("la confirmation doit la nommer :\n%s", posees)
	}
	h.contient("Fiche de Naomi Chéry dans", "à rattacher → @nchery", "rien à renommer",
		"Fiche de @nchery corrigée")
	h.absent("@ ")
	if noms := h.depots(); strings.Join(noms, ",") !=
		"a26.5n6.01.tp1.jean-luc-picard,a26.5n6.01.tp1.naomi-chery" {
		t.Fatalf("dépôts : %v", noms)
	}
	rattacheeANchery(t, h)
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
