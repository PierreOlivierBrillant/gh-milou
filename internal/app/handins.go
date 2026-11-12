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

// La date cible et les remises, au terminal. L'assistant gère un préfixe, et un
// préfixe de la nomenclature courante est un travail : « a26.5n6.01.tp1 » dit à
// la fois le groupe et le travail, et c'est tout ce qu'il faut pour retrouver
// l'échéance retenue et confronter les historiques à elle.
//
// Un préfixe hérité — « tp1-jlpicard » et ses semblables — ne dit pas à quel
// groupe il appartient : les commits s'y comptent quand même, mais rien ne s'y
// rapporte à une date cible. Le déplacer vers une place lui donne le reste.

// travail résout le groupe et le nom du travail que le préfixe géré désigne.
// Le second retour vaut faux pour un préfixe qui ne dit pas sa place.
func (m *manageSession) travail(group *groups.Group) (classroom.Classroom, string, bool) {
	place, nom, reconnu := naming.SplitAssignment(group.Prefix)
	if !reconnu {
		return classroom.Classroom{}, "", false
	}
	cours, err := classroom.AtScope(m.org, place, classroom.DefaultsFrom(m.session.Settings))
	if err != nil {
		return classroom.Classroom{}, "", false
	}
	if connu, existe := m.session.groupStore().Find(cours.Org, cours.Scope()); existe {
		cours = connu
	}
	set, _ := m.session.names(cours.Org)
	// Le registre répond aux trois questions qu'un groupe lui pose : qui se
	// cache derrière un nom de dépôt, quand chaque travail est attendu, et qui
	// enseigne — sans quoi un commit d'enseignant daterait la remise.
	return cours.Scheduling(set).Staffing(set).Enrich(set, m.repos), nom, true
}

// equipesDe lit les équipes du groupe, une fois pour la séance. Leur absence
// n'empêche rien : le travail est alors individuel, et c'est déjà ce que la
// liste vide dit.
func (m *manageSession) equipesDe(cours classroom.Classroom) []teams.Team {
	if m.equipesLues {
		return m.equipes
	}
	m.equipesLues = true
	infos, err := m.session.Client.LoadOrgTeams(cours.Org, m.session.Options.Jobs)
	if err != nil {
		return nil
	}
	m.equipes = cours.Teams(infos)
	return m.equipes
}

// bilans confronte les dépôts affichés à ce qu'on sait déjà de leurs
// historiques. Rien n'est demandé à GitHub : c'est « remises » qui va chercher,
// et la liste ne montre que ce qui a déjà été relevé — et qu'aucun envoi n'a
// dépassé depuis. C'est l'inventaire qui le dit, pas l'âge du relevé : la même
// règle qu'au navigateur, décidée au même endroit.
func (m *manageSession) bilans(group *groups.Group) map[string]classroom.Review {
	noms := make([]string, 0, group.Len())
	for _, repo := range group.Repos {
		noms = append(noms, repo.Name)
	}
	remises := groups.Complete(m.repos, m.resolver.Histories(m.org, noms))
	if len(remises) == 0 {
		return nil
	}
	cours, nom, reconnu := m.travail(group)
	trouves := make(map[string]classroom.Review, len(remises))
	if !reconnu {
		// Sans place, les commits se comptent mais rien ne se juge : il n'y a
		// ni date cible ni liste d'étudiants à confronter. La remise se date
		// quand même hors des commits d'enseignants : le registre de
		// l'organisation dit qui enseigne, même sous un préfixe hérité.
		set, _ := m.session.names(m.org)
		for repo, remise := range remises {
			trouves[repo] = classroom.Review{
				Repo: repo, Commits: remise.CommitsBut(set.Teaches),
				Last: remise.LastBut(set.Teaches),
			}
		}
		return trouves
	}
	due, _ := valid.ParseDue(cours.DueOf(nom))
	equipes := m.equipesDe(cours)
	for repo, remise := range remises {
		trouves[repo] = cours.Review(repo, remise, due, equipes)
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
	cours, _, reconnu := m.travail(group)
	var equipes []teams.Team
	if reconnu {
		equipes = m.equipesDe(cours)
	}

	etats := make(map[string]classroom.HandinState, len(noms))
	for _, nom := range noms {
		bilan, releve := bilans[nom]
		// Sans place reconnue, rien ne dit qui le dépôt vise : une invitation
		// en attente ne s'y rapporte alors à personne.
		attend := reconnu && cours.Awaiting(nom, equipes, acces[nom].Pending())
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
		console.Warning("%d dépôt(s) n'ont pas pu être lus : leur historique reste inconnu.",
			manquants)
	}

	cours, nom, reconnu := m.travail(group)
	if !reconnu {
		console.Note("« %s » ne dit pas à quel groupe il appartient : les commits se "+
			"comptent, mais aucune date cible ne s'y rapporte.", group.Prefix)
		m.show(group)
		return nil
	}
	m.pourquoiRien(cours, remises)
	if due := cours.DueOf(nom); due != "" {
		console.Note("Date cible de « %s » : %s.", nom, due)
	} else {
		console.Note("« %s » n'a pas de date cible : rien n'y sera dit en retard.", nom)
	}
	m.show(group)
	return nil
}

// pourquoiRien va chercher les accès des dépôts qui n'ont rien reçu.
//
// Un dépôt vide pose une question de plus que les autres : la personne a-t-elle
// seulement accepté son invitation ? Sans réponse, on lui reprocherait un
// silence qu'elle n'a pas choisi. Ceux qui ont reçu quelque chose n'en ont pas
// besoin — la question ne se pose plus — et leurs accès coûteraient deux
// requêtes pour rien.
func (m *manageSession) pourquoiRien(cours classroom.Classroom,
	remises map[string]groups.Handin) {
	muets := make([]string, 0, len(remises))
	for repo, remise := range remises {
		if cours.HandedIn(remise) == "" {
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
	cours, _, reconnu := m.travail(group)
	if !reconnu {
		console.Note("« %s » ne dit pas à quel groupe il appartient : rien ne dit qui "+
			"chaque dépôt vise.", group.Prefix)
		return nil
	}
	type paire struct {
		repo, etranger string
		personne       roster.Person
	}
	bilans := m.bilans(group)
	equipes := m.equipesDe(cours)
	paires := make([]paire, 0)
	for _, repo := range group.Repos {
		bilan, releve := bilans[repo.Name]
		if !releve || len(bilan.Strangers) == 0 {
			continue
		}
		// Seul un dépôt qui vise une personne dit à qui réunir le compte :
		// dans un dépôt d'équipe, rien ne dit lequel des membres c'est.
		cibles := cours.Targets(repo.Name, equipes)
		if len(cibles) != 1 {
			continue
		}
		for _, etranger := range bilan.Strangers {
			paires = append(paires, paire{repo: repo.Name, etranger: etranger, personne: cibles[0]})
		}
	}
	if len(paires) == 0 {
		console.Note("Aucun compte inattendu dans les historiques relevés de « %s ». "+
			"« Relever les commits et les remises » les lit.", group.Prefix)
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
