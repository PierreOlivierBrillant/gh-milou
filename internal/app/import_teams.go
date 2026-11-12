package app

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Reprendre un travail d'équipe au terminal. Le parcours est celui de la
// reprise ordinaire — même choix de travail, mêmes dépôts retenus, même place
// d'arrivée — et s'en écarte là où il le doit : rien n'est rapproché d'une
// liste, puisque le dernier niveau du nom désigne une équipe.

// membres rend, pour chaque dépôt, qui l'a fait : l'équipe GitHub à qui il est
// partagé, ses collaborateurs directs, ses auteurs de commits. C'est « identity »
// qui décide de ce que chaque source vaut.
func (i *importSession) membres(prefixe string, seulement []string,
	repos []groups.RepoInfo) map[string]identity.Crew {
	noms := aLire(prefixe, seulement, repos)
	if len(noms) == 0 {
		return nil
	}
	resolveur := identity.New(i.session.Client, i.session.Cache, i.session.Options.Jobs)
	spin := ui.NewSpinner(i.session.Console, "Qui a fait quoi…")
	spin.Start()
	trouves := resolveur.Crews(i.org, noms, i.session.Viewer, nil)
	spin.Stop()
	return trouves
}

// aLire nomme les dépôts d'un préfixe dont les accès sont à lire.
func aLire(prefixe string, seulement []string, repos []groups.RepoInfo) []string {
	groupe := groups.Build(prefixe, repos)
	if groupe.Len() == 0 {
		return nil
	}
	gardes := map[string]bool{}
	for _, nom := range seulement {
		gardes[strings.ToLower(strings.TrimSpace(nom))] = true
	}
	noms := make([]string, 0, groupe.Len())
	for _, depot := range groupe.Repos {
		if len(gardes) > 0 && !gardes[strings.ToLower(depot.Name)] {
			continue
		}
		noms = append(noms, depot.Name)
	}
	return noms
}

// profilsDesMembres retrouve le nom affiché du profil GitHub de chaque membre.
// C'est l'indice le plus sûr après le numéro d'étudiant, et le seul dont on
// dispose quand la liste ne porte aucun compte.
func (i *importSession) profilsDesMembres(
	equipes map[string]identity.Crew) map[string]string {
	vus := map[string]bool{}
	pairs := make([]identity.Pair, 0, len(equipes))
	for _, crew := range equipes {
		for _, membre := range crew.Members {
			if vus[strings.ToLower(membre.Login)] {
				continue
			}
			vus[strings.ToLower(membre.Login)] = true
			pairs = append(pairs, identity.Pair{Repo: membre.Login, Login: membre.Login})
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	resolveur := identity.New(i.session.Client, i.session.Cache, i.session.Options.Jobs)
	spin := ui.NewSpinner(i.session.Console, "Profils GitHub…")
	spin.Start()
	trouves := resolveur.Resolve(pairs, true, nil)
	spin.Stop()

	profils := map[string]string{}
	for compte, nom := range trouves {
		if nom != "" {
			profils[strings.ToLower(compte)] = nom
		}
	}
	return profils
}

// enEquipe déroule la reprise d'un travail d'équipe, une fois le travail, les
// dépôts et la place choisis.
func (i *importSession) enEquipe(arrivee classroom.Classroom, prefixe, nom string,
	entrees []roster.Entry, repos []groups.RepoInfo) (int, error) {
	console := i.session.Console

	infos, err := i.session.Client.LoadOrgTeams(i.org, i.session.Options.Jobs)
	if err != nil {
		return ExitFailure, err
	}
	membres := i.membres(prefixe, i.retenus, repos)
	demande := classroom.TeamImportRequest{
		Prefix: prefixe, Name: nom, Only: i.retenus,
		Members:  membres,
		Known:    i.connus(),
		Existing: arrivee.Teams(infos),
		Chosen:   map[string][]string{},
		Entries:  entrees,
		Profiles: i.profilsDesMembres(membres),
		Guess:    true,
	}
	plan, err := classroom.PlanTeamImport(arrivee, demande, repos)
	if err != nil {
		return ExitValidation, err
	}
	i.montrerEquipes(plan)

	// Une composition que GitHub n'a pas su donner se tranche ici, avant que
	// quoi que ce soit ne soit écrit : c'est le moment où cela ne coûte rien.
	if i.session.Interactive() && !i.session.Options.Yes {
		corrige, err := i.composerAvantReprise(arrivee, demande, plan, repos)
		if err != nil {
			return ExitOK, err
		}
		plan = corrige
	}

	if i.session.Options.DryRun {
		console.Blank()
		console.Note("Simulation : rien n'a été écrit.")
		return ExitOK, nil
	}
	if !i.session.Options.Yes {
		suite, err := i.session.Prompt.Confirm(valid.Agree(len(plan.Moves),
			"Renommer le dépôt, composer son équipe et lui en donner l'accès ?",
			"Renommer "+itoa(len(plan.Moves))+" dépôts, composer leurs équipes et leur "+
				"en donner l'accès ?"), false)
		if err != nil {
			return ExitOK, err
		}
		if !suite {
			console.Warning("Annulé : rien n'a été renommé.")
			return ExitAborted, nil
		}
	}
	return i.appliquerEquipes(arrivee, plan)
}

// composerAvantReprise laisse corriger la composition d'une équipe tant que
// rien n'est écrit. Une équipe que rien n'a peuplée est le cas qui l'exige :
// ni accès, ni équipe GitHub, ni commit n'ont dit qui en était.
func (i *importSession) composerAvantReprise(arrivee classroom.Classroom,
	demande classroom.TeamImportRequest, plan classroom.TeamImport,
	repos []groups.RepoInfo) (classroom.TeamImport, error) {
	console := i.session.Console
	for {
		choix := make([]string, 0, 2*len(plan.Teams)+2)
		for _, equipe := range plan.Teams {
			choix = append(choix, equipe.Short,
				equipe.Short+" — "+membresEnMots(equipe.Members))
		}
		choix = append(choix, "", "Poursuivre sans rien changer")
		court, err := i.session.Prompt.Choose("Composer une équipe ?",
			ui.Options(choix...), "")
		if err != nil {
			return plan, err
		}
		if court == "" {
			return plan, nil
		}

		actuelle, _ := trouverEquipe(plan, court)
		reponse, err := i.session.Prompt.Ask(ui.Question{
			Title:      "Comptes GitHub de « " + court + " », séparés par des virgules",
			Default:    strings.Join(actuelle.Members, ", "),
			AllowEmpty: true,
		})
		if err != nil {
			return plan, err
		}
		demande.Chosen[court] = splitList(reponse)
		if plan, err = classroom.PlanTeamImport(arrivee, demande, repos); err != nil {
			return plan, err
		}
		console.Blank()
		i.montrerEquipes(plan)
	}
}

// trouverEquipe retrouve une équipe du plan par son nom court.
func trouverEquipe(plan classroom.TeamImport, court string) (classroom.ImportedTeam, bool) {
	for _, equipe := range plan.Teams {
		if strings.EqualFold(equipe.Short, court) {
			return equipe, true
		}
	}
	return classroom.ImportedTeam{}, false
}

// montrerEquipes récapitule la reprise avant toute écriture.
func (i *importSession) montrerEquipes(plan classroom.TeamImport) {
	console := i.session.Console
	console.Heading("Reprise en équipe de « " + plan.Prefix + " »")

	lignes := make([][]string, 0, len(plan.Teams))
	for _, equipe := range plan.Teams {
		etat := "à créer"
		if equipe.Exists {
			etat = "déjà là"
		}
		lignes = append(lignes, []string{
			equipe.Short, equipe.Repo + " → " + equipe.Target,
			membresAvecSources(equipe), etat,
		})
	}
	console.Table([]string{"Équipe", "Dépôt", "Membres", "État"}, lignes, 50)

	// Les équipes disent qui a fait le travail ; elles ne disent pas son nom.
	// C'est la liste du groupe qui le donne, et ce rapprochement-là se vérifie
	// comme celui d'un travail individuel.
	console.Blank()
	console.Heading("Rapprochement des comptes")
	comptes := make([][]string, 0, len(plan.Pairings))
	for _, trouve := range plan.Pairings {
		nom, raison := trouve.Entry.FullName, trouve.Reason
		switch {
		case trouve.Ambiguous:
			nom = console.Warn("à trancher : " + strings.Join(trouve.Rivals, ", "))
		case !trouve.Found():
			nom, raison = console.Warn("personne trouvée"), ""
		case trouve.Score < roster.Probable:
			nom = console.Warn(nom + " ?")
		}
		comptes = append(comptes, []string{"@" + trouve.Login, nom, raison})
	}
	console.Table([]string{"Compte", "Étudiant", "Reconnu par"}, comptes, 0)

	console.Blank()
	console.Printf("  %s %s « %s ».", console.OK(itoa(len(plan.Students))),
		valid.Agree(len(plan.Students), "personne rejoindra", "personnes rejoindront"), plan.Scope)
	if len(plan.Unmatched) > 0 {
		console.Printf("  %s : %s",
			console.Warn(valid.Plural(len(plan.Unmatched), "compte sans nom connu",
				"comptes sans nom connu")),
			console.Dim("inscrits sous leur compte ; leur nom se corrige ensuite"))
	}
	if len(plan.Absent) > 0 {
		console.Printf("  %s : %s",
			console.Warn(valid.Plural(len(plan.Absent), "étudiant dans aucune équipe",
				"étudiants dans aucune équipe")),
			console.Dim(strings.Join(plan.Absent, ", ")))
	}
	if len(plan.Silent) > 0 {
		console.Warning("Rien ne dit qui a fait %s — ni équipe GitHub, ni accès, "+
			"ni commit. Sans réponse, leur équipe naîtra vide.",
			strings.Join(plan.Silent, ", "))
	}
}

// appliquerEquipes écrit la reprise : les dépôts d'abord, les équipes ensuite,
// le partage en dernier. Un renommage qui échoue arrête tout ; une équipe qui
// échoue laisse des dépôts bien nommés, que « --team-share » achèvera.
func (i *importSession) appliquerEquipes(arrivee classroom.Classroom,
	plan classroom.TeamImport) (int, error) {
	console := i.session.Console

	// Le registre passe en premier : un nom qui n'y monterait pas ne serait
	// connu que de ce poste.
	if err := i.session.apprendre(i.org, plan.Students); err != nil {
		return ExitFailure, err
	}

	progress := ui.NewProgress(console, "Renommage", len(plan.Moves))
	renommes, echecs := 0, 0
	var suivis []groups.Renamed
	for index, ligne := range plan.Moves {
		apres, err := i.session.Client.RenameRepo(i.org, ligne.Repo, ligne.Target)
		if err != nil {
			progress.Clear()
			console.Failure("%s : %v", ligne.Repo, err)
			echecs++
		} else {
			renommes++
			suivis = append(suivis, groups.Renamed{Before: ligne.Repo, After: apres.Info()})
		}
		progress.Update(index+1, ligne.Repo)
	}
	progress.Finish("")

	var connu []groups.RepoInfo
	if i.session.Cache.Get(cache.ReposKey(i.org), cache.ReposTTL, &connu) && len(connu) > 0 {
		i.session.Cache.Set(cache.ReposKey(i.org), groups.WithRenamed(connu, suivis))
	}
	if echecs > 0 {
		console.Warning("%s, %d en échec. Les équipes n'ont pas été composées.",
			valid.Plural(renommes, "dépôt repris", "dépôts repris"), echecs)
		return ExitFailure, nil
	}

	faites, partages, ratees := i.composerEquipes(arrivee, plan)

	store := classroom.Open(classroom.PathNextTo(i.session.ConfigFile))
	if _, err := store.Save(arrivee.With(plan.Students...)); err != nil {
		return ExitFailure, err
	}
	if ratees > 0 {
		console.Warning("%s, %d en échec. Relancez avec « --team-share » pour achever le partage.",
			valid.Plural(faites, "équipe composée", "équipes composées"), ratees)
		return ExitFailure, nil
	}
	console.Success("%s dans « %s », %s, %s.",
		valid.Plural(renommes, "dépôt repris", "dépôts repris"), arrivee.Scope(),
		valid.Plural(faites, "équipe composée", "équipes composées"),
		valid.Plural(partages, "partage"))
	return ExitOK, nil
}

// composerEquipes crée les équipes, y inscrit leurs membres, et leur partage
// leur dépôt.
func (i *importSession) composerEquipes(arrivee classroom.Classroom,
	plan classroom.TeamImport) (faites, partages, ratees int) {
	console := i.session.Console
	droit := arrivee.Settings(plan.Name).Permission

	for _, equipe := range plan.Teams {
		slug, err := i.assurerEquipe(arrivee, equipe)
		if err != nil {
			ratees++
			console.Failure("équipe %s : %v", equipe.Short, err)
			continue
		}
		faites++
		console.Printf("  %s équipe %s — %s", console.OK("✓"), equipe.Short,
			membresEnMots(equipe.Members))

		if err := i.session.Client.GrantTeamRepo(
			arrivee.Org, slug, arrivee.Org, equipe.Target, droit); err != nil {
			console.Failure("%s : partage impossible — %v", equipe.Target, err)
			continue
		}
		partages++
	}
	return faites, partages, ratees
}

// assurerEquipe crée l'équipe si elle manque, puis la compose. Elle rend
// l'adresse GitHub de l'équipe, celle par laquelle on lui partage un dépôt.
func (i *importSession) assurerEquipe(arrivee classroom.Classroom,
	equipe classroom.ImportedTeam) (string, error) {
	infos, err := i.session.Client.LoadOrgTeams(arrivee.Org, i.session.Options.Jobs)
	if err != nil {
		return "", err
	}
	presentes := arrivee.Teams(infos)
	trouvee, existe := teams.Find(presentes, equipe.Short)
	if !existe {
		cree, err := i.session.Client.CreateTeam(arrivee.Org, equipe.Name,
			teams.Describe(arrivee.Session, arrivee.Course, arrivee.Group, equipe.Short),
			teams.Privacy)
		if err != nil {
			return "", err
		}
		trouvee, _ = teams.Read(*cree)
		presentes = append(presentes, trouvee)
	}

	etapes, err := teams.PlanCompose(presentes, equipe.Short, equipe.Members)
	if err != nil {
		return trouvee.Slug, err
	}
	for _, etape := range etapes {
		if etape.Kind == teams.Join {
			err = i.session.Client.AddTeamMember(arrivee.Org, etape.Slug,
				etape.Username, teams.MemberRole)
		} else {
			err = i.session.Client.RemoveTeamMember(arrivee.Org, etape.Slug, etape.Username)
		}
		if err != nil {
			return trouvee.Slug, err
		}
	}
	return trouvee.Slug, nil
}

// membresAvecSources dit la composition d'une équipe et d'où chacun vient : une
// composition devinée doit pouvoir être démentie, et pour cela il faut voir sur
// quoi elle repose.
func membresAvecSources(equipe classroom.ImportedTeam) string {
	if len(equipe.Members) == 0 {
		return "personne : ni équipe, ni accès, ni commit"
	}
	comptes := make([]string, 0, len(equipe.Members))
	for _, compte := range equipe.Members {
		if source := equipe.Sources[compte]; source != "" {
			comptes = append(comptes, "@"+compte+" ("+source+")")
			continue
		}
		comptes = append(comptes, "@"+compte)
	}
	return strings.Join(comptes, ", ")
}

// membresEnMots dit la composition d'une équipe.
func membresEnMots(membres []string) string {
	if len(membres) == 0 {
		return "aucun membre connu"
	}
	comptes := make([]string, 0, len(membres))
	for _, compte := range membres {
		comptes = append(comptes, "@"+compte)
	}
	return strings.Join(comptes, ", ")
}
