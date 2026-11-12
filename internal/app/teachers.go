package app

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Cloisonner un groupe au terminal, c'est la même opération qu'au navigateur :
// le plan se montre, puis s'écrit. Ce qu'il contient est décidé dans
// « classroom » — ici, on ne fait que le dire et l'appliquer.

// teachingMode écrit l'état du cloisonnement d'un groupe, et l'applique si une
// composition est demandée.
//
// Le groupe se désigne par sa place — « a26.5n6.01 » —, comme pour les
// équipes : l'équipe enseignante appartient à un groupe, pas à un lot de
// dépôts.
func (s *Session) teachingMode(place string) (int, error) {
	if strings.TrimSpace(place) == "" {
		chosen, err := s.askPlace("Quel groupe cloisonner ?")
		if err != nil {
			return ExitOK, err
		}
		place = chosen
	}
	bureau, err := s.teamDesk(place)
	if err != nil {
		return ExitValidation, err
	}
	cours := bureau.cours
	repos, err := s.orgRepos(cours.Org, false)
	if err != nil {
		return ExitFailure, err
	}
	set, _ := s.names(cours.Org)
	s.printTeaching(cours, cours.TeachingOf(bureau.infos, repos, set), set)

	voulus, demande := s.Options.Teachers, s.Options.TeachersOn
	if !demande {
		if !s.Interactive() {
			return ExitOK, nil
		}
		voulus, err = s.askTeachers(cours, bureau.infos, set)
		if err != nil || voulus == nil {
			return ExitOK, err
		}
	}
	return s.applyTeaching(cours, bureau.infos, repos, set, voulus)
}

// printTeaching écrit qui enseigne le groupe, et ce que l'équipe ne ferme pas.
func (s *Session) printTeaching(cours classroom.Classroom,
	etat classroom.Teaching, set *registry.Set) {
	console := s.Console
	console.Heading("Équipe enseignante de « " + cours.Scope() + " »")
	console.Printf("  %s %s", console.Dim(pad("Équipe GitHub", 18)), etat.Name)

	if !etat.Cloistered() {
		console.Warning("Ce groupe n'est pas cloisonné : %s selon ce que l'organisation "+
			"accorde d'office.", valid.Agree(etat.Repos, "son dépôt se lit",
			"ses "+itoa(etat.Repos)+" dépôts se lisent"))
	} else {
		rows := make([][]string, 0, len(etat.Teachers))
		for _, personne := range etat.Teachers {
			nom := personne.FullName
			if nom == "" {
				nom = console.Dim("nom inconnu")
			}
			etiquette := ""
			if contientCompte(etat.Waiting, personne.Username) {
				etiquette = console.Dim("invité, pas encore accepté")
			}
			rows = append(rows, []string{nom, "@" + personne.Username, etiquette})
		}
		console.Table([]string{"Enseignant", "Compte", ""}, rows, 40)
		console.Printf("  %s %s en « %s »", console.Dim(pad("Accès", 18)),
			valid.Plural(etat.Repos, "dépôt accordé", "dépôts accordés"), classroom.TeacherPermission)
	}

	// L'équipe ouvre un accès ; elle n'en ferme aucun. Le taire laisserait
	// croire à un cloisonnement qui n'existe pas.
	console.Note("%s", classroom.OwnersSeeAll)
	if len(set.Teachers()) == 0 {
		console.Warning("Aucun enseignant n'est reconnu dans « %s » : ouvrez la fiche "+
			"de quelqu'un (--user <compte> --teacher) avant de cloisonner.", cours.Org)
	}
}

// askTeachers propose les enseignants de l'organisation et recueille ceux qui
// enseignent ce groupe. Un choix vide rend nil : il n'y a rien à faire.
func (s *Session) askTeachers(cours classroom.Classroom, infos []teams.Info,
	set *registry.Set) ([]string, error) {
	enseignants := set.Teachers()
	if len(enseignants) == 0 {
		return nil, nil
	}
	changer, err := s.Prompt.Confirm(
		"Régler l'équipe enseignante de « "+cours.Scope()+" » ?", false)
	if err != nil || !changer {
		return nil, err
	}

	equipe, existe := cours.TeacherTeam(infos)
	options := make([]ui.Option, 0, len(enseignants))
	coches := make([]bool, 0, len(enseignants))
	for _, fiche := range enseignants {
		nom := fiche.FullName
		if nom == "" {
			nom = fiche.Username
		}
		options = append(options, ui.Option{
			Value: fiche.Username, Label: nom + "  (@" + fiche.Username + ")"})
		coches = append(coches, existe && equipe.Has(fiche.Username))
	}
	rangs, err := s.Prompt.MultiSelect("Qui enseigne ce groupe ?", options, coches)
	if err != nil {
		return nil, err
	}
	// Une composition vide reste une composition : elle retire tout le monde.
	// C'est « ne rien régler » qui rend nil, et c'est la question d'avant.
	voulus := make([]string, 0, len(rangs))
	for _, rang := range rangs {
		voulus = append(voulus, options[rang].Value)
	}
	return voulus, nil
}

// applyTeaching montre le plan puis l'écrit.
func (s *Session) applyTeaching(cours classroom.Classroom, infos []teams.Info,
	repos []groups.RepoInfo, set *registry.Set, voulus []string) (int, error) {
	// S'exclure du groupe qu'on cloisonne ferait perdre l'accès sans aucun
	// chemin pour y revenir.
	if len(voulus) > 0 && set.Teaches(s.Viewer) && !contientCompte(voulus, s.Viewer) {
		return ExitValidation, valid.Errorf(
			"@%s ne figure pas dans la composition demandée : vous perdriez l'accès à "+
				"« %s » sans pouvoir y revenir. Ajoutez-vous, ou faites-le faire par un "+
				"collègue déjà inscrit.", s.Viewer, cours.Scope())
	}
	plan, err := cours.PlanTeaching(infos, repos, voulus, set)
	if err != nil {
		return ExitValidation, err
	}
	if plan.Empty() {
		s.Console.Note("Rien à faire : l'équipe enseignante de « %s » est déjà celle-là.",
			cours.Scope())
		return ExitOK, nil
	}

	s.Console.Heading("Ce qui sera écrit")
	for _, etape := range plan.Steps {
		s.Console.Printf("  %s %s", s.Console.Dim(pad(etape.Kind, 14)), etape.Target)
	}
	if s.Options.DryRun {
		s.Console.Note("Simulation : rien n'a été écrit.")
		return ExitOK, nil
	}
	if !s.Options.Yes {
		valide, err := s.Prompt.Confirm(valid.Agree(len(plan.Steps), "Appliquer cette écriture ?",
			"Appliquer ces "+itoa(len(plan.Steps))+" écritures ?"), false)
		if err != nil || !valide {
			return ExitAborted, err
		}
	}

	slug := plan.Slug
	for _, etape := range plan.Steps {
		if slug, err = s.writeTeachingStep(cours, slug, plan, etape); err != nil {
			return ExitFailure, err
		}
		s.Console.Printf("  %s %s", s.Console.OK("✓"), etape.Kind+" : "+etape.Target)
	}
	s.invalidateCaches()
	s.Console.Success("« %s » est cloisonné derrière « %s ».", cours.Scope(), plan.Team)
	return ExitOK, nil
}

// writeTeachingStep exécute une étape et rend le slug de l'équipe, qui n'est
// connu qu'une fois celle-ci créée.
func (s *Session) writeTeachingStep(cours classroom.Classroom, slug string,
	plan classroom.TeachingPlan, etape classroom.TeachingStep) (string, error) {
	switch etape.Kind {
	case classroom.CreateTeam:
		info, err := s.Client.CreateTeam(
			cours.Org, plan.Team, plan.Description, teams.Privacy)
		if err != nil {
			return slug, err
		}
		return info.Slug, nil
	case classroom.JoinTeam:
		// Un enseignant est responsable de son équipe : il doit pouvoir y
		// inscrire un collègue sans passer par un propriétaire.
		return slug, s.Client.AddTeamMember(cours.Org, slug, etape.Target, "maintainer")
	case classroom.LeaveTeam:
		return slug, s.Client.RemoveTeamMember(cours.Org, slug, etape.Target)
	case classroom.GrantRepo:
		return slug, s.Client.GrantTeamRepo(
			cours.Org, slug, cours.Org, etape.Target, plan.Permission)
	}
	return slug, valid.Errorf("Étape inconnue : « %s ».", etape.Kind)
}

// contientCompte dit si un compte figure dans une liste, casse ignorée.
func contientCompte(liste []string, valeur string) bool {
	for _, item := range liste {
		if strings.EqualFold(strings.TrimSpace(item), valeur) {
			return true
		}
	}
	return false
}
