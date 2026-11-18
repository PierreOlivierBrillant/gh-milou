package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
)

// orphelinsRendus est ce que l'API dit des dépôts sans étudiant.
type orphelinsRendus struct {
	Found []struct {
		Orphan struct {
			Fragment string   `json:"fragment"`
			Repos    []string `json:"repos"`
		} `json:"orphan"`
		Person struct {
			Username string `json:"username"`
			FullName string `json:"full_name"`
		} `json:"person"`
	} `json:"found"`
	Unknown []struct {
		Fragment string `json:"fragment"`
	} `json:"unknown"`
	Repos int `json:"repos"`
}

// cohorteTrouee monte un groupe dont des dépôts portent au dernier niveau des
// comptes que rien ne déclare, l'un sous la marque de doublon de GitHub, et un
// autre qui n'est le compte de personne.
func cohorteTrouee(t *testing.T) (*harnais, *fakegh.State) {
	t.Helper()
	state := fakegh.NewState()
	state.Users["wriker"] = "William Riker"
	state.Users["dtroi"] = ""
	for _, nom := range []string{
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.wriker", "a26.5n6.01.tp2.wriker",
		"a26.5n6.01.tp1.dtroi-1", "a26.5n6.01.tp1.parti-sans-compte",
	} {
		state.AddRepo("acme", nom, true).PushedAt = "2026-10-02T10:00:00Z"
	}
	h := nouveau(t, state)
	h.groupe("a26", "5n6", "01", "Émilie Côté", "emilie-cote")
	return h, state
}

func registreDe(state *fakegh.State) string {
	return state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
}

// L'annuaire montre ce que GitHub reconnaît parmi les dépôts sans étudiant, et
// n'écrit que ce que la page a gardé coché.
func TestLAnnuaireRattacheCeQueLaPageAGarde(t *testing.T) {
	h, state := cohorteTrouee(t)

	var plan orphelinsRendus
	h.json(http.MethodGet, "/api/orphans", nil, &plan)
	if len(plan.Found) != 2 || plan.Repos != 3 || len(plan.Unknown) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Found[0].Person.Username != "dtroi" || plan.Found[1].Person.FullName != "William Riker" {
		t.Errorf("comptes reconnus = %+v", plan.Found)
	}

	var bilan struct {
		Repos    int `json:"repos"`
		Accounts int `json:"accounts"`
	}
	h.json(http.MethodPost, "/api/orphans", map[string]any{"fragments": []string{"wriker"}}, &bilan)
	if bilan.Repos != 2 || bilan.Accounts != 1 {
		t.Fatalf("bilan = %+v", bilan)
	}
	ecrit := registreDe(state)
	if !strings.Contains(ecrit, `"wriker"`) || strings.Contains(ecrit, `"dtroi"`) {
		t.Errorf("registre :\n%s", ecrit)
	}

	// L'annuaire ne compte plus que ce qui reste, et la page d'un travail
	// nomme désormais l'étudiant retrouvé.
	if reste := h.annuaire(""); reste.Unmatched != 2 {
		t.Errorf("restent %d dépôts sans étudiant", reste.Unmatched)
	}
}

// La ligne d'un dépôt hors liste propose le compte quand le dernier niveau en
// est un ; sinon elle le fait donner, et le compte donné est vérifié.
func TestLaLigneDUnDepotSeRattacheALaMain(t *testing.T) {
	h, state := cohorteTrouee(t)
	state.Users["jlpicard"] = "Jean-Luc Picard"

	var fiche struct {
		Orphan  struct{ Fragment string } `json:"orphan"`
		Account *struct {
			Username string `json:"username"`
		} `json:"account"`
	}
	h.json(http.MethodGet, "/api/orphans/WRiker", nil, &fiche)
	if fiche.Account == nil || fiche.Account.Username != "wriker" {
		t.Fatalf("fiche = %+v", fiche)
	}
	h.json(http.MethodGet, "/api/orphans/parti-sans-compte", nil, &fiche)
	if fiche.Account != nil {
		t.Fatalf("un slug ne doit pas passer pour un compte : %+v", fiche.Account)
	}

	reponse, contenu := h.requete(http.MethodPost, "/api/orphans/parti-sans-compte",
		map[string]string{"username": "personne-de-tel"})
	if reponse.StatusCode < 400 || !strings.Contains(string(contenu), "n'existe pas") {
		t.Fatalf("compte inconnu : %d %s", reponse.StatusCode, contenu)
	}
	var bilan struct {
		Repos  int `json:"repos"`
		Person struct {
			FullName string `json:"full_name"`
		} `json:"person"`
	}
	h.json(http.MethodPost, "/api/orphans/parti-sans-compte",
		map[string]string{"username": "jlpicard"}, &bilan)
	if bilan.Repos != 1 || bilan.Person.FullName != "Jean-Luc Picard" {
		t.Fatalf("bilan = %+v", bilan)
	}
	if !strings.Contains(registreDe(state), "parti-sans-compte") {
		t.Errorf("le registre doit retenir le dernier niveau :\n%s", registreDe(state))
	}
}

// Un dernier niveau qui n'est pas celui d'un dépôt sans étudiant est refusé :
// ce qui n'a pas été montré n'a pas été vu.
func TestRattacherRefuseCeQuiNEstPasOrphelin(t *testing.T) {
	h, state := cohorteTrouee(t)
	reponse, _ := h.requete(http.MethodPost, "/api/orphans/emilie-cote",
		map[string]string{"username": "wriker"})
	if reponse.StatusCode < 400 {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	if strings.Contains(registreDe(state), "wriker") {
		t.Errorf("rien ne devait s'écrire :\n%s", registreDe(state))
	}
}

// La page d'un travail marque les dépôts à rattacher, et eux seuls : c'est le
// serveur qui le dit, la page n'a pas à le déduire d'un compte vide.
func TestLaPageDuTravailMarqueLesDepotsARattacher(t *testing.T) {
	h, _ := cohorteTrouee(t)
	var detail struct {
		Repos []struct {
			Student string `json:"student"`
			Orphan  bool   `json:"orphan"`
		} `json:"repos"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01/assignments/tp1", nil, &detail)
	marques := []string{}
	for _, depot := range detail.Repos {
		if depot.Orphan {
			marques = append(marques, depot.Student)
		}
	}
	if strings.Join(marques, ",") != "dtroi-1,parti-sans-compte,wriker" {
		t.Errorf("dépôts à rattacher = %v", marques)
	}
}
