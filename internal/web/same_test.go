package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
)

// commeilfaut monte le cas de l'issue : la même personne, @Mr-Commeilfaut à
// l'automne et @commeilfautt à l'hiver, chacun inscrit dans son groupe.
func commeilfaut(t *testing.T) (*harnais, *fakegh.State) {
	t.Helper()
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-commeilfaut", true).PushedAt = "2026-09-20T10:00:00Z"
	state.AddRepo("acme", "h27.4w6.01.tp1.jean-commeilfaut", true).PushedAt = "2027-02-10T10:00:00Z"
	h := nouveau(t, state)
	h.groupe("a26", "5n6", "01", "Jean Commeilfaut", "Mr-Commeilfaut")
	h.groupe("h27", "4w6", "01", "Jean Commeilfaut", "commeilfautt")
	return h, state
}

// reunionRendue est ce que l'API rend d'une réunion.
type reunionRendue struct {
	Joining struct {
		Account   string   `json:"account"`
		Principal string   `json:"principal"`
		FullName  string   `json:"full_name"`
		Role      string   `json:"role"`
		Accounts  []string `json:"accounts"`
	} `json:"joining"`
	DryRun bool `json:"dry_run"`
}

// Le geste entier, depuis le navigateur : les candidats, l'aperçu qui n'écrit
// rien, la réunion, puis la séparation qui rend l'annuaire tel qu'il était.
func TestReunirPuisSeparerDepuisLeNavigateur(t *testing.T) {
	h, state := commeilfaut(t)
	if rendu := h.annuaire(""); rendu.Total != 2 {
		t.Fatalf("avant : %s", comptesDe(rendu))
	}

	// L'homonyme est proposé d'abord, et signalé comme tel.
	var candidats struct {
		Candidates []struct {
			Username string `json:"username"`
			SameName bool   `json:"same_name"`
		} `json:"candidates"`
	}
	h.json(http.MethodGet, "/api/users/Mr-Commeilfaut/same-as", nil, &candidats)
	if len(candidats.Candidates) != 1 || candidats.Candidates[0].Username != "commeilfautt" ||
		!candidats.Candidates[0].SameName {
		t.Fatalf("candidats = %+v", candidats)
	}

	// L'aperçu dit ce qui l'emporte sans rien écrire.
	var apercu reunionRendue
	h.json(http.MethodPost, "/api/users/Mr-Commeilfaut/same-as",
		map[string]any{"same_as": "commeilfautt", "dry_run": true}, &apercu)
	if !apercu.DryRun || apercu.Joining.Principal != "commeilfautt" ||
		strings.Join(apercu.Joining.Accounts, ",") != "commeilfautt,Mr-Commeilfaut" {
		t.Fatalf("aperçu = %+v", apercu)
	}
	if rendu := h.annuaire(""); rendu.Total != 2 {
		t.Fatalf("l'aperçu ne doit rien écrire : %s", comptesDe(rendu))
	}

	var reunion reunionRendue
	h.json(http.MethodPost, "/api/users/Mr-Commeilfaut/same-as",
		map[string]any{"same_as": "commeilfautt"}, &reunion)
	rendu := h.annuaire("")
	if rendu.Total != 1 || rendu.Users[0].Username != "commeilfautt" ||
		len(rendu.Users[0].Enrollments) != 2 || rendu.Users[0].Repos != 2 {
		t.Fatalf("après : %+v", rendu.Users)
	}
	// La réunion vit au registre, sous forme de renvoi.
	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	if !strings.Contains(contenu, `"same_as": "commeilfautt"`) {
		t.Fatalf("registre :\n%s", contenu)
	}

	// La fiche, ouverte par l'ancien compte, montre la personne entière.
	fiche := h.fiche("Mr-Commeilfaut")
	if fiche.User.Username != "commeilfautt" || fiche.User.Courses != 2 ||
		len(fiche.User.Accounts) != 2 {
		t.Fatalf("fiche = %+v", fiche.User)
	}

	h.json(http.MethodDelete, "/api/users/Mr-Commeilfaut/same-as", nil, nil)
	if rendu := h.annuaire(""); rendu.Total != 2 {
		t.Fatalf("après séparation : %s", comptesDe(rendu))
	}
}

// Réunir est une décision d'enseignant : le refus se dit dans les mots de
// l'outil, et la page sait d'avance si elle doit proposer le geste.
func TestUnEtudiantNeReunitPas(t *testing.T) {
	h, _ := commeilfaut(t)
	if reponse, contenu := h.coopter("commeilfautt", true); reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	reponse, contenu := h.requete(http.MethodPost, "/api/users/Mr-Commeilfaut/same-as",
		map[string]any{"same_as": "commeilfautt"})
	if reponse.StatusCode == http.StatusOK || !strings.Contains(string(contenu), "Seul un enseignant") {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	// Le geste se propose depuis la fiche, et d'elle seule : l'annuaire le
	// répéterait sur chacune de ses centaines de lignes. C'est qui regarde qui
	// en décide, quelle que soit la personne affichée.
	var rendu struct {
		MayDecide bool `json:"may_decide"`
	}
	h.json(http.MethodGet, "/api/users/emilie-cote", nil, &rendu)
	if rendu.MayDecide {
		t.Error("la page ne doit pas proposer le geste à qui ne peut pas le faire")
	}
}

// Séparer un compte que rien ne réunit se refuse, sans rien écrire.
func TestSeparerUnCompteSeulSeRefuse(t *testing.T) {
	h, _ := commeilfaut(t)
	reponse, contenu := h.requete(http.MethodDelete, "/api/users/commeilfautt/same-as", nil)
	if reponse.StatusCode == http.StatusOK || !strings.Contains(string(contenu), "rien à séparer") {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
}

// vieuxCompte monte le cas d'un étudiant dont le poste commet sous un vieux
// compte : son dépôt porte les commits de @vieux-zach, et aucun des siens.
func vieuxCompte(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()
	depot := state.AddRepo("acme", "a26.5n6.01.tp1.zephirin-eltayeb", true)
	depot.History = []fakegh.HistoryEntry{
		{At: "2026-10-05T14:00:00Z", Login: "vieux-zach"},
		{At: "2026-10-01T09:00:00Z", Login: "vieux-zach"},
	}
	state.AddContributors("acme/a26.5n6.01.tp1.zephirin-eltayeb", "vieux-zach", "vieux-zach")
	return avantLeRegistre(t, state,
		cohorte("a26", "5n6", "01", "Zephirin Eltayeb", "zephirin-elt"))
}

// Un compte qui a commis dans le dépôt d'un étudiant sans être le sien se voit
// sur la ligne du travail, se propose en tête quand on réunit, et se réunit
// même s'il n'est d'aucun groupe — il a commis ici, ce n'est pas une faute de
// frappe. Une fois réunis, l'étudiant n'est plus muet dans son propre dépôt.
func TestUnCompteVuDansLeDepotSeReunitDepuisLeTravail(t *testing.T) {
	h := vieuxCompte(t)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)

	var detail struct {
		MayDecide bool `json:"may_decide"`
		Repos     []struct {
			Username  string   `json:"username"`
			Silent    []any    `json:"silent"`
			Strangers []string `json:"strangers"`
		} `json:"repos"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01/assignments/tp1", nil, &detail)
	if !detail.MayDecide || len(detail.Repos) != 1 || len(detail.Repos[0].Silent) != 1 ||
		strings.Join(detail.Repos[0].Strangers, " ") != "vieux-zach" {
		t.Fatalf("détail = %+v", detail)
	}

	var candidats struct {
		Seen []string `json:"seen"`
	}
	h.json(http.MethodGet, "/api/users/zephirin-elt/same-as", nil, &candidats)
	if strings.Join(candidats.Seen, " ") != "vieux-zach" {
		t.Fatalf("vus dans ses dépôts = %v", candidats.Seen)
	}

	var reunion reunionRendue
	h.json(http.MethodPost, "/api/users/vieux-zach/same-as",
		map[string]any{"same_as": "zephirin-elt"}, &reunion)
	if reunion.Joining.Principal != "zephirin-elt" ||
		strings.Join(reunion.Joining.Accounts, ",") != "zephirin-elt,vieux-zach" {
		t.Fatalf("réunion = %+v", reunion.Joining)
	}

	// Une variable neuve : un champ absent du JSON garderait sinon sa valeur
	// d'avant, et le test lirait ce qu'il vient d'effacer.
	var apres struct {
		Repos []struct {
			Silent    []any    `json:"silent"`
			Strangers []string `json:"strangers"`
		} `json:"repos"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01/assignments/tp1", nil, &apres)
	if len(apres.Repos) != 1 || len(apres.Repos[0].Silent) != 0 || len(apres.Repos[0].Strangers) != 0 {
		t.Errorf("réunis, et pourtant : %+v", apres.Repos)
	}
}

// Un compte vu nulle part reste une faute de frappe probable : le témoignage
// des historiques ne vaut que pour ce qu'ils portent.
func TestUnCompteVuNullePartNeSeReunitPas(t *testing.T) {
	h := vieuxCompte(t)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)
	reponse, contenu := h.requete(http.MethodPost, "/api/users/zachh/same-as",
		map[string]any{"same_as": "zephirin-elt"})
	if reponse.StatusCode == http.StatusOK || !strings.Contains(string(contenu), "nulle part") {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
}
