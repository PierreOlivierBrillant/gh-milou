package web_test

import (
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
)

// C'est le cas qui justifie le registre. Jusqu'ici, la correspondance entre le
// dernier niveau d'un nom de dépôt et le compte GitHub de la personne vivait
// dans un fichier local : un collègue ouvrant la même organisation depuis sa
// machine n'y voyait que des slugs, sans savoir qui ils désignaient.

// autreMachine monte une seconde interface sur le même GitHub, avec ses propres
// réglages : c'est le collègue, qui n'a jamais rien déclaré chez lui.
func autreMachine(t *testing.T, state *fakegh.State) *harnais {
	t.Helper()
	return nouveau(t, state)
}

func TestUnCollegueVoitLesNomsSansRienAvoirDeclare(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}

	// Première machine : le groupe est déclaré avec sa liste.
	premiere := nouveau(t, state)
	premiere.groupe("a26", "5N6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard")

	// Le registre a bien été écrit dans l'organisation, privé.
	depot := state.Repos["acme/"+registry.RepoName]
	if depot == nil || !depot.Private {
		t.Fatalf("registre = %+v", depot)
	}

	// Seconde machine : rien de déclaré, et pourtant les noms sont là.
	collegue := autreMachine(t, state)
	annuaire := collegue.annuaire("")
	if annuaire.Total != 2 {
		t.Fatalf("%d personne(s) vue(s) par le collègue : %+v", annuaire.Total, annuaire.Users)
	}
	noms := map[string]string{}
	for _, ligne := range annuaire.Users {
		noms[ligne.Username] = ligne.FullName
	}
	if noms["emilie-cote"] != "Émilie Côté" || noms["jlpicard"] != "Jean-Luc Picard" {
		t.Fatalf("noms vus par le collègue = %v", noms)
	}
	if annuaire.Unmatched != 0 {
		t.Errorf("%d dépôt(s) rattaché(s) à personne", annuaire.Unmatched)
	}
}

// Le collègue voit aussi la liste du groupe lui-même, pas seulement l'annuaire.
func TestUnCollegueVoitLaListeDuGroupe(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)

	premiere := nouveau(t, state)
	place := premiere.groupe("a26", "5N6", "01", "Émilie Côté", "emilie-cote")

	collegue := autreMachine(t, state)
	var vue struct {
		Students []struct {
			FullName    string `json:"full_name"`
			Username    string `json:"username"`
			Assignments []struct {
				Repo string `json:"repo"`
			} `json:"assignments"`
		} `json:"students"`
	}
	collegue.json(http.MethodGet, "/api/classrooms/"+place+"/students", nil, &vue)
	if len(vue.Students) != 1 {
		t.Fatalf("liste vue par le collègue = %+v", vue.Students)
	}
	if vue.Students[0].FullName != "Émilie Côté" || vue.Students[0].Username != "emilie-cote" {
		t.Fatalf("étudiante = %+v", vue.Students[0])
	}
	if len(vue.Students[0].Assignments) != 1 {
		t.Errorf("ses dépôts n'ont pas suivi : %+v", vue.Students[0].Assignments)
	}
}

// Le compte d'une personne dont on n'a jamais eu le nom complet — celle d'un
// dépôt repris, nommé par son compte — doit figurer dans la liste du groupe.
// C'est le seul endroit d'où la nommer ou la déplacer, et le registre sait
// pourtant très bien de qui il s'agit.
func TestUnCompteSansNomFigureDansLaListeDuGroupe(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	state.AddRepo("acme", "a26.5n6.01.tp1.aleksilesaj", true)

	premiere := nouveau(t, state)
	place := premiere.groupe("a26", "5N6", "01",
		"Émilie Côté", "emilie-cote", "", "aleksilesaj")

	// Le collègue n'a rien déclaré : tout ce qu'il voit du groupe vient du
	// registre et des dépôts.
	collegue := autreMachine(t, state)
	var vue struct {
		Students []struct {
			FullName    string `json:"full_name"`
			Username    string `json:"username"`
			Assignments []struct {
				Repo string `json:"repo"`
			} `json:"assignments"`
		} `json:"students"`
		MissingNames int `json:"missing_names"`
	}
	collegue.json(http.MethodGet, "/api/classrooms/"+place+"/students", nil, &vue)
	if len(vue.Students) != 2 {
		t.Fatalf("liste vue par le collègue = %+v", vue.Students)
	}
	// Un nom qui manque se range en tête : c'est ce qu'on vient y chercher.
	sansNom := vue.Students[0]
	if sansNom.Username != "aleksilesaj" || sansNom.FullName != "" {
		t.Fatalf("étudiant sans nom = %+v", sansNom)
	}
	if len(sansNom.Assignments) != 1 || sansNom.Assignments[0].Repo != "a26.5n6.01.tp1.aleksilesaj" {
		t.Errorf("son dépôt n'a pas suivi : %+v", sansNom.Assignments)
	}
	if vue.MissingNames != 1 {
		t.Errorf("noms manquants annoncés = %d", vue.MissingNames)
	}

	// Et il peut le nommer de là, sans avoir rien déclaré : c'est ce que la
	// liste promet en le montrant.
	collegue.json(http.MethodPost, "/api/classrooms/"+place+"/students/rename", map[string]any{
		"username": "aleksilesaj", "full_name": "Aleksi Lesaj", "repos": false,
	}, nil)
	collegue.json(http.MethodGet, "/api/classrooms/"+place+"/students", nil, &vue)
	for _, ligne := range vue.Students {
		if ligne.Username == "aleksilesaj" && ligne.FullName != "Aleksi Lesaj" {
			t.Fatalf("nom retenu = %q", ligne.FullName)
		}
	}
	if vue.MissingNames != 0 {
		t.Errorf("noms manquants après correction = %d", vue.MissingNames)
	}
}

// Ce que le registre a révélé n'est pas écrit dans le fichier local : ce que la
// machine déclare doit rester ce qu'on lui a dit, non ce qu'elle a déduit.
func TestCeQueLeRegistreRevelaNEstPasDeclare(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)

	premiere := nouveau(t, state)
	place := premiere.groupe("a26", "5N6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard")

	// Le collègue déclare le même groupe, mais avec une seule personne.
	collegue := autreMachine(t, state)
	collegue.groupe("a26", "5N6", "01", "Aminata Diallo", "aminata-d")

	// Il voit les trois — deux déduites du registre, une déclarée…
	var vue struct {
		Students []struct {
			Username string `json:"username"`
		} `json:"students"`
	}
	collegue.json(http.MethodGet, "/api/classrooms/"+place+"/students", nil, &vue)
	if len(vue.Students) != 3 {
		t.Fatalf("liste vue = %+v", vue.Students)
	}

	// … mais son fichier local ne retient que ce qu'il a déclaré. Une écriture
	// sur le groupe — ici l'ajout d'une personne — ne doit pas y verser au
	// passage les deux que le registre avait révélées.
	collegue.json(http.MethodPost, "/api/classrooms/"+place+"/students/add", map[string]any{
		"full_name": "Marc Aurèle", "username": "prof",
	}, nil)
	declares := collegue.declares(place)
	if len(declares) != 2 || declares[0] != "aminata-d" || declares[1] != "prof" {
		t.Fatalf("le fichier local a adopté des personnes déduites : %v", declares)
	}
}

// Corriger un nom le corrige pour tout le monde, et les dépôts déjà créés sous
// l'ancien slug restent rattachés à leur personne.
func TestUnNomCorrigeVautPourToutLeMondeSansOrphelinerLesDepots(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emlie-cote", true)

	premiere := nouveau(t, state)
	place := premiere.groupe("a26", "5N6", "01", "Emlie Côté", "emilie-cote")

	// La faute est corrigée, sans renommer les dépôts.
	premiere.json(http.MethodPost, "/api/classrooms/"+place+"/students/rename", map[string]any{
		"username": "emilie-cote", "full_name": "Émilie Côté", "repos": false,
	}, nil)

	collegue := autreMachine(t, state)
	annuaire := collegue.annuaire("")
	if annuaire.Total != 1 {
		t.Fatalf("annuaire = %+v", annuaire.Users)
	}
	if annuaire.Users[0].FullName != "Émilie Côté" {
		t.Fatalf("nom = %q", annuaire.Users[0].FullName)
	}
	// Le dépôt porte encore l'ancien slug, et reste pourtant le sien.
	if annuaire.Users[0].Repos != 1 || annuaire.Unmatched != 0 {
		t.Fatalf("le dépôt sous l'ancien slug s'est détaché : %d dépôt(s), %d orphelin(s)",
			annuaire.Users[0].Repos, annuaire.Unmatched)
	}
}

// ------------------------------------------------------------- publication

// publicationVue est ce que l'aperçu et la publication renvoient.
type publicationVue struct {
	Org  string `json:"org"`
	Repo string `json:"repo"`
	Plan struct {
		New []struct {
			Username string   `json:"username"`
			FullName string   `json:"full_name"`
			Slugs    []string `json:"slugs"`
		} `json:"new"`
		Renamed []struct {
			Username string `json:"username"`
			Registry string `json:"registry"`
			Local    string `json:"local"`
		} `json:"renamed"`
		Known     int `json:"known"`
		Ambiguous []struct {
			Username string   `json:"username"`
			Names    []string `json:"names"`
			Chosen   string   `json:"chosen"`
		} `json:"ambiguous"`
		Nameless []string `json:"nameless"`
	} `json:"plan"`
	Total        int      `json:"total"`
	Published    int      `json:"published"`
	Exposure     string   `json:"exposure"`
	RegistrySize int      `json:"registry_size"`
	Teams        []string `json:"teams"`
	Trimmed      int      `json:"trimmed"`
	Backup       string   `json:"backup"`
}

// L'aperçu montre ce que publier ferait, sans rien écrire.
func TestApercuDeLaPublicationNecritRien(t *testing.T) {
	state := fakegh.NewState()
	h := avantLeRegistre(t, state, cohorte("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard"))

	var vue publicationVue
	h.json(http.MethodGet, "/api/orgs/acme/registry", nil, &vue)
	if vue.Total != 2 || len(vue.Plan.New) != 2 {
		t.Fatalf("aperçu = %+v", vue)
	}
	if vue.Repo != registry.RepoName || vue.Org != "acme" {
		t.Fatalf("aperçu = %+v", vue)
	}
	if _, cree := state.Repos["acme/"+registry.RepoName]; cree {
		t.Error("un aperçu ne doit rien écrire")
	}
}

// La publication verse, puis dit ce qui reste à faire — rien, ici.
func TestPublicationParLInterfaceWeb(t *testing.T) {
	state := fakegh.NewState()
	h := avantLeRegistre(t, state, cohorte("a26", "5n6", "01", "Émilie Côté", "emilie-cote"))

	var vue publicationVue
	h.json(http.MethodPost, "/api/orgs/acme/registry", map[string]any{}, &vue)
	if vue.Published != 1 || vue.Total != 0 || vue.RegistrySize != 1 {
		t.Fatalf("publication = %+v", vue)
	}
	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	if !strings.Contains(contenu, "Émilie Côté") {
		t.Fatalf("registre =\n%s", contenu)
	}
}

// Publier deux fois est refusé plutôt que silencieux : un « c'est fait » qui
// n'a rien fait n'apprend rien.
func TestPublierDeuxFoisEstRefuse(t *testing.T) {
	h := avantLeRegistre(t, nil, cohorte("a26", "5n6", "01", "Émilie Côté", "emilie-cote"))
	h.json(http.MethodPost, "/api/orgs/acme/registry", map[string]any{}, nil)

	reponse, contenu := h.requete(http.MethodPost, "/api/orgs/acme/registry", map[string]any{})
	if reponse.StatusCode < 400 {
		t.Fatalf("statut = %d — publier deux fois doit être refusé", reponse.StatusCode)
	}
	if !strings.Contains(string(contenu), "Rien à publier") {
		t.Fatalf("message = %s", contenu)
	}
}

// Le désaccord se montre, et « prefer_local » le tranche dans l'autre sens.
func TestLeDesaccordSeTrancheALaDemande(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", registry.RepoName, true)
	state.SeedCommit("acme/"+registry.RepoName, map[string]string{
		registry.UsersFile: `{"version":1,"students":[` +
			`{"username":"emilie-cote","full_name":"Émilie Côté","slugs":["emilie-cote"]}]}`,
	}, registry.Branch)

	h := avantLeRegistre(t, state, cohorte("a26", "5n6", "01", "Emilie Cote", "emilie-cote"))

	var apercu publicationVue
	h.json(http.MethodGet, "/api/orgs/acme/registry", nil, &apercu)
	if len(apercu.Plan.Renamed) != 1 || apercu.Plan.Renamed[0].Registry != "Émilie Côté" {
		t.Fatalf("aperçu = %+v", apercu.Plan)
	}

	h.json(http.MethodPost, "/api/orgs/acme/registry", map[string]any{"prefer_local": true}, nil)
	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	if !strings.Contains(contenu, `"Emilie Cote"`) {
		t.Fatalf("le nom du poste n'a pas été repris :\n%s", contenu)
	}
	// L'ancien nom a tout de même nommé des dépôts : son slug reste.
	if !strings.Contains(contenu, "emilie-cote") {
		t.Fatalf("le slug de l'ancien nom a disparu :\n%s", contenu)
	}
}

// L'aperçu porte l'avertissement sur la permission de base : c'est le moment
// où l'on s'apprête à déposer des noms dans l'organisation.
func TestLApercuAvertitSurLaPermissionDeBase(t *testing.T) {
	state := fakegh.NewState()
	state.DefaultRepoPermission["acme"] = "read"
	h := avantLeRegistre(t, state, cohorte("a26", "5n6", "01", "Émilie Côté", "emilie-cote"))

	var vue publicationVue
	h.json(http.MethodGet, "/api/orgs/acme/registry", nil, &vue)
	if !strings.Contains(vue.Exposure, "read") {
		t.Fatalf("avertissement = %q", vue.Exposure)
	}
}

// ------------------------------------------------------------- effacement

// Effacer l'historique exige de retaper le nom du dépôt, comme une suppression.
func TestEffacerLHistoriqueExigeLeNomExact(t *testing.T) {
	state := fakegh.NewState()
	h := avantLeRegistre(t, state, cohorte("a26", "5n6", "01", "Émilie Côté", "emilie-cote"))
	h.json(http.MethodPost, "/api/orgs/acme/registry", map[string]any{}, nil)

	reponse, contenu := h.requete(http.MethodPost, "/api/orgs/acme/registry/history",
		map[string]any{"confirm": ".cohorte"})
	if reponse.StatusCode < 400 {
		t.Fatalf("statut = %d — un nom approchant ne doit pas suffire", reponse.StatusCode)
	}
	if !strings.Contains(string(contenu), "acme/"+registry.RepoName) {
		t.Fatalf("le refus doit dire quoi retaper : %s", contenu)
	}
}

// Avec le nom exact, la branche repart d'un commit sans passé — et le contenu
// est intact.
func TestEffacerLHistoriqueGardeLeContenu(t *testing.T) {
	state := fakegh.NewState()
	h := avantLeRegistre(t, state, cohorte("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard"))
	h.json(http.MethodPost, "/api/orgs/acme/registry", map[string]any{}, nil)
	avant := state.Refs["acme/"+registry.RepoName+"@"+registry.Branch]

	var bilan struct {
		Commit  string `json:"commit"`
		Message string `json:"message"`
	}
	h.json(http.MethodPost, "/api/orgs/acme/registry/history",
		map[string]any{"confirm": "acme/" + registry.RepoName}, &bilan)
	if bilan.Commit == "" || bilan.Commit == avant {
		t.Fatalf("bilan = %+v", bilan)
	}
	// Le message ne promet pas plus que ce qui est vrai.
	if !strings.Contains(bilan.Message, "GitHub garde un temps") {
		t.Errorf("message = %q", bilan.Message)
	}

	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	for _, attendu := range []string{"Émilie Côté", "Jean-Luc Picard"} {
		if !strings.Contains(contenu, attendu) {
			t.Fatalf("« %s » a disparu du registre :\n%s", attendu, contenu)
		}
	}
}

// ------------------------------------------------------------- accès d'équipe

// L'aperçu propose les équipes de l'organisation, et en accorder une ouvre un
// accès — ce que le message dit sans laisser croire qu'il en ferme d'autres.
func TestDonnerAccesAUneEquipeDepuisLInterface(t *testing.T) {
	state := fakegh.NewState()
	h := avantLeRegistre(t, state, cohorte("a26", "5n6", "01", "Émilie Côté", "emilie-cote"))

	var apercu publicationVue
	h.json(http.MethodGet, "/api/orgs/acme/registry", nil, &apercu)
	// Le faux serveur rend les équipes par slug : on cherche celle qu'on veut
	// plutôt que de compter sur son rang.
	if len(apercu.Teams) != 2 || !slices.Contains(apercu.Teams, "enseignants") {
		t.Fatalf("équipes proposées = %v", apercu.Teams)
	}

	var bilan struct {
		Team    string `json:"team"`
		Message string `json:"message"`
	}
	h.json(http.MethodPost, "/api/orgs/acme/registry/team",
		map[string]any{"team": "enseignants"}, &bilan)
	if droit := state.TeamRepos["acme/enseignants"]["acme/"+registry.RepoName]; droit == "" {
		t.Fatalf("aucun droit accordé : %+v", state.TeamRepos)
	}
	if !strings.Contains(bilan.Message, "sans en fermer aucun") {
		t.Errorf("message = %q", bilan.Message)
	}
}

// ------------------------------------------------------------- allègement

// Publier depuis l'interface allège aussi le fichier local, après l'avoir
// recopié : le registre est désormais la source des noms.
func TestPublierAllegeLeFichierLocal(t *testing.T) {
	h := avantLeRegistre(t, nil, cohorte("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard"))

	var vue publicationVue
	h.json(http.MethodPost, "/api/orgs/acme/registry", map[string]any{}, &vue)
	if vue.Trimmed != 2 || vue.Backup == "" {
		t.Fatalf("publication = %+v", vue)
	}

	local, err := os.ReadFile(h.Groupes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(local), "Émilie Côté") {
		t.Fatalf("les noms sont restés dans le fichier local :\n%s", local)
	}
	if !strings.Contains(string(local), "emilie-cote") {
		t.Fatalf("les inscriptions ont disparu :\n%s", local)
	}
	sauvegarde, err := os.ReadFile(vue.Backup)
	if err != nil || !strings.Contains(string(sauvegarde), "Émilie Côté") {
		t.Fatalf("sauvegarde = %v, %v", err, string(sauvegarde))
	}
}

// Et l'interface continue de montrer les noms : elle les prend au registre.
func TestLesNomsRestentVisiblesApresAllegement(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	h := avantLeRegistre(t, state, cohorte("a26", "5n6", "01", "Émilie Côté", "emilie-cote"))
	h.json(http.MethodPost, "/api/orgs/acme/registry", map[string]any{}, nil)

	var vue struct {
		Students []struct {
			FullName string `json:"full_name"`
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01/students", nil, &vue)
	if len(vue.Students) != 1 || vue.Students[0].FullName != "Émilie Côté" {
		t.Fatalf("liste = %+v", vue.Students)
	}
}
