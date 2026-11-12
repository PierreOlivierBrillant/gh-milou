package broadcast_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/broadcast"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
)

var jour = time.Date(2026, 9, 30, 10, 0, 0, 0, time.Local)

func groupe() classroom.Classroom {
	return classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote", StudentID: "2412345"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		},
	}
}

func depots(noms ...string) []groups.Repo {
	repos := make([]groups.Repo, 0, len(noms))
	for _, nom := range noms {
		repos = append(repos, groups.Repo{Name: nom, Suffix: nom[strings.LastIndex(nom, ".")+1:]})
	}
	return repos
}

func TestLeGabaritRemplitChaqueChampPourChaqueEtudiant(t *testing.T) {
	contexte, destinataires := broadcast.ForClassroom(groupe(), "a26.5n6.01.tp1", nil,
		depots("a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jean-luc-picard"), jour)
	plan, err := broadcast.Prepare(broadcast.Request{
		Content: []byte("# {travail} — {nom_etudiant}\n{prenom}/{nom_famille} @{compte} " +
			"{matricule}\n{cours} gr. {groupe}, {session_nom} ({session}), le {date}\n"),
		Path: "CONSIGNES-{compte}.md",
	}, contexte, destinataires)
	if err != nil {
		t.Fatalf("Prepare : %v", err)
	}
	voulu := "# tp1 — Émilie Côté\nÉmilie/Côté @emilie-cote 2412345\n" +
		"5n6 gr. 01, Automne 2026 (a26), le 2026-09-30\n"
	if obtenu := string(plan.Items[0].Content); obtenu != voulu {
		t.Errorf("contenu =\n%s\nvoulu =\n%s", obtenu, voulu)
	}
	if plan.Items[0].Path != "CONSIGNES-emilie-cote.md" {
		t.Errorf("chemin = %q", plan.Items[0].Path)
	}
	if plan.Items[0].Message != "Ajoute CONSIGNES-emilie-cote.md" {
		t.Errorf("message = %q", plan.Items[0].Message)
	}
	// Jean-Luc n'a pas de matricule dans la liste : le champ reste vide, et
	// le plan le dit plutôt que de le taire.
	if !reflect.DeepEqual(plan.Items[1].Empty, []string{"matricule"}) || plan.Incomplete() != 1 {
		t.Errorf("champs vides = %v, incomplets = %d", plan.Items[1].Empty, plan.Incomplete())
	}
}

// Un fichier de code porte des accolades qui ne sont pas des champs : elles
// restent telles quelles, et sont nommées pour qu'une faute de frappe se voie.
func TestUnChampInconnuResteTelQuel(t *testing.T) {
	contexte, destinataires := broadcast.ForClassroom(groupe(), "a26.5n6.01.tp1", nil,
		depots("a26.5n6.01.tp1.emilie-cote"), jour)
	plan, err := broadcast.Prepare(broadcast.Request{
		Content: []byte(`print(f"{name} {x + 1}") # {cours} {group}`),
		Path:    "main.py",
	}, contexte, destinataires)
	if err != nil {
		t.Fatalf("Prepare : %v", err)
	}
	if obtenu := string(plan.Items[0].Content); obtenu != `print(f"{name} {x + 1}") # 5n6 {group}` {
		t.Errorf("contenu = %q", obtenu)
	}
	if !reflect.DeepEqual(plan.Unknown, []string{"group", "name"}) {
		t.Errorf("inconnus = %v", plan.Unknown)
	}
}

func TestUnFichierBinaireOuBrutNeRecoitPasLeGabarit(t *testing.T) {
	contexte, destinataires := broadcast.ForClassroom(groupe(), "a26.5n6.01.tp1", nil,
		depots("a26.5n6.01.tp1.emilie-cote"), jour)
	binaire := []byte("\x89PNG\x00{cours}")
	plan, err := broadcast.Prepare(broadcast.Request{Content: binaire, Path: "{cours}.png"},
		contexte, destinataires)
	if err != nil {
		t.Fatalf("Prepare : %v", err)
	}
	if plan.Templated || string(plan.Items[0].Content) != string(binaire) {
		t.Errorf("le binaire a été touché : %q", plan.Items[0].Content)
	}
	// Le chemin, lui, reçoit toujours le gabarit.
	if plan.Items[0].Path != "5n6.png" {
		t.Errorf("chemin = %q", plan.Items[0].Path)
	}

	brut, err := broadcast.Prepare(broadcast.Request{
		Content: []byte("{cours}"), Path: "a.txt", Raw: true,
	}, contexte, destinataires)
	if err != nil || string(brut.Items[0].Content) != "{cours}" {
		t.Errorf("brut = %q, %v", brut.Items[0].Content, err)
	}
}

func TestLeDepotDEquipePorteLeNomDeLEquipe(t *testing.T) {
	equipes := []teams.Team{{Session: "a26", Course: "5n6", Group: "01", Short: "eq1"}}
	contexte, destinataires := broadcast.ForClassroom(groupe(), "a26.5n6.01.tp1", equipes,
		depots("a26.5n6.01.tp1.eq1"), jour)
	plan, err := broadcast.Prepare(broadcast.Request{
		Content: []byte("{nom_etudiant} / {equipe}"), Path: "a.txt",
	}, contexte, destinataires)
	if err != nil {
		t.Fatalf("Prepare : %v", err)
	}
	if obtenu := string(plan.Items[0].Content); obtenu != "Équipe eq1 / eq1" {
		t.Errorf("contenu = %q", obtenu)
	}
}

func TestCleanPath(t *testing.T) {
	bons := map[string]string{
		"README.md":          "README.md",
		" ./docs//notes.md ": "docs/notes.md",
		`docs\notes.md`:      "docs/notes.md",
		".github/ci.yml":     ".github/ci.yml",
	}
	for entree, voulu := range bons {
		if obtenu, err := broadcast.CleanPath(entree); err != nil || obtenu != voulu {
			t.Errorf("CleanPath(%q) = %q, %v ; voulu %q", entree, obtenu, err, voulu)
		}
	}
	for _, mauvais := range []string{"", "/etc/passwd", "C:/x.txt", "../x", "a/../../x",
		".git/config", "docs/", "."} {
		if _, err := broadcast.CleanPath(mauvais); err == nil {
			t.Errorf("CleanPath(%q) accepté", mauvais)
		}
	}
}

// Un champ vide dans le chemin qui le ferait sortir de la racine est refusé
// avant tout envoi, en nommant le dépôt.
func TestUnCheminQuiSeVideEstRefuse(t *testing.T) {
	contexte, destinataires := broadcast.ForClassroom(groupe(), "a26.5n6.01.tp1", nil,
		depots("a26.5n6.01.tp1.emilie-cote"), jour)
	_, err := broadcast.Prepare(broadcast.Request{Content: []byte("x"), Path: "{equipe}/a.txt"},
		contexte, destinataires)
	if err == nil || !strings.Contains(err.Error(), "a26.5n6.01.tp1.emilie-cote") {
		t.Errorf("err = %v", err)
	}
}

func TestLAncienneNomenclatureGardeLeCompte(t *testing.T) {
	contexte, destinataires := broadcast.ForPrefix("tp1", []groups.Repo{
		{Name: "tp1-jlpicard", Suffix: "jlpicard"},
	}, jour)
	if destinataires[0].Person.Username != "jlpicard" || contexte.Assignment != "tp1" {
		t.Errorf("destinataire = %+v, contexte = %+v", destinataires[0], contexte)
	}
}

// ------------------------------------------------------------------ dépôt

func client(t *testing.T, state *fakegh.State) *ghapi.Client {
	t.Helper()
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)
	c, err := ghapi.New(ghapi.Options{
		Host: "127.0.0.1", Token: "jeton-de-test", BaseURL: serveur.URL(),
		Sleep: func(time.Duration) {},
	})
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	return c
}

func planPour(t *testing.T, contenu string, noms ...string) *broadcast.Plan {
	t.Helper()
	contexte, destinataires := broadcast.ForClassroom(groupe(), "a26.5n6.01.tp1", nil,
		depots(noms...), jour)
	plan, err := broadcast.Prepare(broadcast.Request{
		Content: []byte(contenu), Path: "docs/NOTE.md",
	}, contexte, destinataires)
	if err != nil {
		t.Fatalf("Prepare : %v", err)
	}
	return plan
}

func issues(resultats []broadcast.Result) map[string]broadcast.Status {
	parDepot := map[string]broadcast.Status{}
	for _, resultat := range resultats {
		parDepot[resultat.Repo] = resultat.Status
	}
	return parDepot
}

func TestRunAjouteSansToucherAuTravailDejaRemis(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	state.SeedCommit("acme/a26.5n6.01.tp1.emilie-cote", map[string]string{
		"main.py": "print('travail')\n", "docs/NOTE.md": "note de l'étudiante\n",
	}, "main")
	// Un dépôt sans aucun commit : seule l'API des contenus y écrit.
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	c := client(t, state)

	plan := planPour(t, "Note pour {prenom}\n",
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jean-luc-picard")

	// La simulation lit, mais n'écrit rien.
	simules := broadcast.Run(c, "acme", plan, broadcast.Options{DryRun: true})
	if got := issues(simules); got["a26.5n6.01.tp1.emilie-cote"] != broadcast.Kept ||
		got["a26.5n6.01.tp1.jean-luc-picard"] != broadcast.Added {
		t.Fatalf("simulation = %v", got)
	}
	if state.HasCommits("acme/a26.5n6.01.tp1.jean-luc-picard") {
		t.Fatal("la simulation a écrit")
	}

	resultats := broadcast.Run(c, "acme", plan, broadcast.Options{})
	if got := issues(resultats); got["a26.5n6.01.tp1.emilie-cote"] != broadcast.Kept ||
		got["a26.5n6.01.tp1.jean-luc-picard"] != broadcast.Added {
		t.Fatalf("issues = %v (%+v)", got, resultats)
	}
	emilie := state.Files("acme/a26.5n6.01.tp1.emilie-cote", "main")
	if emilie["docs/NOTE.md"] != "note de l'étudiante\n" || emilie["main.py"] == "" {
		t.Errorf("le travail d'Émilie a été touché : %v", emilie)
	}
	if got := state.Files("acme/a26.5n6.01.tp1.jean-luc-picard", "main")["docs/NOTE.md"]; got != "Note pour Jean-Luc\n" {
		t.Errorf("Jean-Luc a reçu %q", got)
	}

	// Relancer ne refait rien : ce qui est arrivé est à jour.
	encore := broadcast.Run(c, "acme", plan, broadcast.Options{})
	if got := issues(encore)["a26.5n6.01.tp1.jean-luc-picard"]; got != broadcast.UpToDate {
		t.Errorf("seconde passe = %v", got)
	}
}

func TestRunRemplaceSurDemandeEtGardeLeReste(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	state.SeedCommit("acme/a26.5n6.01.tp1.emilie-cote", map[string]string{
		"main.py": "print('travail')\n", "docs/NOTE.md": "ancienne\n",
	}, "main")
	c := client(t, state)

	resultats := broadcast.Run(c, "acme", planPour(t, "nouvelle\n", "a26.5n6.01.tp1.emilie-cote"),
		broadcast.Options{Overwrite: true})
	if resultats[0].Status != broadcast.Replaced {
		t.Fatalf("issue = %+v", resultats[0])
	}
	fichiers := state.Files("acme/a26.5n6.01.tp1.emilie-cote", "main")
	if fichiers["docs/NOTE.md"] != "nouvelle\n" || fichiers["main.py"] != "print('travail')\n" {
		t.Errorf("fichiers = %v", fichiers)
	}
}

func TestRunNArretePasLeLotSurUnEchec(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	c := client(t, state)

	var vus []int
	resultats := broadcast.Run(c, "acme",
		planPour(t, "x", "a26.5n6.01.tp1.disparu", "a26.5n6.01.tp1.jean-luc-picard"),
		broadcast.Options{OnResult: func(done, _ int, _ broadcast.Result) { vus = append(vus, done) }})
	if resultats[0].Status != broadcast.Failed || resultats[1].Status != broadcast.Added {
		t.Errorf("issues = %+v", resultats)
	}
	if !reflect.DeepEqual(vus, []int{1, 2}) {
		t.Errorf("progression = %v", vus)
	}
}

// calendrier joue le registre : il ne retient des travaux que leur date.
type calendrier map[string]string

func (c calendrier) Due(assignmentID string) string { return c[strings.ToLower(assignmentID)] }

// Un groupe entier mêle plusieurs travaux : chaque dépôt reçoit les champs du
// sien — son nom, son échéance, la personne qu'il vise. Un dépôt hors
// nomenclature garde ce qu'un préfixe sait dire.
func TestChaqueDepotRecoitLesChampsDeSonTravail(t *testing.T) {
	cours := groupe().Scheduling(calendrier{
		"a26.5n6.01.tp1": "2026-10-01", "a26.5n6.01.tp2": "2026-11-01T23:59",
	})
	lots := broadcast.ForRepos("a26.5n6.01", []classroom.Classroom{cours},
		func(classroom.Classroom) []teams.Team { return nil },
		depots("a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp2.jean-luc-picard",
			"a26.5n6.01.tp1.jean-luc-picard", "vieux-depot"), jour)
	plan, err := broadcast.PrepareAll(broadcast.Request{
		Content: []byte("{nom_etudiant} · {travail} · {echeance}"), Path: "NOTE.md",
	}, lots)
	if err != nil {
		t.Fatalf("PrepareAll : %v", err)
	}
	obtenus := map[string]string{}
	for _, item := range plan.Items {
		obtenus[item.Repo] = string(item.Content)
	}
	attendus := map[string]string{
		"a26.5n6.01.tp1.emilie-cote":     "Émilie Côté · tp1 · 2026-10-01",
		"a26.5n6.01.tp1.jean-luc-picard": "Jean-Luc Picard · tp1 · 2026-10-01",
		"a26.5n6.01.tp2.jean-luc-picard": "Jean-Luc Picard · tp2 · 2026-11-01 23:59",
		"vieux-depot":                    " · a26.5n6.01 · ",
	}
	if !reflect.DeepEqual(obtenus, attendus) {
		t.Errorf("contenus = %v\nattendus = %v", obtenus, attendus)
	}
	if len(plan.Items) != 4 {
		t.Errorf("%d dépôts dans le plan, attendu 4", len(plan.Items))
	}
}
