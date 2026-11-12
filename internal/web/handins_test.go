package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/web"
)

// travailAvecHistoriques monte un groupe de deux étudiantes, chacune avec son
// dépôt et son historique : l'une a remis en retard, l'autre n'a rien écrit.
func travailAvecHistoriques(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()

	tard := state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	tard.History = fakegh.Commits("2026-10-05T14:00:00Z", "2026-09-30T09:00:00Z")
	state.AddContributors("acme/a26.5n6.01.tp1.emilie-cote", "ecote", "ecote")

	muet := state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	// Seuls les fichiers de départ, déposés par qui enseigne.
	muet.History = fakegh.Commits("2026-09-01T08:00:00Z")
	state.AddContributors("acme/a26.5n6.01.tp1.jean-luc-picard", "prof")

	return avantLeRegistre(t, state,
		cohorte("a26", "5n6", "01", "Émilie Côté", "ecote", "Jean-Luc Picard", "jlpicard"))
}

func TestDateCibleSeFixeEtParaitDansLeTravail(t *testing.T) {
	h := travailAvecHistoriques(t)

	var pose struct {
		Name string `json:"name"`
		Due  string `json:"due"`
	}
	h.json(http.MethodPut, "/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": "2026-10-01"}, &pose)
	if pose.Due != "2026-10-01" {
		t.Fatalf("due = %q", pose.Due)
	}

	var fiche struct {
		Assignments []struct {
			Name string `json:"name"`
			Due  string `json:"due"`
		} `json:"assignments"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01", nil, &fiche)
	if len(fiche.Assignments) != 1 || fiche.Assignments[0].Due != "2026-10-01" {
		t.Fatalf("le travail ne porte pas sa date cible : %+v", fiche.Assignments)
	}

	// Une date vide la retire : c'est la même décision prise dans l'autre sens.
	h.json(http.MethodPut, "/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": ""}, &pose)
	if pose.Due != "" {
		t.Errorf("la date cible survit à son retrait : %q", pose.Due)
	}
}

// Une date fixée ici se lit là-bas : c'est tout l'objet de l'avoir mise dans
// l'organisation. Le second poste ne partage ni fichier local ni cache.
func TestUneDateCibleSeLitDepuisUnAutrePoste(t *testing.T) {
	h := travailAvecHistoriques(t)
	h.json(http.MethodPut, "/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": "2026-10-01"}, nil)

	// Un collègue ouvre la même organisation, sans avoir rien déclaré.
	collegue := nouveau(t, h.State)
	var fiche struct {
		Assignments []struct {
			Name string `json:"name"`
			Due  string `json:"due"`
		} `json:"assignments"`
	}
	collegue.json(http.MethodGet, "/api/classrooms/a26.5n6.01", nil, &fiche)
	if len(fiche.Assignments) != 1 {
		t.Fatalf("travaux vus par le collègue : %+v", fiche.Assignments)
	}
	if fiche.Assignments[0].Due != "2026-10-01" {
		t.Errorf("le collègue ne voit pas l'échéance : %+v", fiche.Assignments[0])
	}
}

// Fixer une date n'oblige pas à déclarer le groupe : il existe par ses dépôts,
// et l'échéance va dans l'organisation, pas dans un fichier local.
func TestUneDateCibleSeFixeSurUnGroupeNonDeclare(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	h := nouveau(t, state)

	var pose struct {
		Due string `json:"due"`
	}
	h.json(http.MethodPut, "/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": "2026-10-01"}, &pose)
	if pose.Due != "2026-10-01" {
		t.Fatalf("due = %q", pose.Due)
	}
	if local := h.declares("a26.5n6.01"); local != nil {
		t.Errorf("le groupe a été déclaré localement pour une date : %v", local)
	}
}

func TestDateCibleMalEcriteEstRefusee(t *testing.T) {
	h := travailAvecHistoriques(t)
	reponse, contenu := h.requete(http.MethodPut,
		"/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": "le 1er octobre"})
	if reponse.StatusCode < 400 {
		t.Fatalf("statut %d, attendu un refus — %s", reponse.StatusCode, contenu)
	}
}

func TestReleveDesRemisesSignaleLeRetardEtLeSilence(t *testing.T) {
	h := travailAvecHistoriques(t)
	h.json(http.MethodPut, "/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": "2026-10-01"}, nil)

	etat := h.travail(http.MethodPost,
		"/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)
	bilans, ok := etat["result"].([]any)
	if !ok || len(bilans) != 2 {
		t.Fatalf("bilans = %#v", etat["result"])
	}

	parNom := map[string]map[string]any{}
	for _, brut := range bilans {
		bilan := brut.(map[string]any)
		parNom[bilan["repo"].(string)] = bilan
	}

	tard := parNom["a26.5n6.01.tp1.emilie-cote"]
	if tard["late"] != true {
		t.Errorf("le dépôt remis le 5 octobre n'est pas en retard : %+v", tard)
	}
	if tard["commits"] != float64(2) {
		t.Errorf("commits = %v, attendu 2", tard["commits"])
	}
	if tard["silent"] != nil {
		t.Errorf("celle qui a écrit est dite muette : %+v", tard["silent"])
	}

	muet := parNom["a26.5n6.01.tp1.jean-luc-picard"]
	if muet["late"] == true {
		t.Errorf("le dépôt du 1er septembre est dit en retard : %+v", muet)
	}
	silencieux, _ := muet["silent"].([]any)
	if len(silencieux) != 1 {
		t.Fatalf("silent = %#v, attendu la seule personne visée", muet["silent"])
	}
	if silencieux[0].(map[string]any)["username"] != "jlpicard" {
		t.Errorf("silent = %#v", silencieux[0])
	}
}

// relance rouvre l'interface sur la même mémoire, dont chaque entrée est
// reculée de « par » : c'est le poste qu'on rallume après une pause, sans que
// personne ait rien demandé à GitHub entre-temps.
func (h *harnais) relance(par time.Duration, cours ...classroom.Classroom) *harnais {
	h.t.Helper()
	dossier := filepath.Join(filepath.Dir(h.Groupes), "cache")
	chemin := filepath.Join(dossier, "cache.json")
	contenu, err := os.ReadFile(chemin)
	if err != nil {
		h.t.Fatalf("mémoire : %v", err)
	}
	var entrees map[string]struct {
		At    float64         `json:"at"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(contenu, &entrees); err != nil {
		h.t.Fatalf("mémoire illisible : %v", err)
	}
	for cle, entree := range entrees {
		entree.At -= par.Seconds()
		entrees[cle] = entree
	}
	vieillie, _ := json.Marshal(entrees)
	if err := os.WriteFile(chemin, vieillie, 0o600); err != nil {
		h.t.Fatalf("mémoire : %v", err)
	}
	return nouveauAvec(h.t, h.State, func(deps *web.Deps) {
		deps.Cache = cache.NewIn(dossier, true)
		store := classroom.Open(classroom.PathNextTo(deps.ConfigFile))
		for _, item := range cours {
			if _, err := store.Save(item); err != nil {
				h.t.Fatalf("déclaration : %v", err)
			}
		}
	})
}

// Un relevé ne s'efface pas de l'écran parce qu'une heure a passé : tant que
// rien n'a été poussé depuis, il dit encore la vérité, et le poste qu'on
// rallume deux heures plus tard le montre sans rien redemander.
func TestUnReleveQueRienNADepasseSeMontreEncoreDesHeuresApres(t *testing.T) {
	h := travailAvecHistoriques(t)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)
	lectures := h.State.CallCount("/contributors")

	apres := h.relance(2*time.Hour,
		cohorte("a26", "5n6", "01", "Émilie Côté", "ecote", "Jean-Luc Picard", "jlpicard"))
	var detail struct {
		Repos []struct {
			Name    string `json:"name"`
			Seen    bool   `json:"seen"`
			Commits int    `json:"commits"`
			State   string `json:"state"`
		} `json:"repos"`
	}
	apres.json(http.MethodGet, "/api/classrooms/a26.5n6.01/assignments/tp1", nil, &detail)
	if len(detail.Repos) != 2 {
		t.Fatalf("dépôts : %+v", detail.Repos)
	}
	for _, repo := range detail.Repos {
		if !repo.Seen || repo.State == "non relevé" {
			t.Errorf("%s : relevé il y a deux heures, et pourtant « %s » (%+v)",
				repo.Name, repo.State, repo)
		}
	}
	if encore := h.State.CallCount("/contributors"); encore != lectures {
		t.Errorf("%d historique(s) relus pour montrer ce qu'on savait", encore-lectures)
	}

	// Les pastilles de la liste des travaux lisent la même mémoire.
	var fiche struct {
		Assignments []struct {
			Seen int `json:"seen"`
		} `json:"assignments"`
	}
	apres.json(http.MethodGet, "/api/classrooms/a26.5n6.01", nil, &fiche)
	if len(fiche.Assignments) != 1 || fiche.Assignments[0].Seen != 2 {
		t.Errorf("travaux : %+v", fiche.Assignments)
	}
}

// Un relevé vaut tant qu'aucun envoi ne l'a dépassé, et c'est l'inventaire qui
// le dit : un dépôt qui a reçu quelque chose depuis redevient « non relevé »
// plutôt que de montrer un compte faux, et les autres gardent le leur.
func TestUnEnvoiPosterieurAuReleveLeRendANouveauNonReleve(t *testing.T) {
	h := travailAvecHistoriques(t)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)

	// Émilie pousse deux minutes après le relevé : au-delà de la marge que le
	// relevé accorde aux horloges.
	h.State.Touch("acme/a26.5n6.01.tp1.emilie-cote",
		time.Now().Add(2*time.Minute).UTC().Format(time.RFC3339))

	var detail struct {
		Repos []struct {
			Name  string `json:"name"`
			Seen  bool   `json:"seen"`
			State string `json:"state"`
		} `json:"repos"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01/assignments/tp1?refresh=1", nil, &detail)
	for _, repo := range detail.Repos {
		switch repo.Name {
		case "a26.5n6.01.tp1.emilie-cote":
			if repo.Seen || repo.State != "non relevé" {
				t.Errorf("un dépôt poussé depuis le relevé paraît encore relevé : %+v", repo)
			}
		case "a26.5n6.01.tp1.jean-luc-picard":
			if !repo.Seen {
				t.Errorf("un dépôt que rien n'a dépassé a perdu son relevé : %+v", repo)
			}
		}
	}
}

// Le relevé ordinaire — celui que la page lance d'elle-même quand un dépôt
// n'est pas relevé — relit ce qu'un envoi a dépassé, et seulement cela. S'il
// s'en remettait à la mémoire, le dépôt resterait « non relevé », et la page
// redemanderait un relevé à chaque ouverture.
func TestLeReleveOrdinaireRelitCeQuUnEnvoiADepasse(t *testing.T) {
	h := travailAvecHistoriques(t)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)
	// Le relevé date d'une demi-heure, mais la mémoire le garde encore : elle
	// ne l'oublie qu'au bout d'une heure. Émilie a poussé il y a une minute.
	apres := h.releveVieilli(30*time.Minute,
		cohorte("a26", "5n6", "01", "Émilie Côté", "ecote", "Jean-Luc Picard", "jlpicard"))
	h.State.Touch("acme/a26.5n6.01.tp1.emilie-cote",
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339))
	avant := apres.etatsDesDepots("/api/classrooms/a26.5n6.01/assignments/tp1?refresh=1")
	if avant["a26.5n6.01.tp1.emilie-cote"] != "non relevé" {
		t.Fatalf("le dépôt poussé depuis le relevé est dit %q avant tout relevé",
			avant["a26.5n6.01.tp1.emilie-cote"])
	}
	lectures := h.State.CallCount("/contributors")

	apres.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)

	if relus := h.State.CallCount("/contributors") - lectures; relus != 1 {
		t.Errorf("%d historique(s) relus, attendu le seul dépôt poussé depuis", relus)
	}
	etats := apres.etatsDesDepots("/api/classrooms/a26.5n6.01/assignments/tp1")
	if etats["a26.5n6.01.tp1.emilie-cote"] == "non relevé" {
		t.Error("le dépôt poussé depuis le relevé reste « non relevé » après un relevé")
	}
}

// releveVieilli rouvre l'interface sur la même mémoire, dont chaque relevé
// d'historique est daté de « par » plus tôt. L'âge des entrées, lui, ne bouge
// pas : la mémoire les rend encore, comme si le relevé datait vraiment de là.
func (h *harnais) releveVieilli(par time.Duration, cours ...classroom.Classroom) *harnais {
	h.t.Helper()
	dossier := filepath.Join(filepath.Dir(h.Groupes), "cache")
	chemin := filepath.Join(dossier, "cache.json")
	contenu, err := os.ReadFile(chemin)
	if err != nil {
		h.t.Fatalf("mémoire : %v", err)
	}
	var entrees map[string]map[string]any
	if err := json.Unmarshal(contenu, &entrees); err != nil {
		h.t.Fatalf("mémoire illisible : %v", err)
	}
	for cle, entree := range entrees {
		remise, historique := entree["value"].(map[string]any)
		if !strings.HasPrefix(cle, "handin:") || !historique {
			continue
		}
		vu, err := time.Parse(time.RFC3339, fmt.Sprint(remise["seen"]))
		if err != nil {
			h.t.Fatalf("%s : relevé sans date (%v)", cle, err)
		}
		remise["seen"] = vu.Add(-par).Format(time.RFC3339)
	}
	vieillie, _ := json.Marshal(entrees)
	if err := os.WriteFile(chemin, vieillie, 0o600); err != nil {
		h.t.Fatalf("mémoire : %v", err)
	}
	return h.relance(0, cours...)
}

func TestLesPastillesDunTravailSeLisentSansRienRedemander(t *testing.T) {
	h := travailAvecHistoriques(t)
	h.json(http.MethodPut, "/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": "2026-10-01"}, nil)

	// Avant tout relevé, le travail ne prétend rien savoir.
	var avant struct {
		Assignments []struct {
			Seen    int `json:"seen"`
			Commits int `json:"commits"`
			Late    int `json:"late"`
			Silent  int `json:"silent"`
		} `json:"assignments"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01", nil, &avant)
	if len(avant.Assignments) != 1 || avant.Assignments[0].Seen != 0 {
		t.Fatalf("un travail jamais relevé prétend savoir : %+v", avant.Assignments)
	}

	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/handins", nil)

	var apres struct {
		Assignments []struct {
			Seen    int `json:"seen"`
			Commits int `json:"commits"`
			Late    int `json:"late"`
			Silent  int `json:"silent"`
		} `json:"assignments"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01", nil, &apres)
	travail := apres.Assignments[0]
	if travail.Seen != 2 {
		t.Errorf("Seen = %d, attendu 2", travail.Seen)
	}
	if travail.Commits != 3 {
		t.Errorf("Commits = %d, attendu 3", travail.Commits)
	}
	if travail.Late != 1 {
		t.Errorf("Late = %d, attendu 1", travail.Late)
	}
	if travail.Silent != 1 {
		t.Errorf("Silent = %d, attendu 1", travail.Silent)
	}
}

func TestLaDateCibleSuitLeTravailRenomme(t *testing.T) {
	h := travailAvecHistoriques(t)
	h.json(http.MethodPut, "/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": "2026-10-01"}, nil)

	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/rename",
		map[string]any{"id": "a26.5n6.01.tp1", "name": "projet-final"})

	var fiche struct {
		Assignments []struct {
			Name string `json:"name"`
			Due  string `json:"due"`
		} `json:"assignments"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01?refresh=1", nil, &fiche)
	if len(fiche.Assignments) != 1 {
		t.Fatalf("travaux = %+v", fiche.Assignments)
	}
	if fiche.Assignments[0].Name != "projet-final" || fiche.Assignments[0].Due != "2026-10-01" {
		t.Errorf("la date cible n'a pas suivi le renommage : %+v", fiche.Assignments[0])
	}
}

// Une correction déposée après l'échéance est l'œuvre de qui enseigne : elle ne
// doit pas mettre l'étudiante en retard pour ce qu'elle n'a pas fait.
func TestUneCorrectionDEnseignantNeMetPersonneEnRetard(t *testing.T) {
	state := fakegh.NewState()
	depot := state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	depot.History = []fakegh.HistoryEntry{
		{At: "2026-10-03T16:00:00Z", Login: "prof"},
		{At: "2026-09-30T20:45:00Z", Login: "ecote"},
	}
	state.AddContributors("acme/a26.5n6.01.tp1.emilie-cote", "ecote", "prof")

	h := avantLeRegistre(t, state, cohorte("a26", "5n6", "01", "Émilie Côté", "ecote"))
	h.coopter("prof", true)
	h.json(http.MethodPut, "/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": "2026-10-01"}, nil)

	etat := h.travail(http.MethodPost,
		"/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)
	bilans, ok := etat["result"].([]any)
	if !ok || len(bilans) != 1 {
		t.Fatalf("bilans = %#v", etat["result"])
	}
	bilan := bilans[0].(map[string]any)
	if bilan["late"] == true {
		t.Errorf("le commit de l'enseignant met l'étudiante en retard : %+v", bilan)
	}
	if bilan["last"] != "2026-09-30T20:45:00Z" {
		t.Errorf("la remise est datée %v, attendu le dernier commit d'Émilie", bilan["last"])
	}
}

// travailAvecInvitation monte un travail de deux dépôts : l'un remis, l'autre
// dont l'étudiant n'a pas encore accepté son invitation — il n'a donc pas pu
// remettre.
func travailAvecInvitation(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()

	remis := state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	remis.History = []fakegh.HistoryEntry{{At: "2026-09-30T20:45:00Z", Login: "ecote"}}
	state.AddContributors("acme/a26.5n6.01.tp1.emilie-cote", "ecote")
	state.AddCollaborator("acme/a26.5n6.01.tp1.emilie-cote", "ecote", "push")

	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	state.Invite("acme/a26.5n6.01.tp1.jean-luc-picard", "jlpicard", "push")

	return avantLeRegistre(t, state,
		cohorte("a26", "5n6", "01", "Émilie Côté", "ecote", "Jean-Luc Picard", "jlpicard"))
}

// etatsDesDepots rend l'état de la remise de chaque dépôt d'un travail.
func (h *harnais) etatsDesDepots(adresse string) map[string]string {
	h.t.Helper()
	var fiche struct {
		Repos []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"repos"`
	}
	h.json(http.MethodGet, adresse, nil, &fiche)
	etats := map[string]string{}
	for _, repo := range fiche.Repos {
		etats[repo.Name] = repo.State
	}
	return etats
}

// Une invitation qu'on n'a pas acceptée n'est pas un silence : la personne n'a
// pas pu remettre, et l'écran doit le dire autrement.
func TestUneInvitationEnAttenteSeDistingueDUneRemiseManquante(t *testing.T) {
	h := travailAvecInvitation(t)
	// Le relevé des remises va chercher les accès des dépôts qui n'ont rien
	// reçu : c'est là que la question « a-t-il accepté ? » se pose.
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)

	etats := h.etatsDesDepots("/api/classrooms/a26.5n6.01/assignments/tp1")
	if etats["a26.5n6.01.tp1.jean-luc-picard"] != "non accepté" {
		t.Errorf("le dépôt dont l'invitation attend est dit %q",
			etats["a26.5n6.01.tp1.jean-luc-picard"])
	}
	if etats["a26.5n6.01.tp1.emilie-cote"] != "remis" {
		t.Errorf("le dépôt remis est dit %q", etats["a26.5n6.01.tp1.emilie-cote"])
	}
}

// Le filtre par état se pose dans l'adresse, comme les autres critères : c'est
// ce qui lui fait dire la même chose qu'au terminal.
func TestLesDepotsSeFiltrentParEtatDeRemise(t *testing.T) {
	h := travailAvecInvitation(t)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)

	attendus := h.etatsDesDepots(
		"/api/classrooms/a26.5n6.01/assignments/tp1?handin=" + url.QueryEscape("non accepté"))
	if len(attendus) != 1 || attendus["a26.5n6.01.tp1.jean-luc-picard"] != "non accepté" {
		t.Fatalf("« non accepté » retient %v", attendus)
	}
	remis := h.etatsDesDepots("/api/classrooms/a26.5n6.01/assignments/tp1?handin=remis")
	if len(remis) != 1 || remis["a26.5n6.01.tp1.emilie-cote"] != "remis" {
		t.Fatalf("« remis » retient %v", remis)
	}
	// Un état inconnu s'arrête ici plutôt que de se perdre en cours de route.
	reponse, contenu := h.requete(http.MethodGet,
		"/api/classrooms/a26.5n6.01/assignments/tp1?handin=presque", nil)
	if reponse.StatusCode < 400 {
		t.Fatalf("statut %d, attendu un refus — %s", reponse.StatusCode, contenu)
	}
}

// La synthèse qui coiffe la liste parle du travail entier : filtrer la liste
// n'en retire rien. Chaque dépôt relevé dit aussi quand il l'a été.
func TestLaSyntheseDUnTravailIgnoreLeFiltre(t *testing.T) {
	h := travailAvecInvitation(t)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)

	var detail struct {
		States []struct {
			State string `json:"state"`
			Count int    `json:"count"`
		} `json:"states"`
		Private int `json:"private"`
		Total   int `json:"total"`
		Repos   []struct {
			Name   string `json:"name"`
			SeenAt string `json:"seen_at"`
		} `json:"repos"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01/assignments/tp1?handin=remis", nil, &detail)
	if len(detail.Repos) != 1 {
		t.Fatalf("le filtre « remis » retient %d dépôts", len(detail.Repos))
	}
	synthese := map[string]int{}
	for _, item := range detail.States {
		synthese[item.State] = item.Count
	}
	if synthese["remis"] != 1 || synthese["non accepté"] != 1 || len(synthese) != 2 {
		t.Errorf("synthèse = %v, attendu le travail entier malgré le filtre", detail.States)
	}
	if detail.Private != detail.Total {
		t.Errorf("%d dépôts privés sur %d", detail.Private, detail.Total)
	}
	if _, err := time.Parse(time.RFC3339, detail.Repos[0].SeenAt); err != nil {
		t.Errorf("seen_at = %q, attendu l'instant du relevé : %v", detail.Repos[0].SeenAt, err)
	}
}

// L'accueil montre les groupes de la session en cours avec ce qui manque à
// leurs travaux : la liste des groupes porte ce que les historiques relevés en
// disent, sans rien redemander à GitHub.
func TestLaListeDesGroupesPorteLesRemisesDeLaSessionEnCours(t *testing.T) {
	h := travailAvecHistoriques(t)
	h.json(http.MethodPut, "/api/classrooms/a26.5n6.01/assignments/tp1/deadline",
		map[string]any{"due": "2026-10-01"}, nil)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/assignments/tp1/handins", nil)
	lectures := h.State.CallCount("/contributors")

	var liste struct {
		Classrooms []struct {
			Scope       string `json:"scope"`
			Assignments []struct {
				Name   string `json:"name"`
				Seen   int    `json:"seen"`
				Late   int    `json:"late"`
				Silent int    `json:"silent"`
			} `json:"assignments"`
		} `json:"classrooms"`
	}
	h.json(http.MethodGet, "/api/classrooms?fresh=1", nil, &liste)
	if len(liste.Classrooms) != 1 || len(liste.Classrooms[0].Assignments) != 1 {
		t.Fatalf("groupes : %+v", liste.Classrooms)
	}
	travail := liste.Classrooms[0].Assignments[0]
	if travail.Seen != 2 || travail.Late != 1 || travail.Silent != 1 {
		t.Errorf("tp1 = %+v, attendu deux dépôts relevés, un en retard, un sans remise", travail)
	}
	if encore := h.State.CallCount("/contributors"); encore != lectures {
		t.Errorf("%d historique(s) relus pour dresser la liste des groupes", encore-lectures)
	}
}
