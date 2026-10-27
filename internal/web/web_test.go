package web_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/brand"
	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/scopes"
	"github.com/PierreOlivierBrillant/gh-milou/internal/web"
)

// harnais monte l'interface web au-dessus d'un faux GitHub, sur un vrai port
// local : le filtrage des requêtes est ainsi éprouvé comme en usage réel.
type harnais struct {
	t       *testing.T
	State   *fakegh.State
	Github  *ghapi.Client
	Serveur *web.Server
	Client  *http.Client
	Base    string
	// Groupes est le fichier local des groupes de cette machine.
	Groupes string
}

func nouveau(t *testing.T, state *fakegh.State) *harnais {
	t.Helper()
	return nouveauAvec(t, state, nil)
}

// nouveauAvec laisse le test retoucher les dépendances de l'interface : c'est
// par là qu'un faux « gh auth refresh » remplace le vrai.
func nouveauAvec(t *testing.T, state *fakegh.State, ajuster func(*web.Deps)) *harnais {
	t.Helper()
	if state == nil {
		state = fakegh.NewState()
	}
	faux := fakegh.New(state)
	t.Cleanup(faux.Close)

	client, err := ghapi.New(ghapi.Options{
		Host: "github.com", Token: "jeton-de-test", BaseURL: faux.URL(),
		Sleep: func(time.Duration) {}, Now: time.Now,
	})
	if err != nil {
		t.Fatalf("client GitHub : %v", err)
	}
	// L'interface reçoit un client déjà authentifié, comme le fait la session
	// du terminal : c'est cet appel qui révèle les portées du jeton.
	if _, err := client.AuthenticatedUser(); err != nil {
		t.Fatalf("authentification : %v", err)
	}

	dossier := t.TempDir()
	// L'interface travaille dans une organisation à la fois : c'est le choix
	// fait à l'arrivée, et l'API s'y tient.
	reglages := config.Default()
	reglages.Org = "acme"
	deps := web.Deps{
		Client:      client,
		Cache:       cache.NewIn(filepath.Join(dossier, "cache"), true),
		Settings:    reglages,
		ConfigFile:  filepath.Join(dossier, "config.json"),
		Viewer:      state.Viewer,
		Host:        "github.com",
		TokenOrigin: "oauth_token",
		Version:     "test",
		ReportDir:   filepath.Join(dossier, "rapports"),
		Jobs:        2,
		SaveConfig:  true,
	}
	if ajuster != nil {
		ajuster(&deps)
	}
	fichierGroupes := classroom.PathNextTo(deps.ConfigFile)
	serveur, err := web.New(deps)
	if err != nil {
		t.Fatalf("interface web : %v", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("bocal à témoins : %v", err)
	}
	// Le navigateur a son propre transport : ses connexions ne sont qu'à lui,
	// et les fermer ne dérange aucun autre client.
	navigateur := &http.Client{
		Jar: jar, Timeout: 20 * time.Second,
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
	}

	lifetime, arret := context.WithCancel(context.Background())
	fini := make(chan struct{})
	go func() {
		defer close(fini)
		if err := serveur.Serve(lifetime); err != nil {
			t.Errorf("service interrompu : %v", err)
		}
	}()
	t.Cleanup(func() {
		// Quand une requête part avant que la précédente ait rendu sa
		// connexion, le transport en compose une seconde, puis la garde sans
		// jamais s'en servir. Le serveur la voit neuve, et Shutdown ne tient
		// une connexion neuve pour inactive qu'au bout de cinq secondes : il
		// attendrait sa borne de deux secondes, un test sur trois. On rend
		// donc les connexions inactives avant d'arrêter.
		navigateur.CloseIdleConnections()
		arret()
		<-fini
	})

	h := &harnais{
		t: t, State: state, Github: client, Serveur: serveur, Base: serveur.Address(),
		Client: navigateur, Groupes: fichierGroupes,
	}
	// L'adresse remise dans le terminal ouvre la session et pose le témoin.
	reponse, err := h.Client.Get(serveur.URL())
	if err != nil {
		t.Fatalf("ouverture de session : %v", err)
	}
	reponse.Body.Close()
	return h
}

// requete envoie une requête d'interface complète, en-tête comprise.
func (h *harnais) requete(methode, chemin string, corps any) (*http.Response, []byte) {
	h.t.Helper()
	var lecteur io.Reader
	if corps != nil {
		encode, err := json.Marshal(corps)
		if err != nil {
			h.t.Fatalf("encodage : %v", err)
		}
		lecteur = bytes.NewReader(encode)
	}
	requete, err := http.NewRequest(methode, h.Base+chemin, lecteur)
	if err != nil {
		h.t.Fatalf("requête : %v", err)
	}
	requete.Header.Set("Origin", h.Base)
	requete.Header.Set("X-Milou", "1")
	if corps != nil {
		requete.Header.Set("Content-Type", "application/json")
	}
	reponse, err := h.Client.Do(requete)
	if err != nil {
		h.t.Fatalf("appel %s %s : %v", methode, chemin, err)
	}
	defer reponse.Body.Close()
	contenu, err := io.ReadAll(reponse.Body)
	if err != nil {
		h.t.Fatalf("lecture : %v", err)
	}
	return reponse, contenu
}

// json envoie une requête et décode la réponse, en exigeant un succès.
func (h *harnais) json(methode, chemin string, corps any, cible any) {
	h.t.Helper()
	reponse, contenu := h.requete(methode, chemin, corps)
	if reponse.StatusCode >= 300 {
		h.t.Fatalf("%s %s : statut %d — %s", methode, chemin, reponse.StatusCode, contenu)
	}
	if cible == nil {
		return
	}
	if err := json.Unmarshal(contenu, cible); err != nil {
		h.t.Fatalf("réponse illisible (%s) : %v", contenu, err)
	}
}

// decoder lit un corps de réponse déjà lu, refus compris.
func (h *harnais) decoder(contenu []byte, cible any) {
	h.t.Helper()
	if err := json.Unmarshal(contenu, cible); err != nil {
		h.t.Fatalf("réponse illisible (%s) : %v", contenu, err)
	}
}

// avantLeRegistre monte une interface dont le fichier local porte déjà des
// groupes, sans que le registre de l'organisation en sache rien.
//
// C'est l'état d'un poste d'avant le registre : des années de noms dans un
// fichier que plus rien ne lira. Les groupes sont écrits avant que le serveur
// n'ouvre son magasin, sans quoi il ne les verrait pas.
func avantLeRegistre(t *testing.T, state *fakegh.State,
	cours ...classroom.Classroom) *harnais {
	t.Helper()
	return nouveauAvec(t, state, func(deps *web.Deps) {
		store := classroom.Open(classroom.PathNextTo(deps.ConfigFile))
		for _, item := range cours {
			if _, err := store.Save(item); err != nil {
				t.Fatalf("déclaration : %v", err)
			}
		}
	})
}

// cohorte compose un groupe à partir de couples « nom complet, compte ».
func cohorte(session, cours, section string, couples ...string) classroom.Classroom {
	gens := make([]roster.Person, 0, len(couples)/2)
	for index := 0; index+1 < len(couples); index += 2 {
		gens = append(gens, roster.Person{FullName: couples[index], Username: couples[index+1]})
	}
	return classroom.Classroom{
		Org: "acme", Session: session, Course: cours, Group: section, Students: gens,
	}
}

// declares relit le fichier local des groupes : ce que cette machine a
// vraiment déclaré, par opposition à ce que l'interface montre une fois le
// registre versé dessus.
func (h *harnais) declares(scope string) []string {
	h.t.Helper()
	contenu, err := os.ReadFile(h.Groupes)
	if err != nil {
		return nil
	}
	var lu struct {
		Classrooms []struct {
			Session  string `json:"session"`
			Course   string `json:"course"`
			Group    string `json:"group"`
			Students []struct {
				Username string `json:"username"`
			} `json:"students"`
		} `json:"classrooms"`
	}
	if err := json.Unmarshal(contenu, &lu); err != nil {
		h.t.Fatalf("groupes.json illisible : %v", err)
	}
	for _, item := range lu.Classrooms {
		if !strings.EqualFold(item.Session+"."+item.Course+"."+item.Group, scope) {
			continue
		}
		comptes := make([]string, 0, len(item.Students))
		for _, student := range item.Students {
			comptes = append(comptes, student.Username)
		}
		sort.Strings(comptes)
		return comptes
	}
	return nil
}

// depots rend les dépôts d'étudiants de l'organisation, triés.
//
// Les dépôts de service en sont écartés : « .cohorte », que le registre des
// étudiants amène dès qu'un nom est appris, n'est pas le dépôt de quelqu'un et
// n'a rien à faire dans ce que ces tests comparent.
func (h *harnais) depots() []string {
	gardes := make([]string, 0)
	for _, nom := range h.State.RepoNames("acme") {
		if !groups.Service(nom) {
			gardes = append(gardes, nom)
		}
	}
	return gardes
}

// groupe déclare un groupe de la nomenclature courante et rend sa place.
func (h *harnais) groupe(session, cours, section string, couples ...string) string {
	h.t.Helper()
	etudiants := make([]map[string]string, 0, len(couples)/2)
	for index := 0; index+1 < len(couples); index += 2 {
		etudiants = append(etudiants, map[string]string{
			"full_name": couples[index], "username": couples[index+1],
		})
	}
	var cree struct {
		Scope string `json:"scope"`
	}
	h.json(http.MethodPost, "/api/classrooms", map[string]any{
		"session": session, "course": cours, "group": section, "students": etudiants,
	}, &cree)
	if cree.Scope == "" {
		h.t.Fatal("groupe sans place")
	}
	return cree.Scope
}

// heritage déclare un groupe resté à l'ancienne nomenclature.
func (h *harnais) sansNoms(session, cours, section string, comptes ...string) string {
	h.t.Helper()
	// Un groupe repris d'ailleurs ne connaît que les comptes : les noms
	// complets restent à retrouver.
	etudiants := make([]map[string]string, 0, len(comptes))
	for _, compte := range comptes {
		etudiants = append(etudiants, map[string]string{"username": compte, "full_name": ""})
	}
	var cree struct {
		Scope string `json:"scope"`
	}
	h.json(http.MethodPost, "/api/classrooms", map[string]any{
		"session": session, "course": cours, "group": section, "students": etudiants,
	}, &cree)
	if cree.Scope == "" {
		h.t.Fatal("groupe sans place")
	}
	return cree.Scope
}

// travail lance une opération en arrière-plan et attend son bilan.
func (h *harnais) travail(methode, chemin string, corps any) map[string]any {
	h.t.Helper()
	var fiche struct {
		ID string `json:"id"`
	}
	h.json(methode, chemin, corps, &fiche)
	if fiche.ID == "" {
		h.t.Fatal("aucun identifiant de travail")
	}
	limite := time.Now().Add(15 * time.Second)
	for time.Now().Before(limite) {
		var etat map[string]any
		h.json(http.MethodGet, "/api/jobs/"+fiche.ID, nil, &etat)
		if etat["status"] != "en cours" {
			return etat
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("travail %s toujours en cours", fiche.ID)
	return nil
}

// ------------------------------------------------------------------ sécurité

func TestSessionRefuseeSansTemoin(t *testing.T) {
	h := nouveau(t, nil)
	nu := &http.Client{Timeout: 5 * time.Second}
	reponse, err := nu.Get(h.Base + "/api/context")
	if err != nil {
		t.Fatalf("appel : %v", err)
	}
	defer reponse.Body.Close()
	if reponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("statut %d, attendu 401", reponse.StatusCode)
	}
}

func TestJetonInvalideRefuse(t *testing.T) {
	h := nouveau(t, nil)
	nu := &http.Client{Timeout: 5 * time.Second}
	reponse, err := nu.Get(h.Base + "/?jeton=faux")
	if err != nil {
		t.Fatalf("appel : %v", err)
	}
	defer reponse.Body.Close()
	if reponse.StatusCode != http.StatusForbidden {
		t.Fatalf("statut %d, attendu 403", reponse.StatusCode)
	}
}

func TestHoteInattenduRefuse(t *testing.T) {
	h := nouveau(t, nil)
	requete, _ := http.NewRequest(http.MethodGet, h.Base+"/api/context", nil)
	requete.Host = "exemple.test"
	reponse, err := h.Client.Do(requete)
	if err != nil {
		t.Fatalf("appel : %v", err)
	}
	defer reponse.Body.Close()
	if reponse.StatusCode != http.StatusForbidden {
		t.Fatalf("statut %d, attendu 403", reponse.StatusCode)
	}
}

func TestOrigineEtrangereRefusee(t *testing.T) {
	h := nouveau(t, nil)
	requete, _ := http.NewRequest(http.MethodPost, h.Base+"/api/cache/clear", nil)
	requete.Header.Set("Origin", "http://exemple.test")
	requete.Header.Set("X-Milou", "1")
	reponse, err := h.Client.Do(requete)
	if err != nil {
		t.Fatalf("appel : %v", err)
	}
	defer reponse.Body.Close()
	if reponse.StatusCode != http.StatusForbidden {
		t.Fatalf("statut %d, attendu 403", reponse.StatusCode)
	}
}

func TestEcritureSansEnteteRefusee(t *testing.T) {
	h := nouveau(t, nil)
	requete, _ := http.NewRequest(http.MethodPost, h.Base+"/api/cache/clear", nil)
	requete.Header.Set("Origin", h.Base)
	reponse, err := h.Client.Do(requete)
	if err != nil {
		t.Fatalf("appel : %v", err)
	}
	defer reponse.Body.Close()
	if reponse.StatusCode != http.StatusForbidden {
		t.Fatalf("statut %d, attendu 403", reponse.StatusCode)
	}
}

// ------------------------------------------------------------------- lecture

func TestContexteDecritLaSession(t *testing.T) {
	h := nouveau(t, nil)
	var contexte struct {
		Viewer   string          `json:"viewer"`
		Version  string          `json:"version"`
		Token    jeton           `json:"token"`
		Settings config.Settings `json:"settings"`
	}
	h.json(http.MethodGet, "/api/context", nil, &contexte)

	if contexte.Viewer != "prof" {
		t.Fatalf("compte %q, attendu « prof »", contexte.Viewer)
	}
	if etat := contexte.Token.etat("repo"); etat != scopes.Present {
		t.Fatalf("portée repo : %q", etat)
	}
	if contexte.Settings.NamePattern != config.DefaultNamePattern {
		t.Fatalf("gabarit %q", contexte.Settings.NamePattern)
	}
}

func TestListeCollectiveLue(t *testing.T) {
	h := nouveau(t, nil)

	var liste struct {
		People []struct {
			FullName string `json:"full_name"`
			Username string `json:"username"`
		} `json:"people"`
		Issues []struct {
			Message string `json:"message"`
		} `json:"issues"`
	}
	h.json(http.MethodPost, "/api/roster/parse", map[string]any{
		"text": "Jean-Luc Picard, jlpicard\n\u00c9milie C\u00f4t\u00e9, emilie-cote\nligne bancale\n",
	}, &liste)
	if len(liste.People) != 2 {
		t.Fatalf("%d personne(s) retenue(s), attendu 2", len(liste.People))
	}
	if len(liste.Issues) != 1 {
		t.Fatalf("%d rejet(s), attendu 1", len(liste.Issues))
	}
}

// ------------------------------------------------------------------- groupes

func TestGroupeDeclareEtRelu(t *testing.T) {
	h := nouveau(t, nil)
	place := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote")
	if place != "a26.5n6.01" {
		t.Fatalf("place %q", place)
	}

	var fiche struct {
		Scope       string `json:"scope"`
		Label       string `json:"label"`
		SessionName string `json:"session_name"`
		Session     string `json:"session"`
		Course      string `json:"course"`
		Group       string `json:"group"`
		Known       bool   `json:"known"`
		Students    []struct {
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place, nil, &fiche)
	if fiche.Session != "a26" || fiche.Course != "5n6" || fiche.Group != "01" ||
		len(fiche.Students) != 2 || !fiche.Known {
		t.Fatalf("groupe relu : %+v", fiche)
	}
	// Le libellé et le nom de session se déduisent de la place : rien n'est
	// retenu localement pour les afficher.
	if fiche.Label != "Groupe 01" || fiche.SessionName != "Automne 2026" {
		t.Fatalf("libellés : %+v", fiche)
	}

	var liste struct {
		Classrooms []struct {
			Scope string `json:"scope"`
		} `json:"classrooms"`
	}
	h.json(http.MethodGet, "/api/classrooms", nil, &liste)
	if len(liste.Classrooms) != 1 || liste.Classrooms[0].Scope != place {
		t.Fatalf("liste des groupes : %+v", liste)
	}
}

func TestSessionsListeesDeLaPlusRecenteALaPlusAncienne(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	state.AddRepo("acme", "h27.5n6.01.tp1.emilie-cote", true)
	state.AddRepo("acme", "h26.4w6.01.tp1.emilie-cote", true)
	h := nouveau(t, state)

	var liste struct {
		Sessions []struct {
			Short string `json:"short"`
			Name  string `json:"name"`
		} `json:"sessions"`
	}
	h.json(http.MethodGet, "/api/classrooms", nil, &liste)
	// L'ordre remonte le calendrier, il ne suit pas l'alphabet : « h27 » vient
	// avant « a26 », qui vient avant « h26 ».
	rendu := make([]string, 0, len(liste.Sessions))
	for _, session := range liste.Sessions {
		rendu = append(rendu, session.Short)
	}
	if strings.Join(rendu, " ") != "h27 a26 h26" {
		t.Fatalf("sessions : %+v", liste.Sessions)
	}
}

func TestGroupeVisibleSansAvoirEteDeclare(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	h := nouveau(t, state)

	// Rien n'a été déclaré : le groupe existe parce que ses dépôts existent.
	var liste struct {
		Classrooms []struct {
			Scope       string `json:"scope"`
			Label       string `json:"label"`
			SessionName string `json:"session_name"`
			Known       bool   `json:"known"`
			Assignments []struct {
				Name  string `json:"name"`
				Repos int    `json:"repos"`
			} `json:"assignments"`
		} `json:"classrooms"`
		Sessions []struct {
			Short string `json:"short"`
			Name  string `json:"name"`
		} `json:"sessions"`
	}
	h.json(http.MethodGet, "/api/classrooms", nil, &liste)
	if len(liste.Classrooms) != 1 {
		t.Fatalf("groupes : %+v", liste.Classrooms)
	}
	trouve := liste.Classrooms[0]
	if trouve.Scope != "a26.5n6.01" || trouve.Known ||
		len(trouve.Assignments) != 1 || trouve.Assignments[0].Repos != 2 {
		t.Fatalf("groupe lu des dépôts : %+v", trouve)
	}
	if len(liste.Sessions) != 1 || liste.Sessions[0].Name != "Automne 2026" {
		t.Fatalf("sessions : %+v", liste.Sessions)
	}

	// Et il s'ouvre par sa place, sans avoir été déclaré.
	var fiche struct {
		Label string `json:"label"`
		Known bool   `json:"known"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01", nil, &fiche)
	if fiche.Label != "Groupe 01" || fiche.Known {
		t.Fatalf("groupe ouvert : %+v", fiche)
	}
}

func TestPlusieursGroupesDansUnCours(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	state.AddRepo("acme", "a26.5n6.02.tp1.emilie-cote", true)
	h := nouveau(t, state)

	premier := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")
	second := h.groupe("a26", "5n6", "02", "Émilie Côté", "emilie-cote")

	for _, cas := range []struct {
		id      string
		attendu string
	}{{premier, "a26.5n6.01.tp1"}, {second, "a26.5n6.02.tp1"}} {
		var fiche struct {
			Assignments []struct {
				ID       string `json:"id"`
				Repos    int    `json:"repos"`
				Students int    `json:"students"`
			} `json:"assignments"`
		}
		h.json(http.MethodGet, "/api/classrooms/"+cas.id, nil, &fiche)
		if len(fiche.Assignments) != 1 || fiche.Assignments[0].ID != cas.attendu {
			t.Fatalf("travaux du groupe : %+v", fiche.Assignments)
		}
		if fiche.Assignments[0].Repos != 1 || fiche.Assignments[0].Students != 1 {
			t.Fatalf("comptage : %+v", fiche.Assignments[0])
		}
	}
}

func TestApercuAvantDistribution(t *testing.T) {
	h := nouveau(t, nil)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote")

	var apercu struct {
		Assignment string `json:"assignment"`
		ShortName  string `json:"short_name"`
		Items      []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+id+"/assignments/preview",
		map[string]any{"name": "tp1"}, &apercu)

	if apercu.Assignment != "a26.5n6.01.tp1" || apercu.ShortName != "tp1" {
		t.Fatalf("aperçu : %+v", apercu)
	}
	// Le nom du dépôt porte le nom de l'étudiant, plus son compte.
	noms := make([]string, 0, len(apercu.Items))
	for _, item := range apercu.Items {
		noms = append(noms, item.Name)
	}
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.emilie-cote,a26.5n6.01.tp1.jean-luc-picard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts prévus : %v", noms)
	}
}

func TestNomCompletManquantBloqueLaDistribution(t *testing.T) {
	h := nouveau(t, nil)
	// « aminata-d » n'a pas de nom complet : son dépôt ne peut pas être nommé.
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard", "", "aminata-d")

	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+id+"/assignments/preview",
		map[string]any{"name": "tp1"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400", reponse.StatusCode)
	}
	if !strings.Contains(string(contenu), "aminata-d") {
		t.Fatalf("message peu explicite : %s", contenu)
	}
}

func TestHomonymesRefusesAvantTouteEcriture(t *testing.T) {
	h := nouveau(t, nil)
	id := h.groupe("a26", "5n6", "01",
		"Jean Tremblay", "jtremblay", "Jean Tremblay", "jean-t")

	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+id+"/assignments",
		map[string]any{"name": "tp1"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400", reponse.StatusCode)
	}
	if !strings.Contains(string(contenu), "Jean Tremblay") {
		t.Fatalf("message : %s", contenu)
	}
	if noms := h.depots(); len(noms) != 0 {
		t.Fatalf("des dépôts ont été créés : %v", noms)
	}
}

// ------------------------------------------------------------------ écriture

func TestDistributionAToutLeGroupe(t *testing.T) {
	h := nouveau(t, nil)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/assignments",
		map[string]any{"name": "tp1"})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["created"] != float64(2) {
		t.Fatalf("%v dépôt(s) créé(s), attendu 2", resultat["created"])
	}
	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.emilie-cote,a26.5n6.01.tp1.jean-luc-picard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts créés : %v", noms)
	}
}

func TestSimulationNeCreeRien(t *testing.T) {
	h := nouveau(t, nil)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/assignments",
		map[string]any{"name": "tp1", "dry_run": true})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v", bilan["status"])
	}
	if noms := h.depots(); len(noms) != 0 {
		t.Fatalf("la simulation a créé %v", noms)
	}
}

func TestRedistributionEcarteLesDejaServis(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/assignments",
		map[string]any{"name": "tp1"})
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["created"] != float64(1) {
		t.Fatalf("%v dépôt(s) créé(s), attendu 1", resultat["created"])
	}
	ecartes, _ := resultat["skipped"].([]any)
	if len(ecartes) != 1 {
		t.Fatalf("%d étudiant(s) écarté(s), attendu 1", len(ecartes))
	}

	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+id+"/assignments",
		map[string]any{"name": "tp1"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400 — %s", reponse.StatusCode, contenu)
	}
}

func TestDistributionRestreinteAQuelquesEtudiants(t *testing.T) {
	h := nouveau(t, nil)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/assignments",
		map[string]any{"name": "rattrapage", "usernames": []string{"emilie-cote"}})
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["created"] != float64(1) {
		t.Fatalf("%v dépôt(s) créé(s), attendu 1", resultat["created"])
	}
	if noms := h.depots(); len(noms) != 1 ||
		noms[0] != "a26.5n6.01.rattrapage.emilie-cote" {
		t.Fatalf("dépôts créés : %v", noms)
	}
}

func TestSelectionVideNeSertPersonne(t *testing.T) {
	h := nouveau(t, nil)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote")

	// Une sélection explicitement vide ne doit pas être lue comme « tout le
	// groupe » : décocher tout le monde ne crée rien.
	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+id+"/assignments",
		map[string]any{"name": "tp1", "usernames": []string{}})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400 — %s", reponse.StatusCode, contenu)
	}
	if noms := h.depots(); len(noms) != 0 {
		t.Fatalf("des dépôts ont été créés : %v", noms)
	}
}

func TestDetailDUnTravail(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.jean-luc-picard", "a26.5n6.01.tp1.visiteur-inconnu",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")

	var detail struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Repos []struct {
			Name     string `json:"name"`
			Student  string `json:"student"`
			FullName string `json:"full_name"`
			Username string `json:"username"`
		} `json:"repos"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+id+"/assignments/tp1", nil, &detail)
	if detail.ID != "a26.5n6.01.tp1" || len(detail.Repos) != 2 {
		t.Fatalf("détail : %+v", detail)
	}
	// Le dépôt d'un étudiant inscrit porte son nom et son compte ; celui d'un
	// inconnu reste visible, sans compte.
	parNom := map[string]string{}
	for _, repo := range detail.Repos {
		parNom[repo.Student] = repo.Username
	}
	if parNom["jean-luc-picard"] != "jlpicard" {
		t.Fatalf("dépôts : %+v", detail.Repos)
	}
	if parNom["visiteur-inconnu"] != "" {
		t.Fatalf("un dépôt hors liste a été rattaché : %+v", detail.Repos)
	}
}

// La liste d'un travail se filtre et se trie comme celle des étudiants, et par
// le même paquet : ce sont les mêmes lignes, un dépôt par personne.
func TestListeDUnTravailSeFiltreEtSeTrie(t *testing.T) {
	state := fakegh.NewState()
	envois := map[string]string{
		"a26.5n6.01.tp1.jean-luc-picard":  "2026-10-15T10:00:00Z",
		"a26.5n6.01.tp1.emilie-cote":      "2026-09-20T10:00:00Z",
		"a26.5n6.01.tp1.visiteur-inconnu": "",
	}
	for nom, envoi := range envois {
		state.AddRepo("acme", nom, true).PushedAt = envoi
	}
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01",
		"Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote")

	depots := func(requete string) string {
		var reponse struct {
			Repos []struct {
				Student string `json:"student"`
			} `json:"repos"`
			Names []string `json:"names"`
			Total int      `json:"total"`
			Shown int      `json:"shown"`
		}
		h.json(http.MethodGet, "/api/classrooms/"+id+"/assignments/tp1"+requete, nil, &reponse)
		noms := make([]string, 0, len(reponse.Repos))
		for _, repo := range reponse.Repos {
			noms = append(noms, repo.Student)
		}
		// « names » nomme tous les dépôts du travail : ce que le filtre écarte
		// de l'écran n'en sort pas pour autant.
		if reponse.Shown != len(noms) || reponse.Total != 3 || len(reponse.Names) != 3 {
			t.Fatalf("%s : %d affiché(s) sur %d, %d nommé(s)",
				requete, reponse.Shown, reponse.Total, len(reponse.Names))
		}
		return strings.Join(noms, ",")
	}

	// Un dépôt hors liste n'a pas de nom complet : il se range avant le reste.
	if ordre := depots(""); ordre != "visiteur-inconnu,emilie-cote,jean-luc-picard" {
		t.Fatalf("par nom : %s", ordre)
	}
	if ordre := depots("?sort=envoi&desc=1"); ordre != "jean-luc-picard,emilie-cote,visiteur-inconnu" {
		t.Fatalf("par envoi : %s", ordre)
	}
	// L'accent ne se met pas en travers d'une recherche tapée sans lui.
	if ordre := depots("?q=cote"); ordre != "emilie-cote" {
		t.Fatalf("recherche : %s", ordre)
	}
	if ordre := depots("?after=2026-10-01"); ordre != "jean-luc-picard" {
		t.Fatalf("après le 1er octobre : %s", ordre)
	}
	if ordre := depots("?activity=muet"); ordre != "visiteur-inconnu" {
		t.Fatalf("jamais d'envoi : %s", ordre)
	}

	// Un critère que le serveur ne peut pas appliquer est refusé, plutôt que
	// silencieusement ignoré.
	reponse, contenu := h.requete(http.MethodGet,
		"/api/classrooms/"+id+"/assignments/tp1?after=hier", nil)
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
}

func TestEtudiantsDuGroupeCroisesAvecLesTravaux(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.jean-luc-picard", "a26.5n6.01.tp1.emilie-cote",
		"a26.5n6.01.travail-session.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01",
		"Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote", "Aminata Diallo", "aminata-d")

	var reponse struct {
		Students []struct {
			Username    string `json:"username"`
			Assignments []struct {
				Name string `json:"name"`
			} `json:"assignments"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+id+"/students", nil, &reponse)
	if len(reponse.Students) != 3 {
		t.Fatalf("%d étudiant(s), attendu 3", len(reponse.Students))
	}
	compte := map[string]int{}
	for _, etudiant := range reponse.Students {
		compte[etudiant.Username] = len(etudiant.Assignments)
	}
	if compte["jlpicard"] != 2 || compte["emilie-cote"] != 1 || compte["aminata-d"] != 0 {
		t.Fatalf("travaux par étudiant : %v", compte)
	}
}

// La liste s'ordonne et se réduit côté serveur : c'est là que « dernier envoi
// avant le 1er octobre » a un sens, et il doit être le même partout.
func TestListeDesEtudiantsSeFiltreEtSeTrie(t *testing.T) {
	state := fakegh.NewState()
	envois := map[string]string{
		"a26.5n6.01.tp1.jean-luc-picard": "2026-10-15T10:00:00Z",
		"a26.5n6.01.tp1.emilie-cote":     "2026-09-20T10:00:00Z",
	}
	for nom, envoi := range envois {
		state.AddRepo("acme", nom, true).PushedAt = envoi
	}
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01",
		"Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote",
		"Aminata Diallo", "aminata-d")

	comptes := func(requete string) string {
		var reponse struct {
			Students []struct {
				Username string `json:"username"`
				PushedAt string `json:"pushed_at"`
			} `json:"students"`
			Total        int `json:"total"`
			Shown        int `json:"shown"`
			MissingNames int `json:"missing_names"`
		}
		h.json(http.MethodGet, "/api/classrooms/"+id+"/students"+requete, nil, &reponse)
		noms := make([]string, 0, len(reponse.Students))
		for _, etudiant := range reponse.Students {
			noms = append(noms, etudiant.Username)
		}
		if reponse.Shown != len(noms) || reponse.Total != 3 {
			t.Fatalf("%s : %d affiché(s) sur %d", requete, reponse.Shown, reponse.Total)
		}
		return strings.Join(noms, ",")
	}

	if ordre := comptes("?sort=envoi&desc=1"); ordre != "jlpicard,emilie-cote,aminata-d" {
		t.Fatalf("par envoi : %s", ordre)
	}
	if ordre := comptes("?sort=compte"); ordre != "aminata-d,emilie-cote,jlpicard" {
		t.Fatalf("par compte : %s", ordre)
	}
	// L'accent ne se met pas en travers d'une recherche tapée sans lui.
	if ordre := comptes("?q=cote"); ordre != "emilie-cote" {
		t.Fatalf("recherche : %s", ordre)
	}
	if ordre := comptes("?after=2026-10-01"); ordre != "jlpicard" {
		t.Fatalf("après le 1er octobre : %s", ordre)
	}
	if ordre := comptes("?activity=sans"); ordre != "aminata-d" {
		t.Fatalf("sans dépôt : %s", ordre)
	}

	// Un critère que le serveur ne peut pas appliquer est refusé, plutôt que
	// silencieusement ignoré.
	reponse, contenu := h.requete(http.MethodGet,
		"/api/classrooms/"+id+"/students?after=hier", nil)
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
}

// Une inscription tardive n'a pas à passer par le fichier : la liste s'augmente
// d'une personne sans que le reste bouge.
func TestEtudiantAjouteALaListe(t *testing.T) {
	h := nouveau(t, nil)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")

	var ajout struct {
		Student struct {
			FullName string `json:"full_name"`
			Username string `json:"username"`
		} `json:"student"`
		Classroom struct {
			Students []struct {
				Username string `json:"username"`
			} `json:"students"`
		} `json:"classroom"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+id+"/students/add",
		map[string]string{"full_name": " Émilie  Côté ", "username": "@emilie-cote"}, &ajout)
	// Le nom est mis en forme et le « @ » collé du profil est accepté.
	if ajout.Student.FullName != "Émilie Côté" || ajout.Student.Username != "emilie-cote" {
		t.Fatalf("personne : %+v", ajout.Student)
	}
	if len(ajout.Classroom.Students) != 2 {
		t.Fatalf("liste : %+v", ajout.Classroom.Students)
	}

	// Ajouter deux fois le même compte ne fait rien : le taire laisserait
	// croire le contraire.
	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+id+"/students/add",
		map[string]string{"full_name": "Émilie Côté", "username": "EMILIE-COTE"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "est déjà dans") {
		t.Fatalf("message : %s", contenu)
	}

	// Un compte qui n'existe pas sur GitHub ne sert à rien dans une liste :
	// aucun dépôt ne pourra jamais lui être remis.
	reponse, contenu = h.requete(http.MethodPost, "/api/classrooms/"+id+"/students/add",
		map[string]string{"full_name": "Personne", "username": "fantome-introuvable"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "n'existe pas sur GitHub") {
		t.Fatalf("message : %s", contenu)
	}

	// Et un compte vide non plus.
	reponse, _ = h.requete(http.MethodPost, "/api/classrooms/"+id+"/students/add",
		map[string]string{"full_name": "Sans compte", "username": ""})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400", reponse.StatusCode)
	}
}

// Une arrivée en cours de session ne devrait pas obliger à revenir distribuer
// travail par travail : les travaux cochés lui sont remis dans la foulée, aux
// réglages que le groupe retient.
func TestEtudiantAjouteRecoitLesTravauxCoches(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.jean-luc-picard", "a26.5n6.01.tp2.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/students/add",
		map[string]any{
			"full_name": "Émilie Côté", "username": "emilie-cote",
			"assignments": []string{"tp1", "tp2"},
		})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["created"] != float64(2) || resultat["failed"] != float64(0) {
		t.Fatalf("bilan : %+v", resultat)
	}

	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.emilie-cote,a26.5n6.01.tp1.jean-luc-picard," +
		"a26.5n6.01.tp2.emilie-cote,a26.5n6.01.tp2.jean-luc-picard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}

	// Sans nom complet, aucun dépôt ne peut être nommé : l'ajout est refusé
	// avant d'inscrire qui que ce soit.
	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+id+"/students/add",
		map[string]any{"username": "aminata-d", "assignments": []string{"tp1"}})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "nom complet") {
		t.Fatalf("message : %s", contenu)
	}
	var liste struct {
		Students []struct {
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+id, nil, &liste)
	if len(liste.Students) != 2 {
		t.Fatalf("personne ne devait être inscrit à moitié : %+v", liste.Students)
	}
}

// Une faute dans un prénom ne devrait pas obliger à remplacer la liste de tout
// le monde : la fiche se corrige seule, et ses dépôts la suivent si on le
// demande.
func TestEtudiantRenommeSeulEtSesDepotsAvecLui(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp2.emilie-cote",
		"a26.5n6.01.tp1.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/students/rename",
		map[string]any{
			"username": "emilie-cote", "full_name": "Émilie Côté-Tremblay", "repos": true,
		})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["renamed"] != float64(2) || resultat["failed"] != float64(0) {
		t.Fatalf("bilan : %+v", resultat)
	}

	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.emilie-cote-tremblay,a26.5n6.01.tp1.jean-luc-picard," +
		"a26.5n6.01.tp2.emilie-cote-tremblay"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}

	// La liste n'a pas grossi : la fiche a été remplacée, pas doublée.
	var liste struct {
		Students []struct {
			FullName string `json:"full_name"`
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+id, nil, &liste)
	if len(liste.Students) != 2 {
		t.Fatalf("liste : %+v", liste.Students)
	}
	for _, personne := range liste.Students {
		if personne.Username == "emilie-cote" && personne.FullName != "Émilie Côté-Tremblay" {
			t.Fatalf("fiche non corrigée : %+v", personne)
		}
	}
}

// Le compte n'entre pas dans le nom des dépôts : le changer ne touche qu'à la
// liste, et un compte introuvable sur GitHub est refusé comme à l'inscription.
func TestCompteRenommeSansToucherAuxDepots(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01", "Émilie Côté", "emilie-cote")

	var bilan struct {
		Student struct {
			FullName string `json:"full_name"`
			Username string `json:"username"`
		} `json:"student"`
		Renamed int `json:"renamed"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+id+"/students/rename", map[string]any{
		"username": "emilie-cote", "full_name": "Émilie Côté",
		"new_username": "aminata-d", "repos": true,
	}, &bilan)
	// Le nom n'a pas changé : il n'y avait rien à renommer, et le serveur a
	// répondu sans passer par un travail de fond.
	if bilan.Student.Username != "aminata-d" || bilan.Renamed != 0 {
		t.Fatalf("bilan : %+v", bilan)
	}
	if noms := h.depots(); strings.Join(noms, ",") != "a26.5n6.01.tp1.emilie-cote" {
		t.Fatalf("dépôts : %v", noms)
	}

	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+id+"/students/rename",
		map[string]any{"username": "aminata-d", "full_name": "Émilie Côté",
			"new_username": "fantome-introuvable"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "n'existe pas sur GitHub") {
		t.Fatalf("message : %s", contenu)
	}
}

// Le plan se compose en entier avant la première écriture : un nom déjà pris
// refuse le renommage au lieu de l'interrompre à mi-chemin.
func TestRenommageRefuseAvantDEcrireQuoiQueCeSoit(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp2.emilie-cote",
		"a26.5n6.01.tp2.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard")

	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+id+"/students/rename",
		map[string]any{
			"username": "emilie-cote", "full_name": "Jean-Luc Picard", "repos": true,
		})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "existe déjà") {
		t.Fatalf("message : %s", contenu)
	}

	// Ni les dépôts ni la liste n'ont bougé.
	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.emilie-cote,a26.5n6.01.tp2.emilie-cote," +
		"a26.5n6.01.tp2.jean-luc-picard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}
	var liste struct {
		Students []struct {
			FullName string `json:"full_name"`
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+id, nil, &liste)
	for _, personne := range liste.Students {
		if personne.Username == "emilie-cote" && personne.FullName != "Émilie Côté" {
			t.Fatalf("la fiche a été enregistrée malgré le refus : %+v", personne)
		}
	}
}

func TestSuppressionExigeLeNomExact(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-jlpicard", true)
	h := nouveau(t, state)

	reponse, _ := h.requete(http.MethodDelete, "/api/orgs/acme/repos/tp1-jlpicard",
		map[string]string{"confirm": "tp1"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400", reponse.StatusCode)
	}
	if len(h.depots()) != 1 {
		t.Fatal("le dépôt a été supprimé malgré une confirmation incorrecte")
	}

	h.json(http.MethodDelete, "/api/orgs/acme/repos/tp1-jlpicard",
		map[string]string{"confirm": "tp1-jlpicard"}, nil)
	if noms := h.depots(); len(noms) != 0 {
		t.Fatalf("dépôts restants : %v", noms)
	}
}

func TestAccesEtCollaborateurs(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-jlpicard", true)
	h := nouveau(t, state)

	h.json(http.MethodPost, "/api/orgs/acme/repos/tp1-jlpicard/collaborators",
		map[string]string{"username": "jlpicard", "permission": "push"}, nil)

	var acces struct {
		Collaborators []string `json:"collaborators"`
		Invitations   []struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"invitations"`
	}
	h.json(http.MethodGet, "/api/orgs/acme/repos/tp1-jlpicard/access", nil, &acces)
	if len(acces.Invitations) != 1 || acces.Invitations[0].Login != "jlpicard" {
		t.Fatalf("invitations : %+v", acces.Invitations)
	}

	h.json(http.MethodDelete,
		"/api/orgs/acme/repos/tp1-jlpicard/invitations/"+itoa(acces.Invitations[0].ID), nil, nil)
	h.json(http.MethodGet, "/api/orgs/acme/repos/tp1-jlpicard/access", nil, &acces)
	if len(acces.Invitations) != 0 {
		t.Fatalf("invitation encore présente : %+v", acces.Invitations)
	}
}

func TestCompteInexistantRefuse(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "tp1-jlpicard", true)
	h := nouveau(t, state)

	reponse, contenu := h.requete(http.MethodPost, "/api/orgs/acme/repos/tp1-jlpicard/collaborators",
		map[string]string{"username": "fantome", "permission": "push"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400", reponse.StatusCode)
	}
	if !strings.Contains(string(contenu), "n'existe pas") {
		t.Fatalf("message : %s", contenu)
	}
}

// ------------------------------------------------------------------- travaux

func TestFluxDEvenementsSuitUnTravail(t *testing.T) {
	h := nouveau(t, nil)
	var fiche struct {
		ID string `json:"id"`
	}
	h.json(http.MethodPost, "/api/accounts/verify", map[string]any{
		"people": []map[string]string{
			{"full_name": "Jean-Luc Picard", "username": "jlpicard"},
			{"full_name": "Fantôme", "username": "fantome"},
		},
	}, &fiche)

	requete, _ := http.NewRequest(http.MethodGet, h.Base+"/api/jobs/"+fiche.ID+"/events", nil)
	reponse, err := h.Client.Do(requete)
	if err != nil {
		t.Fatalf("flux : %v", err)
	}
	defer reponse.Body.Close()
	if type_ := reponse.Header.Get("Content-Type"); !strings.HasPrefix(type_, "text/event-stream") {
		t.Fatalf("type de contenu %q", type_)
	}

	var natures []string
	lecteur := bufio.NewScanner(reponse.Body)
	for lecteur.Scan() {
		ligne := lecteur.Text()
		if !strings.HasPrefix(ligne, "data: ") {
			continue
		}
		var evenement struct {
			Kind string `json:"kind"`
			Data struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(ligne, "data: ")), &evenement); err != nil {
			t.Fatalf("événement illisible : %v", err)
		}
		natures = append(natures, evenement.Kind)
		if evenement.Kind == "fin" {
			if evenement.Data.Status != "terminé" {
				t.Fatalf("issue %q", evenement.Data.Status)
			}
			break
		}
	}
	if len(natures) == 0 || natures[len(natures)-1] != "fin" {
		t.Fatalf("événements reçus : %v", natures)
	}
	if !contient(natures, "avancement") || !contient(natures, "avertissement") {
		t.Fatalf("le flux ne rapporte pas le détail : %v", natures)
	}
}

func TestFermetureDemandeeParLInterface(t *testing.T) {
	h := nouveau(t, nil)
	h.json(http.MethodPost, "/api/quit", nil, nil)

	// Le serveur se retire : les appels suivants ne joignent plus personne.
	limite := time.Now().Add(5 * time.Second)
	for time.Now().Before(limite) {
		requete, _ := http.NewRequest(http.MethodGet, h.Base+"/api/context", nil)
		if _, err := h.Client.Do(requete); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("le serveur répond encore après la demande de fermeture")
}

func contient(valeurs []string, cherche string) bool {
	for _, valeur := range valeurs {
		if valeur == cherche {
			return true
		}
	}
	return false
}

func itoa(valeur int64) string { return strconv.FormatInt(valeur, 10) }

// ------------------------------------------------------------------ réglages

func TestReglagesMemorises(t *testing.T) {
	h := nouveau(t, nil)
	reglages := config.Default()
	reglages.Org, reglages.Assignment = "acme", "tp1"

	var bilan struct {
		Saved bool   `json:"saved"`
		Path  string `json:"path"`
	}
	h.json(http.MethodPut, "/api/settings", reglages, &bilan)
	if !bilan.Saved {
		t.Fatal("réglages non enregistrés")
	}
	contenu, err := os.ReadFile(bilan.Path)
	if err != nil {
		t.Fatalf("lecture des réglages : %v", err)
	}
	if !strings.Contains(string(contenu), `"assignment": "tp1"`) {
		t.Fatalf("réglages écrits : %s", contenu)
	}
}

// La destination proposée voyage à côté des réglages : renvoyés tels que la page
// les a reçus, ceux-ci ne doivent pas la figer dans config.json.
func TestDestinationProposeeHorsDesReglages(t *testing.T) {
	h := nouveau(t, nil)
	var contexte struct {
		Settings    config.Settings `json:"settings"`
		CloneParent string          `json:"clone_parent"`
	}
	h.json(http.MethodGet, "/api/context", nil, &contexte)
	if contexte.CloneParent == "" {
		t.Fatal("aucune destination proposée")
	}
	if contexte.Settings.CloneDir != "" {
		t.Fatalf("la proposition a pris la place du réglage : %q", contexte.Settings.CloneDir)
	}

	var bilan struct {
		Path string `json:"path"`
	}
	h.json(http.MethodPut, "/api/settings", contexte.Settings, &bilan)
	contenu, err := os.ReadFile(bilan.Path)
	if err != nil {
		t.Fatalf("lecture des réglages : %v", err)
	}
	var relus config.Settings
	if err := json.Unmarshal(contenu, &relus); err != nil {
		t.Fatalf("réglages illisibles : %v", err)
	}
	if relus.CloneDir != "" {
		t.Fatalf("destination figée dans les réglages : %q", relus.CloneDir)
	}
}

func TestReglagesInvalidesRefuses(t *testing.T) {
	h := nouveau(t, nil)
	reglages := config.Default()
	reglages.Visibility = "secret"

	reponse, contenu := h.requete(http.MethodPut, "/api/settings", reglages)
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400", reponse.StatusCode)
	}
	if !strings.Contains(string(contenu), "secret") {
		t.Fatalf("message : %s", contenu)
	}
}

func TestNomsCompletsRetrouves(t *testing.T) {
	h := nouveau(t, nil)
	// Les comptes sont connus, les noms complets non : c'est GitHub qui les donne.
	id := h.sansNoms("a26", "5n6", "01", "jlpicard", "emilie-cote")

	var avant struct {
		Students []struct {
			FullName string `json:"full_name"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+id, nil, &avant)

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/students/names", nil)
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["resolved"] != float64(2) {
		t.Fatalf("%v nom(s) retrouvé(s), attendu 2", resultat["resolved"])
	}

	var apres struct {
		Students []struct {
			Username string `json:"username"`
			FullName string `json:"full_name"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+id, nil, &apres)
	noms := map[string]string{}
	for _, student := range apres.Students {
		noms[student.Username] = student.FullName
	}
	if noms["jlpicard"] != "Jean-Luc Picard" {
		t.Fatalf("noms retenus : %v", noms)
	}
}

func TestPageServie(t *testing.T) {
	h := nouveau(t, nil)
	reponse, contenu := h.requete(http.MethodGet, "/", nil)
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut %d", reponse.StatusCode)
	}
	if !strings.Contains(string(contenu), brand.Name) {
		t.Fatalf("page inattendue : %.100s", contenu)
	}
	// Une adresse inconnue rend la page : l'interface se recharge sans erreur.
	reponse, _ = h.requete(http.MethodGet, "/groupes", nil)
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut %d pour une adresse inconnue", reponse.StatusCode)
	}
}

// ----------------------------------------------------------------- migration

func TestMigrationRenommeLesDepots(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"h25.5n6.02.travailsession.jlpicard", "h25.5n6.02.travailsession.emilie-cote",
		"h25.5n6.02.tp1.jlpicard",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	id := h.sansNoms("h25", "5n6", "02", "jlpicard", "emilie-cote")

	// Les noms complets sont nécessaires : c'est eux qui nomment les dépôts.
	h.travail(http.MethodPost, "/api/classrooms/"+id+"/students/names", nil)

	var apercu struct {
		Course  string `json:"course"`
		Group   string `json:"group"`
		Ready   int    `json:"ready"`
		Blocked int    `json:"blocked"`
		Rows    []struct {
			Repo    string `json:"repo"`
			Target  string `json:"target"`
			Problem string `json:"problem"`
		} `json:"rows"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+id+"/migration/preview",
		map[string]any{"session": "a26", "course": "5n6", "group": "01"}, &apercu)
	if apercu.Ready != 3 || apercu.Blocked != 0 {
		t.Fatalf("aperçu : %+v", apercu)
	}
	cibles := map[string]string{}
	for _, ligne := range apercu.Rows {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["h25.5n6.02.tp1.jlpicard"] != "a26.5n6.01.tp1.jean-luc-picard" {
		t.Fatalf("cibles : %v", cibles)
	}

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/migration/apply",
		map[string]any{"session": "a26", "course": "5n6", "group": "01"})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["renamed"] != float64(3) || resultat["switched"] != true {
		t.Fatalf("bilan : %+v", resultat)
	}

	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.jean-luc-picard," +
		"a26.5n6.01.travailsession.emilie-cote,a26.5n6.01.travailsession.jean-luc-picard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts après migration : %v", noms)
	}

	// Le groupe a changé de place : c'est à sa nouvelle place qu'il se
	// retrouve, et ce qu'on retenait de lui l'y a suivi.
	var fiche struct {
		Session     string     `json:"session"`
		Course      string     `json:"course"`
		Group       string     `json:"group"`
		Prefix      string     `json:"prefix"`
		Known       bool       `json:"known"`
		Students    []struct{} `json:"students"`
		Assignments []struct {
			ID string `json:"id"`
		} `json:"assignments"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01?refresh=1", nil, &fiche)
	if !fiche.Known || len(fiche.Students) != 2 {
		t.Fatalf("liste perdue en chemin : %+v", fiche)
	}
	if ancien, encore := h.requete(http.MethodGet, "/api/classrooms/"+id, nil); encore != nil &&
		ancien.StatusCode == http.StatusOK && strings.Contains(string(encore), `"known":true`) {
		t.Fatalf("l'ancienne place retient encore quelque chose : %s", encore)
	}
	if fiche.Session != "a26" || fiche.Course != "5n6" || fiche.Group != "01" || fiche.Prefix != "" {
		t.Fatalf("groupe après migration : %+v", fiche)
	}
	if len(fiche.Assignments) != 2 {
		t.Fatalf("travaux après migration : %+v", fiche.Assignments)
	}
}

func TestMigrationRefuseTantQuUnDepotEstBloque(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "h25.5n6.02.tp1.jlpicard", true)
	// « visiteur » n'est pas dans la liste du groupe : son dépôt ne peut pas
	// être renommé sans savoir de qui il s'agit.
	state.AddRepo("acme", "h25.5n6.02.tp1.visiteur", true)
	h := nouveau(t, state)
	id := h.sansNoms("h25", "5n6", "02", "jlpicard")
	h.travail(http.MethodPost, "/api/classrooms/"+id+"/students/names", nil)

	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+id+"/migration/apply",
		map[string]any{"session": "a26", "course": "5n6", "group": "01"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400 — %s", reponse.StatusCode, contenu)
	}
	if noms := h.depots(); len(noms) != 2 ||
		!strings.HasPrefix(noms[0], "h25.5n6.02.") {
		t.Fatalf("des dépôts ont été renommés : %v", noms)
	}

	// En acceptant de les laisser en place, la migration passe — mais le
	// groupe ne bascule pas tant qu'un dépôt reste en arrière : basculer, ce
	// serait cesser de le voir.
	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/migration/apply",
		map[string]any{"session": "a26", "course": "5n6", "group": "01", "skip_blocked": true})
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["renamed"] != float64(1) || resultat["skipped"] != float64(1) ||
		resultat["switched"] != false {
		t.Fatalf("bilan : %+v", resultat)
	}
	noms := h.depots()
	sort.Strings(noms)
	if strings.Join(noms, ",") != "a26.5n6.01.tp1.jean-luc-picard,h25.5n6.02.tp1.visiteur" {
		t.Fatalf("dépôts : %v", noms)
	}

	// Le groupe est resté à sa place, avec sa liste : c'est de là qu'on reprend
	// la migration une fois « visiteur » identifié.
	var reste struct {
		Scope string `json:"scope"`
		Known bool   `json:"known"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+id+"?refresh=1", nil, &reste)
	if !reste.Known || reste.Scope != "h25.5n6.02" {
		t.Fatalf("groupe après migration partielle : %+v", reste)
	}
}

func TestMigrationRefuseSansNomComplet(t *testing.T) {
	state := fakegh.NewState()
	// « aminata-d » a un profil GitHub sans nom complet.
	state.AddRepo("acme", "h25.5n6.02.tp1.aminata-d", true)
	h := nouveau(t, state)
	id := h.sansNoms("h25", "5n6", "02", "aminata-d")

	var apercu struct {
		Ready   int  `json:"ready"`
		Blocked int  `json:"blocked"`
		Switch  bool `json:"switch"`
		Rows    []struct {
			Problem string `json:"problem"`
		} `json:"rows"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+id+"/migration/preview",
		map[string]any{"session": "a26", "course": "5n6", "group": "01"}, &apercu)
	if apercu.Blocked != 1 || apercu.Ready != 0 {
		t.Fatalf("aperçu : %+v", apercu)
	}
	// L'aperçu annonce déjà que le groupe ne suivra pas : c'est ce que
	// l'interface affiche quand on accepte de laisser ce dépôt en place.
	if apercu.Switch {
		t.Fatalf("le groupe basculerait malgré un dépôt bloqué : %+v", apercu)
	}
	if !strings.Contains(apercu.Rows[0].Problem, "aminata-d") {
		t.Fatalf("raison : %q", apercu.Rows[0].Problem)
	}
}
