package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Reprendre des dépôts qu'une autre convention a nommés — ceux de GitHub
// Classroom, « travail-compte ». Trois routes, dans l'ordre des questions :
// ce que l'organisation porte hors nomenclature, ce qu'une importation ferait,
// et enfin l'importation elle-même.

// importInput est ce que l'interface envoie pour préparer ou lancer.
type importInput struct {
	// Prefix est le travail tel que les dépôts le portent, Name celui qu'il
	// prendra à l'arrivée.
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
	// Scope est la place d'arrivée : « a26.5n6.1030 ».
	Scope string `json:"scope"`
	// Path est le fichier de la liste, Content ses octets quand elle a été
	// déposée dans la page — un navigateur ne donne jamais le chemin d'un
	// fichier déposé, mais il en donne le contenu. Filename porte alors son
	// nom, qui dit le cours et le groupe là où le chemin manque.
	Path     string `json:"path"`
	Filename string `json:"filename"`
	Content  []byte `json:"content"`
	// NamedOnly laisse où ils sont les dépôts dont on ne connaît pas la
	// personne.
	NamedOnly bool `json:"named_only"`
	// Teams dit que le travail a été fait en équipe : ce qui suit le préfixe
	// nomme alors une équipe, pas une personne, et il n'y a rien à rapprocher
	// d'une liste.
	Teams bool `json:"teams"`
	// Crews impose la composition d'une équipe, par son nom court. Sa présence
	// dit qu'on a tranché à l'écran : le serveur ne redevine plus rien pour
	// cette équipe, y compris quand on a choisi de n'y mettre personne.
	Crews map[string][]string `json:"crews"`
	// Only nomme les dépôts retenus. Vide, le travail est repris entier.
	Only []string `json:"only"`
	// People remplace la liste quand un rapprochement a été corrigé à l'écran.
	// Sa présence dit aussi que plus rien ne doit être deviné : le jugement
	// rendu tient, y compris quand il consiste à ne rapprocher personne.
	People []roster.Person `json:"people"`
}

// handleForeign montre ce que l'organisation porte hors nomenclature.
func (s *Server) handleForeign(writer http.ResponseWriter, request *http.Request) {
	org, err := valid.Login(request.PathValue("org"), "Organisation")
	if err != nil {
		fail(writer, err)
		return
	}
	repos, source, err := s.repos(org, request.URL.Query().Get("refresh") == "1")
	if err != nil {
		fail(writer, err)
		return
	}
	dehors := classroom.ForeignOf(repos)
	writeJSON(writer, http.StatusOK, map[string]any{
		"repos": dehors.Repos, "assignments": dehors.Assignments,
		"source": source, "help": roster.OmnivoxHelp,
	})
}

// foreignRepo est un dépôt qu'on peut retenir ou écarter, tel que l'écran le
// montre : son nom, où le lire sur GitHub, sa dernière trace de vie, et qui est
// derrière lui quand on le sait déjà.
type foreignRepo struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Private  bool   `json:"private"`
	PushedAt string `json:"pushed_at"`
	// Login est le compte que les accès désignent, Student le nom complet que
	// l'organisation lui donne. Savoir cela avant de cocher change ce qu'on
	// coche — et savoir combien de dépôts sont déjà nommés dit si la liste des
	// étudiants a encore quelque chose à apprendre.
	Login   string `json:"login"`
	Student string `json:"student"`
}

// handleForeignRepos énumère les dépôts d'un travail à reprendre. L'inventaire
// est déjà en main : c'est une lecture, pas une requête de plus.
func (s *Server) handleForeignRepos(writer http.ResponseWriter, request *http.Request) {
	org, body, err := s.importRequest(request)
	if err != nil {
		fail(writer, err)
		return
	}
	repos, _, err := s.repos(org, false)
	if err != nil {
		fail(writer, err)
		return
	}
	groupe := groups.Build(body.Prefix, repos)
	// Les accès se lisent ici plutôt qu'à la vérification : ils coûtent le même
	// prix, le cache les garde, et ils disent dès maintenant qui est derrière
	// chaque dépôt — ce qui est justement ce qu'on regarde pour cocher.
	proprietaires := s.owners(org, body.Prefix, nil, repos)
	connus := s.connus(org)

	lignes := make([]foreignRepo, 0, groupe.Len())
	nommes := 0
	for _, depot := range groupe.Repos {
		compte := depot.Suffix
		if login := proprietaires[depot.Name]; login != "" {
			compte = login
		}
		nom := connus[strings.ToLower(compte)]
		if nom != "" {
			nommes++
		}
		lignes = append(lignes, foreignRepo{
			Name: depot.Name, URL: s.urlOf(org, depot),
			Private: depot.Private, PushedAt: depot.PushedAt,
			Login: compte, Student: nom,
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"prefix": groupe.Prefix, "repos": lignes,
		"total": len(lignes), "known": nommes,
	})
}

// importPlan compose ce qu'une importation ferait.
//
// La liste vient du fichier, ou de ce que l'interface renvoie après correction :
// un rapprochement se juge à l'écran, et ce jugement doit pouvoir l'emporter.
func (s *Server) importPlan(org string, body importInput) (
	classroom.Import, classroom.Classroom, error) {
	var vide classroom.Classroom
	arrivee, err := classroom.AtScope(org, body.Scope, classroom.DefaultsFrom(s.Settings()))
	if err != nil {
		return classroom.Import{}, vide, err
	}
	repos, _, err := s.repos(org, false)
	if err != nil {
		return classroom.Import{}, vide, err
	}

	entrees, deviner, err := s.entries(body)
	if err != nil {
		return classroom.Import{}, vide, err
	}
	// Qui a accès à quoi se lit avant tout le reste : c'est ce qui dit le
	// compte de chaque dépôt, et donc où finit le travail dans son nom.
	proprietaires := s.owners(org, body.Prefix, body.Only, repos)
	demande := classroom.ImportRequest{
		Prefix: body.Prefix, Name: body.Name, Entries: entrees, Guess: deviner,
		NamedOnly: body.NamedOnly, Owners: proprietaires, Known: s.connus(org),
		Only: body.Only,
	}
	if deviner {
		// Les profils GitHub ne servent qu'à la première lecture : après une
		// correction, plus rien n'est deviné. Ce que l'organisation sait, lui,
		// est toujours donné — il confirme les comptes, et le plan sait ne
		// plus s'en servir pour rapprocher.
		demande.Profiles = s.profiles(org, body.Prefix, repos, proprietaires)
	}
	plan, err := classroom.PlanImport(arrivee, demande, repos)
	return plan, arrivee, err
}

// connus rend le nom complet des comptes que l'organisation sait déjà nommer :
// son registre d'abord, puis les groupes déjà déclarés sur ce poste. Un compte
// qui s'y trouve n'a pas à repasser par un rapprochement — la réponse est
// écrite, et c'est autant de vérifications en moins à l'écran.
func (s *Server) connus(org string) map[string]string {
	noms := map[string]string{}
	registre, _ := s.names(org)
	for _, etudiant := range registre.All() {
		if nom := strings.TrimSpace(etudiant.FullName); nom != "" {
			noms[strings.ToLower(etudiant.Username)] = nom
		}
	}
	// Ce que le poste retient et que le registre ignore encore vaut aussi :
	// une reprise faite avant la publication en est pleine.
	for _, personne := range s.classrooms.People(org) {
		compte := strings.ToLower(strings.TrimSpace(personne.Username))
		if compte == "" || strings.TrimSpace(personne.FullName) == "" {
			continue
		}
		if _, deja := noms[compte]; !deja {
			noms[compte] = personne.FullName
		}
	}
	return noms
}

// owners relève, pour les dépôts d'un préfixe, le compte GitHub que leurs accès
// désignent. Un appel par dépôt la première fois, rien ensuite : le cache les
// retient, et l'écran les redemande à chaque correction de rapprochement.
func (s *Server) owners(org, prefix string, seulement []string,
	repos []groups.RepoInfo) map[string]string {
	noms := s.aLire(prefix, seulement, repos)
	if len(noms) == 0 {
		return nil
	}
	trouves := s.resolver(org).Owners(org, noms, s.deps.Viewer, nil)
	comptes := make(map[string]string, len(trouves))
	for nom, proprietaire := range trouves {
		if proprietaire.Login != "" {
			comptes[nom] = proprietaire.Login
		}
	}
	return comptes
}

// aLire nomme les dépôts d'un préfixe dont les accès sont à lire. Un dépôt
// écarté ne sera ni repris ni rapproché : lire les siens coûterait une requête
// pour rien.
func (s *Server) aLire(prefix string, seulement []string,
	repos []groups.RepoInfo) []string {
	groupe := groups.Build(prefix, repos)
	if groupe.Len() == 0 {
		return nil
	}
	retenus := map[string]bool{}
	for _, nom := range seulement {
		retenus[strings.ToLower(strings.TrimSpace(nom))] = true
	}
	noms := make([]string, 0, groupe.Len())
	for _, depot := range groupe.Repos {
		if len(retenus) > 0 && !retenus[strings.ToLower(depot.Name)] {
			continue
		}
		noms = append(noms, depot.Name)
	}
	return noms
}

// membres rend, pour chaque dépôt, qui l'a fait : l'équipe GitHub à qui il est
// partagé, ses collaborateurs directs, ses auteurs de commits. C'est « identity »
// qui décide de ce que chaque source vaut.
func (s *Server) membres(org, prefix string, seulement []string,
	repos []groups.RepoInfo) map[string]identity.Crew {
	noms := s.aLire(prefix, seulement, repos)
	if len(noms) == 0 {
		return nil
	}
	return s.resolver(org).Crews(org, noms, s.deps.Viewer, nil)
}

// entries rend la liste du groupe et dit s'il reste quelque chose à deviner.
func (s *Server) entries(body importInput) ([]roster.Entry, bool, error) {
	if len(body.People) > 0 {
		entrees := make([]roster.Entry, 0, len(body.People))
		for _, personne := range body.People {
			entrees = append(entrees, roster.Entry{
				FullName: personne.FullName, Username: personne.Username,
			})
		}
		return entrees, false, nil
	}

	liste := roster.Roster{}
	switch {
	case len(body.Content) > 0:
		liste = roster.ParseBytes(body.Content)
	case strings.TrimSpace(body.Path) != "":
		lue, err := roster.Load(body.Path)
		if err != nil {
			return nil, false, err
		}
		liste = lue
	default:
		// Pas de liste : l'organisation connaît peut-être déjà tout le monde,
		// et une liste n'apprendrait alors rien. Ce que le registre ignore
		// restera sans nom, ce que la revue dira.
		return nil, true, nil
	}
	if len(liste.Entries) == 0 {
		return nil, false, valid.Errorf("Aucun étudiant dans la liste fournie.")
	}
	return liste.Entries, true, nil
}

// handleGuessPlace devine la place d'arrivée, pour que les trois champs de
// l'étape suivante arrivent déjà remplis.
//
// La date qui donne la session coûte deux requêtes sur un seul dépôt : c'est
// le prix d'une devinette, et il ne dépend pas de la taille du travail.
func (s *Server) handleGuessPlace(writer http.ResponseWriter, request *http.Request) {
	org, body, err := s.importRequest(request)
	if err != nil {
		fail(writer, err)
		return
	}
	entrees, _, err := s.entries(body)
	if err != nil {
		fail(writer, err)
		return
	}
	repos, _, err := s.repos(org, false)
	if err != nil {
		fail(writer, err)
		return
	}
	nom := body.Path
	if nom == "" {
		nom = body.Filename
	}
	debut := classroom.AssignmentStart(body.Prefix, repos, func(depot string) (time.Time, error) {
		return s.deps.Client.FirstCommit(org, depot)
	})
	writeJSON(writer, http.StatusOK, classroom.GuessPlace(nom, entrees, debut))
}

// handleImportPreview montre le renommage sans rien écrire.
func (s *Server) handleImportPreview(writer http.ResponseWriter, request *http.Request) {
	org, body, err := s.importRequest(request)
	if err != nil {
		fail(writer, err)
		return
	}
	// Une reprise en équipe suit les mêmes écrans : c'est ici qu'elle bifurque,
	// et le corps de la réponse dit laquelle des deux il décrit.
	if body.Teams {
		plan, _, err := s.teamImportPlan(org, body)
		if err != nil {
			fail(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, plan)
		return
	}
	plan, _, err := s.importPlan(org, body)
	if err != nil {
		fail(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, plan)
}

// handleImport renomme les dépôts, déclare le groupe, et confie les noms au
// registre.
func (s *Server) handleImport(writer http.ResponseWriter, request *http.Request) {
	org, body, err := s.importRequest(request)
	if err != nil {
		fail(writer, err)
		return
	}
	if body.Teams {
		s.handleTeamImport(writer, org, body)
		return
	}
	plan, arrivee, err := s.importPlan(org, body)
	if err != nil {
		fail(writer, err)
		return
	}
	if plan.Divided() {
		fail(writer, valid.Errorf(
			"« %s » couvre %d travaux : reprenez-les un à la fois.",
			body.Prefix, len(plan.Splits)))
		return
	}
	if !plan.Ready() {
		fail(writer, valid.Errorf("Aucun dépôt à reprendre pour « %s ».", body.Prefix))
		return
	}
	// Le registre passe en premier : un nom qui n'y monterait pas ne serait
	// connu que de ce poste.
	if err := s.apprendre(org, plan.Students...); err != nil {
		fail(writer, err)
		return
	}

	label := "Reprise de « " + plan.Prefix + " » vers " + arrivee.Scope()
	job := s.jobs.Start("importation", label, func(job *Job) (any, error) {
		renommes, echecs := 0, 0
		var suivis []groups.Renamed
		for index, ligne := range plan.Moves {
			if job.Canceled() {
				break
			}
			apres, err := s.deps.Client.RenameRepo(org, ligne.Repo, ligne.Target)
			if err != nil {
				echecs++
				job.Line(ligne.Repo+" : échec — "+err.Error(),
					map[string]string{"status": "échec"})
			} else {
				renommes++
				suivis = append(suivis, groups.Renamed{Before: ligne.Repo, After: apres.Info()})
				job.Line(ligne.Repo+" → "+ligne.Target,
					map[string]string{"status": "mis à jour"})
			}
			job.Progress(index+1, len(plan.Moves), ligne.Repo)
		}
		s.renamed(org, suivis)

		enregistre, err := s.classrooms.Save(arrivee.With(plan.Students...))
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"renamed": renommes, "failed": echecs,
			"scope": enregistre.Scope(), "students": len(plan.Students),
		}, nil
	})
	writeJSON(writer, http.StatusAccepted, job.State())
}

// importRequest lit l'organisation et le corps d'une demande d'importation.
func (s *Server) importRequest(request *http.Request) (string, importInput, error) {
	var body importInput
	org, err := valid.Login(request.PathValue("org"), "Organisation")
	if err != nil {
		return "", body, err
	}
	if err := decode(request, &body); err != nil {
		return "", body, err
	}
	if strings.TrimSpace(body.Prefix) == "" {
		return "", body, valid.Errorf("Aucun travail à reprendre n'a été indiqué.")
	}
	return org, body, nil
}

// profiles demande à GitHub le nom affiché des comptes d'un travail. C'est
// l'indice le plus sûr après le numéro d'étudiant, et il ne coûte qu'une
// requête par compte inconnu.
func (s *Server) profiles(org, prefix string, repos []groups.RepoInfo,
	proprietaires map[string]string) map[string]string {
	groupe := groups.Build(prefix, repos)
	pairs := make([]identity.Pair, 0, groupe.Len())
	for _, depot := range groupe.Repos {
		// Le compte des accès d'abord : demander le profil de ce que le nom
		// portait — « firebase-Wasim7Zed » — ne ramènerait rien.
		compte := depot.Suffix
		if login := proprietaires[depot.Name]; login != "" {
			compte = login
		}
		pairs = append(pairs, identity.Pair{Repo: compte, Login: compte})
	}
	if len(pairs) == 0 {
		return nil
	}
	noms := s.resolver(org).Resolve(pairs, true, nil)
	profils := map[string]string{}
	for compte, nom := range noms {
		if nom != "" {
			profils[strings.ToLower(compte)] = nom
		}
	}
	return profils
}
