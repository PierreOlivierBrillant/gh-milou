package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
)

func TestParcoursInteractifDeCreation(t *testing.T) {
	h := nouveau(t, nil)
	csv := h.cohorteCSV()

	code, scripte := h.script(
		"creer",   // Que voulez-vous faire ?
		"fichier", // Source de la liste
		csv,       // Chemin du fichier CSV
		"oui",     // Vérifier les comptes ?
		"tp1",     // Identifiant du travail
		"",        // Gabarit de nom (défaut)
		"",        // Dépôt modèle (aucun)
		"",        // Dossier de fichiers de départ (aucun)
		"",        // Visibilité (privé)
		"oui",     // Inviter chaque personne ?
		"push",    // Droit accordé
		"oui",     // Confirmation finale
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if scripte.Remaining() != 0 {
		t.Errorf("%d réponse(s) inutilisée(s)", scripte.Remaining())
	}

	attendu := []string{"tp1-aminata-d", "tp1-emilie-cote", "tp1-jlpicard"}
	if noms := h.depots(); strings.Join(noms, ",") != strings.Join(attendu, ",") {
		t.Fatalf("dépôts = %v", noms)
	}
	h.contient("Connecté en tant que", "@prof", "3 personnes valides",
		"Récapitulatif", "3 créés", "Bilan")
	if len(h.bilans()) != 1 {
		t.Errorf("bilans écrits : %v", h.bilans())
	}
	// Les réglages sont mémorisés, sans jamais contenir de jeton.
	contenu, err := os.ReadFile(h.Reglages)
	if err != nil {
		t.Fatalf("réglages non enregistrés : %v", err)
	}
	if !strings.Contains(string(contenu), `"assignment": "tp1"`) {
		t.Errorf("réglages = %s", contenu)
	}
	if strings.Contains(strings.ToLower(string(contenu)), "jeton") ||
		strings.Contains(string(contenu), "token") {
		t.Errorf("les réglages contiennent un secret : %s", contenu)
	}
}

func TestCreationNonInteractive(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); len(noms) != 3 {
		t.Fatalf("dépôts = %v", noms)
	}
	// Hors terminal, aucune couleur ni retour chariot.
	if strings.Contains(h.texte(), "\x1b[") || strings.Contains(h.texte(), "\r") {
		t.Errorf("sortie non lisible dans un journal :\n%q", h.texte())
	}
}

// cohorteSansCompte écrit une liste comme le collège la donne : des noms et des
// matricules, aucun compte GitHub.
func (h *harnais) cohorteSansCompte() string {
	h.t.Helper()
	chemin := filepath.Join(h.t.TempDir(), "cohorte.csv")
	if err := os.WriteFile(chemin,
		[]byte("nom_complet,matricule\nNaomi Chéry,2100456\nAminata Diallo,2100789\n"),
		0o644); err != nil {
		h.t.Fatal(err)
	}
	return chemin
}

// Au terminal comme dans le navigateur, qui n'a qu'un matricule reçoit son
// dépôt, nommé par son nom, et le bilan dit qu'il reste un compte à rattacher.
func TestUneCohorteSansCompteRecoitSesDepots(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteSansCompte()
	h.Options.Assignment = "tp1"
	h.Options.Pattern = "{assignment}-{name}"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := strings.Join(h.depots(), ","); noms != "tp1-aminata-diallo,tp1-naomi-chery" &&
		noms != "tp1-naomi-chery,tp1-aminata-diallo" {
		t.Fatalf("dépôts = %s\n%s", noms, h.texte())
	}
	h.contient("à rattacher")
}

// Le gabarit par défaut nomme par le compte : il le dit, et ne crée rien,
// plutôt que de nommer « tp1- » la première et d'annoncer ensuite une
// collision sans cause.
func TestLeGabaritParCompteRefuseUneCohorteSansCompte(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteSansCompte()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code == app.ExitOK {
		t.Fatalf("un refus était attendu\n%s", h.texte())
	}
	h.contient("Naomi Chéry", "{name}")
	h.absent("Collision")
	if noms := h.depots(); len(noms) != 0 {
		t.Fatalf("rien ne devait être créé : %v", noms)
	}
}

func TestNonInteractifExigeLOrganisation(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Org = ""
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("--org")
}

func TestNonInteractifExigeLaListe(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("--roster")
}

func TestNonInteractifExigeLeTravail(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("--assignment")
}

func TestNonInteractifExigeLaConfirmation(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("--yes")
	if noms := h.depots(); len(noms) != 0 {
		t.Errorf("rien ne devait être créé : %v", noms)
	}
}

func TestConfirmationRefuseeNeCreeRien(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"

	code, _ := h.script(
		"oui",  // Vérifier les comptes ?
		"",     // Gabarit de nom
		"",     // Dépôt modèle
		"",     // Fichiers de départ
		"",     // Visibilité
		"oui",  // Inviter ?
		"push", // Droit
		"non",  // Confirmation finale
	)
	if code != app.ExitAborted {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); len(noms) != 0 {
		t.Fatalf("des dépôts ont été créés : %v", noms)
	}
	h.contient("Annulé")
}

func TestSimulationNeCreeRien(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.DryRun = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); len(noms) != 0 {
		t.Fatalf("la simulation a créé des dépôts : %v", noms)
	}
	h.contient("SIMULATION", "à créer", "rien n'a été créé sur GitHub")
}

func TestIdempotenceDeuxExecutions(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("première exécution : %d\n%s", code, h.texte())
	}
	h.Sortie.Reset()
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("seconde exécution : %d\n%s", code, h.texte())
	}
	h.contient("3 déjà présents")
	if noms := h.depots(); len(noms) != 3 {
		t.Errorf("dépôts = %v", noms)
	}
}

func TestEchecIsoleDonneLeCodeUn(t *testing.T) {
	state := fakegh.NewState()
	state.FailOn["PUT /repos/acme/tp1-jlpicard/collaborators/jlpicard"] =
		fakegh.Failure{Status: 422, Message: "Invalid user"}
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitFailure {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("1 en échec", "Relancez la commande")
	if noms := h.depots(); len(noms) != 3 {
		t.Errorf("le lot doit aller à son terme : %v", noms)
	}
}

func TestComptesInexistantsRetires(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV(
		"Émilie Côté,emilie-cote",
		"Personne Fictive,fantome",
	)
	h.Options.Assignment = "tp1"

	code, _ := h.script(
		"oui",     // Vérifier les comptes ?
		"retirer", // Que faire des comptes introuvables ?
		"",        // Gabarit
		"",        // Modèle
		"",        // Fichiers de départ
		"",        // Visibilité
		"oui",     // Inviter ?
		"push",    // Droit
		"oui",     // Confirmation
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); len(noms) != 1 || noms[0] != "tp1-emilie-cote" {
		t.Fatalf("dépôts = %v", noms)
	}
	h.contient("1 compte inexistant", "@fantome")
}

func TestComptesInexistantsBloquentEnModeScript(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Personne Fictive,fantome")
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("introuvables")
}

func TestVerificationDesComptesDesactivee(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true
	h.Options.NoVerifyAccounts = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d", code)
	}
	if appels := h.State.CallCount("GET /users/"); appels != 0 {
		t.Errorf("%d vérification(s) de compte alors qu'elles sont désactivées", appels)
	}
}

func TestLignesRejeteesSignaleesEtIgnorables(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV(
		"Émilie Côté,emilie-cote",
		"Compte cassé,-invalide-",
	)
	h.Options.Assignment = "tp1"

	code, _ := h.script(
		"oui",  // Poursuivre en ignorant les lignes rejetées ?
		"oui",  // Vérifier les comptes ?
		"",     // Gabarit
		"",     // Modèle
		"",     // Fichiers de départ
		"",     // Visibilité
		"oui",  // Inviter ?
		"push", // Droit
		"oui",  // Confirmation
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("1 ligne rejetée", "ligne 3")
	if noms := h.depots(); len(noms) != 1 {
		t.Errorf("dépôts = %v", noms)
	}
}

func TestLignesRejeteesBloquentEnModeScript(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote", "Cassé,-invalide-")
	h.Options.Assignment = "tp1"

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("--yes")
}

func TestFichiersDeDepartDeposes(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Starter = h.squelette()
	h.Options.StarterSet = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	fichiers := h.State.Files("acme/tp1-emilie-cote", "main")
	if sansMarque(fichiers["README.md"]) != "# À faire\n" ||
		fichiers["src/main.py"] != "# à compléter\n" {
		t.Fatalf("contenu = %+v", fichiers)
	}
	if len(fichiers) != 2 {
		t.Errorf("auto_init doit rester faux : %+v", fichiers)
	}
}

func TestDepotModeleNonDeclareRefuseEnModeScript(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "ordinaire", false) // existe, mais n'est pas un modèle
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Template = "acme/ordinaire"
	h.Options.TemplateSet = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Template repository")
}

func TestDepotModeleIntrouvable(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Template = "acme/absent"
	h.Options.TemplateSet = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("introuvable")
}

func TestCreationDepuisUnModele(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "modele-tp", false)
	state.SeedCommit("acme/modele-tp", map[string]string{"consignes.md": "# TP1\n"}, "main")
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Template = "acme/modele-tp"
	h.Options.TemplateSet = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if fichiers := h.State.Files("acme/tp1-emilie-cote", "main"); fichiers["consignes.md"] == "" {
		t.Errorf("contenu = %+v", fichiers)
	}
}

func TestSaisieManuelleEtEnregistrementCSV(t *testing.T) {
	h := nouveau(t, nil)
	cible := filepath.Join(t.TempDir(), "ma-cohorte.csv")

	code, _ := h.script(
		"creer",           // Mode
		"saisie",          // Source de la liste
		"Émilie Côté",     // Nom complet #1
		"emilie-cote",     // Compte GitHub
		"Jean-Luc Picard", // Nom complet #2
		"jlpicard",        // Compte GitHub
		"",                // Nom complet #3 : fin de saisie
		"oui",             // Enregistrer la liste ?
		cible,             // Chemin du fichier
		"oui",             // Vérifier les comptes ?
		"tp1",             // Travail
		"",                // Gabarit
		"",                // Modèle
		"",                // Fichiers de départ
		"",                // Visibilité
		"oui",             // Inviter ?
		"push",            // Droit
		"oui",             // Confirmation
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	contenu, err := os.ReadFile(cible)
	if err != nil {
		t.Fatalf("liste non enregistrée : %v", err)
	}
	if !strings.Contains(string(contenu), "Émilie Côté,emilie-cote") {
		t.Errorf("CSV = %s", contenu)
	}
	if noms := h.depots(); len(noms) != 2 {
		t.Errorf("dépôts = %v", noms)
	}
}

func TestSaisieManuelleRefuseLesDoublons(t *testing.T) {
	h := nouveau(t, nil)
	code, _ := h.script(
		"creer", "saisie",
		"Émilie Côté", "emilie-cote",
		"Une autre", "EMILIE-COTE", // doublon refusé : la question est reposée
		"jlpicard",
		"",    // fin de saisie
		"non", // ne pas enregistrer la liste
		"oui", // vérifier les comptes
		"tp1", "", "", "", "", "oui", "push", "oui",
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("figure déjà dans la liste")
	if noms := h.depots(); len(noms) != 2 {
		t.Errorf("dépôts = %v", noms)
	}
}

func TestVisibilitePubliqueEtDroitPersonnalise(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Visibility = "public"
	h.Options.Permission = "maintain"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	depot := h.State.Repos["acme/tp1-emilie-cote"]
	if depot == nil || depot.Private {
		t.Fatalf("dépôt = %+v", depot)
	}
	h.contient("public", "maintain")
}

func TestVisibiliteInvalideRefusee(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Visibility = "secrete"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
}

func TestSansInvitation(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.NoCollaborator = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d", code)
	}
	if h.State.CallCount("/collaborators/") != 0 {
		t.Error("aucune invitation ne devait partir")
	}
}

func TestOrganisationIntrouvable(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Org = "inconnue"
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("introuvable")
}

func TestRoleInsuffisantSignaleSansBloquer(t *testing.T) {
	state := fakegh.NewState()
	state.MembershipRole = "member"
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Vous êtes « member »")
}

func TestPorteeRepoAbsenteSignalee(t *testing.T) {
	state := fakegh.NewState()
	state.Scopes = "read:org"
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("« repo » semble absente")
}

func TestJetonRefuse(t *testing.T) {
	state := fakegh.NewState()
	state.FailOn["GET /user"] = fakegh.Failure{Status: 401, Message: "Bad credentials"}
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("gh auth status")
}

func TestBilanJSONComplet(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d", code)
	}
	bilans := h.bilans()
	if len(bilans) != 1 {
		t.Fatalf("bilans = %v", bilans)
	}
	contenu, err := os.ReadFile(bilans[0])
	if err != nil {
		t.Fatal(err)
	}
	var bilan struct {
		Org     string `json:"org"`
		Results []struct {
			FullName string `json:"full_name"`
			Repo     string `json:"repo"`
			Status   string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(contenu, &bilan); err != nil {
		t.Fatal(err)
	}
	if bilan.Org != "acme" || len(bilan.Results) != 3 {
		t.Fatalf("bilan = %+v", bilan)
	}
	if bilan.Results[0].FullName == "" {
		t.Error("le nom complet doit figurer au bilan")
	}
	// Un bilan CSV accompagne le JSON.
	if _, err := os.Stat(strings.TrimSuffix(bilans[0], ".json") + ".csv"); err != nil {
		t.Errorf("bilan CSV absent : %v", err)
	}
}

func TestInterruptionPendantLAssistant(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"

	code, _ := h.script("\x03") // interruption dès la première question
	if code != app.ExitAborted {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); len(noms) != 0 {
		t.Errorf("rien ne devait être créé : %v", noms)
	}
}

func TestReglagesNonEnregistres(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Yes = true
	h.Options.NoSaveConfig = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d", code)
	}
	if _, err := os.Stat(h.Reglages); !os.IsNotExist(err) {
		t.Error("--no-save-config ne doit rien écrire")
	}
}

func TestReglagesMemorisesReutilises(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d", code)
	}

	// Une seconde session sans --org reprend l'organisation mémorisée.
	suivant := nouveau(t, h.State)
	suivant.Options.ConfigPath = h.Reglages
	suivant.Options.Org = ""
	suivant.Options.Roster = suivant.cohorteCSV("Jean-Luc Picard,jlpicard")
	suivant.Options.Assignment = "tp2"
	suivant.Options.Yes = true
	if code := suivant.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, suivant.texte())
	}
	if _, existe := h.State.Repos["acme/tp2-jlpicard"]; !existe {
		t.Errorf("dépôts = %v", h.depots())
	}
}

func TestMargeEntreCreationsRespectee(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Yes = true
	h.Options.Delay = 1
	h.Options.DelaySet = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d", code)
	}
	if len(h.Pauses) != 2 {
		t.Errorf("pauses = %v", h.Pauses)
	}
}

func TestPurgeDuCacheSansAuthentification(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.ClearCache = true

	if code := h.executer(&ui.ScriptPrompter{}); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Cache vidé")
	if appels := h.State.AllCalls(); len(appels) != 0 {
		t.Errorf("aucun appel réseau attendu : %v", appels)
	}
}

func TestGabaritInvalideRefuse(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Pattern = "{assignment}-{inconnu}"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("{inconnu}")
	if noms := h.depots(); len(noms) != 0 {
		t.Errorf("rien ne devait être créé : %v", noms)
	}
}

func TestGabaritNonDistinctifRefuse(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV()
	h.Options.Assignment = "tp1"
	h.Options.Pattern = "{assignment}"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("{username}, {name} ou {index}")
}

func TestCollisionDeNomsRefuseeAvantToutEcrit(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.Roster = h.cohorteCSV(
		"Jean Tremblay,jtremblay1",
		"Jean Tremblay,jtremblay2",
	)
	h.Options.Assignment = "tp1"
	h.Options.Pattern = "{assignment}-{name}"
	h.Options.Yes = true
	h.Options.NoVerifyAccounts = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	// Le refus dit ce qu'il faut faire : distinguer les noms, ou déclarer que
	// c'est la même personne sous deux comptes.
	h.contient("Jean Tremblay", "porteraient le même nom")
	if noms := h.depots(); len(noms) != 0 {
		t.Errorf("rien ne devait être créé : %v", noms)
	}
}

func TestFichiersDeDepartSurUneAutreBranche(t *testing.T) {
	state := fakegh.NewState()
	depot := state.AddRepo("acme", "tp1-emilie-cote", true)
	depot.DefaultBranch = "master" // dépôt ancien, branche historique
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Starter = h.squelette()
	h.Options.StarterSet = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if fichiers := h.State.Files("acme/tp1-emilie-cote", "master"); len(fichiers) != 2 {
		t.Fatalf("la branche par défaut du dépôt doit être respectée : %+v", fichiers)
	}
}

func TestDepotGarniPreserveEtDepotVideComplete(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-emilie-cote", true)
	state.SeedCommit("acme/tp1-emilie-cote", map[string]string{"README.md": "# déjà remis\n"}, "main")
	state.AddRepo("acme", "tp1-jlpicard", true) // créé mais resté vide
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote", "Jean-Luc Picard,jlpicard")
	h.Options.Assignment = "tp1"
	h.Options.Starter = h.squelette()
	h.Options.StarterSet = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	garni := h.State.Files("acme/tp1-emilie-cote", "main")
	if garni["README.md"] != "# déjà remis\n" || len(garni) != 1 {
		t.Errorf("le travail déjà remis doit être préservé : %+v", garni)
	}
	vide := h.State.Files("acme/tp1-jlpicard", "main")
	if len(vide) != 2 {
		t.Errorf("un dépôt resté vide doit être complété : %+v", vide)
	}
	h.contient("ignoré (dépôt non vide)")
}

func TestForcerLesFichiersDeDepartDepuisLaLigneDeCommande(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-emilie-cote", true)
	state.SeedCommit("acme/tp1-emilie-cote", map[string]string{"README.md": "# ancien\n"}, "main")
	h := nouveau(t, state)
	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Starter = h.squelette()
	h.Options.StarterSet = true
	h.Options.ForceStarter = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	fichiers := h.State.Files("acme/tp1-emilie-cote", "main")
	if sansMarque(fichiers["README.md"]) != "# À faire\n" {
		t.Errorf("--force-starter doit écraser : %+v", fichiers)
	}
}

func TestParcoursEnModeLigneSortieRedirigee(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.CLI = true
	csv := h.cohorteCSV("Émilie Côté,emilie-cote", "Jean-Luc Picard,jlpicard")

	// Les réponses arrivent sur l'entrée standard ; la sortie est un fichier.
	entree := strings.Join([]string{
		"1",   // Que voulez-vous faire ? → créer
		"1",   // Source de la liste → fichier CSV
		csv,   // Chemin
		"o",   // Vérifier les comptes
		"tp1", // Travail
		"",    // Gabarit par défaut
		"",    // Aucun modèle
		"",    // Aucun dossier de départ
		"",    // Visibilité par défaut
		"o",   // Inviter
		"",    // Droit par défaut
		"o",   // Confirmation
		"",
	}, "\n")

	code := h.executer(ui.NewLinePrompter(h.Console, strings.NewReader(entree)))
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); len(noms) != 2 {
		t.Fatalf("dépôts = %v", noms)
	}
	sortie := h.texte()
	if strings.Contains(sortie, "\x1b[") || strings.Contains(sortie, "\r") {
		t.Errorf("un journal doit rester lisible :\n%q", sortie)
	}
	// Les listes numérotées prennent le relais des flèches.
	h.contient("1  Nouveau travail : un dépôt par personne d'une liste", "2 créés")
}

func TestSelectionParExpressionEnModeLigne(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{"tp1-a", "tp1-b", "tp1-c"} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	h.Options.ManageRequested = true
	h.Options.Manage = "tp1"

	// Les entrées de menu se désignent par leur nom plutôt que par leur rang :
	// une liste numérotée change dès qu'on lui ajoute une action, et une
	// épreuve qui compte les lignes casse à chaque fois sans rien apprendre.
	entree := strings.Join([]string{
		"depots",  // Que faire ? → les dépôts
		"urls",    // → afficher les URL
		"1,3",     // sélection par expression
		"n",       // ne pas enregistrer
		"quitter", // Quitter
		"",
	}, "\n")

	code := h.executer(ui.NewLinePrompter(h.Console, strings.NewReader(entree)))
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("https://github.com/acme/tp1-a", "https://github.com/acme/tp1-c")
	h.absent("https://github.com/acme/tp1-b")
}

func TestCacheRenouveleApresCreation(t *testing.T) {
	h := nouveau(t, nil)
	// Un inventaire périmé est déposé dans le cache avant la création.
	stockage := cache.NewIn(h.CacheDir, true)
	stockage.Set(cache.ReposKey("acme"), []groups.RepoInfo{{Name: "vieil-inventaire"}})

	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}

	relu := cache.NewIn(h.CacheDir, true)
	var inventaire []groups.RepoInfo
	if relu.Get(cache.ReposKey("acme"), cache.ReposTTL, &inventaire) {
		t.Errorf("le cache devait être oublié après une création : %+v", inventaire)
	}
}

func TestCacheConserveApresSimulation(t *testing.T) {
	h := nouveau(t, nil)
	stockage := cache.NewIn(h.CacheDir, true)
	stockage.Set(cache.ReposKey("acme"), []groups.RepoInfo{{Name: "tp0-a"}})

	h.Options.Roster = h.cohorteCSV("Émilie Côté,emilie-cote")
	h.Options.Assignment = "tp1"
	h.Options.Yes = true
	h.Options.DryRun = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d", code)
	}

	relu := cache.NewIn(h.CacheDir, true)
	var inventaire []groups.RepoInfo
	if !relu.Get(cache.ReposKey("acme"), cache.ReposTTL, &inventaire) || len(inventaire) != 1 {
		t.Errorf("une simulation ne doit pas toucher au cache : %+v", inventaire)
	}
}

func TestQuestionsDeCheminOffrentLaCompletion(t *testing.T) {
	h := nouveau(t, nil)
	csv := h.cohorteCSV("Émilie Côté,emilie-cote")
	depart := h.squelette()

	code, scripte := h.script(
		"creer",   // Mode
		"fichier", // Source de la liste
		csv,       // Chemin du fichier CSV
		"oui",     // Vérifier les comptes
		"tp1",     // Travail
		"",        // Gabarit
		"",        // Modèle
		depart,    // Dossier de fichiers de départ
		"",        // Message du commit
		"",        // Visibilité
		"oui",     // Inviter
		"push",    // Droit
		"oui",     // Confirmation
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}

	cas := map[string]complete.Mode{
		"Chemin du fichier CSV":         complete.Path,
		"Dossier de fichiers de départ": complete.Dir,
	}
	for fragment, attendu := range cas {
		question, trouvee := scripte.AskedFor(fragment)
		if !trouvee {
			t.Errorf("question « %s » jamais posée", fragment)
			continue
		}
		if question.Complete != attendu {
			t.Errorf("« %s » : complétion = %v, attendu %v", fragment, question.Complete, attendu)
		}
	}
	// Les questions qui n'attendent pas de chemin n'en proposent pas.
	if question, trouvee := scripte.AskedFor("Identifiant du travail"); !trouvee ||
		question.Complete != complete.None {
		t.Errorf("le travail ne doit pas se compléter comme un chemin : %+v", question)
	}
}

func TestQuestionsDeCheminEnGestion(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-a", true)
	state.AddRepo("acme", "tp1-b", true)
	h := nouveau(t, state)
	h.Options.ManageRequested = true
	h.Options.Manage = "tp1"
	cible := filepath.Join(t.TempDir(), "urls.csv")

	code, scripte := h.script("depots", "urls", "tous", "oui", cible, "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	question, trouvee := scripte.AskedFor("Chemin du fichier")
	if !trouvee || question.Complete != complete.Path {
		t.Errorf("question = %+v, trouvée = %v", question, trouvee)
	}
}

// sansMarque retire d'un contenu la marque invisible, pour que les épreuves de
// contenu comparent ce qui se lit.
//
// La marque est faite de blancs posés sur des lignes vides : la retirer revient
// à rendre à ces lignes leur vacuité. Une épreuve qui la verrait comparerait
// autre chose que ce qu'un étudiant ouvre.
func sansMarque(contenu string) string {
	lignes := strings.Split(contenu, "\n")
	nettoyees := make([]string, 0, len(lignes))
	for _, ligne := range lignes {
		if strings.TrimSpace(ligne) == "" {
			nettoyees = append(nettoyees, "")
			continue
		}
		nettoyees = append(nettoyees, ligne)
	}
	// Les lignes vides ajoutées en queue par la marque disparaissent avec elle.
	for len(nettoyees) > 1 && nettoyees[len(nettoyees)-1] == "" &&
		nettoyees[len(nettoyees)-2] == "" {
		nettoyees = nettoyees[:len(nettoyees)-1]
	}
	return strings.Join(nettoyees, "\n")
}
