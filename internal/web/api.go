package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/clone"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/orgs"
	"github.com/PierreOlivierBrillant/gh-milou/internal/picker"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plan"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// ------------------------------------------------------------------- contexte

// choice est une valeur proposée dans un menu de l'interface.
type choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// pathInfo décrit un fichier géré par l'outil.
type pathInfo struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// contextPayload donne à la page tout ce qu'elle doit savoir au chargement.
type contextPayload struct {
	Viewer       string          `json:"viewer"`
	Host         string          `json:"host"`
	Version      string          `json:"version"`
	Settings     config.Settings `json:"settings"`
	Token        tokenPayload    `json:"token"`
	Paths        []pathInfo      `json:"paths"`
	Permissions  []choice        `json:"permissions"`
	Placeholders []string        `json:"placeholders"`
	SaveConfig   bool            `json:"save_config"`
	Jobs         int             `json:"jobs"`
	Depth        int             `json:"depth"`
	// NativePicker nomme la fenêtre de sélection du système quand il y en a
	// une ; vide, l'interface montre son propre explorateur.
	NativePicker string `json:"native_picker"`
	// CloneParent est la destination proposée tant que Settings.CloneDir est
	// vide. Elle voyage à part : glissée dans les réglages, elle serait
	// réécrite dans config.json au premier enregistrement, comme si quelqu'un
	// l'avait choisie, et y resterait figée même sur une autre machine.
	CloneParent string `json:"clone_parent"`
}

// handleContext décrit la session en cours.
func (s *Server) handleContext(writer http.ResponseWriter, _ *http.Request) {
	permissions := make([]choice, 0, len(config.Permissions))
	for _, value := range config.Permissions {
		permissions = append(permissions, choice{Value: value, Label: config.PermissionLabels[value]})
	}
	writeJSON(writer, http.StatusOK, contextPayload{
		Viewer:       s.deps.Viewer,
		Host:         s.deps.Host,
		Version:      s.deps.Version,
		Settings:     s.Settings(),
		Token:        s.tokenState(nil),
		Paths:        s.paths(),
		Permissions:  permissions,
		Placeholders: plan.Placeholders,
		SaveConfig:   s.deps.SaveConfig,
		Jobs:         s.deps.Jobs,
		Depth:        s.deps.Depth,
		NativePicker: picker.Name(),
		CloneParent:  clone.DefaultParent(),
	})
}

// paths renvoie l'emplacement et l'état des fichiers gérés par l'outil.
func (s *Server) paths() []pathInfo {
	settingsState := "absent"
	if info, err := os.Stat(s.deps.ConfigFile); err == nil && !info.IsDir() {
		settingsState = "présent"
	}
	return []pathInfo{
		{"Réglages", s.deps.ConfigFile, settingsState},
		{"Cache", s.deps.Cache.Path(), s.deps.Cache.Describe()},
		{"Bilans", s.reportDir(), countReports(s.reportDir())},
	}
}

// handleSaveSettings retient les réglages et les écrit sur le disque.
func (s *Server) handleSaveSettings(writer http.ResponseWriter, request *http.Request) {
	var incoming config.Settings
	if err := decode(request, &incoming); err != nil {
		fail(writer, err)
		return
	}
	incoming, err := sanitize(incoming)
	if err != nil {
		fail(writer, err)
		return
	}
	s.mutex.Lock()
	s.settings = incoming
	s.mutex.Unlock()

	if !s.deps.SaveConfig {
		writeJSON(writer, http.StatusOK, map[string]any{"saved": false})
		return
	}
	if err := incoming.Save(s.deps.ConfigFile); err != nil {
		fail(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"saved": true, "path": s.deps.ConfigFile})
}

// handleClearCache vide le cache local et oublie l'inventaire en mémoire.
func (s *Server) handleClearCache(writer http.ResponseWriter, _ *http.Request) {
	removed := s.deps.Cache.Clear()
	s.mutex.Lock()
	s.inventory = map[string][]groups.RepoInfo{}
	s.squads = map[string][]teams.Info{}
	s.squadNames = map[string][]teams.Info{}
	s.resolvers = map[string]*identity.Resolver{}
	s.mutex.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{
		"removed": removed,
		"paths":   s.paths(),
	})
}

// -------------------------------------------------------------- organisations

// handleOrgs liste les organisations du compte connecté.
func (s *Server) handleOrgs(writer http.ResponseWriter, _ *http.Request) {
	accesses, err := orgs.List(s.deps.Client, s.deps.Cache, s.deps.Viewer, s.deps.Jobs, nil)
	if err != nil {
		// Sans « read:org », GitHub ne révèle pas les adhésions : l'interface
		// se rabat alors sur la saisie libre d'un nom.
		writeJSON(writer, http.StatusOK, map[string]any{
			"orgs":   []orgs.Access{},
			"notice": "Liste des organisations indisponible (" + err.Error() + "). Saisissez un nom.",
		})
		return
	}
	labelled := make([]map[string]any, 0, len(accesses))
	for _, access := range accesses {
		labelled = append(labelled, map[string]any{
			"login": access.Login, "name": access.Name, "role": access.Role,
			"can_create": access.CanCreate, "known": access.Known, "label": access.Label(),
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"orgs": labelled})
}

// handleOrg vérifie une organisation et signale un rôle insuffisant.
func (s *Server) handleOrg(writer http.ResponseWriter, request *http.Request) {
	org, err := valid.Login(request.PathValue("org"), "Organisation")
	if err != nil {
		fail(writer, err)
		return
	}
	data, err := s.deps.Client.GetOrg(org)
	if err != nil {
		fail(writer, err)
		return
	}
	if data == nil {
		fail(writer, valid.Errorf("L'organisation « %s » est introuvable ou invisible pour @%s.",
			org, s.deps.Viewer))
		return
	}
	name := data.Name
	if name == "" {
		name = org
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"login": org, "name": name, "warning": s.roleWarning(org),
	})
}

// roleWarning dit, sans bloquer, si la création de dépôts risque d'échouer.
func (s *Server) roleWarning(org string) string {
	accesses, err := orgs.List(s.deps.Client, s.deps.Cache, s.deps.Viewer, s.deps.Jobs, nil)
	if err == nil {
		if access, found := orgs.Find(accesses, org); found {
			switch {
			case access.Role == "admin" || access.CanCreate:
				return ""
			case access.Known:
				return fmt.Sprintf("Vous êtes « %s » et la création de dépôts est réservée "+
					"aux propriétaires de cette organisation.", access.Role)
			default:
				return fmt.Sprintf("Vous êtes « %s » : la création de dépôts doit être autorisée "+
					"aux membres dans les réglages de l'organisation.", access.Role)
			}
		}
	}
	role, err := s.deps.Client.OrgMembership(org, s.deps.Viewer)
	switch {
	case err != nil:
		return ""
	case role == "":
		return "Rôle indéterminé dans l'organisation (portée « read:org » absente ?) : " +
			"la création peut échouer."
	case role == "admin":
		return ""
	default:
		return fmt.Sprintf("Vous êtes « %s » : la création de dépôts doit être autorisée "+
			"aux membres dans les réglages de l'organisation.", role)
	}
}

// ---------------------------------------------------------------- inventaire

// repos charge les dépôts de l'organisation : mémoire, puis cache, puis API.
func (s *Server) repos(org string, force bool) ([]groups.RepoInfo, string, error) {
	if !force {
		if known, source, found := s.knownRepos(org); found {
			return known, source, nil
		}
	}
	fetched, err := s.fetchRepos(org)
	if err != nil {
		return nil, "", err
	}
	return fetched, "GitHub", nil
}

// knownRepos rend l'inventaire qu'on peut tenir pour à jour : celui de la
// mémoire, ou celui du cache tant qu'il vaut.
func (s *Server) knownRepos(org string) ([]groups.RepoInfo, string, bool) {
	s.mutex.Lock()
	known, found := s.inventory[org]
	s.mutex.Unlock()
	if found {
		return known, "mémoire", true
	}
	var cached []groups.RepoInfo
	if s.deps.Cache.Get(cache.ReposKey(org), cache.ReposTTL, &cached) && len(cached) > 0 {
		s.remember(org, cached)
		return cached, "cache (" + s.deps.Cache.Describe() + ")", true
	}
	return nil, "", false
}

// reposOrStale rend aussitôt l'inventaire, fût-il périmé, à un écran qui ne
// fait que le montrer. Le matin, le cache de la veille ne vaut plus, et le
// relire coûte une seconde par centaine de dépôts — l'écran restait vide tout
// ce temps. Il montre maintenant ce qu'on savait hier, pendant que la relecture
// part en arrière-plan ; « age » dit de quand date ce qu'il montre, et reste
// vide quand l'inventaire est à jour.
//
// Rien ne s'écrit d'après un inventaire périmé : les autres écrans passent par
// « repos », qui attend la relecture plutôt que d'en lancer une seconde.
func (s *Server) reposOrStale(org string) (repos []groups.RepoInfo, source, age string, err error) {
	if known, source, found := s.knownRepos(org); found {
		return known, source, "", nil
	}
	var stale []groups.RepoInfo
	if vieux, found := s.deps.Cache.Peek(cache.ReposKey(org), &stale); found && len(stale) > 0 {
		s.fond.Go(func() { _, _ = s.fetchRepos(org) })
		return stale, "cache périmé", cache.Age(vieux), nil
	}
	repos, source, err = s.repos(org, false)
	return repos, source, "", err
}

// releve est une relecture de l'inventaire en cours. Ceux qui en ont besoin
// pendant qu'elle court l'attendent : deux relectures de front coûteraient
// deux fois les mêmes pages, pour le même résultat.
type releve struct {
	fini  chan struct{}
	repos []groups.RepoInfo
	err   error
	// detache dit qu'une écriture est passée pendant la relecture ; il ne se
	// lit et ne s'écrit que sous le verrou du serveur.
	detache bool
}

// fetchRepos relit l'inventaire sur GitHub, ou attend la relecture déjà en
// route.
//
// Une écriture survenue pendant la relecture la détache (voir detacher) : ce
// qu'elle a lu n'est retenu ni en mémoire ni dans le cache — partie avant
// l'écriture, elle l'ignore peut-être, et la retenir effacerait ce que
// l'écriture vient d'y corriger. Ceux qui l'attendaient en relancent une.
func (s *Server) fetchRepos(org string) ([]groups.RepoInfo, error) {
	s.mutex.Lock()
	if enCours, found := s.releves[org]; found {
		s.mutex.Unlock()
		<-enCours.fini
		s.mutex.Lock()
		detache := enCours.detache
		s.mutex.Unlock()
		if detache {
			return s.fetchRepos(org)
		}
		return enCours.repos, enCours.err
	}
	enCours := &releve{fini: make(chan struct{})}
	s.releves[org] = enCours
	s.mutex.Unlock()
	defer close(enCours.fini)

	fetched, err := s.deps.Client.ListOrgRepos(org, nil)
	// Les dépôts de service sont écartés ici, une fois : rien de ce qui suit
	// n'a alors à se demander si « .github » est un groupe.
	if err == nil {
		fetched = groups.Ordinary(fetched)
	}
	enCours.repos, enCours.err = fetched, err

	// Le cache s'écrit sous le verrou : une écriture qui détacherait la
	// relecture entre les deux verrait son oubli recouvert.
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.releves[org] != enCours {
		return fetched, err
	}
	delete(s.releves, org)
	if err == nil {
		s.inventory[org] = fetched
		s.deps.Cache.Set(cache.ReposKey(org), fetched)
	}
	return fetched, err
}

// detacher retire la relecture en cours de ce qui sera retenu. Il s'appelle
// sous le verrou, au moment où une écriture change l'inventaire.
func (s *Server) detacher(org string) {
	if enCours, found := s.releves[org]; found {
		enCours.detache = true
		delete(s.releves, org)
	}
}

// remember retient l'inventaire d'une organisation pour la suite de la session.
func (s *Server) remember(org string, repos []groups.RepoInfo) {
	s.mutex.Lock()
	s.inventory[org] = repos
	s.mutex.Unlock()
}

// forget oublie l'inventaire d'une organisation, après une écriture.
func (s *Server) forget(org string) {
	s.mutex.Lock()
	delete(s.inventory, org)
	s.detacher(org)
	s.mutex.Unlock()
	s.deps.Cache.Forget(cache.ReposKey(org))
}

// orgTeams charge les équipes de l'organisation : mémoire, puis cache, puis
// API. Elles suivent le même chemin que les dépôts — elles vivent sur GitHub,
// et rien n'en est retenu localement.
func (s *Server) orgTeams(org string, force bool) ([]teams.Info, error) {
	if !force {
		if known, found := s.knownTeams(org); found {
			return known, nil
		}
	}

	fetched, err := s.deps.Client.LoadOrgTeams(org, s.deps.Jobs)
	if err != nil {
		return nil, err
	}
	s.deps.Cache.Set(cache.TeamsKey(org), fetched)
	s.rememberTeams(org, fetched)
	return fetched, nil
}

// knownTeams rend les équipes déjà lues, membres compris : celles de la
// mémoire, ou celles du cache tant qu'il vaut.
func (s *Server) knownTeams(org string) ([]teams.Info, bool) {
	s.mutex.Lock()
	known, found := s.squads[org]
	s.mutex.Unlock()
	if found {
		return known, true
	}
	var cached []teams.Info
	if s.deps.Cache.Get(cache.TeamsKey(org), cache.TeamsTTL, &cached) {
		s.rememberTeams(org, cached)
		return cached, true
	}
	return nil, false
}

// teamNames rend les équipes de l'organisation pour un écran qui n'a besoin
// que de leurs noms : la liste des groupes, qui y lit quels travaux sont
// d'équipe. Lire leurs membres coûte deux requêtes par équipe — plus de quatre
// secondes pour trente équipes, dès que le cache des équipes a plus d'une
// heure —, pour un écran qui ne les montre pas.
//
// Ce qui est lu ici n'est pas retenu comme « les équipes » : des équipes sans
// membres feraient croire aux écrans qui les composent qu'elles sont vides.
// Une lecture qui échoue rend une liste vide, comme avant : les travaux
// paraissent alors individuels, sans que rien d'autre ne soit refusé.
func (s *Server) teamNames(org string) []teams.Info {
	if known, found := s.knownTeams(org); found {
		return known
	}
	s.mutex.Lock()
	named, found := s.squadNames[org]
	s.mutex.Unlock()
	if found {
		return named
	}
	named, err := s.deps.Client.TeamNames(org)
	if err != nil {
		return nil
	}
	s.mutex.Lock()
	s.squadNames[org] = named
	s.mutex.Unlock()
	return named
}

func (s *Server) rememberTeams(org string, list []teams.Info) {
	s.mutex.Lock()
	s.squads[org] = list
	s.mutex.Unlock()
}

// forgetTeams oublie les équipes d'une organisation, après une écriture.
func (s *Server) forgetTeams(org string) {
	s.mutex.Lock()
	delete(s.squads, org)
	delete(s.squadNames, org)
	s.mutex.Unlock()
	s.deps.Cache.Forget(cache.TeamsKey(org))
}

// updateInventory applique à l'inventaire retenu — en mémoire comme dans le
// cache — un changement que « groups » sait décrire. Sans inventaire connu, il
// n'y a rien à corriger : la prochaine lecture partira de GitHub.
func (s *Server) updateInventory(org string, apply func([]groups.RepoInfo) []groups.RepoInfo) {
	s.mutex.Lock()
	known, found := s.inventory[org]
	s.detacher(org)
	s.mutex.Unlock()

	if !found {
		var cached []groups.RepoInfo
		if !s.deps.Cache.Get(cache.ReposKey(org), cache.ReposTTL, &cached) || len(cached) == 0 {
			s.forget(org)
			return
		}
		known = cached
	}
	corrige := apply(known)
	s.remember(org, corrige)
	s.deps.Cache.Set(cache.ReposKey(org), corrige)
}

// renamed suit dans l'inventaire les dépôts qu'on vient de renommer. Ils sont
// repris d'un bloc, après la boucle : le cache s'écrit une fois, pas une fois
// par dépôt.
func (s *Server) renamed(org string, done []groups.Renamed) {
	if len(done) == 0 {
		return
	}
	s.updateInventory(org, func(repos []groups.RepoInfo) []groups.RepoInfo {
		return groups.WithRenamed(repos, done)
	})
}

// deleted retire de l'inventaire un dépôt qu'on vient de supprimer.
func (s *Server) deleted(org, name string) {
	s.updateInventory(org, func(repos []groups.RepoInfo) []groups.RepoInfo {
		return groups.WithoutRepo(repos, name)
	})
	// Un dépôt recréé sous le même nom n'hériterait pas des accès de l'ancien :
	// ce qu'on en savait doit partir avec lui.
	s.resolver(org).ForgetAccess(org, name)
}

// precharger prépare en arrière-plan les deux lectures chères — les historiques
// et les accès — pour les dépôts des cours de la session la plus récente.
//
// Sans mémoire, il n'y a rien à préparer : un cache désactivé demande
// précisément que tout soit redemandé au moment où on le regarde.
func (s *Server) precharger(org string, cours []classroom.Classroom, repos []groups.RepoInfo) {
	if s.deps.Cache == nil || !s.deps.Cache.Enabled {
		return
	}
	s.warmer.Warm(s.resolver(org), org, cours, repos)
}

// resolver retrouve, par organisation, le service qui nomme les personnes.
func (s *Server) resolver(org string) *identity.Resolver {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if existing, found := s.resolvers[org]; found {
		return existing
	}
	fresh := identity.New(s.deps.Client, s.deps.Cache, s.deps.Jobs)
	s.resolvers[org] = fresh
	return fresh
}

// ------------------------------------------------------------------ registre

// registryOf retrouve, par organisation, le registre des étudiants.
func (s *Server) registryOf(org string) *registry.Store {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if existing, found := s.registries[org]; found {
		return existing
	}
	fresh := registry.New(s.deps.Client, org, s.deps.Cache)
	s.registries[org] = fresh
	return fresh
}

// names lit le registre de l'organisation, et rend avec lui ce qu'il faut en
// dire à qui regarde.
//
// Un registre qu'on n'a pas pu lire ne prive de rien : les groupes s'affichent
// quand même, les noms manquent. Mais cela se dit — une liste trouée qu'on
// prend pour une liste entière est pire qu'une liste annoncée trouée.
func (s *Server) names(org string) (*registry.Set, string) {
	snapshot, err := s.registryOf(org).Load()
	switch {
	case err != nil:
		return registry.Empty(), "Registre des étudiants illisible (" + err.Error() +
			") : les noms complets manquent."
	case snapshot.Stale:
		return snapshot.Set, "GitHub est injoignable : le registre des étudiants " +
			"affiché est celui de la dernière lecture."
	case len(snapshot.Issues) > 0:
		return snapshot.Set, "Registre des étudiants : " + strings.Join(snapshot.Issues, " ; ")
	}
	return snapshot.Set, ""
}

// urlOf reconstitue l'adresse d'un dépôt quand l'API ne l'a pas donnée.
func (s *Server) urlOf(org string, repo groups.Repo) string {
	if repo.URL != "" {
		return repo.URL
	}
	host := s.deps.Host
	if host == "" {
		host = "github.com"
	}
	return "https://" + host + "/" + org + "/" + repo.Name
}

// reportDir renvoie le dossier des bilans, chemin développé.
func (s *Server) reportDir() string {
	expanded, err := roster.ExpandPath(s.deps.ReportDir)
	if err != nil {
		return s.deps.ReportDir
	}
	return expanded
}

// --------------------------------------------------------------------- travaux

// handleJob renvoie l'état d'un travail.
func (s *Server) handleJob(writer http.ResponseWriter, request *http.Request) {
	job, found := s.jobs.Get(request.PathValue("id"))
	if !found {
		fail(writer, valid.Errorf("Travail inconnu."))
		return
	}
	writeJSON(writer, http.StatusOK, job.State())
}

// handleCancelJob demande l'arrêt d'un travail.
func (s *Server) handleCancelJob(writer http.ResponseWriter, request *http.Request) {
	if !s.jobs.Cancel(request.PathValue("id")) {
		fail(writer, valid.Errorf("Travail inconnu."))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": JobCanceled})
}

// handleJobEvents diffuse le déroulement d'un travail au fil de l'eau.
func (s *Server) handleJobEvents(writer http.ResponseWriter, request *http.Request) {
	job, found := s.jobs.Get(request.PathValue("id"))
	if !found {
		fail(writer, valid.Errorf("Travail inconnu."))
		return
	}
	flusher, streamable := writer.(http.Flusher)
	if !streamable {
		fail(writer, valid.Errorf("Diffusion impossible."))
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Une reprise de connexion repart du dernier événement reçu.
	seq := 0
	if from := request.URL.Query().Get("from"); from != "" {
		seq, _ = strconv.Atoi(from)
	}
	for {
		events, finished, changed := job.since(seq)
		for _, event := range events {
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(writer, "data: %s\n\n", payload)
			seq = event.Seq
		}
		if len(events) > 0 {
			flusher.Flush()
		}
		if finished && len(events) == 0 {
			return
		}
		select {
		case <-changed:
		case <-request.Context().Done():
			return
		case <-time.After(20 * time.Second):
			// Un commentaire garde la connexion vivante à travers un proxy local.
			fmt.Fprint(writer, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// countReports résume ce que contient le dossier des bilans.
func countReports(directory string) string {
	matches, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil || len(matches) == 0 {
		return "aucun bilan"
	}
	return strconv.Itoa(len(matches)) + " bilan(s)"
}

// itoa met un nombre sous forme de texte.
func itoa(value int) string { return strconv.Itoa(value) }
