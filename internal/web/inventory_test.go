package web_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/web"
)

// L'inventaire d'une organisation est ce qui coûte le plus cher à lire : à
// l'échelle d'un département, il tient des dizaines de pages. Une écriture dont
// on connaît exactement l'effet — un renommage, une suppression — doit donc s'y
// répercuter plutôt que le périmer.

// pagesLues compte les requêtes d'inventaire reçues par le faux GitHub.
func pagesLues(h *harnais) int {
	return h.State.CallCount("GET /orgs/acme/repos")
}

// travaux renvoie les noms courts des travaux que l'interface montre pour un
// groupe : c'est par là qu'on voit ce que l'inventaire retenu contient.
func travaux(h *harnais, place string) []string {
	var vue struct {
		Assignments []struct {
			Name string `json:"name"`
		} `json:"assignments"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place, nil, &vue)
	noms := make([]string, 0, len(vue.Assignments))
	for _, travail := range vue.Assignments {
		noms = append(noms, travail.Name)
	}
	return noms
}

func TestRenommageSuitLInventaireSansLeRelire(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jlpicard",
		"a26.5n6.01.tp2.jlpicard",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	place := h.groupe("a26", "5N6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard")
	travaux(h, place) // l'inventaire est désormais en main
	avant := pagesLues(h)

	bilan := h.travail(http.MethodPost,
		"/api/classrooms/"+place+"/assignments/rename", map[string]any{
			"id": "a26.5n6.01.tp1", "name": "projet-final",
		})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}

	// Le nouveau nom s'affiche, et l'ancien a disparu : l'inventaire a suivi.
	noms := travaux(h, place)
	vus := map[string]bool{}
	for _, nom := range noms {
		vus[nom] = true
	}
	if !vus["projet-final"] || vus["tp1"] || !vus["tp2"] {
		t.Fatalf("travaux après renommage = %v", noms)
	}
	if apres := pagesLues(h); apres != avant {
		t.Errorf("%d page(s) d'inventaire relue(s) : un renommage se suit, il ne se relit pas",
			apres-avant)
	}
}

func TestSuppressionRetireDeLInventaireSansLeRelire(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{"a26.5n6.01.tp1.jlpicard", "a26.5n6.01.tp2.jlpicard"} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	place := h.groupe("a26", "5N6", "01", "Jean-Luc Picard", "jlpicard")
	travaux(h, place)
	avant := pagesLues(h)

	h.json(http.MethodDelete, "/api/orgs/acme/repos/a26.5n6.01.tp1.jlpicard",
		map[string]any{"confirm": "a26.5n6.01.tp1.jlpicard"}, nil)

	noms := travaux(h, place)
	if len(noms) != 1 || noms[0] != "tp2" {
		t.Fatalf("travaux après suppression = %v", noms)
	}
	if apres := pagesLues(h); apres != avant {
		t.Errorf("%d page(s) d'inventaire relue(s) : une suppression se suit, elle ne se relit pas",
			apres-avant)
	}
}

// Une création, elle, doit bien relire : le dépôt neuf n'est pas dans
// l'inventaire, et sa date de dernier envoi ne s'invente pas.
func TestCreationReliteLInventaire(t *testing.T) {
	h := nouveau(t, nil)
	place := h.groupe("a26", "5N6", "01", "Jean-Luc Picard", "jlpicard")
	travaux(h, place)
	avant := pagesLues(h)

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "tp1"})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	if noms := travaux(h, place); len(noms) != 1 || noms[0] != "tp1" {
		t.Fatalf("travaux après distribution = %v", noms)
	}
	if apres := pagesLues(h); apres <= avant {
		t.Error("une création doit relire l'inventaire : le dépôt neuf n'y est pas")
	}
}

// inventaireDeLaVeille amorce le cache d'un inventaire lu il y a sept heures :
// au-delà de ce qu'il vaut, c'est le poste qu'on rallume le lendemain.
func inventaireDeLaVeille(t *testing.T, noms ...string) string {
	t.Helper()
	depots := make([]groups.RepoInfo, 0, len(noms))
	for _, nom := range noms {
		depots = append(depots, groups.RepoInfo{Name: nom, Private: true})
	}
	valeur, err := json.Marshal(depots)
	if err != nil {
		t.Fatal(err)
	}
	contenu, err := json.Marshal(map[string]any{cache.ReposKey("acme"): map[string]any{
		"at":    float64(time.Now().Add(-cache.ReposTTL - time.Hour).Unix()),
		"value": json.RawMessage(valeur),
	}})
	if err != nil {
		t.Fatal(err)
	}
	dossier := t.TempDir()
	if err := os.WriteFile(filepath.Join(dossier, "cache.json"), contenu, 0o600); err != nil {
		t.Fatal(err)
	}
	return dossier
}

// retenirLInventaire retient la page voulue de la première lecture d'inventaire
// jusqu'à ce que le test la libère. Le délai ne sert qu'à ne pas figer un test
// qui échoue.
func retenirLInventaire(state *fakegh.State, page string) chan struct{} {
	liberer := make(chan struct{})
	var une sync.Once
	state.Hook = func(request *http.Request) {
		if request.URL.Path != "/orgs/acme/repos" || request.URL.Query().Get("page") != page {
			return
		}
		une.Do(func() {
			select {
			case <-liberer:
			case <-time.After(10 * time.Second):
			}
		})
	}
	return liberer
}

// listeDesGroupes est la liste telle que la page la reçoit.
type listeDesGroupes struct {
	Classrooms []struct {
		Scope string `json:"scope"`
	} `json:"classrooms"`
	Stale string `json:"stale"`
}

func (l listeDesGroupes) scopes() []string {
	noms := make([]string, 0, len(l.Classrooms))
	for _, groupe := range l.Classrooms {
		noms = append(noms, groupe.Scope)
	}
	return noms
}

// Le matin, l'inventaire de la veille ne vaut plus : la liste le montre quand
// même aussitôt, en disant son âge, et la relecture part en arrière-plan. La
// page qui redemande la liste à jour attend cette relecture plutôt que d'en
// lancer une seconde.
func TestUnInventairePerimeSeMontreAussitotPuisSeRelit(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	dossier := inventaireDeLaVeille(t, "a25.5n6.01.tp1.emilie-cote")
	liberer := retenirLInventaire(state, "1")
	h := nouveauAvec(t, state, func(deps *web.Deps) { deps.Cache = cache.NewIn(dossier, true) })

	var hier listeDesGroupes
	h.json(http.MethodGet, "/api/classrooms", nil, &hier)
	if hier.Stale == "" || !slices.Equal(hier.scopes(), []string{"a25.5n6.01"}) {
		t.Fatalf("la liste d'hier devait venir aussitôt, avec son âge : %+v", hier)
	}

	close(liberer)
	var aujourdhui listeDesGroupes
	h.json(http.MethodGet, "/api/classrooms?fresh=1", nil, &aujourdhui)
	if aujourdhui.Stale != "" || !slices.Equal(aujourdhui.scopes(), []string{"a26.5n6.01"}) {
		t.Fatalf("la liste à jour devait suivre : %+v", aujourdhui)
	}
	if lues := pagesLues(h); lues != 1 {
		t.Errorf("%d lectures d'inventaire : la relecture en cours devait être attendue", lues)
	}
}

// Une écriture faite pendant la relecture n'est pas effacée par elle : partie
// avant, la relecture a pu lire un dépôt que l'écriture vient de supprimer.
func TestUneRelecturePartieAvantUneEcritureNEstPasRetenue(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jlpicard", true)
	state.AddRepo("acme", "a26.5n6.01.tp2.jlpicard", true)
	// Une page par dépôt : la première part avant la suppression, la seconde
	// est retenue jusqu'après. La relecture rapporte donc « tp1 ».
	state.PerPage = 1
	dossier := inventaireDeLaVeille(t, "a26.5n6.01.tp1.jlpicard", "a26.5n6.01.tp2.jlpicard")
	liberer := retenirLInventaire(state, "2")
	h := nouveauAvec(t, state, func(deps *web.Deps) { deps.Cache = cache.NewIn(dossier, true) })

	var hier listeDesGroupes
	h.json(http.MethodGet, "/api/classrooms", nil, &hier)
	if hier.Stale == "" {
		t.Fatalf("l'inventaire de la veille devait être montré : %+v", hier)
	}
	h.json(http.MethodDelete, "/api/orgs/acme/repos/a26.5n6.01.tp1.jlpicard",
		map[string]any{"confirm": "a26.5n6.01.tp1.jlpicard"}, nil)
	close(liberer)

	h.json(http.MethodGet, "/api/classrooms?fresh=1", nil, nil)
	if noms := travaux(h, "a26.5n6.01"); !slices.Equal(noms, []string{"tp2"}) {
		t.Fatalf("travaux après suppression = %v : la relecture d'avant a été retenue", noms)
	}
}
