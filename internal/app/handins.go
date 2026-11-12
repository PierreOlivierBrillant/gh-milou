package app

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/users"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// La date cible et les remises, au terminal. L'assistant gère un préfixe : un
// travail — « a26.5n6.01.tp1 » —, mais aussi un groupe entier, un cours ou une
// session, qui en réunissent plusieurs. Chaque dépôt porte sa place dans son
// nom, et c'est par elle qu'il retrouve son groupe : sa liste, ses équipes, et
// l'échéance de son propre travail.
//
// Un préfixe hérité — « tp1-jlpicard » et ses semblables — ne dit pas à quel
// groupe il appartient : les commits s'y comptent quand même, mais rien ne s'y
// rapporte à une date cible. Le déplacer vers une place lui donne le reste.

// travail résout le groupe et le nom du travail que le préfixe géré désigne.
// Le dernier retour vaut faux pour un préfixe qui ne désigne pas un travail : un
// préfixe hérité, ou un groupe entier qui en réunit plusieurs.
func (m *manageSession) travail(group *groups.Group) (classroom.Classroom, string, bool) {
	place, nom, reconnu := naming.SplitAssignment(group.Prefix)
	if !reconnu {
		return classroom.Classroom{}, "", false
	}
	groupes := m.composer(place)
	if len(groupes) == 0 {
		return classroom.Classroom{}, "", false
	}
	return groupes[0], nom, true
}

// groupesDe compose les groupes auxquels appartiennent les dépôts listés. Un
// travail n'en touche qu'un, mais un cours ou une session en réunissent
// plusieurs : chaque dépôt se nomme et se juge ensuite par le sien, que
// « classroom.Of » retrouve. Un dépôt hors nomenclature n'appartient à aucun.
func (m *manageSession) groupesDe(group *groups.Group) []classroom.Classroom {
	infos := make([]groups.RepoInfo, 0, group.Len())
	for _, repo := range group.Repos {
		infos = append(infos, groups.RepoInfo{Name: repo.Name})
	}
	return m.composer(classroom.Places(infos)...)
}

// composer compose les groupes qui occupent ces places, comme le navigateur le
// fait : ce que ce poste en a déclaré, puis ce que le registre en sait. Le
// registre répond aux trois questions qu'un groupe lui pose : qui se cache
// derrière un nom de dépôt, quand chaque travail est attendu, et qui enseigne —
// sans quoi un commit d'enseignant daterait la remise. Il n'est lu qu'une fois
// pour toutes les places : une session en compte une dizaine.
func (m *manageSession) composer(places ...string) []classroom.Classroom {
	if len(places) == 0 {
		return nil
	}
	store := m.session.groupStore()
	set, _ := m.session.names(m.org)
	defauts := classroom.DefaultsFrom(m.session.Settings)
	trouves := make([]classroom.Classroom, 0, len(places))
	for _, place := range places {
		cours, err := classroom.AtScope(m.org, place, defauts)
		if err != nil {
			continue
		}
		if connu, existe := store.Find(cours.Org, cours.Scope()); existe {
			cours = connu
		}
		trouves = append(trouves, cours.Scheduling(set).Staffing(set).Enrich(set, m.repos))
	}
	return trouves
}

// equipesDe rend les équipes d'un groupe. Celles de l'organisation sont lues une
// fois pour la séance : un cours en demande pour chacun de ses groupes, et la
// liste s'affiche trop souvent pour les redemander. Leur absence n'empêche
// rien : le travail est alors individuel, et c'est déjà ce que la liste vide
// dit.
func (m *manageSession) equipesDe(cours classroom.Classroom) []teams.Team {
	if !m.equipesLues {
		m.equipesLues = true
		m.infos, _ = m.session.Client.LoadOrgTeams(cours.Org, m.session.Options.Jobs)
	}
	return cours.Teams(m.infos)
}

// bilans confronte les dépôts affichés à ce qu'on sait déjà de leurs
// historiques. Rien n'est demandé à GitHub : c'est « remises » qui va chercher,
// et la liste ne montre que ce qui a déjà été relevé — et qu'aucun envoi n'a
// dépassé depuis. C'est l'inventaire qui le dit, pas l'âge du relevé : la même
// règle qu'au navigateur, décidée au même endroit.
//
// Chaque dépôt se juge sur l'échéance de son propre travail. Un groupe entier
// en mêle plusieurs, et une seule date — ou aucune — y dirait « remis » d'une
// copie que la page de son travail dit en retard.
func (m *manageSession) bilans(group *groups.Group) map[string]classroom.Review {
	noms := make([]string, 0, group.Len())
	for _, repo := range group.Repos {
		noms = append(noms, repo.Name)
	}
	remises := groups.Complete(m.repos, m.resolver.Histories(m.org, noms))
	if len(remises) == 0 {
		return nil
	}
	groupes := m.groupesDe(group)
	set, _ := m.session.names(m.org)
	trouves := make(map[string]classroom.Review, len(remises))
	for repo, remise := range remises {
		if cours, place := classroom.Of(groupes, repo); place {
			trouves[repo] = cours.ReviewOf(repo, remise, m.equipesDe(cours))
			continue
		}
		// Sans place, les commits se comptent mais rien ne se juge : il n'y a
		// ni date cible ni liste d'étudiants à confronter. La remise se date
		// quand même hors des commits d'enseignants : le registre de
		// l'organisation dit qui enseigne, même sous un préfixe hérité.
		trouves[repo] = classroom.Review{
			Repo: repo, Commits: remise.CommitsBut(set.Teaches),
			Last: remise.LastBut(set.Teaches),
		}
	}
	return trouves
}

// etats dit où en est la remise de chaque dépôt du groupe.
//
// Rien n'est demandé à GitHub : les historiques et les accès déjà mémorisés
// suffisent, et un dépôt qu'on n'a pas relevé le dit — « non relevé » n'est pas
// « rien remis ». C'est la même règle qu'au navigateur, décidée au même
// endroit : seule la façon de la montrer change.
func (m *manageSession) etats(group *groups.Group) map[string]classroom.HandinState {
	bilans := m.bilans(group)
	noms := make([]string, 0, group.Len())
	for _, repo := range group.Repos {
		noms = append(noms, repo.Name)
	}
	acces := m.resolver.Accesses(m.org, noms, identity.Cached, nil)
	groupes := m.groupesDe(group)

	etats := make(map[string]classroom.HandinState, len(noms))
	for _, nom := range noms {
		bilan, releve := bilans[nom]
		// Sans place reconnue, rien ne dit qui le dépôt vise : une invitation
		// en attente ne s'y rapporte alors à personne.
		attend := false
		if cours, place := classroom.Of(groupes, nom); place {
			attend = cours.Awaiting(nom, m.equipesDe(cours), acces[nom].Pending())
		}
		etats[nom] = classroom.StateOf(bilan, releve, attend)
	}
	return etats
}

// remises relève les historiques des dépôts du groupe, puis remontre la liste.
// C'est deux requêtes par dépôt : le geste est explicite, et son résultat
// mémorisé — la liste le reprend ensuite sans rien redemander.
func (m *manageSession) remises(group *groups.Group) error {
	noms := make([]string, 0, group.Len())
	for _, repo := range group.Repos {
		noms = append(noms, repo.Name)
	}
	if len(noms) == 0 {
		return valid.Errorf("Aucun dépôt dans « %s ».", group.Prefix)
	}
	console := m.session.Console
	progression := ui.NewProgress(console, "Historiques", len(noms))
	remises := m.resolver.Handins(m.org, noms, identity.Refresh,
		func(done, _ int, repo string) { progression.Update(done, repo) })
	progression.Clear()
	if manquants := len(noms) - len(remises); manquants > 0 {
		console.Warning("%s", valid.Plural(manquants,
			"dépôt n'a pas pu être lu : son historique reste inconnu.",
			"dépôts n'ont pas pu être lus : leur historique reste inconnu."))
	}

	groupes := m.groupesDe(group)
	if len(groupes) == 0 {
		console.Note("« %s » ne dit pas à quel groupe il appartient : les commits se "+
			"comptent, mais aucune date cible ne s'y rapporte.", group.Prefix)
		m.show(group)
		return nil
	}
	m.pourquoiRien(groupes, remises)
	if cours, nom, travail := m.travail(group); travail {
		if due := cours.DueOf(nom); due != "" {
			console.Note("Date cible de « %s » : %s.", nom, due)
		} else {
			console.Note("« %s » n'a pas de date cible : rien n'y sera dit en retard.", nom)
		}
	} else if sans := travauxSansEcheance(groupes, group); len(sans) > 0 {
		// Plusieurs travaux : chacun a sa date, que la colonne applique. Seuls
		// ceux qui n'en ont pas méritent un mot — rien n'y sera jamais en retard,
		// et une colonne sans retard ne dit pas pourquoi.
		console.Note("Sans date cible, rien n'est dit en retard dans : %s.",
			strings.Join(sans, ", "))
	}
	m.show(group)
	return nil
}

// travauxSansEcheance nomme les travaux de la liste qui n'ont pas de date
// cible : par leur nom court quand la liste ne couvre qu'un groupe, par leur
// identifiant complet quand elle en couvre plusieurs — deux groupes ont souvent
// un « tp1 » chacun.
func travauxSansEcheance(groupes []classroom.Classroom, group *groups.Group) []string {
	infos := make([]groups.RepoInfo, 0, group.Len())
	for _, repo := range group.Repos {
		infos = append(infos, groups.RepoInfo{Name: repo.Name})
	}
	sans := make([]string, 0)
	for _, cours := range groupes {
		for _, travail := range cours.Assignments(infos, nil) {
			if travail.Due != "" {
				continue
			}
			if len(groupes) > 1 {
				sans = append(sans, travail.ID)
			} else {
				sans = append(sans, travail.Name)
			}
		}
	}
	return sans
}

// pourquoiRien va chercher les accès des dépôts qui n'ont rien reçu.
//
// Un dépôt vide pose une question de plus que les autres : la personne a-t-elle
// seulement accepté son invitation ? Sans réponse, on lui reprocherait un
// silence qu'elle n'a pas choisi. Ceux qui ont reçu quelque chose n'en ont pas
// besoin — la question ne se pose plus — et leurs accès coûteraient deux
// requêtes pour rien.
func (m *manageSession) pourquoiRien(groupes []classroom.Classroom,
	remises map[string]groups.Handin) {
	muets := make([]string, 0, len(remises))
	for repo, remise := range remises {
		if cours, place := classroom.Of(groupes, repo); place && cours.HandedIn(remise) == "" {
			muets = append(muets, repo)
		}
	}
	if len(muets) == 0 {
		return
	}
	progression := ui.NewProgress(m.session.Console, "Accès", len(muets))
	m.resolver.Accesses(m.org, muets, identity.Fetch,
		func(done, _ int, repo string) { progression.Update(done, repo) })
	progression.Clear()
}

// echeance demande la date cible du travail géré et l'enregistre.
func (m *manageSession) echeance(group *groups.Group) error {
	cours, nom, reconnu := m.travail(group)
	if !reconnu {
		return valid.Errorf(
			"« %s » ne dit pas à quel groupe il appartient : une date cible s'attache "+
				"à un travail d'un groupe. Déplacez-le d'abord vers une place "+
				"« session%[2]scours%[2]sgroupe ».", group.Prefix, naming.Separator)
	}
	console := m.session.Console
	console.Note("Une date seule vaut la fin de la journée. Laissez vide pour retirer "+
		"la date cible de « %s ».", nom)
	reponse, err := m.session.Prompt.Ask(ui.Question{
		Title:      "Date cible (AAAA-MM-JJ ou AAAA-MM-JJTHH:MM)",
		Default:    cours.DueOf(nom),
		AllowEmpty: true,
		Validate:   valid.NormalizeDue,
	})
	if err != nil {
		return err
	}
	return m.fixerEcheance(cours, nom, reponse)
}

// fixerEcheance porte au registre de l'organisation la date cible d'un travail,
// et dit ce qu'elle est devenue.
//
// Elle monte là où vivent déjà les noms que les dépôts ne disent pas : une date
// fixée ici vaut pour l'équipe entière, et suit d'un poste à l'autre.
func (m *manageSession) fixerEcheance(cours classroom.Classroom, nom, due string) error {
	lignes, err := cours.SetDue(nom, due)
	if err != nil {
		return err
	}
	set, err := m.session.registryOf(cours.Org).Apply(echeances(lignes))
	if err != nil {
		return err
	}
	if fixee := set.Due(cours.AssignmentID(nom)); fixee != "" {
		m.session.Console.Success("« %s » est à remettre le %s.", nom, fixee)
	} else {
		m.session.Console.Success("« %s » n'a plus de date cible.", nom)
	}
	return nil
}

// resumeDeRemise dit en deux cases ce qu'un dépôt a reçu : combien de commits,
// et où en est la remise. Un dépôt qu'on n'a pas encore relevé ne prétend
// rien : ses deux cases sont vides, et ce n'est pas la même chose qu'un dépôt
// vide.
func resumeDeRemise(console *ui.Console, bilan classroom.Review,
	etat classroom.HandinState) (string, string) {
	if etat == classroom.Unread {
		return console.Dim("—"), console.Dim("—")
	}
	return itoa(bilan.Commits), verdictDeRemise(console, bilan, etat) + etrangers(console, bilan)
}

// verdictDeRemise dit d'un mot où en est la remise.
//
// L'état vient du domaine, et le mot qu'il porte est celui que le navigateur
// montre. Ce qui s'y ajoute ici est le détail d'une équipe qui a remis sans que
// tous ses membres y aient touché : l'état la dit remise, et il l'est.
func verdictDeRemise(console *ui.Console, bilan classroom.Review,
	etat classroom.HandinState) string {
	switch etat {
	case classroom.Overdue:
		return console.Err(string(classroom.Overdue))
	case classroom.Unaccepted:
		return console.Warn(string(classroom.Unaccepted))
	case classroom.Unsent:
		// Nommer ceux qui se taisent ne dirait rien de plus que la colonne
		// d'à côté, qui porte déjà leur nom.
		if bilan.Commits == 0 && !bilan.Missing() {
			return console.Dim("aucun commit")
		}
		return string(classroom.Unsent)
	}
	if bilan.Missing() {
		// Une équipe où l'un seulement se tait : le dépôt a reçu quelque chose,
		// et savoir de qui il ne porte rien est tout ce qui compte.
		noms := make([]string, 0, len(bilan.Silent))
		for _, personne := range bilan.Silent {
			noms = append(noms, nommer(personne))
		}
		return "rien de " + strings.Join(noms, ", ")
	}
	return string(classroom.Delivered)
}

// etrangers nomme les comptes qui ont commis sans y être attendus : le plus
// souvent l'étudiant sous un autre compte, que « Réunir » lui rattache.
func etrangers(console *ui.Console, bilan classroom.Review) string {
	if len(bilan.Strangers) == 0 {
		return ""
	}
	return " · " + console.Warn("commits de @"+strings.Join(bilan.Strangers, ", @"))
}

// reunirEtranger réunit à son étudiant un compte qui a commis dans son dépôt
// sans être le sien. C'est le cas courant d'un courriel resté configuré sur un
// vieux compte : GitHub attribue les commits à ce compte-là, et l'étudiant
// paraît muet dans son propre dépôt. Les candidats viennent du dernier relevé ;
// la décision reste celle de qui enseigne, et elle s'écrit au registre.
func (m *manageSession) reunirEtranger(group *groups.Group) error {
	console := m.session.Console
	groupes := m.groupesDe(group)
	if len(groupes) == 0 {
		console.Note("« %s » ne dit pas à quel groupe il appartient : rien ne dit qui "+
			"chaque dépôt vise.", group.Prefix)
		return nil
	}
	type paire struct {
		repo, etranger string
		personne       roster.Person
	}
	bilans := m.bilans(group)
	paires := make([]paire, 0)
	for _, repo := range group.Repos {
		bilan, releve := bilans[repo.Name]
		if !releve || len(bilan.Strangers) == 0 {
			continue
		}
		cours, place := classroom.Of(groupes, repo.Name)
		if !place {
			continue
		}
		// Seul un dépôt qui vise une personne dit à qui réunir le compte :
		// dans un dépôt d'équipe, rien ne dit lequel des membres c'est.
		cibles := cours.Targets(repo.Name, m.equipesDe(cours))
		if len(cibles) != 1 {
			continue
		}
		for _, etranger := range bilan.Strangers {
			paires = append(paires, paire{repo: repo.Name, etranger: etranger, personne: cibles[0]})
		}
	}
	if len(paires) == 0 {
		console.Note("Aucun compte inattendu dans les historiques relevés de « %s ». "+
			"« Relever les remises » les lit.", group.Prefix)
		return nil
	}

	parChoix := make(map[string]paire, len(paires))
	options := make([]ui.Option, 0, len(paires)+1)
	for _, item := range paires {
		valeur := item.repo + "\x00" + item.etranger
		parChoix[valeur] = item
		options = append(options, ui.Option{Value: valeur,
			Label: "@" + item.etranger + " → " + nommer(item.personne) + "  (" + item.repo + ")"})
	}
	options = append(options, ui.Option{Value: "annuler", Label: "Annuler"})
	console.Note("Rien ne se déduit d'un commit : un ami qui aide commet aussi. Réunir " +
		"deux comptes dit qu'ils sont une même personne.")
	choix, err := m.session.Prompt.Choose("Compte à réunir", options, options[0].Value)
	if err != nil || choix == "annuler" {
		return err
	}
	item := parChoix[choix]

	vue, err := m.session.orgView(m.org, false)
	if err != nil {
		return err
	}
	// Le compte n'est d'aucun groupe : c'est l'historique qui en témoigne.
	lignes := users.Witness(vue.rows(), m.session.histoires(m.org, vue.repos), item.etranger)
	reunion, err := users.PlanJoin(lignes, vue.set, m.session.Viewer, m.org,
		item.etranger, item.personne.Username)
	if err != nil {
		console.Failure("%v", err)
		return nil
	}
	m.session.printJoining(reunion)
	confirme, err := m.session.Prompt.Confirm(
		"Réunir @"+reunion.Account+" à @"+reunion.Principal+" ?", false)
	if err != nil || !confirme {
		return err
	}
	if m.session.applyJoin(m.org, reunion) == ExitOK {
		// Le registre a changé : la liste le relit, et l'étudiant n'y est
		// plus muet.
		m.show(group)
	}
	return nil
}

// dueFromFlag applique la date cible donnée en ligne de commande, sans rien
// demander. Elle est déjà mise en forme par la lecture des drapeaux : une date
// mal écrite arrête la ligne de commande avant qu'aucun appel n'ait lieu.
func (m *manageSession) dueFromFlag(group *groups.Group) error {
	cours, nom, reconnu := m.travail(group)
	if !reconnu {
		return valid.Errorf(
			"--due : « %s » ne dit pas à quel groupe il appartient. Une date cible "+
				"s'attache à un travail d'un groupe — déplacez-le d'abord vers une "+
				"place « session%[2]scours%[2]sgroupe ».", group.Prefix, naming.Separator)
	}
	return m.fixerEcheance(cours, nom, m.session.Options.Due)
}
