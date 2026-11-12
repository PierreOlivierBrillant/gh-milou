package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// Les gestes que l'assistant range dans ses sections ont leur drapeau, et
// chacun ouvre la gestion de lui-même : il porte sur les dépôts d'un préfixe.
func TestLaLigneDeCommandeLitLesGestesDUnTravail(t *testing.T) {
	cas := []struct {
		args  []string
		verif func(*app.Options) bool
	}{
		{[]string{"--access"}, func(o *app.Options) bool { return o.Access }},
		{[]string{"--urls", "--export-csv", "liste.csv"}, func(o *app.Options) bool {
			return o.URLs && o.ExportCSV == "liste.csv"
		}},
		// Sans valeur, le dossier est celui que l'assistant proposerait.
		{[]string{"--clone", "--manage", "tp1"}, func(o *app.Options) bool {
			return o.CloneSet && o.Clone == "" && o.Manage == "tp1"
		}},
		{[]string{"--clone", "clones"}, func(o *app.Options) bool { return o.Clone == "clones" }},
		{[]string{"--update-clones"}, func(o *app.Options) bool {
			return o.UpdateClonesSet && o.UpdateClones == ""
		}},
		{[]string{"--add-collaborator", "jlpicard", "--repo", "tp1-jlpicard"},
			func(o *app.Options) bool { return o.AddCollaborator == "jlpicard" && o.Repo == "tp1-jlpicard" }},
		{[]string{"--delete-repo", "tp1-jlpicard"}, func(o *app.Options) bool {
			return o.DeleteRepo == "tp1-jlpicard"
		}},
	}
	for _, c := range cas {
		options, err := app.Parse(c.args, nil)
		if err != nil {
			t.Errorf("%v : %v", c.args, err)
			continue
		}
		if !c.verif(options) || !options.ManageRequested {
			t.Errorf("%v : options = %+v", c.args, options)
		}
	}

	// Ce qui ne peut pas vouloir dire quelque chose est refusé avant tout appel.
	for _, args := range [][]string{
		{"--access", "--urls"},                                    // deux gestes à la fois
		{"--add-collaborator", "jlpicard"},                        // sur quel dépôt ?
		{"--repo", "tp1-jlpicard"},                                // pour quoi faire ?
		{"--remove-collaborator", "pas un compte", "--repo", "x"}, // compte illisible
	} {
		if _, err := app.Parse(args, nil); err == nil {
			t.Errorf("%v est accepté", args)
		}
	}
}

func TestDrapeauAccessMontreQuiAAcces(t *testing.T) {
	state := groupe(t)
	state.AddCollaborator("acme/tp1-emilie-cote", "emilie-cote", "push")
	state.Invite("acme/tp1-jlpicard", "jlpicard", "push")
	h := gestion(t, state, "tp1")
	h.Options.Access = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Accès des dépôts de « tp1 »", "emilie-cote", "jlpicard (invité)",
		"3 dépôts inspectés")
}

// On exporte ce qu'on voit : les critères de la liste s'appliquent, comme au
// navigateur, et un dépôt sans nom connu porte son suffixe.
func TestDrapeauURLsEtExportCSVSuiventLaListe(t *testing.T) {
	state := groupe(t)
	h := gestion(t, state, "tp1")
	cible := filepath.Join(t.TempDir(), "urls.csv")
	h.Options.URLs, h.Options.ExportCSV = true, cible
	h.Options.Filter.Text = "picard"

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("https://github.com/acme/tp1-jlpicard", "1 URL enregistrée")
	h.absent("https://github.com/acme/tp1-emilie-cote")
	contenu, err := os.ReadFile(cible)
	if err != nil {
		t.Fatalf("CSV non écrit : %v", err)
	}
	if string(contenu) != "nom_complet,depot,url\n"+
		"Jean-Luc Picard,tp1-jlpicard,https://github.com/acme/tp1-jlpicard\n" {
		t.Errorf("CSV = %q", contenu)
	}

	tous := nouveauDansLeMemeDossier(t, h)
	tous.Options.ExportCSV = cible
	if code := tous.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, tous.texte())
	}
	if contenu, _ := os.ReadFile(cible); !strings.Contains(string(contenu), "aminata-d,tp1-aminata-d,") {
		t.Errorf("un dépôt sans nom connu doit porter son suffixe : %s", contenu)
	}

	// Une simulation n'écrit rien, pas même un fichier local.
	simule := nouveauDansLeMemeDossier(t, h)
	ailleurs := filepath.Join(t.TempDir(), "jamais.csv")
	simule.Options.ExportCSV, simule.Options.DryRun = ailleurs, true
	if code := simule.muet(); code != app.ExitOK {
		t.Fatalf("simulation : code = %d\n%s", code, simule.texte())
	}
	if _, err := os.Stat(ailleurs); err == nil {
		t.Error("la simulation a écrit le fichier")
	}
}

func TestDrapeauxCloneEtMiseAJourDesClones(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git est absent : rien à cloner")
	}
	state := fakegh.NewState()
	origines := t.TempDir()
	for _, nom := range []string{"tp1-emilie-cote", "tp1-jlpicard"} {
		depot := state.AddRepo("acme", nom, true)
		depot.URLOverride = depotGit(t, origines, nom)
	}
	destination := filepath.Join(t.TempDir(), "clones")

	// Simulé, le clonage ne crée même pas le dossier.
	simule := gestion(t, state, "tp1")
	simule.Options.CloneSet, simule.Options.Clone, simule.Options.DryRun = true, destination, true
	if code := simule.muet(); code != app.ExitOK {
		t.Fatalf("simulation : code = %d\n%s", code, simule.texte())
	}
	simule.contient("2 dépôts vers", "Simulation : rien n'a été cloné.")
	if _, err := os.Stat(destination); err == nil {
		t.Fatal("la simulation a créé le dossier")
	}

	clonage := nouveauDansLeMemeDossier(t, simule)
	clonage.Options.CloneSet, clonage.Options.Clone = true, destination
	if code := clonage.muet(); code != app.ExitOK {
		t.Fatalf("clonage : code = %d\n%s", code, clonage.texte())
	}
	clonage.contient("2 clonés")
	for _, nom := range []string{"tp1-emilie-cote", "tp1-jlpicard"} {
		if _, err := os.Stat(filepath.Join(destination, nom, "README.md")); err != nil {
			t.Errorf("%s non cloné : %v", nom, err)
		}
	}

	misesAJour := nouveauDansLeMemeDossier(t, simule)
	misesAJour.Options.UpdateClonesSet, misesAJour.Options.UpdateClones = true, destination
	if code := misesAJour.muet(); code != app.ExitOK {
		t.Fatalf("mise à jour : code = %d\n%s", code, misesAJour.texte())
	}
	misesAJour.contient("2 mis à jour")
}

func TestDrapeauxDonnentEtRetirentLAccesDUnCompte(t *testing.T) {
	state := groupe(t)

	// Le dépôt se désigne par son suffixe, comme partout dans l'assistant.
	ajout := gestion(t, state, "tp1")
	ajout.Options.AddCollaborator, ajout.Options.Repo = "jlpicard", "jlpicard"
	ajout.Options.Permission = "push"
	if code := ajout.muet(); code != app.ExitOK {
		t.Fatalf("ajout : code = %d\n%s", code, ajout.texte())
	}
	ajout.contient("@jlpicard : invitation envoyée (push).")
	if invitations := state.Invitations["acme/tp1-jlpicard"]; len(invitations) != 1 ||
		invitations[0].Login != "jlpicard" || invitations[0].Permission != "push" {
		t.Fatalf("invitations = %+v", invitations)
	}

	// Retirer un compte qui n'a qu'une invitation l'annule.
	retrait := nouveauDansLeMemeDossier(t, ajout)
	retrait.Options.RemoveCollaborator, retrait.Options.Repo = "jlpicard", "tp1-jlpicard"
	if code := retrait.muet(); code != app.ExitOK {
		t.Fatalf("retrait : code = %d\n%s", code, retrait.texte())
	}
	retrait.contient("Invitation annulée.")
	if invitations := state.Invitations["acme/tp1-jlpicard"]; len(invitations) != 0 {
		t.Errorf("invitations restantes = %+v", invitations)
	}

	// Retirer un collaborateur établi lui ôte son accès.
	state.AddCollaborator("acme/tp1-emilie-cote", "emilie-cote", "push")
	collaboratrice := nouveauDansLeMemeDossier(t, ajout)
	collaboratrice.Options.RemoveCollaborator = "Emilie-Cote"
	collaboratrice.Options.Repo = "tp1-emilie-cote"
	if code := collaboratrice.muet(); code != app.ExitOK {
		t.Fatalf("retrait d'un collaborateur : code = %d\n%s", code, collaboratrice.texte())
	}
	collaboratrice.contient("@emilie-cote n'a plus accès à « tp1-emilie-cote ».")

	// Ce qui ne désigne rien se refuse, sans rien écrire.
	for _, essai := range []struct {
		ajouter, retirer, depot, permission, attendu string
	}{
		{retirer: "personne", depot: "tp1-jlpicard", attendu: "n'a ni accès ni invitation"},
		{ajouter: "fantome", depot: "tp1-jlpicard", attendu: "n'existe pas sur GitHub"},
		{ajouter: "jlpicard", depot: "tp9-ailleurs", attendu: "n'est pas un dépôt de « tp1 »"},
		{ajouter: "jlpicard", depot: "tp1-jlpicard", permission: "tout", attendu: "Droit"},
	} {
		refus := nouveauDansLeMemeDossier(t, ajout)
		refus.Options.AddCollaborator, refus.Options.RemoveCollaborator = essai.ajouter, essai.retirer
		refus.Options.Repo, refus.Options.Permission = essai.depot, essai.permission
		if code := refus.muet(); code != app.ExitValidation {
			t.Errorf("%+v : code = %d, attendu un refus\n%s", essai, code, refus.texte())
		}
		refus.contient(essai.attendu)
	}
	if invitations := state.Invitations["acme/tp1-jlpicard"]; len(invitations) != 0 {
		t.Errorf("un refus a invité quelqu'un : %+v", invitations)
	}
}

// Supprimer au drapeau demande ce que l'assistant fait retaper : le nom complet
// du dépôt, exactement. Le suffixe ou une autre casse, qui désignent un dépôt
// partout ailleurs, n'en tiennent pas lieu, et « --yes » n'y change rien.
func TestDrapeauDeleteRepoExigeLeNomExact(t *testing.T) {
	state := groupe(t)
	for _, nom := range []string{"jlpicard", "TP1-JLPICARD"} {
		h := gestion(t, state, "tp1")
		h.Options.DeleteRepo, h.Options.Yes = nom, true
		if code := h.muet(); code != app.ExitValidation {
			t.Errorf("« %s » : code = %d, attendu un refus\n%s", nom, code, h.texte())
		}
		h.contient("n'est le nom exact d'aucun dépôt")
	}
	if state.CallCount("DELETE /repos/acme/tp1-jlpicard") != 0 {
		t.Fatal("une suppression a été tentée sur un nom approché")
	}

	simule := gestion(t, state, "tp1")
	simule.Options.DeleteRepo, simule.Options.DryRun = "tp1-jlpicard", true
	if code := simule.muet(); code != app.ExitOK {
		t.Fatalf("simulation : code = %d\n%s", code, simule.texte())
	}
	simule.contient("Suppression définitive de acme/tp1-jlpicard",
		"Simulation : « tp1-jlpicard » n'a pas été supprimé.")
	if _, existe := state.Repos["acme/tp1-jlpicard"]; !existe {
		t.Fatal("la simulation a supprimé le dépôt")
	}

	h := gestion(t, state, "tp1")
	h.Options.DeleteRepo = "tp1-jlpicard"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("« tp1-jlpicard » supprimé.")
	if _, existe := state.Repos["acme/tp1-jlpicard"]; existe {
		t.Error("le dépôt existe encore")
	}
}

// Sans la portée « delete_repo », rien ne part : la suppression serait refusée,
// et la ligne de commande dit comment l'obtenir.
func TestDrapeauDeleteRepoSansLaPorteeNeTenteRien(t *testing.T) {
	state := groupe(t)
	state.Scopes = "repo, read:org"
	h := gestion(t, state, "tp1")
	h.Options.DeleteRepo = "tp1-jlpicard"

	if code := h.muet(); code != app.ExitFailure {
		t.Fatalf("code = %d, attendu un échec\n%s", code, h.texte())
	}
	h.contient("n'a pas la portée « delete_repo »", "Annulé : rien n'a été supprimé.")
	if state.CallCount("DELETE /repos/acme/tp1-jlpicard") != 0 {
		t.Error("la suppression n'aurait pas dû être tentée")
	}
}

// Un groupe entier n'est pas un travail. Les gestes qui portent sur un seul
// travail le refusent en nommant ceux qu'on peut gérer à la place — au lieu de
// prétendre que le groupe ne dit pas sa place, ou, pour un déplacement, de
// fondre tous ses travaux en un seul.
func TestUnGroupeEntierNePassePasPourUnTravail(t *testing.T) {
	cas := []struct {
		nom    string
		poser  func(*app.Options)
		attend string
	}{
		{"déplacement", func(o *app.Options) { o.MoveTo, o.Yes = "a26.5n6.02", true }, "Un déplacement"},
		{"renommage", func(o *app.Options) { o.RenameTo, o.Yes = "projet", true }, "Un renommage"},
		{"date cible", func(o *app.Options) { o.DueSet, o.Due = true, "2026-10-01" }, "--due : une date cible"},
		{"comparaison", func(o *app.Options) { o.Plagiarism = true }, "La comparaison des copies"},
		{"index", func(o *app.Options) { o.PublishIndex = true }, "Un index d'empreintes"},
		{"envoi", func(o *app.Options) { o.ExportZip = filepath.Join(t.TempDir(), "envoi.zip") },
			"L'envoi anonymisé"},
	}
	for _, c := range cas {
		state := cohorteNommee(t)
		h := gestion(t, state, "a26.5n6.01")
		c.poser(h.Options)
		if code := h.muet(); code != app.ExitValidation {
			t.Errorf("%s : code = %d, attendu un refus\n%s", c.nom, code, h.texte())
			continue
		}
		h.contient(c.attend + " porte sur un seul travail, et « a26.5n6.01 » en réunit 2 : " +
			"gérez l'un d'eux — a26.5n6.01.tp1, a26.5n6.01.tp2.")
		h.absent("ne dit pas à quel groupe il appartient")
		if renommes := state.CallCount("PATCH /repos/"); renommes != 0 {
			t.Errorf("%s : %d dépôts renommés malgré le refus", c.nom, renommes)
		}
	}
}

// Corriger un étudiant se fait dans la liste de son groupe : l'assistant
// l'accepte depuis le groupe entier, comme « --student » le faisait déjà.
func TestCorrigerUnEtudiantDepuisLeGroupeEntier(t *testing.T) {
	h := gestion(t, cohorteNommee(t), "a26.5n6.01")
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{Username: "jlpicard"},
		},
	})
	code, _ := h.script("groupe", "etudiant", "emilie-cote", "Émilie Côté-Roy", "", "oui", "oui",
		"quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Fiche de @emilie-cote corrigée", "1 dépôt renommé.")

	// Un cours réunit des groupes : c'est l'un d'eux qu'il faut gérer.
	cours := gestion(t, cohorteNommee(t), "a26.5n6")
	if code, _ := cours.script("groupe", "etudiant", "quitter"); code != app.ExitOK {
		t.Fatalf("cours : code = %d\n%s", code, cours.texte())
	}
	cours.contient("« a26.5n6 » n'est pas un groupe : un étudiant se corrige dans la liste " +
		"du sien. Gérez « a26.5n6.01 ».")
}
