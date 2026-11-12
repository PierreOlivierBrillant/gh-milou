package app

import (
	"errors"
	"strings"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/brand"
	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plagiarism"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plan"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/rules"
	"github.com/PierreOlivierBrillant/gh-milou/internal/scopes"
	"github.com/PierreOlivierBrillant/gh-milou/internal/starter"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Codes de retour de l'outil.
const (
	ExitOK         = 0
	ExitFailure    = 1
	ExitValidation = 2
	ExitAborted    = 130
)

// Saisies qui effacent une valeur mémorisée lors d'une exécution précédente.
var clearKeywords = map[string]bool{"-": true, "aucun": true, "none": true, "annuler": true}

// Session déroule une exécution complète, du jeton jusqu'au bilan.
type Session struct {
	Options  *Options
	Console  *ui.Console
	Prompt   ui.Prompter
	Settings config.Settings
	Cache    *cache.Cache
	Client   *ghapi.Client
	Viewer   string
	Starter  *starter.Bundle
	// Rules porte ce que « --rules » a chargé : des équivalences de sigles et
	// des profils d'inspection qui surchargent ceux de l'organisation, le temps
	// de cette exécution.
	Rules rules.Rules

	ConfigFile string
	Sleep      func(time.Duration)
	Now        func() time.Time
	// Refresher renouvelle le jeton ; nil branche la session sur le vrai gh.
	Refresher *scopes.Refresher

	// tokenOrigin dit d'où vient le jeton — « gh », « oauth_token », ou une
	// variable d'environnement. Ce que gh peut renouveler en dépend.
	tokenOrigin string

	// registries retient le registre des étudiants, par organisation.
	registries map[string]*registry.Store

	// saved est l'état des réglages au chargement : il dit ce qui a changé.
	saved       config.Settings
	forgotten   bool        // les réglages ont été oubliés à la demande
	orgAccesses []orgAccess // organisations du compte, une fois établies
	manager     *manageSession
}

// New prépare une session à partir des drapeaux analysés.
func New(options *Options, console *ui.Console, prompter ui.Prompter) *Session {
	configFile := options.ConfigPath
	if configFile == "" {
		configFile = config.Path()
	}
	store := cache.New(!options.NoCache)
	if options.CacheDir != "" {
		store = cache.NewIn(options.CacheDir, !options.NoCache)
	}
	settings := config.Load(configFile)
	return &Session{
		Options:    options,
		Console:    console,
		Prompt:     prompter,
		Settings:   settings,
		saved:      settings,
		Cache:      store,
		ConfigFile: configFile,
		Sleep:      time.Sleep,
		Now:        time.Now,
	}
}

// registryOf retrouve, par organisation, le registre des étudiants.
func (s *Session) registryOf(org string) *registry.Store {
	if s.registries == nil {
		s.registries = map[string]*registry.Store{}
	}
	if existing, found := s.registries[org]; found {
		return existing
	}
	fresh := registry.New(s.Client, org, s.Cache)
	s.registries[org] = fresh
	return fresh
}

// names lit le registre de l'organisation, et rend avec lui ce qu'il faut en
// dire. Un registre qu'on n'a pas pu lire ne prive de rien : les groupes
// s'affichent quand même, les noms manquent — mais cela se dit.
// activite rend l'inventaire dont le dernier envoi ignore ce que les
// enseignants ont poussé, là où l'historique relevé permet de le dire. C'est
// ce que montrent et filtrent les colonnes « Dernier envoi » ; le reste
// continue de lire l'inventaire tel que GitHub l'a rendu.
func (s *Session) activite(org string, repos []groups.RepoInfo) []groups.RepoInfo {
	set, _ := s.names(org)
	return groups.Activity(repos, s.histoires(org, repos), set.Teaches)
}

// histoires rend les historiques déjà relevés des dépôts d'un inventaire, si
// vieux soient-ils, sans rien demander à GitHub.
func (s *Session) histoires(org string, repos []groups.RepoInfo) map[string]groups.Handin {
	noms := make([]string, 0, len(repos))
	for _, repo := range repos {
		noms = append(noms, repo.Name)
	}
	return identity.New(s.Client, s.Cache, s.Options.Jobs).Histories(org, noms)
}

func (s *Session) names(org string) (*registry.Set, string) {
	snapshot, err := s.registryOf(org).Load()
	switch {
	case err != nil:
		return registry.Empty(), "Registre des étudiants illisible (" + err.Error() +
			") : les noms complets manquent."
	case snapshot.Stale:
		return snapshot.Set, "GitHub est injoignable : le registre affiché est celui " +
			"de la dernière lecture."
	case len(snapshot.Issues) > 0:
		return snapshot.Set, "Registre des étudiants : " + strings.Join(snapshot.Issues, " ; ")
	}
	return snapshot.Set, ""
}

// echeances traduit ce que le domaine veut faire des dates de remise en une
// écriture du registre. C'est le seul endroit où les deux vocabulaires se
// rencontrent : la notion de groupe décrit ses intentions, le registre les
// écrit, et ni l'un ni l'autre n'a besoin de connaître l'autre paquet.
func echeances(lignes []classroom.Deadline) registry.Change {
	travaux := make([]registry.Assignment, 0, len(lignes))
	for _, ligne := range lignes {
		travaux = append(travaux, registry.Assignment{ID: ligne.Assignment, Due: ligne.Due})
	}
	return registry.Reschedule(travaux...)
}

// apprendre confie au registre de l'organisation ce qu'on vient d'apprendre des
// personnes : leur nom, et le slug que ce nom donnera à leurs dépôts.
//
// C'est fait avant toute écriture sur GitHub. Un nom qui n'atteindrait pas le
// registre ne serait connu que de cette machine, ce qui est précisément ce
// qu'on veut cesser ; mieux vaut donc s'arrêter là que distribuer d'abord.
//
// L'écriture est idempotente : redire au registre ce qu'il sait déjà n'y écrit
// rien, et le cas courant ne coûte qu'une lecture.
func (s *Session) apprendre(org string, people []roster.Person) error {
	nommees := make([]roster.Person, 0, len(people))
	for _, person := range people {
		if strings.TrimSpace(person.Username) != "" {
			nommees = append(nommees, person)
		}
	}
	if len(nommees) == 0 {
		return nil
	}
	_, err := s.registryOf(org).Apply(registry.Learn(nommees...))
	return err
}

// Interactive indique si des questions peuvent être posées.
func (s *Session) Interactive() bool { return s.Prompt.Interactive() }

// require impose une valeur en mode script : une absence y est une erreur.
func (s *Session) require(value, flagName, label string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return value, nil
	}
	return "", valid.Errorf("%s manquant : passez %s en mode non interactif.", label, flagName)
}

// Run enchaîne les étapes et renvoie le code de retour.
func (s *Session) Run() int {
	s.Console.Banner(brand.Name+" "+Version, brand.Tagline)

	code, err := s.run()
	// Les réglages sont mémorisés quoi qu'il arrive : une organisation choisie
	// reste connue même si la session est interrompue ou annulée ensuite.
	s.persist()
	if err == nil {
		return code
	}
	s.Console.Blank()
	switch {
	case errors.Is(err, ui.ErrAborted):
		s.Console.Print(s.Console.Warn("Interrompu : rien n'a été laissé à moitié fait."))
		return ExitAborted
	case valid.IsValidation(err):
		s.Console.Print(s.Console.Err("Erreur : " + err.Error()))
		return ExitValidation
	case ghapi.IsGitHub(err):
		s.Console.Print(s.Console.Err("Erreur GitHub : " + err.Error()))
		return ExitFailure
	default:
		s.Console.Print(s.Console.Err("Erreur : " + err.Error()))
		return ExitFailure
	}
}

func (s *Session) run() (int, error) {
	// Les règles sont chargées avant tout le reste : un fichier illisible doit
	// se dire tout de suite, et non au milieu d'une analyse.
	declarees, err := rules.Load(s.Options.Rules)
	if err != nil {
		return ExitValidation, err
	}
	s.Rules = declarees
	// Le drapeau vaut pour cette exécution et pour les suivantes : la marque
	// est un réglage de distribution, comme le droit accordé ou la visibilité.
	if s.Options.NoSignSet {
		s.Settings.NoSign = s.Options.NoSign
	}

	// Déposer le gabarit ne demande ni jeton ni réseau : c'est un fichier à
	// écrire, et rien d'autre.
	if s.Options.EmitWorkflowSet {
		chemin, err := plagiarism.EmitWorkflow(s.Options.EmitWorkflow)
		if err != nil {
			return ExitValidation, err
		}
		s.Console.Success("Gabarit déposé : %s", chemin)
		s.Console.Note("Lisez-le avant de vous en servir : il demande un jeton à " +
			"portée fine sur l'organisation, et le dépôt qui le porte doit être " +
			"protégé en écriture.")
		return ExitOK, nil
	}

	if s.Options.ClearCache {
		// Purge demandée en ligne de commande : ni jeton ni réseau nécessaires.
		removed := s.Cache.Clear()
		s.Console.Success("Cache vidé (%s).", valid.Plural(removed, "entrée"))
		return ExitOK, nil
	}

	if s.Options.RefreshToken {
		// Renouveler le jeton ne demande ni organisation ni mode : c'est une
		// séance à soi seule, comme la purge du cache.
		return ExitOK, s.refreshTokenFromFlags()
	}

	mode, err := s.chooseMode()
	if err != nil {
		return ExitOK, err
	}
	if mode == "quitter" {
		return ExitOK, nil
	}

	if err := s.authenticate(); err != nil {
		return ExitOK, err
	}
	// L'interface web choisit elle-même l'organisation : le terminal n'a plus
	// de question à poser une fois le jeton résolu.
	if mode == "web" {
		return s.serveWeb()
	}
	if err := s.chooseOrg(); err != nil {
		return ExitOK, err
	}

	if mode == "importer" {
		return s.importRepos()
	}
	if mode == "registre" {
		if s.Options.PublishRules != "" {
			return s.publishRules(s.Options.PublishRules)
		}
		if s.Options.ForgetRegistryHistory {
			return s.forgetRegistryHistory()
		}
		if s.Options.RegistryTeam != "" {
			return s.grantRegistryTeam(s.Options.RegistryTeam)
		}
		return s.publishRegistry()
	}
	if mode == "demandes" {
		return s.demandesMode()
	}
	if mode == "index-manquants" {
		return s.publierLesIndexManquants()
	}
	if mode == "etudiants" {
		return newDirectorySession(s).run()
	}
	if mode == "fiche" {
		return s.showProfile(s.Options.User)
	}
	if mode == "etudiant" {
		return s.correctionMode()
	}
	// L'équipe enseignante d'un groupe n'est pas une de ses équipes : on ne
	// lui distribue rien, elle reçoit l'accès aux dépôts. Elle a donc son
	// propre chemin.
	if mode == "cloisonner" {
		return s.teachingMode(s.Options.Manage)
	}
	// Les équipes appartiennent à un groupe, pas à un préfixe : quand les
	// drapeaux en parlent, « --manage » ne désigne plus un lot de dépôts mais
	// la place du groupe — « a26.5n6.01 ».
	if mode == "equipes" || (mode == "gerer" && s.Options.wantsTeams()) {
		return s.teamsMode()
	}
	if mode == "gerer" {
		s.manager = newManageSession(s, s.Options.Manage)
		return s.manager.run()
	}
	return s.create()
}

// chooseMode décide du mode : création, gestion d'un groupe, options avancées.
//
// Sans rien préciser, l'interface graphique l'emporte : c'est là que tout est
// accessible. Le terminal reste maître dès qu'un drapeau dit quoi faire, et
// « --cli » y ramène explicitement — la règle du gh CLI est qu'une commande
// pilotée par des drapeaux ou branchée sur un tuyau ne doit rien ouvrir ni rien
// demander.
func (s *Session) chooseMode() (string, error) {
	if s.Options.Web {
		return "web", nil
	}
	if s.Options.ImportRequested {
		return "importer", nil
	}
	if s.Options.PublishRegistry || s.Options.ForgetRegistryHistory ||
		s.Options.RegistryTeam != "" || s.Options.PublishRules != "" {
		return "registre", nil
	}
	// Trancher ou déposer une demande ne gère aucun travail : la demande porte
	// elle-même le travail qu'elle concerne.
	if s.Options.decidesAsk() {
		return "demandes", nil
	}
	// « --publish-index » sans travail ne gère rien non plus : c'est le
	// catalogue qui dit lesquels n'ont pas d'index.
	if s.Options.PublishIndex && strings.TrimSpace(s.Options.Manage) == "" {
		return "index-manquants", nil
	}
	// La fiche d'un compte prime sur l'annuaire : « --students --user X » veut
	// dire « cet utilisateur-là », et non « la liste, plus lui ».
	if strings.TrimSpace(s.Options.User) != "" {
		return "fiche", nil
	}
	// « --student » corrige une ligne de la liste d'un groupe : « --manage » n'y
	// désigne que le groupe, et il n'y a rien d'autre à gérer.
	if strings.TrimSpace(s.Options.Student) != "" {
		return "etudiant", nil
	}
	if s.Options.StudentsRequested {
		return "etudiants", nil
	}
	if s.Options.TeachersOn {
		return "cloisonner", nil
	}
	if s.Options.ManageRequested {
		return "gerer", nil
	}
	if !s.Interactive() {
		if s.Options.wantsTeams() && s.Options.Assignment == "" {
			return "equipes", nil
		}
		return "creer", nil
	}
	// Un lancement déjà paramétré pour créer ne doit pas poser de question.
	if s.Options.Roster != "" || s.Options.Assignment != "" || s.Options.TemplateSet ||
		s.Options.StarterSet || s.Options.Pattern != "" || s.Options.Yes {
		return "creer", nil
	}
	if s.Options.wantsTeams() {
		return "equipes", nil
	}
	if !s.Options.CLI {
		return "web", nil
	}

	for {
		choice, err := s.Prompt.Choose("Que voulez-vous faire ?", ui.Options(
			"creer", "Nouveau travail : un dépôt par personne d'une liste",
			"gerer", "Lister et gérer un groupe de dépôts existant",
			"equipes", "Gérer les équipes d'un groupe",
			"etudiants", "Lister les étudiants de l'organisation",
			"importer", "Reprendre des dépôts nommés autrement",
			"registre", "Publier les noms au registre de l'organisation",
			"web", "Ouvrir l'interface graphique dans le navigateur",
			"avance", "Options avancées",
			"quitter", "Quitter",
		), "creer")
		if err != nil {
			return "", err
		}
		if choice != "avance" {
			return choice, nil
		}
		if err := s.advancedMenu(); err != nil {
			return "", err
		}
	}
}

// authenticate résout le jeton par gh et affiche le compte connecté.
func (s *Session) authenticate() error {
	// Les options avancées peuvent avoir déjà lu le jeton : rien à refaire.
	if s.Client != nil {
		return nil
	}
	s.Console.Heading("Authentification GitHub")
	host := s.Options.Host
	if host == "" {
		host = ghapi.DefaultHost()
	}

	token, origin := "", ""
	if s.Options.BaseURL == "" {
		token, origin = ghapi.TokenForHost(host)
		if strings.TrimSpace(token) == "" {
			return valid.Errorf(
				"Aucun jeton disponible pour %s : lancez « gh auth login », "+
					"ou définissez GH_TOKEN.", host)
		}
	} else {
		token, origin = "jeton-de-test", "configuration de test"
	}

	client, err := ghapi.New(ghapi.Options{
		Host: host, Token: token, BaseURL: s.Options.BaseURL, Sleep: s.Sleep, Now: s.Now,
	})
	if err != nil {
		return err
	}
	var user *ghapi.User
	ui.Await(s.Console, "Vérification du jeton auprès de "+host+"…", func() {
		user, err = client.AuthenticatedUser()
	})
	if err != nil {
		if ghapi.StatusOf(err) == 401 {
			return valid.Errorf(
				"Jeton refusé par GitHub. Vérifiez « gh auth status » " +
					"ou renouvelez-le avec « gh auth refresh -s repo ».")
		}
		return err
	}
	s.Client = client
	s.Viewer = user.Login
	s.tokenOrigin = origin
	s.Console.Printf("  Jeton fourni par %s.", s.Console.Dim(origin))
	s.Console.Printf("  Connecté en tant que %s sur %s.", s.Console.OK("@"+s.Viewer), s.Console.Dim(host))

	// Un jeton « fine-grained » n'annonce aucune portée : on n'alerte que si la
	// liste existe vraiment.
	if present, known := client.HasScope("repo"); known && !present {
		s.Console.Warning("La portée « repo » semble absente : la création de dépôts privés peut échouer.")
		// « --refresh-token » s'apprête déjà à la demander : rien à proposer.
		if !s.Options.RefreshToken {
			s.offerScope("repo")
		}
	}
	return nil
}

// chooseOrg retient l'organisation cible et signale un rôle insuffisant.
func (s *Session) chooseOrg() error {
	s.Console.Heading("Organisation cible")
	// En mode script, l'organisation mémorisée sert de repli.
	candidate := s.Options.Org
	if candidate == "" && !s.Interactive() {
		candidate = s.Settings.Org
	}

	for {
		var org string
		var err error
		if candidate != "" {
			org, err = valid.Login(candidate, "Organisation")
			if err != nil {
				if !s.Interactive() {
					return err
				}
				s.Console.Failure("%v", err)
				candidate = ""
				continue
			}
		} else {
			if !s.Interactive() {
				if _, err := s.require("", "--org", "Organisation"); err != nil {
					return err
				}
			}
			// Le choix se fait parmi les organisations du compte, avec ce qu'on
			// peut y faire ; un nom peut toujours être saisi à la place.
			answer, err := s.pickOrg()
			if err != nil {
				return err
			}
			org = answer
		}

		var data *ghapi.Org
		ui.Await(s.Console, "Lecture de l'organisation "+org+"…", func() {
			data, err = s.Client.GetOrg(org)
		})
		if err != nil {
			message := err.Error()
			switch ghapi.StatusOf(err) {
			case 404:
				message = "L'organisation « " + org + " » est introuvable ou invisible pour @" + s.Viewer + "."
			case 403:
				message = "Accès refusé à l'organisation « " + org + " »."
			}
			if !s.Interactive() {
				return valid.Errorf("%s", message)
			}
			s.Console.Failure("%s", message)
			candidate = ""
			continue
		}

		s.Settings.Org = org
		label := data.Name
		if label == "" {
			label = org
		}
		s.Console.Printf("  Organisation : %s %s", s.Console.OK(label),
			s.Console.Dim("(github.com/"+org+")"))
		s.warnIfNotAdmin(org)
		return nil
	}
}

// warnIfNotAdmin signale, sans bloquer, un rôle insuffisant pour créer des dépôts.
func (s *Session) warnIfNotAdmin(org string) {
	// Ce que l'on sait déjà de l'organisation évite un appel de plus.
	if access, found := s.accessFor(org); found {
		switch {
		case access.Role == "admin" || access.CanCreate:
		case access.Known:
			s.Console.Warning("Vous êtes « %s » et la création de dépôts est réservée "+
				"aux propriétaires de cette organisation.", access.Role)
		default:
			s.Console.Warning("Vous êtes « %s » : la création de dépôts doit être autorisée "+
				"aux membres dans les réglages de l'organisation.", access.Role)
		}
		return
	}

	var role string
	var err error
	ui.Await(s.Console, "Vérification de votre rôle dans "+org+"…", func() {
		role, err = s.Client.OrgMembership(org, s.Viewer)
	})
	if err != nil {
		return
	}
	switch role {
	case "":
		s.Console.Warning("Rôle indéterminé dans l'organisation (portée « read:org » absente ?) : " +
			"la création peut échouer.")
	case "admin":
	default:
		s.Console.Warning("Vous êtes « %s » : la création de dépôts doit être autorisée "+
			"aux membres dans les réglages de l'organisation.", role)
	}
}

// persist enregistre les réglages réutilisables ; le jeton n'y figure jamais.
// Rien n'est écrit si rien n'a changé, ni après un oubli volontaire.
func (s *Session) persist() {
	if s.Options.NoSaveConfig || s.forgotten || s.Settings == s.saved {
		return
	}
	if err := s.Settings.Save(s.ConfigFile); err != nil {
		s.Console.Warning("Réglages non enregistrés : %v", err)
		return
	}
	s.saved = s.Settings
	s.Console.Note("Réglages mémorisés dans %s", s.ConfigFile)
}

// resetSettings repart des réglages par défaut après un oubli volontaire :
// plus rien ne sera réécrit d'ici la fin de la session.
func (s *Session) resetSettings() {
	s.Settings = config.Default()
	s.Settings.Org = s.Options.Org
	s.forgotten = true
}

// invalidateCaches oublie ce qui a été retenu en mémoire pendant la session.
func (s *Session) invalidateCaches() {
	if s.manager != nil {
		s.manager.forget()
	}
}

// placeholderHint énumère les champs disponibles dans les gabarits.
func placeholderHint() string {
	fields := make([]string, 0, len(plan.Placeholders))
	for _, name := range plan.Placeholders {
		fields = append(fields, "{"+name+"}")
	}
	return strings.Join(fields, ", ")
}

// teacherGrant rend l'équipe enseignante du groupe d'un travail, sous la forme
// que le runner attend : le slug, et le droit à lui donner.
//
// Un groupe non cloisonné n'en a pas, et rien n'est alors accordé. Une lecture
// qui échoue ne fait pas échouer la distribution : mieux vaut un dépôt créé
// sans l'accès de l'équipe — que « --cloisonner » redonnera — qu'aucun dépôt.
func (s *Session) teacherGrant(org, assignmentID string) (string, string) {
	scope, _, ok := naming.SplitAssignment(assignmentID)
	if !ok {
		return "", ""
	}
	niveaux := strings.Split(scope, naming.Separator)
	infos, err := s.Client.LoadOrgTeams(org, s.Options.Jobs)
	if err != nil {
		return "", ""
	}
	equipe, existe := teams.TeacherTeam(niveaux[0], niveaux[1], niveaux[2], infos)
	if !existe {
		return "", ""
	}
	return equipe.Slug, classroom.TeacherPermission
}
