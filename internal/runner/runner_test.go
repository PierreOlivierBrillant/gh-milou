package runner_test

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plan"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/runner"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
	"github.com/PierreOlivierBrillant/gh-milou/internal/starter"
)

var cohorte = []roster.Person{
	{FullName: "Émilie Côté", Username: "emilie-cote"},
	{FullName: "Jean-Luc Picard", Username: "jlpicard"},
	{FullName: "Aminata Diallo", Username: "aminata-d"},
}

func reglages() config.Settings {
	settings := config.Default()
	settings.Org = "acme"
	settings.Assignment = "tp1"
	settings.DelaySeconds = 0
	return settings
}

func monter(t *testing.T, state *fakegh.State) (*ghapi.Client, *fakegh.Server) {
	t.Helper()
	if state == nil {
		state = fakegh.NewState()
	}
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)
	client, err := ghapi.New(ghapi.Options{
		Host: "127.0.0.1", Token: "jeton-de-test", BaseURL: serveur.URL(),
		Sleep: func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, serveur
}

func construire(t *testing.T, settings config.Settings, gens []roster.Person) []plan.PlannedRepo {
	t.Helper()
	items, err := plan.Build(gens, settings)
	if err != nil {
		t.Fatalf("plan.Build : %v", err)
	}
	return items
}

func squelette(t *testing.T) *starter.Bundle {
	t.Helper()
	racine := t.TempDir()
	if err := os.WriteFile(filepath.Join(racine, "README.md"), []byte("# Départ\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(racine, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(racine, "src", "main.py"), []byte("print('à faire')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := starter.Load(racine)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestCreationComplete(t *testing.T) {
	client, serveur := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte)

	rapport, err := runner.New(client, settings, nil).Run(items, runner.Options{})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if rapport.Count(runner.Created) != 3 || len(rapport.Failures()) != 0 {
		t.Fatalf("bilan = %+v", rapport.Results)
	}
	attendu := []string{"tp1-aminata-d", "tp1-emilie-cote", "tp1-jlpicard"}
	if noms := serveur.State.RepoNames("acme"); strings.Join(noms, ",") != strings.Join(attendu, ",") {
		t.Fatalf("dépôts = %v", noms)
	}
	for _, resultat := range rapport.Results {
		if resultat.Collaborator != ghapi.CollaboratorInvited {
			t.Errorf("%s : invitation = %q", resultat.Repo, resultat.Collaborator)
		}
		if resultat.URL == "" {
			t.Errorf("%s : URL absente", resultat.Repo)
		}
	}
}

func TestIdempotenceRelance(t *testing.T) {
	client, serveur := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte)
	executeur := runner.New(client, settings, nil)

	if _, err := executeur.Run(items, runner.Options{}); err != nil {
		t.Fatal(err)
	}
	serveur.State.AcceptInvitations("acme/tp1-emilie-cote")

	second, err := executeur.Run(items, runner.Options{})
	if err != nil {
		t.Fatalf("relance : %v", err)
	}
	if second.Count(runner.Existing) != 3 || second.Count(runner.Created) != 0 {
		t.Fatalf("la relance doit tout signaler « déjà présent » : %+v", second.Results)
	}
	if len(second.Failures()) != 0 {
		t.Errorf("la relance ne doit rien échouer : %+v", second.Failures())
	}
	if noms := serveur.State.RepoNames("acme"); len(noms) != 3 {
		t.Errorf("aucun dépôt ne doit être recréé : %v", noms)
	}
}

func TestInvitationManquanteRenvoyee(t *testing.T) {
	state := fakegh.NewState()
	// Le dépôt existe déjà, sans invitation : la relance doit la créer.
	state.AddRepo("acme", "tp1-jlpicard", true)
	client, serveur := monter(t, state)
	settings := reglages()
	items := construire(t, settings, []roster.Person{cohorte[1]})

	rapport, err := runner.New(client, settings, nil).Run(items, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rapport.Results[0].Status != runner.Existing {
		t.Fatalf("statut = %q", rapport.Results[0].Status)
	}
	if rapport.Results[0].Collaborator != ghapi.CollaboratorInvited {
		t.Fatalf("invitation = %q", rapport.Results[0].Collaborator)
	}
	if invitations := serveur.State.Invitations["acme/tp1-jlpicard"]; len(invitations) != 1 {
		t.Errorf("invitations = %+v", invitations)
	}
}

func TestEchecIsoleNInterrompsPasLeLot(t *testing.T) {
	state := fakegh.NewState()
	// L'invitation de la deuxième personne échoue, la création reste possible.
	state.FailOn["PUT /repos/acme/tp1-jlpicard/collaborators/jlpicard"] = fakegh.Failure{
		Status: 422, Message: "Invalid user",
	}
	client, serveur := monter(t, state)
	settings := reglages()
	items := construire(t, settings, cohorte)

	rapport, err := runner.New(client, settings, nil).Run(items, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rapport.Failures()) != 1 {
		t.Fatalf("échecs = %+v", rapport.Failures())
	}
	if rapport.Failures()[0].Repo != "tp1-jlpicard" {
		t.Errorf("échec sur %q", rapport.Failures()[0].Repo)
	}
	if !strings.Contains(rapport.Failures()[0].Error, "invitation impossible") {
		t.Errorf("motif = %q", rapport.Failures()[0].Error)
	}
	// Les deux autres personnes ont bien été servies malgré l'échec du milieu.
	if len(serveur.State.RepoNames("acme")) != 3 {
		t.Errorf("dépôts = %v", serveur.State.RepoNames("acme"))
	}
	if rapport.Count(runner.Created) != 2 {
		t.Errorf("créés = %d", rapport.Count(runner.Created))
	}
}

func TestFichiersDeDepart(t *testing.T) {
	client, serveur := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte[:1])

	rapport, err := runner.New(client, settings, squelette(t)).Run(items, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rapport.Results[0].Starter != "2 fichiers" {
		t.Fatalf("fichiers de départ = %q", rapport.Results[0].Starter)
	}
	contenu := serveur.State.Files("acme/tp1-emilie-cote", "main")
	if contenu["README.md"] != "# Départ\n" || contenu["src/main.py"] != "print('à faire')\n" {
		t.Fatalf("contenu = %+v", contenu)
	}
	// auto_init doit rester faux : aucun README généré par GitHub ne s'ajoute.
	if len(contenu) != 2 {
		t.Errorf("%d fichier(s) dans le dépôt : %+v", len(contenu), contenu)
	}
}

func TestDepotDejaGarniPreserve(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-emilie-cote", true)
	state.SeedCommit("acme/tp1-emilie-cote", map[string]string{
		"README.md":    "# mon travail\n",
		"src/rendu.py": "print('déjà remis')\n",
	}, "main")
	client, serveur := monter(t, state)
	settings := reglages()
	items := construire(t, settings, cohorte[:1])

	rapport, err := runner.New(client, settings, squelette(t)).Run(items, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rapport.Results[0].Starter != runner.StarterSkipped {
		t.Fatalf("fichiers de départ = %q", rapport.Results[0].Starter)
	}
	contenu := serveur.State.Files("acme/tp1-emilie-cote", "main")
	if contenu["README.md"] != "# mon travail\n" {
		t.Error("le travail déjà remis doit primer")
	}
	if _, présent := contenu["src/main.py"]; présent {
		t.Error("aucun fichier de départ ne doit être ajouté")
	}
}

func TestForcerLesFichiersDeDepart(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-emilie-cote", true)
	state.SeedCommit("acme/tp1-emilie-cote", map[string]string{"README.md": "# mon travail\n"}, "main")
	client, serveur := monter(t, state)
	settings := reglages()
	items := construire(t, settings, cohorte[:1])

	rapport, err := runner.New(client, settings, squelette(t)).
		Run(items, runner.Options{ForceStarter: true})
	if err != nil {
		t.Fatal(err)
	}
	if rapport.Results[0].Starter != "2 fichiers" {
		t.Fatalf("fichiers de départ = %q", rapport.Results[0].Starter)
	}
	contenu := serveur.State.Files("acme/tp1-emilie-cote", "main")
	if contenu["README.md"] != "# Départ\n" {
		t.Error("--force-starter doit écraser le fichier de même nom")
	}
}

func TestRepriseApresEnvoiInterrompu(t *testing.T) {
	// Le dépôt a été créé mais l'envoi des fichiers a échoué : il est resté vide.
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-emilie-cote", true)
	client, serveur := monter(t, state)
	settings := reglages()
	items := construire(t, settings, cohorte[:1])

	rapport, err := runner.New(client, settings, squelette(t)).Run(items, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rapport.Results[0].Status != runner.Existing {
		t.Fatalf("statut = %q", rapport.Results[0].Status)
	}
	if rapport.Results[0].Starter != "2 fichiers" {
		t.Fatalf("un dépôt vide doit être complété : %q", rapport.Results[0].Starter)
	}
	if len(serveur.State.Files("acme/tp1-emilie-cote", "main")) != 2 {
		t.Error("les fichiers de départ devraient être déposés")
	}
}

func TestEchecDeLEnvoiDesFichiers(t *testing.T) {
	state := fakegh.NewState()
	state.FailOn["POST /repos/acme/tp1-emilie-cote/git/blobs"] = fakegh.Failure{
		Status: 403, Message: "refusing to allow an OAuth App to create or update workflow",
	}
	client, _ := monter(t, state)
	settings := reglages()
	items := construire(t, settings, cohorte[:1])

	rapport, err := runner.New(client, settings, squelette(t)).Run(items, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	resultat := rapport.Results[0]
	if resultat.Status != runner.Failed || resultat.Starter != runner.StarterFailed {
		t.Fatalf("résultat = %+v", resultat)
	}
	if !strings.Contains(resultat.Error, "workflow") {
		t.Errorf("le motif doit rappeler la portée manquante : %q", resultat.Error)
	}
	if resultat.Collaborator != runner.CollaboratorNo {
		t.Errorf("l'invitation ne doit pas être tentée après un échec : %q", resultat.Collaborator)
	}
}

func TestDepuisUnModele(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "modele-tp", false)
	state.SeedCommit("acme/modele-tp", map[string]string{"consignes.md": "# TP1\n"}, "main")
	client, serveur := monter(t, state)
	settings := reglages()
	settings.Template = "acme/modele-tp"
	items := construire(t, settings, cohorte[:1])

	rapport, err := runner.New(client, settings, nil).Run(items, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rapport.Count(runner.Created) != 1 {
		t.Fatalf("bilan = %+v", rapport.Results)
	}
	if contenu := serveur.State.Files("acme/tp1-emilie-cote", "main"); contenu["consignes.md"] == "" {
		t.Errorf("le contenu du modèle doit être repris : %+v", contenu)
	}
}

func TestSimulationNEcritRien(t *testing.T) {
	client, serveur := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte)

	rapport, err := runner.New(client, settings, squelette(t)).
		Run(items, runner.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if rapport.Count(runner.Created) != 3 {
		t.Fatalf("bilan = %+v", rapport.Results)
	}
	if noms := serveur.State.RepoNames("acme"); len(noms) != 0 {
		t.Fatalf("la simulation a créé des dépôts : %v", noms)
	}
	for _, appel := range serveur.State.AllCalls() {
		if strings.HasPrefix(appel, "POST") || strings.HasPrefix(appel, "PUT") ||
			strings.HasPrefix(appel, "DELETE") || strings.HasPrefix(appel, "PATCH") {
			t.Errorf("écriture pendant une simulation : %s", appel)
		}
	}
	if rapport.Results[0].Collaborator != runner.CollaboratorYes {
		t.Errorf("invitation annoncée = %q", rapport.Results[0].Collaborator)
	}
	if rapport.Results[0].Starter != "2 fichiers prévus" {
		t.Errorf("fichiers annoncés = %q", rapport.Results[0].Starter)
	}
}

func TestSimulationSignaleUnDepotDejaGarni(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-emilie-cote", true)
	state.SeedCommit("acme/tp1-emilie-cote", map[string]string{"README.md": "# remis\n"}, "main")
	client, _ := monter(t, state)
	settings := reglages()
	items := construire(t, settings, cohorte[:1])

	rapport, err := runner.New(client, settings, squelette(t)).
		Run(items, runner.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if rapport.Results[0].Starter != runner.StarterSkipped {
		t.Errorf("annonce = %q", rapport.Results[0].Starter)
	}
}

func TestMargeEntreDeuxCreations(t *testing.T) {
	client, _ := monter(t, nil)
	settings := reglages()
	settings.DelaySeconds = 1.5
	items := construire(t, settings, cohorte)

	var pauses []time.Duration
	executeur := runner.New(client, settings, nil).
		WithClock(func(delay time.Duration) { pauses = append(pauses, delay) }, time.Now)
	if _, err := executeur.Run(items, runner.Options{}); err != nil {
		t.Fatal(err)
	}
	// Deux pauses pour trois créations : aucune après la dernière.
	if len(pauses) != 2 {
		t.Fatalf("pauses = %v", pauses)
	}
	for _, pause := range pauses {
		if pause != 1500*time.Millisecond {
			t.Errorf("pause = %v, attendu 1.5s", pause)
		}
	}
}

func TestAucunePauseEnSimulation(t *testing.T) {
	client, _ := monter(t, nil)
	settings := reglages()
	settings.DelaySeconds = 2
	items := construire(t, settings, cohorte)

	var pauses []time.Duration
	executeur := runner.New(client, settings, nil).
		WithClock(func(delay time.Duration) { pauses = append(pauses, delay) }, time.Now)
	if _, err := executeur.Run(items, runner.Options{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if len(pauses) != 0 {
		t.Errorf("la simulation ne doit pas temporiser : %v", pauses)
	}
}

func TestSansInvitation(t *testing.T) {
	client, serveur := monter(t, nil)
	settings := reglages()
	settings.AddCollaborator = false
	items := construire(t, settings, cohorte[:1])

	rapport, err := runner.New(client, settings, nil).Run(items, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rapport.Results[0].Collaborator != runner.CollaboratorNo {
		t.Errorf("invitation = %q", rapport.Results[0].Collaborator)
	}
	if serveur.State.CallCount("/collaborators/") != 0 {
		t.Error("aucune invitation ne devait être envoyée")
	}
}

func TestBilanJSONetCSV(t *testing.T) {
	client, _ := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte)

	rapport, err := runner.New(client, settings, nil).Run(items, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dossier := t.TempDir()
	cheminJSON, cheminCSV, err := rapport.Save(dossier)
	if err != nil {
		t.Fatalf("Save : %v", err)
	}

	contenu, err := os.ReadFile(cheminJSON)
	if err != nil {
		t.Fatal(err)
	}
	var relu runner.Report
	if err := json.Unmarshal(contenu, &relu); err != nil {
		t.Fatalf("JSON illisible : %v", err)
	}
	if relu.Org != "acme" || relu.Assignment != "tp1" || len(relu.Results) != 3 {
		t.Fatalf("bilan relu = %+v", relu)
	}
	if relu.Results[0].FullName != "Émilie Côté" {
		t.Errorf("le nom complet doit figurer au bilan : %+v", relu.Results[0])
	}

	fichier, err := os.Open(cheminCSV)
	if err != nil {
		t.Fatal(err)
	}
	defer fichier.Close()
	lignes, err := csv.NewReader(fichier).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(lignes) != 4 || lignes[0][0] != "nom_complet" {
		t.Fatalf("CSV = %+v", lignes)
	}
	// Aucun secret ne doit se retrouver dans un fichier écrit.
	for _, chemin := range []string{cheminJSON, cheminCSV} {
		données, _ := os.ReadFile(chemin)
		if strings.Contains(string(données), "jeton-de-test") {
			t.Errorf("%s contient le jeton", chemin)
		}
	}
}

func TestProgressionAppeleeUneFoisParDepot(t *testing.T) {
	client, _ := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte)

	var vus []string
	_, err := runner.New(client, settings, nil).Run(items, runner.Options{
		OnProgress: func(index, total int, result runner.Result) {
			if total != 3 || index != len(vus)+1 {
				t.Errorf("progression incohérente : %d/%d", index, total)
			}
			vus = append(vus, result.Repo)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(vus) != 3 {
		t.Errorf("progression = %v", vus)
	}
}

// Un groupe cloisonné doit le rester. Le dépôt qu'on vient de créer est donné
// à l'équipe enseignante du groupe dans la foulée : sans cela, l'enseignant ne
// verrait pas les dépôts qu'il vient lui-même de distribuer.
func TestUnDepotCreeVaALEquipeEnseignante(t *testing.T) {
	client, serveur := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte)

	// C'est le slug qui désigne l'équipe dans l'API : GitHub y remplace le
	// point par un tiret, et le nom ne s'y substitue pas.
	equipe, err := client.CreateTeam("acme", "a26.5n6.01.enseignants", "", "closed")
	if err != nil {
		t.Fatal(err)
	}
	rapport, err := runner.New(client, settings, nil).Run(items, runner.Options{
		TeacherTeam: equipe.Slug, TeacherPermission: "admin",
	})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if len(rapport.Failures()) != 0 {
		t.Fatalf("échecs = %+v", rapport.Failures())
	}
	partages := serveur.State.TeamRepos["acme/"+equipe.Slug]
	if len(partages) != len(items) {
		t.Fatalf("dépôts partagés = %v", partages)
	}
	for _, item := range items {
		if droit := partages["acme/"+item.Name]; droit != "admin" {
			t.Errorf("%s : droit de l'équipe = %q", item.Name, droit)
		}
	}
}

// Sans équipe enseignante, rien n'est accordé : un groupe non cloisonné se
// comporte exactement comme avant.
func TestSansEquipeEnseignanteRienNEstAccorde(t *testing.T) {
	client, serveur := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte)

	if _, err := runner.New(client, settings, nil).Run(items, runner.Options{}); err != nil {
		t.Fatal(err)
	}
	if len(serveur.State.TeamRepos) != 0 {
		t.Fatalf("partages = %v", serveur.State.TeamRepos)
	}
}

// La marque est déposée dans le README du dépôt, et elle ne s'y voit pas.
func TestLaDistributionDeposeUneMarqueInvisible(t *testing.T) {
	client, serveur := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte)

	rapport, err := runner.New(client, settings, squelette(t)).Run(items,
		runner.Options{Sign: true})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if rapport.Signed() != len(cohorte) {
		t.Fatalf("dépôts signés : %d sur %d", rapport.Signed(), len(cohorte))
	}

	marques := map[string]bool{}
	for _, result := range rapport.Results {
		if result.Signature == "" {
			t.Fatalf("dépôt non signé : %+v", result)
		}
		// Deux personnes ne peuvent pas porter la même marque : la détection
		// les confondrait, et c'est ce qu'elle existe pour éviter.
		if marques[result.Signature] {
			t.Fatalf("deux dépôts portent la marque %s", result.Signature)
		}
		marques[result.Signature] = true

		fichiers := serveur.State.Files("acme/"+result.Repo, "main")
		contenu, present := fichiers[signature.ReadmeFile]
		if !present {
			t.Fatalf("aucun README dans « %s »", result.Repo)
		}
		// Rien de visible n'a changé : le texte de départ est là, intact.
		if !strings.HasPrefix(contenu, "# Départ\n") {
			t.Fatalf("le README a été abîmé : %q", contenu)
		}
		token, signee := signature.First([]byte(contenu))
		if !signee || signature.Text(token) != result.Signature {
			t.Fatalf("la marque du bilan ne se retrouve pas dans le dépôt : %+v", result)
		}
	}

	// Les marques se versent au registre : c'est le seul endroit qui dira un
	// jour à qui appartient l'une d'elles.
	delivrees := rapport.Signatures("a26.5n6.01.tp1")
	if len(delivrees) != len(cohorte) {
		t.Fatalf("marques à verser : %+v", delivrees)
	}
	for _, delivree := range delivrees {
		if delivree.Username == "" || delivree.Assignment != "a26.5n6.01.tp1" {
			t.Fatalf("marque incomplète : %+v", delivree)
		}
	}
}

// Une inscrite sans compte — toute cohorte importée du collège — reçoit son
// dépôt, et sa marque. Celle-ci se verse au registre comme les autres, sous son
// matricule : le jeton est tiré au hasard, et ce qui n'y monte pas est perdu
// pour de bon, même une fois son compte rattaché.
func TestLaMarqueDUneInscriteSansCompteSeVerseSousSonMatricule(t *testing.T) {
	client, _ := monter(t, nil)
	settings := reglages()
	settings.NamePattern = "{assignment}-{name}"
	gens := []roster.Person{
		{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		{FullName: "Naomi Chéry", StudentID: "2100456"},
	}

	rapport, err := runner.New(client, settings, squelette(t)).Run(
		construire(t, settings, gens), runner.Options{Sign: true})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if rapport.Signed() != len(gens) {
		t.Fatalf("dépôts signés : %d sur %d", rapport.Signed(), len(gens))
	}

	delivrees := rapport.Signatures("a26.5n6.01.tp1")
	if len(delivrees) != len(gens) {
		t.Fatalf("marques à verser : %+v", delivrees)
	}
	for _, delivree := range delivrees {
		// Chacun est désigné comme dans sa liste de classe : son compte, à
		// défaut son matricule — jamais les deux, rien ne change pour qui a un
		// compte.
		parCompte := delivree.Username == "jlpicard" && delivree.StudentID == ""
		parMatricule := delivree.Username == "" && delivree.StudentID == "2100456"
		if !parCompte && !parMatricule {
			t.Fatalf("marque mal rattachée : %+v", delivree)
		}
	}
	if _, _, err := (signature.Book{}).With(delivrees); err != nil {
		t.Fatalf("le registre refuse une marque délivrée : %v", err)
	}
}

// Sans le drapeau, rien n'est déposé : c'est un réglage, et il se respecte.
func TestSansLeDrapeauAucuneMarqueNEstDeposee(t *testing.T) {
	client, serveur := monter(t, nil)
	settings := reglages()
	items := construire(t, settings, cohorte)

	rapport, err := runner.New(client, settings, squelette(t)).Run(items, runner.Options{})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if rapport.Signed() != 0 {
		t.Fatalf("%d dépôts signés à tort", rapport.Signed())
	}
	fichiers := serveur.State.Files("acme/"+rapport.Results[0].Repo, "main")
	if len(signature.Find([]byte(fichiers[signature.ReadmeFile]))) != 0 {
		t.Fatal("une marque a été déposée sans qu'on la demande")
	}
}

// Un dépôt sans README n'est pas signé : déposer un fichier que personne n'a
// demandé serait une modification visible, pour une marque qui se veut
// invisible.
func TestUnDepotSansReadmeNEstPasSigne(t *testing.T) {
	racine := t.TempDir()
	if err := os.WriteFile(filepath.Join(racine, "main.py"), []byte("print('x')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := starter.Load(racine)
	if err != nil {
		t.Fatal(err)
	}

	client, serveur := monter(t, nil)
	settings := reglages()
	rapport, err := runner.New(client, settings, bundle).Run(
		construire(t, settings, cohorte[:1]), runner.Options{Sign: true})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if rapport.Signed() != 0 {
		t.Fatalf("un dépôt sans README a été signé : %+v", rapport.Results)
	}
	fichiers := serveur.State.Files("acme/"+rapport.Results[0].Repo, "main")
	if _, present := fichiers[signature.ReadmeFile]; present {
		t.Fatal("un README a été créé pour l'occasion")
	}
}
