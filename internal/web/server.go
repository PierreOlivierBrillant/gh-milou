// Package web sert une interface graphique locale au-dessus des mêmes paquets
// que l'assistant du terminal : un petit serveur sur la boucle locale, une API
// JSON, et une page embarquée dans le binaire.
//
// Le serveur n'écoute que sur 127.0.0.1, exige un jeton tiré au hasard à chaque
// lancement, et vérifie l'origine de toute requête qui écrit. Le jeton GitHub
// ne quitte jamais le processus : le navigateur ne parle qu'à cette API.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/exchange"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/preload"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/rules"
	"github.com/PierreOlivierBrillant/gh-milou/internal/scopes"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

//go:embed assets
var assets embed.FS

// MaxBody borne la taille d'un corps de requête : une liste collée reste
// modeste, et rien ne justifie d'avaler davantage.
const MaxBody = 8 << 20

// Deps rassemble ce que la session du terminal transmet à l'interface.
type Deps struct {
	Client     *ghapi.Client
	Cache      *cache.Cache
	Settings   config.Settings
	ConfigFile string
	Viewer     string
	Host       string
	// TokenOrigin dit d'où vient le jeton — « oauth_token », « gh », ou une
	// variable d'environnement. Ce que gh peut renouveler en dépend.
	TokenOrigin string
	// Refresher renouvelle le jeton ; nil branche l'interface sur le vrai gh.
	Refresher *scopes.Refresher
	Version   string
	ReportDir string
	// Rules surcharge les règles de l'organisation, telles que « --rules » les
	// a chargées au lancement.
	Rules      rules.Rules
	Jobs       int
	Depth      int
	SaveConfig bool
}

// Server est l'interface web d'une session.
type Server struct {
	deps       Deps
	jobs       *Jobs
	classrooms *classroom.Store
	// warmer prépare d'avance les lectures chères des cours de la session la
	// plus récente : l'écran qu'on ouvre les montre alors sans attendre.
	warmer   *preload.Warmer
	listener net.Listener
	token    string
	port     string
	handler  http.Handler
	stop     chan struct{}
	stopOnce sync.Once
	// fond compte les relectures d'inventaire parties en arrière-plan : elles
	// écrivent dans le cache, et la fermeture leur laisse le temps de finir.
	fond sync.WaitGroup

	mutex      sync.Mutex
	settings   config.Settings
	inventory  map[string][]groups.RepoInfo  // organisation → dépôts connus
	releves    map[string]*releve            // organisation → relecture de l'inventaire en cours
	squads     map[string][]teams.Info       // organisation → équipes connues
	squadNames map[string][]teams.Info       // organisation → équipes sans leurs membres
	resolvers  map[string]*identity.Resolver // organisation → noms complets
	registries map[string]*registry.Store    // organisation → registre des étudiants
	// indexStores retient, par organisation, le dépôt des index d'empreintes.
	indexStores map[string]*exchange.Store
	// reports garde les rapports de plagiat déjà lus et la dernière paire
	// ouverte : un rapport pèse parfois des méga-octets, et ouvrir une paire
	// retélécharge deux dépôts.
	reports reportCache
}

// New prépare le serveur et réserve son port sur la boucle locale.
func New(deps Deps) (*Server, error) {
	if deps.Jobs < 1 {
		deps.Jobs = 4
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	token, err := newToken()
	if err != nil {
		listener.Close()
		return nil, err
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		listener.Close()
		return nil, err
	}

	server := &Server{
		deps:        deps,
		jobs:        NewJobs(),
		warmer:      preload.New(),
		listener:    listener,
		token:       token,
		port:        port,
		classrooms:  classroom.Open(classroom.PathNextTo(deps.ConfigFile)),
		stop:        make(chan struct{}),
		settings:    deps.Settings,
		inventory:   map[string][]groups.RepoInfo{},
		releves:     map[string]*releve{},
		squads:      map[string][]teams.Info{},
		squadNames:  map[string][]teams.Info{},
		resolvers:   map[string]*identity.Resolver{},
		registries:  map[string]*registry.Store{},
		indexStores: map[string]*exchange.Store{},
	}
	// Le magasin consulte le registre avant d'écrire : un nom que le registre
	// porte déjà n'a pas à être redit dans le fichier local.
	server.classrooms.Resolving(func(org string) classroom.Names {
		set, _ := server.names(org)
		return set
	})
	server.handler = server.guard(server.routes())
	return server, nil
}

// URL est l'adresse à ouvrir, jeton compris. Elle n'est affichée qu'une fois.
func (s *Server) URL() string {
	return "http://127.0.0.1:" + s.port + "/?" + tokenParam + "=" + s.token
}

// Address est l'adresse sans jeton, pour les messages du terminal.
func (s *Server) Address() string { return "http://127.0.0.1:" + s.port }

// RegistryApply écrit au registre d'une organisation.
//
// Il n'existe que pour les épreuves : elles ont besoin d'y déposer ce qu'un
// collègue y aurait écrit depuis son poste, et le faux serveur n'a pas de
// second client.
func (s *Server) RegistryApply(org string, change registry.Change) error {
	_, err := s.registryOf(org).Apply(change)
	return err
}

// Settings renvoie les réglages tels que l'interface les a laissés : la session
// du terminal les mémorise en quittant.
func (s *Server) Settings() config.Settings {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.settings
}

// Serve tient l'interface jusqu'à l'annulation du contexte ou la fermeture
// demandée depuis le navigateur.
func (s *Server) Serve(lifetime context.Context) error {
	server := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	failures := make(chan error, 1)
	go func() {
		err := server.Serve(s.listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		failures <- err
	}()

	select {
	case err := <-failures:
		return err
	case <-lifetime.Done():
	case <-s.stop:
	}

	// Ce qui se préparait en arrière-plan n'a plus personne à servir.
	s.warmer.Stop()
	// Une relecture d'inventaire en route ne s'interrompt pas non plus ; elle
	// a le même délai que le préchargement pour poser ce qu'elle a lu.
	relue := make(chan struct{})
	go func() {
		defer close(relue)
		s.fond.Wait()
	}()
	select {
	case <-relue:
	case <-time.After(preload.Arret):
	}

	// Les flux d'événements restent ouverts : la fermeture est bornée.
	shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	return nil
}

// routes déclare l'API et la page.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// --- contexte et réglages
	mux.HandleFunc("GET /api/context", s.handleContext)
	mux.HandleFunc("PUT /api/settings", s.handleSaveSettings)
	mux.HandleFunc("GET /api/token", s.handleToken)
	mux.HandleFunc("POST /api/token/refresh", s.handleRefreshToken)
	mux.HandleFunc("POST /api/cache/clear", s.handleClearCache)
	mux.HandleFunc("POST /api/quit", s.handleQuit)

	// --- organisations et inventaire
	mux.HandleFunc("GET /api/orgs", s.handleOrgs)
	mux.HandleFunc("GET /api/orgs/{org}", s.handleOrg)

	// --- utilisateurs de l'organisation
	// L'annuaire traverse les groupes : une personne y a une seule ligne, quels
	// que soient les cours et les sessions qu'elle a suivis. La fiche, elle,
	// prend une personne et déroule tout son passage — cours suivis et cours
	// donnés mêlés.
	mux.HandleFunc("GET /api/users", s.handleDirectory)
	mux.HandleFunc("GET /api/users/{account}", s.handleUser)
	mux.HandleFunc("POST /api/users/{account}/role", s.handleUserRole)
	mux.HandleFunc("PUT /api/users/{account}/name", s.handleUserName)
	mux.HandleFunc("GET /api/users/{account}/same-as", s.handleSameCandidates)
	mux.HandleFunc("POST /api/users/{account}/same-as", s.handleJoin)
	mux.HandleFunc("DELETE /api/users/{account}/same-as", s.handleSplit)

	// --- groupes
	// Un groupe se désigne par sa place — « a26.5n6.1010 » —, celle-là même qui
	// est écrite dans le nom de chacun de ses dépôts.
	mux.HandleFunc("GET /api/classrooms", s.handleClassrooms)
	mux.HandleFunc("POST /api/classrooms", s.handleCreateClassroom)
	mux.HandleFunc("GET /api/classrooms/{scope}", s.handleClassroom)
	mux.HandleFunc("PUT /api/classrooms/{scope}", s.handleUpdateClassroom)
	mux.HandleFunc("DELETE /api/classrooms/{scope}", s.handleForgetClassroom)
	mux.HandleFunc("GET /api/classrooms/{scope}/students", s.handleClassroomStudents)
	mux.HandleFunc("POST /api/classrooms/{scope}/students", s.handleSetStudents)
	mux.HandleFunc("POST /api/classrooms/{scope}/students/add", s.handleAddStudent)
	mux.HandleFunc("POST /api/classrooms/{scope}/students/names", s.handleResolveStudentNames)
	mux.HandleFunc("POST /api/classrooms/{scope}/students/move", s.handleMoveStudent)
	mux.HandleFunc("POST /api/classrooms/{scope}/students/rename", s.handleRenameStudent)
	mux.HandleFunc("POST /api/classrooms/{scope}/students/accounts", s.handleAttachAccount)
	// --- équipes du groupe
	// Une équipe se désigne par son nom court — « eq1 » —, et son nom complet
	// sur GitHub porte la place du groupe : « a26.5n6.01.eq1 ».
	mux.HandleFunc("GET /api/classrooms/{scope}/teams", s.handleTeams)
	mux.HandleFunc("POST /api/classrooms/{scope}/teams", s.handleCreateTeam)
	mux.HandleFunc("POST /api/classrooms/{scope}/teams/adopt", s.handleAdoptTeam)
	mux.HandleFunc("POST /api/classrooms/{scope}/teams/members", s.handleAssignTeam)
	mux.HandleFunc("PUT /api/classrooms/{scope}/teams/{team}", s.handleRenameTeam)
	mux.HandleFunc("DELETE /api/classrooms/{scope}/teams/{team}", s.handleDeleteTeam)
	mux.HandleFunc("POST /api/classrooms/{scope}/teams/{team}/members", s.handleComposeTeam)
	mux.HandleFunc("DELETE /api/classrooms/{scope}/teams/{team}/members/{login}", s.handleLeaveTeam)
	mux.HandleFunc("POST /api/classrooms/{scope}/teams/{team}/move", s.handleMoveTeam)
	// --- équipe enseignante du groupe
	// Elle n'est pas une équipe du groupe : on ne lui distribue rien, elle
	// reçoit l'accès à ses dépôts. C'est elle qui cloisonne.
	mux.HandleFunc("GET /api/classrooms/{scope}/teachers", s.handleTeaching)
	mux.HandleFunc("POST /api/classrooms/{scope}/teachers/preview", s.handleTeachingPreview)
	mux.HandleFunc("POST /api/classrooms/{scope}/teachers", s.handleTeachingApply)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments", s.handleCreateAssignment)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/preview", s.handlePreviewAssignment)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/move/preview", s.handleRelocatePreview)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/move", s.handleRelocate)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/rename/preview", s.handleRenameAssignmentPreview)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/rename", s.handleRenameAssignment)
	mux.HandleFunc("POST /api/classrooms/{scope}/migration/preview", s.handleMigrationPreview)
	mux.HandleFunc("POST /api/classrooms/{scope}/migration/apply", s.handleMigrationApply)
	mux.HandleFunc("GET /api/classrooms/{scope}/assignments/{name}", s.handleAssignment)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/access", s.handleAssignmentAccess)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/invitations",
		s.handleAssignmentInvitations)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/repos/{repo}/invitations",
		s.handleRepoInvitation)
	mux.HandleFunc("GET /api/broadcast/fields", s.handleBroadcastFields)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/file/preview",
		s.handleFilePreview)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/file", s.handleFilePush)
	mux.HandleFunc("PUT /api/classrooms/{scope}/assignments/{name}/deadline", s.handleSetDeadline)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/handins", s.handleAssignmentHandins)
	mux.HandleFunc("POST /api/classrooms/{scope}/handins", s.handleClassroomHandins)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/share", s.handleShareAssignment)
	mux.HandleFunc("GET /api/orgs/{org}/foreign", s.handleForeign)
	mux.HandleFunc("GET /api/orgs/{org}/teams", s.handleLooseTeams)
	mux.HandleFunc("POST /api/orgs/{org}/import/repos", s.handleForeignRepos)
	mux.HandleFunc("POST /api/orgs/{org}/import/place", s.handleGuessPlace)
	mux.HandleFunc("POST /api/orgs/{org}/import/preview", s.handleImportPreview)
	mux.HandleFunc("POST /api/orgs/{org}/import", s.handleImport)
	mux.HandleFunc("GET /api/orgs/{org}/registry", s.handleRegistryPreview)
	mux.HandleFunc("POST /api/orgs/{org}/registry", s.handleRegistryPublish)
	mux.HandleFunc("POST /api/orgs/{org}/registry/history", s.handleRegistryForgetHistory)
	mux.HandleFunc("POST /api/orgs/{org}/registry/team", s.handleRegistryGrant)

	// --- listes et code de départ
	mux.HandleFunc("POST /api/roster/parse", s.handleParseRoster)
	mux.HandleFunc("POST /api/roster/load", s.handleLoadRoster)
	mux.HandleFunc("POST /api/roster/save", s.handleSaveRoster)
	mux.HandleFunc("POST /api/template/check", s.handleCheckTemplate)
	mux.HandleFunc("POST /api/starter/inspect", s.handleInspectStarter)
	mux.HandleFunc("POST /api/accounts/verify", s.handleVerifyAccounts)

	// --- accès et dépôts
	mux.HandleFunc("GET /api/orgs/{org}/repos/{repo}/access", s.handleAccess)
	mux.HandleFunc("POST /api/orgs/{org}/repos/{repo}/collaborators", s.handleAddCollaborator)
	mux.HandleFunc("DELETE /api/orgs/{org}/repos/{repo}/collaborators/{login}", s.handleRemoveCollaborator)
	mux.HandleFunc("DELETE /api/orgs/{org}/repos/{repo}/invitations/{id}", s.handleCancelInvitation)
	mux.HandleFunc("POST /api/orgs/{org}/repos/{repo}/invitations/{id}/resend", s.handleResendInvitation)
	mux.HandleFunc("DELETE /api/orgs/{org}/repos/{repo}", s.handleDeleteRepo)

	// --- clones
	mux.HandleFunc("POST /api/clones/find", s.handleFindClones)
	mux.HandleFunc("POST /api/clones/clone", s.handleClone)
	mux.HandleFunc("POST /api/clones/pull", s.handlePull)
	mux.HandleFunc("POST /api/paths/pick", s.handlePickPath)
	mux.HandleFunc("POST /api/paths/browse", s.handleBrowsePath)

	// --- travaux en arrière-plan
	mux.HandleFunc("GET /api/orgs/{org}/catalog", s.handleCatalog)
	mux.HandleFunc("GET /api/orgs/{org}/asks", s.handleAsks)
	mux.HandleFunc("POST /api/orgs/{org}/asks", s.handleAsk)
	mux.HandleFunc("POST /api/orgs/{org}/asks/{id}/grant", s.handleGrantAsk)
	mux.HandleFunc("POST /api/orgs/{org}/asks/{id}/deny", s.handleDenyAsk)
	mux.HandleFunc("GET /api/orgs/{org}/rules", s.handleRules)
	mux.HandleFunc("PUT /api/orgs/{org}/rules", s.handleSetRules)

	// Détection de plagiat.
	mux.HandleFunc("GET /api/plagiat/options", s.handlePlagiarismOptions)
	mux.HandleFunc("GET /api/plagiat/reports", s.handlePlagiarismReports)
	mux.HandleFunc("GET /api/plagiat/reports/{name}", s.handlePlagiarismReport)
	mux.HandleFunc("POST /api/plagiat/reports/{name}/publish", s.handlePlagiarismPublish)
	mux.HandleFunc("POST /api/plagiat/reports/{name}/pair", s.handlePlagiarismPair)
	mux.HandleFunc("POST /api/plagiat/reports/{name}/file", s.handlePlagiarismFile)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/plagiat/preview",
		s.handlePlagiarismPreview)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/plagiat",
		s.handlePlagiarismRun)
	mux.HandleFunc("POST /api/classrooms/{scope}/assignments/{name}/plagiat/export",
		s.handlePlagiarismExport)

	mux.HandleFunc("GET /api/jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /api/jobs/{id}/events", s.handleJobEvents)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleCancelJob)

	mux.Handle("/", s.page())
	return mux
}

// page sert les fichiers embarqués ; toute autre adresse renvoie l'interface.
func (s *Server) page() http.Handler {
	files, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Path != "/" {
			if _, err := fs.Stat(files, strings.TrimPrefix(request.URL.Path, "/")); err != nil {
				request = request.Clone(request.Context())
				request.URL.Path = "/"
			}
		}
		server.ServeHTTP(writer, request)
	})
}

// handleQuit referme l'interface à la demande du navigateur.
func (s *Server) handleQuit(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "fermeture"})
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
	s.stopOnce.Do(func() { close(s.stop) })
}

// ------------------------------------------------------------------ échanges

// writeJSON écrit une réponse JSON.
func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(writer).Encode(payload)
}

// decode lit un corps JSON, en bornant sa taille.
func decode(request *http.Request, target any) error {
	body := http.MaxBytesReader(nil, request.Body, MaxBody)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return valid.Errorf("Requête vide.")
		}
		return valid.Errorf("Requête illisible : %v", err)
	}
	return nil
}

// fail traduit une erreur du domaine en réponse HTTP. La portée manquante, si
// GitHub l'a nommée, part avec : l'interface propose alors de renouveler le
// jeton au lieu d'annoncer un refus sans suite.
func fail(writer http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case valid.IsValidation(err):
		status = http.StatusBadRequest
	case ghapi.IsGitHub(err):
		// Le code de GitHub est repris tel quel s'il décrit le problème.
		switch code := ghapi.StatusOf(err); code {
		case 401, 403, 404, 422:
			status = code
		default:
			status = http.StatusBadGateway
		}
	}
	payload := map[string]string{"error": err.Error()}
	if scope := ghapi.MissingScope(err); scope != "" {
		payload["scope"] = scope
	}
	writeJSON(writer, status, payload)
}

// failScope refuse une action que le jeton n'autorise pas, avant même de la
// tenter : l'interface en fait la même proposition que sur un refus de GitHub.
func failScope(writer http.ResponseWriter, scope, message string) {
	writeJSON(writer, http.StatusForbidden, map[string]string{
		"error": message, "scope": scope,
	})
}
