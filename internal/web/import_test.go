package web_test

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
)

// classroomOrg monte une organisation telle que GitHub Classroom la laisse.
func classroomOrg() *fakegh.State {
	state := fakegh.NewState()
	for _, nom := range []string{
		"tp1-laubelachance", "tp1-felixboisvert", "tp1-loiselle",
		"projet-final-loiselle", "projet-final-felixboisvert",
	} {
		state.AddRepo("acme", nom, true)
	}
	return state
}

// listeOmnivox écrit une liste telle qu'Omnivox l'exporte : Windows-1252, CRLF,
// champs blindés, aucun compte GitHub.
func listeOmnivox(t *testing.T) string {
	t.Helper()
	lignes := []string{
		"No étudiant;Groupe;Nom de l'étudiant;Prénom de l'étudiant;Code perm.;",
		`="1600105";="1030";="Aube-Lachance";="Lorenzo";="AUBL10109901";`,
		`="2100106";="1030";="Boisvert";="Félix";="BOIF10109902";`,
		`="1900107";="1030";="Loiselle";="Édouard";="LOIE10109903";`,
	}
	var octets []byte
	for _, ligne := range lignes {
		for _, r := range ligne {
			octets = append(octets, byte(r))
		}
		octets = append(octets, '\r', '\n')
	}
	chemin := filepath.Join(t.TempDir(), "ListeEtudiants.csv")
	if err := os.WriteFile(chemin, octets, 0o600); err != nil {
		t.Fatal(err)
	}
	return chemin
}

// planVue est ce que l'aperçu rend.
type planVue struct {
	Prefix   string `json:"prefix"`
	Name     string `json:"name"`
	Scope    string `json:"scope"`
	Pairings []struct {
		Login string `json:"login"`
		Entry struct {
			FullName string `json:"full_name"`
		} `json:"entry"`
		Score  int    `json:"score"`
		Reason string `json:"reason"`
	} `json:"pairings"`
	Moves []struct {
		Repo   string `json:"repo"`
		Target string `json:"target"`
	} `json:"moves"`
	Unmatched []string `json:"unmatched"`
	Absent    []string `json:"absent"`
	Splits    []struct {
		Prefix string `json:"prefix"`
		Count  int    `json:"count"`
	} `json:"splits"`
	Unconfirmed []string `json:"unconfirmed"`
}

// Ce que l'organisation porte hors nomenclature, et l'aide pour aller chercher
// la liste.
func TestDepotsHorsNomenclatureEtAide(t *testing.T) {
	h := nouveau(t, classroomOrg())
	var vue struct {
		Repos       []string `json:"repos"`
		Assignments []struct {
			Prefix string `json:"prefix"`
			Count  int    `json:"count"`
		} `json:"assignments"`
		Help string `json:"help"`
	}
	h.json(http.MethodGet, "/api/orgs/acme/foreign", nil, &vue)
	if len(vue.Repos) != 5 {
		t.Fatalf("dépôts = %v", vue.Repos)
	}
	trouves := map[string]int{}
	for _, travail := range vue.Assignments {
		trouves[travail.Prefix] = travail.Count
	}
	if trouves["tp1"] != 3 || trouves["projet-final"] != 2 {
		t.Fatalf("travaux = %+v", vue.Assignments)
	}
	// L'aide dit où prendre la liste, à l'endroit où on la demande.
	for _, attendu := range []string{"Léa", "Pour Excel", "Code permanent"} {
		if !strings.Contains(vue.Help, attendu) {
			t.Fatalf("l'aide ne dit pas « %s » :\n%s", attendu, vue.Help)
		}
	}
}

// L'aperçu rapproche et compose le renommage, sans rien écrire.
func TestApercuDeLImportationNecritRien(t *testing.T) {
	state := classroomOrg()
	h := nouveau(t, state)

	var plan planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", map[string]any{
		"prefix": "tp1", "name": "tp1", "scope": "a26.5n6.1030",
		"path": listeOmnivox(t),
	}, &plan)

	if len(plan.Moves) != 3 || len(plan.Unmatched) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	cibles := map[string]string{}
	for _, ligne := range plan.Moves {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["tp1-laubelachance"] != "a26.5n6.1030.tp1.lorenzo-aube-lachance" {
		t.Fatalf("cibles = %v", cibles)
	}
	// Chaque rapprochement dit par quoi il a été reconnu.
	for _, trouve := range plan.Pairings {
		if trouve.Entry.FullName == "" || trouve.Reason == "" {
			t.Fatalf("rapprochement muet : %+v", trouve)
		}
	}
	if noms := h.depots(); !slices.Contains(noms, "tp1-laubelachance") {
		t.Fatalf("un aperçu a renommé : %v", noms)
	}
}

// L'importation renomme, déclare le groupe, et confie les noms au registre.
func TestImportationParLInterfaceWeb(t *testing.T) {
	state := classroomOrg()
	h := nouveau(t, state)

	bilan := h.travail(http.MethodPost, "/api/orgs/acme/import", map[string]any{
		"prefix": "tp1", "name": "tp1", "scope": "a26.5n6.1030",
		"path": listeOmnivox(t),
	})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["renamed"] != float64(3) || resultat["failed"] != float64(0) {
		t.Fatalf("bilan = %+v", resultat)
	}

	noms := h.depots()
	if !slices.Contains(noms, "a26.5n6.1030.tp1.edouard-loiselle") {
		t.Fatalf("dépôts = %v", noms)
	}
	// L'autre travail n'a pas bougé.
	if !slices.Contains(noms, "projet-final-loiselle") {
		t.Fatalf("un travail non demandé a bougé : %v", noms)
	}
	// Les noms sont montés au registre.
	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	if !strings.Contains(contenu, "Lorenzo Aube-Lachance") {
		t.Fatalf("registre =\n%s", contenu)
	}
	// Et le groupe est déclaré, avec sa liste.
	var fiche struct {
		Students []struct {
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.1030", nil, &fiche)
	if len(fiche.Students) != 3 {
		t.Fatalf("liste du groupe = %+v", fiche.Students)
	}
}

// Un rapprochement corrigé à l'écran l'emporte sur ce que l'outil avait deviné.
func TestUnRapprochementCorrigeLEmporte(t *testing.T) {
	h := nouveau(t, classroomOrg())
	var plan planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", map[string]any{
		"prefix": "tp1", "name": "tp1", "scope": "a26.5n6.1030",
		"people": []map[string]string{
			{"full_name": "Quelqu'un d'Autre", "username": "laubelachance"},
		},
	}, &plan)

	if len(plan.Moves) != 3 {
		t.Fatalf("plan = %+v", plan.Moves)
	}
	cibles := map[string]string{}
	for _, ligne := range plan.Moves {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["tp1-laubelachance"] != "a26.5n6.1030.tp1.quelqu-un-d-autre" {
		t.Fatalf("la correction n'a pas été suivie : %v", cibles)
	}
	// Les deux autres comptes ne mènent à personne : ils gardent leur nom.
	if len(plan.Unmatched) != 2 {
		t.Fatalf("comptes sans personne = %v", plan.Unmatched)
	}
}

// Un travail qu'aucun dépôt ne porte se refuse plutôt que de rendre un plan
// vide qu'on prendrait pour un succès.
func TestUnTravailAbsentEstRefuse(t *testing.T) {
	h := nouveau(t, classroomOrg())
	reponse, contenu := h.requete(http.MethodPost, "/api/orgs/acme/import/preview",
		map[string]any{"prefix": "absent", "scope": "a26.5n6.1030", "path": listeOmnivox(t)})
	if reponse.StatusCode < 400 {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	if !strings.Contains(string(contenu), "absent") {
		t.Fatalf("message = %s", contenu)
	}
}

// L'interface propose la place d'arrivée avant qu'on ait rien tapé : le cours
// vient du nom du fichier, le groupe de sa colonne, la session du premier
// commit du travail.
func TestLInterfaceDevineLaPlaceDArrivee(t *testing.T) {
	state := classroomOrg()
	state.Repos["acme/tp1-loiselle"].History = fakegh.Commits("2027-02-03T10:00:00Z")
	h := nouveau(t, state)

	var place struct {
		Session string   `json:"session"`
		Course  string   `json:"course"`
		Group   string   `json:"group"`
		Groups  []string `json:"groups"`
	}
	// Une liste déposée dans la page : pas de chemin, mais un contenu et un
	// nom — et c'est le nom qui porte le cours.
	contenu, err := os.ReadFile(listeOmnivox(t))
	if err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodPost, "/api/orgs/acme/import/place", map[string]any{
		"prefix": "tp1", "content": contenu,
		"filename": "ListeEtudiants_cours4204N6EM_gr1030.csv",
	}, &place)

	// Février tombe dans la session d'hiver ; le printemps, lui, ne se devine
	// jamais.
	if place.Session != "h27" {
		t.Fatalf("session = %q", place.Session)
	}
	if place.Course != "4N6" {
		t.Fatalf("cours = %q", place.Course)
	}
	// La colonne « Groupe » de la liste l'emporte sur le nom du fichier.
	if place.Group != "1030" || len(place.Groups) != 0 {
		t.Fatalf("groupe = %q, groupes = %v", place.Group, place.Groups)
	}
}

// Un dépôt sans historique ne dit rien de la session, et se taire vaut mieux
// qu'inventer une place où des dépôts iraient atterrir.
func TestSansHistoriqueLaSessionResteAChoisir(t *testing.T) {
	h := nouveau(t, classroomOrg())
	var place struct {
		Session string `json:"session"`
		Course  string `json:"course"`
	}
	h.json(http.MethodPost, "/api/orgs/acme/import/place", map[string]any{
		"prefix": "tp1", "path": listeOmnivox(t),
	}, &place)
	if place.Session != "" {
		t.Fatalf("session inventée : %q", place.Session)
	}
}

// L'interface peut ne reprendre que les dépôts dont l'étudiant est connu :
// les autres restent où ils sont.
func TestLInterfaceLaisseLesDepotsSansEtudiant(t *testing.T) {
	state := classroomOrg()
	state.AddRepo("acme", "tp1-visiteur-anonyme-42", true)
	h := nouveau(t, state)

	var plan planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", map[string]any{
		"prefix": "tp1", "name": "tp1", "scope": "a26.5n6.1030",
		"path": listeOmnivox(t), "named_only": true,
	}, &plan)

	if len(plan.Moves) != 3 {
		t.Fatalf("renommages = %+v", plan.Moves)
	}
	for _, ligne := range plan.Moves {
		if strings.Contains(ligne.Repo, "visiteur-anonyme") {
			t.Fatalf("un dépôt sans étudiant a été repris : %+v", ligne)
		}
	}
	// Il est quand même nommé : le laisser derrière en silence ferait croire
	// le travail entièrement repris.
	if len(plan.Unmatched) != 1 {
		t.Fatalf("dépôts sans personne = %v", plan.Unmatched)
	}
}

// Le cas rapporté : « kickmyb-firebase » est le travail, « Wasim7Zed » le
// compte, et le nom seul ne dit pas où couper. Sans les accès, l'interface
// affichait « @firebase-Wasim7Zed » et lui cherchait un nom.
func TestLeCompteVientDesAccesPasDuNom(t *testing.T) {
	state := fakegh.NewState()
	for nom, compte := range map[string]string{
		"kickmyb-firebase-laubelachance": "laubelachance",
		"kickmyb-firebase-felixboisvert": "felixboisvert",
		"kickmyb-firebase-loiselle":      "loiselle",
	} {
		state.AddRepo("acme", nom, true)
		state.AddCollaborator("acme/"+nom, compte, "push")
		// L'enseignant a accès à tout : il ne doit désigner personne.
		state.AddCollaborator("acme/"+nom, "prof", "admin")
	}
	h := nouveau(t, state)

	var plan planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", map[string]any{
		// Le préfixe deviné s'arrête à « kickmyb » : c'est ce que l'écran propose.
		"prefix": "kickmyb", "name": "kickmyb", "scope": "a26.5n6.1030",
		"path": listeOmnivox(t),
	}, &plan)

	if plan.Prefix != "kickmyb-firebase" {
		t.Fatalf("travail = %q : les accès devaient le révéler", plan.Prefix)
	}
	for _, trouve := range plan.Pairings {
		if strings.Contains(strings.ToLower(trouve.Login), "firebase") {
			t.Fatalf("le compte porte encore le travail : %+v", trouve)
		}
		if trouve.Entry.FullName == "" {
			t.Fatalf("le bon compte devait mener à quelqu'un : %+v", trouve)
		}
	}
	if len(plan.Unconfirmed) != 0 {
		t.Fatalf("tous les comptes viennent des accès : %v", plan.Unconfirmed)
	}
	cibles := map[string]string{}
	for _, ligne := range plan.Moves {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["kickmyb-firebase-laubelachance"] != "a26.5n6.1030.kickmyb-firebase.lorenzo-aube-lachance" {
		t.Fatalf("cibles = %v", cibles)
	}
}

// Deux travaux sous un même préfixe : l'aperçu les nomme et n'écrit rien, et la
// reprise est refusée tant qu'on n'a pas choisi.
func TestUnPrefixeQuiCacheDeuxTravauxDemandeAChoisir(t *testing.T) {
	state := fakegh.NewState()
	for nom, compte := range map[string]string{
		"kickmyb-firebase-laubelachance": "laubelachance",
		"kickmyb-firebase-felixboisvert": "felixboisvert",
		"kickmyb-android-loiselle":       "loiselle",
	} {
		state.AddRepo("acme", nom, true)
		state.AddCollaborator("acme/"+nom, compte, "push")
	}
	h := nouveau(t, state)
	corps := map[string]any{
		"prefix": "kickmyb", "name": "kickmyb", "scope": "a26.5n6.1030",
		"path": listeOmnivox(t),
	}

	var plan planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", corps, &plan)
	if len(plan.Splits) != 2 || len(plan.Moves) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Splits[0].Prefix != "kickmyb-firebase" || plan.Splits[0].Count != 2 {
		t.Fatalf("travaux = %+v", plan.Splits)
	}

	// Écrire sans avoir choisi est refusé, et rien n'a bougé.
	reponse, contenu := h.requete(http.MethodPost, "/api/orgs/acme/import", corps)
	if reponse.StatusCode < 400 {
		t.Fatalf("statut = %d : la reprise devait être refusée", reponse.StatusCode)
	}
	if !strings.Contains(string(contenu), "un à la fois") {
		t.Fatalf("message = %s", contenu)
	}
	if noms := h.depots(); !slices.Contains(noms, "kickmyb-android-loiselle") {
		t.Fatalf("un dépôt a été renommé : %v", noms)
	}

	// Le travail choisi, la reprise redevient ordinaire.
	corps["prefix"] = "kickmyb-firebase"
	corps["name"] = "kickmyb-firebase"
	var choisi planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", corps, &choisi)
	if len(choisi.Splits) != 1 || len(choisi.Moves) != 2 {
		t.Fatalf("plan = %+v", choisi)
	}
}

// Le registre de l'organisation répond avant qu'on cherche : un compte qu'il
// nomme est rapproché d'emblée, et ne demande plus de coup d'œil.
func TestLeRegistreEviteUnRapprochementAChercher(t *testing.T) {
	state := classroomOrg()
	// « xy42 » ne ressemble à aucun nom de la liste ; le registre, lui, sait.
	state.AddRepo("acme", "tp2-xy42", true)
	h := nouveau(t, state)
	h.json(http.MethodPost, "/api/classrooms", map[string]any{
		"session": "a25", "course": "5n6", "group": "1030",
		"students": []map[string]string{{"username": "xy42", "full_name": "Édouard Loiselle"}},
	}, nil)

	var plan planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", map[string]any{
		"prefix": "tp2", "name": "tp2", "scope": "a26.5n6.1030",
		"path": listeOmnivox(t),
	}, &plan)

	if len(plan.Pairings) != 1 {
		t.Fatalf("rapprochements = %+v", plan.Pairings)
	}
	trouve := plan.Pairings[0]
	if trouve.Entry.FullName != "Édouard Loiselle" || trouve.Score != 100 {
		t.Fatalf("rapprochement = %+v", trouve)
	}
	if trouve.Reason != "déjà connu de l'organisation" {
		t.Fatalf("raison = %q", trouve.Reason)
	}
}

// Les dépôts se choisissent un à un : ce qu'on écarte n'est ni repris, ni
// rapproché.
func TestLaSelectionDeDepotsLimiteLaReprise(t *testing.T) {
	h := nouveau(t, classroomOrg())

	// L'écran demande d'abord ce que le travail contient, pour le montrer.
	var vue struct {
		Prefix string `json:"prefix"`
		Repos  []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"repos"`
	}
	h.json(http.MethodPost, "/api/orgs/acme/import/repos",
		map[string]any{"prefix": "tp1"}, &vue)
	if len(vue.Repos) != 3 {
		t.Fatalf("dépôts = %+v", vue.Repos)
	}
	// Chaque nom mène au dépôt : l'écran en fait un lien.
	for _, depot := range vue.Repos {
		if !strings.Contains(depot.URL, "/acme/"+depot.Name) {
			t.Fatalf("adresse = %q", depot.URL)
		}
	}

	var plan planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", map[string]any{
		"prefix": "tp1", "name": "tp1", "scope": "a26.5n6.1030",
		"path": listeOmnivox(t),
		"only": []string{"tp1-laubelachance", "tp1-felixboisvert"},
	}, &plan)

	if len(plan.Moves) != 2 || len(plan.Pairings) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	for _, ligne := range plan.Moves {
		if ligne.Repo == "tp1-loiselle" {
			t.Fatalf("un dépôt écarté a été repris : %+v", plan.Moves)
		}
	}

	// Et la reprise elle-même s'y tient.
	h.travail(http.MethodPost, "/api/orgs/acme/import", map[string]any{
		"prefix": "tp1", "name": "tp1", "scope": "a26.5n6.1030",
		"path": listeOmnivox(t),
		"only": []string{"tp1-laubelachance", "tp1-felixboisvert"},
	})
	if noms := h.depots(); !slices.Contains(noms, "tp1-loiselle") {
		t.Fatalf("le dépôt écarté a bougé : %v", noms)
	}
}

// L'étape de la liste dit ce que l'organisation sait déjà : c'est ce qui
// permet de la passer en connaissance de cause.
func TestLesDepotsDisentQuiEstDejaConnu(t *testing.T) {
	state := classroomOrg()
	state.AddCollaborator("acme/tp1-laubelachance", "laubelachance", "push")
	h := nouveau(t, state)
	h.json(http.MethodPost, "/api/classrooms", map[string]any{
		"session": "a25", "course": "5n6", "group": "1030",
		"students": []map[string]string{
			{"username": "laubelachance", "full_name": "Lorenzo Aube-Lachance"},
		},
	}, nil)

	var vue struct {
		Total int `json:"total"`
		Known int `json:"known"`
		Repos []struct {
			Name    string `json:"name"`
			Login   string `json:"login"`
			Student string `json:"student"`
		} `json:"repos"`
	}
	h.json(http.MethodPost, "/api/orgs/acme/import/repos",
		map[string]any{"prefix": "tp1"}, &vue)

	if vue.Total != 3 || vue.Known != 1 {
		t.Fatalf("décompte = %d sur %d", vue.Known, vue.Total)
	}
	for _, depot := range vue.Repos {
		if depot.Name != "tp1-laubelachance" {
			continue
		}
		if depot.Login != "laubelachance" || depot.Student != "Lorenzo Aube-Lachance" {
			t.Fatalf("dépôt = %+v", depot)
		}
	}
}

// La liste peut être passée : ce que l'organisation nomme suffit alors, et le
// reste garde le compte qu'il porte.
func TestLaRepriseSePasseDeListe(t *testing.T) {
	state := classroomOrg()
	h := nouveau(t, state)
	h.json(http.MethodPost, "/api/classrooms", map[string]any{
		"session": "a25", "course": "5n6", "group": "1030",
		"students": []map[string]string{
			{"username": "laubelachance", "full_name": "Lorenzo Aube-Lachance"},
			{"username": "felixboisvert", "full_name": "Félix Boisvert"},
			{"username": "loiselle", "full_name": "Édouard Loiselle"},
		},
	}, nil)

	var plan planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", map[string]any{
		"prefix": "tp1", "name": "tp1", "scope": "a26.5n6.1030",
	}, &plan)

	if len(plan.Moves) != 3 || len(plan.Unmatched) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	cibles := map[string]string{}
	for _, ligne := range plan.Moves {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["tp1-laubelachance"] != "a26.5n6.1030.tp1.lorenzo-aube-lachance" {
		t.Fatalf("cibles = %v", cibles)
	}
}

// Le préfixe proposé porte parfois le tiret que le nom doublait —
// « tp3-h23-4204n6-kickmyb- ». Le compte ne doit pas hériter du second : rien
// sur GitHub ne s'appelle « -anwarlamari ».
func TestLeCompteNheritePasDuTiretDouble(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"TP3-H23-4204N6-KickMyB--anwarlamari",
		"TP3-H23-4204N6-KickMyB--laubelachance",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)

	// Ce que l'étape des dépôts montre.
	var vue struct {
		Repos []struct {
			Name  string `json:"name"`
			Login string `json:"login"`
		} `json:"repos"`
	}
	h.json(http.MethodPost, "/api/orgs/acme/import/repos",
		map[string]any{"prefix": "tp3-h23-4204n6-kickmyb-"}, &vue)
	for _, depot := range vue.Repos {
		if strings.HasPrefix(depot.Login, "-") {
			t.Fatalf("compte = %q pour « %s »", depot.Login, depot.Name)
		}
	}

	// Et ce que la revue des rapprochements affiche.
	var plan planVue
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", map[string]any{
		"prefix": "tp3-h23-4204n6-kickmyb-", "name": "tp3", "scope": "a26.5n6.1030",
		"path": listeOmnivox(t),
	}, &plan)
	if len(plan.Pairings) != 2 {
		t.Fatalf("rapprochements = %+v", plan.Pairings)
	}
	for _, trouve := range plan.Pairings {
		if strings.HasPrefix(trouve.Login, "-") {
			t.Fatalf("compte = %q", trouve.Login)
		}
	}
	// Et le renommage s'en tient au même compte.
	for _, ligne := range plan.Moves {
		if strings.Contains(ligne.Target, "..") || strings.Contains(ligne.Target, ".-") {
			t.Fatalf("cible = %q", ligne.Target)
		}
	}
}
