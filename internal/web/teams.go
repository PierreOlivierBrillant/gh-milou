package web

import (
	"net/http"
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Les équipes d'un groupe. Elles vivent sur GitHub — ce sont de vraies équipes
// d'organisation —, et rien n'en est retenu localement : leur nom dit à quel
// groupe elles appartiennent, leur composition dit qui en est. Cette façade ne
// fait que traduire des requêtes en écritures ; ce qui est permis, ce qui se
// heurte, ce qui doit précéder quoi, tout cela se décide dans « teams ».

// squadOf résout le groupe demandé et ses équipes.
func (s *Server) squadOf(request *http.Request) (classroom.Classroom, []teams.Team, error) {
	cours, err := s.place(request)
	if err != nil {
		return cours, nil, err
	}
	infos, err := s.orgTeams(cours.Org, request.URL.Query().Get("refresh") == "1")
	if err != nil {
		return cours, nil, err
	}
	return cours, cours.Teams(infos), nil
}

// teamsIn renvoie les équipes d'un groupe.
func (s *Server) teamsIn(cours classroom.Classroom) ([]teams.Team, error) {
	infos, err := s.orgTeams(cours.Org, false)
	if err != nil {
		return nil, err
	}
	return cours.Teams(infos), nil
}

// handleTeams liste les équipes du groupe, leurs membres nommés, et les
// étudiants qu'aucune équipe n'accueille encore.
func (s *Server) handleTeams(writer http.ResponseWriter, request *http.Request) {
	cours, equipes, err := s.squadOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	// Le nombre de dépôts rendus accompagne chaque équipe : c'est ce qu'une
	// suppression emporterait, et on ne le coche pas à l'aveugle.
	tous, _, err := s.repos(cours.Org, false)
	if err != nil {
		fail(writer, err)
		return
	}
	rendus := make(map[string]int, len(equipes))
	for _, equipe := range equipes {
		if compte := len(cours.TeamRepos(equipe, tous)); compte > 0 {
			rendus[equipe.Short] = compte
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"teams": cours.Describe(equipes), "unassigned": cours.Unassigned(equipes),
		"repos": rendus, "scope": cours.Scope(),
	})
}

// handleLooseTeams propose les équipes de l'organisation qui ne relèvent
// d'aucun groupe : celles qu'on peut adopter.
func (s *Server) handleLooseTeams(writer http.ResponseWriter, request *http.Request) {
	org, err := valid.Login(request.PathValue("org"), "Organisation")
	if err != nil {
		fail(writer, err)
		return
	}
	infos, err := s.orgTeams(org, request.URL.Query().Get("refresh") == "1")
	if err != nil {
		fail(writer, err)
		return
	}
	libres := teams.Loose(infos)
	writeJSON(writer, http.StatusOK, map[string]any{
		"teams": libres, "total": len(infos),
	})
}

// teamInput décrit une équipe à créer, avec sa composition initiale.
type teamInput struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// handleCreateTeam crée une équipe du groupe sur GitHub et y inscrit ses
// membres. L'équipe est créée d'abord, la composition ensuite : une inscription
// qui échoue laisse une équipe vide, qu'on complète — jamais une équipe qu'on
// ne saurait pas retrouver.
func (s *Server) handleCreateTeam(writer http.ResponseWriter, request *http.Request) {
	cours, equipes, err := s.squadOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	var body teamInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	short, err := teams.ShortName(body.Name)
	if err != nil {
		fail(writer, err)
		return
	}
	if err := teams.Available(equipes, short); err != nil {
		fail(writer, err)
		return
	}
	membres, err := s.members(cours, body.Members)
	if err != nil {
		fail(writer, err)
		return
	}

	cree, err := s.deps.Client.CreateTeam(cours.Org, cours.TeamName(short),
		teams.Describe(cours.Session, cours.Course, cours.Group, short), teams.Privacy)
	if err != nil {
		fail(writer, err)
		return
	}
	s.forgetTeams(cours.Org)

	neuve, _ := teams.Read(*cree)
	etapes, err := teams.PlanAssign(append(equipes, neuve), short, membres)
	if err != nil && len(membres) > 0 {
		fail(writer, err)
		return
	}
	applied, echecs := s.applyTeamSteps(cours.Org, etapes)
	writeJSON(writer, http.StatusCreated, map[string]any{
		"team": neuve, "joined": applied, "failed": echecs,
	})
}

// handleRenameTeam renomme une équipe du groupe. Le nom court change ; la place,
// elle, ne bouge pas — une équipe ne change pas de groupe.
func (s *Server) handleRenameTeam(writer http.ResponseWriter, request *http.Request) {
	cours, equipes, err := s.squadOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	equipe, cible, err := teams.PlanRename(equipes, request.PathValue("team"), body.Name)
	if err != nil {
		fail(writer, err)
		return
	}
	parts, _ := teams.Read(teams.Info{Name: cible})
	renommee, err := s.deps.Client.UpdateTeam(cours.Org, equipe.Slug, cible,
		teams.Describe(cours.Session, cours.Course, cours.Group, parts.Short))
	if err != nil {
		fail(writer, err)
		return
	}
	s.forgetTeams(cours.Org)
	lue, _ := teams.Read(*renommee)
	lue.Members = equipe.Members
	writeJSON(writer, http.StatusOK, map[string]any{
		"team": lue, "previous": equipe.Short,
	})
}

// handleDeleteTeam supprime une équipe. Ses dépôts restent : c'est l'accès qui
// disparaît, et le travail qu'elle a rendu reste lisible sous son nom.
func (s *Server) handleDeleteTeam(writer http.ResponseWriter, request *http.Request) {
	cours, equipes, err := s.squadOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	var body struct {
		// Repos demande de supprimer aussi les dépôts que l'équipe a rendus.
		// Ils ne disparaissent pas avec elle : ce sont des dépôts comme les
		// autres, et le travail qu'ils portent survit à l'équipe qui l'a fait.
		Repos bool `json:"repos"`
		// Confirm redit le nom de l'équipe. La suppression d'un dépôt l'exige
		// déjà ; en supprimer plusieurs d'un coup ne peut pas l'exiger moins.
		Confirm string `json:"confirm"`
	}
	// Un corps absent vaut « l'équipe seule » : supprimer une équipe n'a
	// jamais rien demandé d'autre que son nom dans l'adresse.
	_ = decode(request, &body)
	equipe, trouvee := teams.Find(equipes, request.PathValue("team"))
	if !trouvee {
		fail(writer, valid.Errorf("Aucune équipe « %s » dans ce groupe.",
			strings.TrimSpace(request.PathValue("team"))))
		return
	}

	var depots []string
	if body.Repos {
		if strings.TrimSpace(body.Confirm) != equipe.Short {
			fail(writer, valid.Errorf(
				"Confirmation incorrecte : retapez « %s » exactement.", equipe.Short))
			return
		}
		if present, known := s.deps.Client.HasScope("delete_repo"); known && !present {
			failScope(writer, "delete_repo",
				"Le jeton n'a pas la portée « delete_repo » : la suppression serait refusée.")
			return
		}
		tous, _, err := s.repos(cours.Org, false)
		if err != nil {
			fail(writer, err)
			return
		}
		depots = cours.TeamRepos(equipe, tous)
	}

	// Les dépôts d'abord : l'équipe supprimée, plus rien ne dirait lesquels
	// étaient les siens.
	supprimes := 0
	for _, depot := range depots {
		if err := s.deps.Client.DeleteRepo(cours.Org, depot); err != nil {
			fail(writer, err)
			return
		}
		s.deleted(cours.Org, depot)
		supprimes++
	}
	if err := s.deps.Client.DeleteTeam(cours.Org, equipe.Slug); err != nil {
		fail(writer, err)
		return
	}
	s.forgetTeams(cours.Org)

	message := "« " + equipe.Label() + " » supprimée. Ses dépôts restent sur GitHub ; " +
		"seul l'accès qu'elle donnait a disparu."
	switch {
	case body.Repos && supprimes == 0:
		message = "« " + equipe.Label() + " » supprimée. Elle n'avait aucun dépôt."
	case body.Repos && supprimes == 1:
		message = "« " + equipe.Label() + " » supprimée, avec son dépôt."
	case body.Repos:
		message = "« " + equipe.Label() + " » supprimée, avec ses " +
			itoa(supprimes) + " dépôts."
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"team": equipe.Short, "repos": supprimes, "message": message,
	})
}

// handleMoveTeam fait passer une équipe dans un autre groupe.
//
// Une équipe appartient à un groupe : son nom le dit, ses membres en font
// partie, et ses dépôts portent la place de ce groupe. La déplacer emporte donc
// les trois — sans quoi il resterait des dépôts d'un groupe rendus par une
// équipe d'un autre, et plus rien ne se lirait.
//
// Les dépôts sont renommés d'abord, l'équipe et les listes ensuite : c'est
// GitHub qui dit à quel groupe un dépôt appartient, et une liste qui aurait
// bougé sans lui décrirait un rangement qui n'a pas eu lieu.
func (s *Server) handleMoveTeam(writer http.ResponseWriter, request *http.Request) {
	var body moveInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	depart, equipes, err := s.squadOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	equipe, trouvee := teams.Find(equipes, request.PathValue("team"))
	if !trouvee {
		fail(writer, valid.Errorf("Aucune équipe « %s » dans ce groupe.",
			strings.TrimSpace(request.PathValue("team"))))
		return
	}
	repos, _, err := s.repos(depart.Org, false)
	if err != nil {
		fail(writer, err)
		return
	}
	depart = s.enrichi(depart, repos)

	arrivee, neuf, err := s.moveTarget(body.Target, body.NewGroup, repos)
	if err != nil {
		fail(writer, err)
		return
	}
	if classroom.NormalizeScope(depart.Scope()) == classroom.NormalizeScope(arrivee.Scope()) {
		fail(writer, valid.Errorf("Le groupe d'arrivée est celui de départ."))
		return
	}
	// L'arrivée doit pouvoir l'accueillir sous son nom : deux équipes d'un même
	// groupe ne peuvent pas le partager, et le renommage échouerait sur GitHub.
	deja, err := s.teamsIn(arrivee)
	if err != nil {
		fail(writer, err)
		return
	}
	if err := teams.Available(deja, equipe.Short); err != nil {
		fail(writer, err)
		return
	}

	membres := depart.TeamMovers(equipe)
	renommages, err := classroom.PlanMoveTeam(depart, arrivee, equipe, membres, repos)
	if err != nil {
		fail(writer, err)
		return
	}

	bilan := map[string]any{
		"team": equipe.Short, "moved": comptes(membres), "count": len(membres),
		"target": arrivee.Label(), "target_scope": arrivee.Scope(),
		"created": neuf, "renamed": 0, "failed": 0,
	}

	// Sans dépôt à renommer, rien ne peut contredire ce qu'on écrit : l'équipe
	// et les fiches suivent tout de suite.
	if len(renommages) == 0 {
		if err := s.equipeSuit(depart, arrivee, equipe, membres); err != nil {
			fail(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, bilan)
		return
	}

	label := "Déplacement de « " + equipe.Label() + " » vers « " + arrivee.Label() + " »"
	job := s.jobs.Start("deplacement", label, func(job *Job) (any, error) {
		renommes, echecs := 0, 0
		var suivis []groups.Renamed
		for index, ligne := range renommages {
			if job.Canceled() {
				break
			}
			apres, err := s.deps.Client.RenameRepo(depart.Org, ligne.Repo, ligne.Target)
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
			job.Progress(index+1, len(renommages), ligne.Repo)
		}
		s.renamed(depart.Org, suivis)
		bilan["renamed"], bilan["failed"] = renommes, echecs

		if echecs > 0 || job.Canceled() {
			job.Warn("L'équipe n'a pas bougé : tous ses dépôts n'ont pas suivi.")
			bilan["moved"], bilan["count"] = []string{}, 0
			return bilan, nil
		}
		if err := s.equipeSuit(depart, arrivee, equipe, membres); err != nil {
			return nil, err
		}
		return bilan, nil
	})
	writeJSON(writer, http.StatusAccepted, job.State())
}

// equipeSuit fait suivre l'équipe et ses membres une fois les dépôts arrivés :
// l'équipe est renommée à la place du groupe d'arrivée, et les fiches changent
// de liste.
func (s *Server) equipeSuit(depart, arrivee classroom.Classroom, equipe teams.Team,
	membres []roster.Person) error {
	nom := arrivee.TeamName(equipe.Short)
	if _, err := s.deps.Client.UpdateTeam(arrivee.Org, equipe.Slug, nom,
		teams.Describe(arrivee.Session, arrivee.Course, arrivee.Group, equipe.Short)); err != nil {
		return err
	}
	s.forgetTeams(depart.Org)
	if len(membres) == 0 {
		return nil
	}
	return s.suivent(depart, arrivee, membres)
}

// handleAdoptTeam fait entrer dans le groupe une équipe déjà présente dans
// l'organisation, en la renommant. Elle garde ses membres et ses accès : c'est
// ce qui permet de reprendre un travail d'équipe commencé sans l'outil.
func (s *Server) handleAdoptTeam(writer http.ResponseWriter, request *http.Request) {
	cours, equipes, err := s.squadOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	var body struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	infos, err := s.orgTeams(cours.Org, false)
	if err != nil {
		fail(writer, err)
		return
	}
	source, cible, err := teams.PlanAdopt(equipes, infos, body.Slug, body.Name,
		cours.Session, cours.Course, cours.Group)
	if err != nil {
		fail(writer, err)
		return
	}
	parts, _ := teams.Read(teams.Info{Name: cible})
	adoptee, err := s.deps.Client.UpdateTeam(cours.Org, source.Slug, cible,
		teams.Describe(cours.Session, cours.Course, cours.Group, parts.Short))
	if err != nil {
		fail(writer, err)
		return
	}
	s.forgetTeams(cours.Org)
	lue, _ := teams.Read(*adoptee)
	lue.Members = source.Members
	writeJSON(writer, http.StatusOK, map[string]any{
		"team": lue, "previous": source.Name,
	})
}

// handleAssignTeam inscrit des personnes dans une équipe. Celles qui étaient
// dans une autre la quittent : c'est le déplacement d'une équipe à l'autre, et
// il ne demande rien de plus que de dire où l'on va.
func (s *Server) handleAssignTeam(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Team      string   `json:"team"`
		Usernames []string `json:"usernames"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	s.applyMembership(writer, request, body.Team, body.Usernames, teams.PlanAssign)
}

// handleComposeTeam donne à une équipe la composition exacte demandée.
func (s *Server) handleComposeTeam(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Usernames []string `json:"usernames"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	s.applyMembership(writer, request, request.PathValue("team"), body.Usernames,
		teams.PlanCompose)
}

// handleLeaveTeam retire une personne de son équipe. Elle reste du groupe :
// n'être dans aucune équipe n'est pas en être exclu.
func (s *Server) handleLeaveTeam(writer http.ResponseWriter, request *http.Request) {
	s.applyMembership(writer, request, request.PathValue("team"),
		[]string{request.PathValue("login")}, teams.PlanRemove)
}

// applyMembership compose puis applique un changement de composition. Le plan
// est établi en entier avant la première écriture : une composition impossible
// est refusée plutôt qu'appliquée à moitié.
func (s *Server) applyMembership(writer http.ResponseWriter, request *http.Request,
	target string, usernames []string,
	planifier func([]teams.Team, string, []string) ([]teams.Step, error)) {
	cours, equipes, err := s.squadOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	membres, err := s.members(cours, usernames)
	if err != nil {
		fail(writer, err)
		return
	}
	etapes, err := planifier(equipes, target, membres)
	if err != nil {
		fail(writer, err)
		return
	}
	applied, echecs := s.applyTeamSteps(cours.Org, etapes)
	if len(echecs) > 0 {
		writeJSON(writer, http.StatusBadGateway, map[string]any{
			"error": "Composition partiellement appliquée : " + strings.Join(echecs, " ; "),
			"steps": applied,
		})
		return
	}
	infos, err := s.orgTeams(cours.Org, false)
	if err != nil {
		fail(writer, err)
		return
	}
	apres := cours.Teams(infos)
	writeJSON(writer, http.StatusOK, map[string]any{
		"steps": applied, "teams": cours.Describe(apres),
		"unassigned": cours.Unassigned(apres),
	})
}

// applyTeamSteps exécute les écritures d'une composition et renvoie ce qui a
// été fait, puis ce qui a échoué.
func (s *Server) applyTeamSteps(org string, etapes []teams.Step) ([]teams.Step, []string) {
	applied := make([]teams.Step, 0, len(etapes))
	var echecs []string
	for _, etape := range etapes {
		var err error
		if etape.Kind == teams.Join {
			err = s.deps.Client.AddTeamMember(org, etape.Slug, etape.Username, teams.MemberRole)
		} else {
			err = s.deps.Client.RemoveTeamMember(org, etape.Slug, etape.Username)
		}
		if err != nil {
			echecs = append(echecs, "@"+etape.Username+" ("+etape.Kind+") : "+err.Error())
			continue
		}
		applied = append(applied, etape)
	}
	if len(applied) > 0 {
		s.forgetTeams(org)
	}
	return applied, echecs
}

// members vérifie que les comptes donnés sont bien des étudiants du groupe.
// Inscrire quelqu'un d'étranger au groupe dans une de ses équipes lui donnerait
// accès à des dépôts sans qu'aucune liste ne le mentionne.
func (s *Server) members(cours classroom.Classroom, usernames []string) ([]string, error) {
	propres := make([]string, 0, len(usernames))
	for _, brut := range usernames {
		if strings.TrimSpace(brut) == "" {
			continue
		}
		username, err := valid.Login(brut, "Compte GitHub")
		if err != nil {
			return nil, err
		}
		if _, inscrit := cours.Find(username); !inscrit {
			return nil, valid.Errorf(
				"@%s n'est pas dans « %s » : inscrivez-le au groupe avant de lui donner une équipe.",
				username, cours.Label())
		}
		// Le compte demandé, non celui qui désigne la personne : c'est sous
		// celui-là qu'elle entre dans l'équipe, et lui substituer son compte
		// principal la refuserait dès que ce dernier y est déjà.
		propres = append(propres, username)
	}
	return propres, nil
}

// ------------------------------------------------- accès des dépôts d'équipe

// handleShareAssignment redonne à chaque équipe l'accès au dépôt qui porte son
// nom. C'est ce qui achève l'adoption d'un travail fait en équipe avant
// l'outil : les dépôts sont déjà là et bien nommés, mais rien ne les a jamais
// partagés avec l'équipe.
func (s *Server) handleShareAssignment(writer http.ResponseWriter, request *http.Request) {
	cours, id, repos, err := s.assignmentOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	equipes, err := s.teamsIn(cours)
	if err != nil {
		fail(writer, err)
		return
	}
	droit := cours.Settings(cours.ShortName(id)).Permission

	type partage struct {
		Repo string
		Slug string
		Team string
	}
	var partages []partage
	for _, depot := range cours.Repos(id, repos) {
		equipe, appartient := cours.TeamOf(depot.Name, equipes)
		if !appartient {
			continue
		}
		partages = append(partages, partage{Repo: depot.Name, Slug: equipe.Slug, Team: equipe.Short})
	}
	if len(partages) == 0 {
		fail(writer, valid.Errorf(
			"Aucun dépôt de « %s » ne porte le nom d'une équipe du groupe.", cours.ShortName(id)))
		return
	}

	label := "Partage de « " + cours.ShortName(id) + " » avec " +
		valid.Plural(len(partages), "équipe")
	job := s.jobs.Start("partage", label, func(job *Job) (any, error) {
		faits, echecs := 0, 0
		for index, item := range partages {
			if job.Canceled() {
				break
			}
			err := s.deps.Client.GrantTeamRepo(cours.Org, item.Slug, cours.Org, item.Repo, droit)
			if err != nil {
				echecs++
				job.Line(item.Repo+" : échec — "+err.Error(), map[string]string{"status": "échec"})
			} else {
				faits++
				job.Line(item.Repo+" → équipe "+item.Team, map[string]string{"status": "partagé"})
			}
			job.Progress(index+1, len(partages), item.Repo)
		}
		return map[string]any{"shared": faits, "failed": echecs, "permission": droit}, nil
	})
	writeJSON(writer, http.StatusAccepted, job.State())
}

// ------------------------------------------------------------------ affichage

// teamNames rend les noms courts d'équipes, triés, pour un message.
func teamNames(list []teams.Team) []string {
	noms := teams.Names(list)
	sort.Strings(noms)
	return noms
}
