package app

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/clone"
	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plan"
	"github.com/PierreOlivierBrillant/gh-milou/internal/preload"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/runner"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/users"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// sectionDeGestion range des gestes du mode gestion sous un titre.
type sectionDeGestion struct {
	valeur, titre string
	// contenu résume la section dans le premier menu : on y choisit une
	// section sans l'ouvrir pour voir ce qu'elle range.
	contenu string
	gestes  []ui.Option
}

// sectionsDeGestion rangent les gestes du mode gestion. Vingt-deux entrées à
// plat ne se lisaient plus, et chaque geste se cherche ici où le navigateur le
// range, sous le même verbe : « Inspecter les accès », « Relever les remises ».
var sectionsDeGestion = []sectionDeGestion{
	{"depots", "Dépôts", "ajouter, déposer un fichier, cloner, exporter les URL, supprimer",
		ui.Options(
			"ajouter", "Ajouter des dépôts au groupe",
			"fichier", "Déposer un fichier dans tous les dépôts",
			"cloner", "Cloner des dépôts",
			"pull", "Mettre à jour des clones",
			"urls", "Afficher les URL, ou les exporter en CSV",
			"supprimer", "Supprimer un dépôt",
		)},
	{"acces", "Accès et invitations", "inspecter, inviter, collaborateurs",
		ui.Options(
			"inspecter", "Inspecter les accès",
			"invitations", "Envoyer les invitations manquantes",
			"collaborateurs", "Gérer les collaborateurs d'un dépôt",
		)},
	{"remises", "Remises", "relever, réunir un compte à son étudiant",
		ui.Options(
			"relever", "Relever les remises",
			"reunir", "Réunir à son étudiant un compte vu dans son dépôt",
		)},
	{"comparaisons", "Comparaisons", "comparer les copies, publier l'index, échanger avec un collègue",
		ui.Options(
			"plagiat", "Comparer les copies",
			"envoi", "Envoyer ces copies à un collègue",
			"publier", "Publier l'index de ce travail",
			"demandes", "Demandes : en faire une, répondre à celles des collègues",
		)},
	{"travail", "Ce travail", "date cible, renommer, déplacer",
		ui.Options(
			"echeance", "Date cible",
			"renommer", "Renommer",
			"deplacer", "Déplacer vers un autre groupe",
		)},
	{"groupe", "Groupe", "équipe enseignante, corriger un étudiant",
		ui.Options(
			"enseignants", "Équipe enseignante",
			"etudiant", "Corriger un étudiant : son nom, son compte, ses dépôts",
		)},
	{"liste", "Liste", "filtrer, trier, recharger",
		ui.Options(
			"filtrer", "Filtrer ou trier",
			"rafraichir", "Recharger",
		)},
}

// menuDeGestion est le premier menu : une entrée par section, puis de quoi
// changer de groupe ou s'en aller.
var menuDeGestion = func() []ui.Option {
	options := make([]ui.Option, 0, len(sectionsDeGestion)+2)
	for _, section := range sectionsDeGestion {
		options = append(options, ui.Option{
			Value: section.valeur, Label: section.titre + " — " + section.contenu,
		})
	}
	return append(options,
		ui.Option{Value: "changer", Label: "Changer de groupe"},
		ui.Option{Value: "quitter", Label: "Quitter"})
}()

// choisirGeste demande une section, puis l'un de ses gestes. « Revenir » ramène
// au premier menu sans rien avoir fait : la liste n'a pas à être redonnée.
func (m *manageSession) choisirGeste() (string, error) {
	choix, err := m.session.Prompt.Choose("Que faire ?", menuDeGestion, "quitter")
	if err != nil {
		return "", err
	}
	for _, section := range sectionsDeGestion {
		if section.valeur != choix {
			continue
		}
		gestes := append(slices.Clone(section.gestes),
			ui.Option{Value: "revenir", Label: "Revenir"})
		return m.session.Prompt.Choose(section.titre, gestes, "revenir")
	}
	return choix, nil
}

// manageSession pilote le mode « gérer un groupe existant ».
type manageSession struct {
	session       *Session
	org           string
	initialPrefix string
	resolver      *identity.Resolver
	repos         []groups.RepoInfo
	loaded        bool
	// Ce que la liste montre et dans quel ordre. Les actions, elles,
	// continuent de travailler sur le groupe entier : filtrer sert à choisir,
	// pas à faire oublier des dépôts.
	filter   users.Filter
	sortKey  users.Key
	sortDesc bool
	// handin ne garde que les dépôts dont la remise en est là. Il vit à part du
	// filtre des personnes : ce qu'il regarde n'est pas une ligne d'annuaire,
	// c'est ce qu'un historique et des accès déjà lus disent d'un dépôt.
	handin classroom.HandinState
	// Les équipes de l'organisation, lues une fois : elles disent à qui un
	// dépôt d'équipe est destiné, et la liste s'affiche trop souvent pour les
	// redemander à chaque fois.
	infos       []teams.Info
	equipesLues bool
	// prechargement va chercher d'avance les historiques et les accès des
	// cours de la session la plus récente, pendant qu'on navigue dans le menu.
	prechargement *preload.Warmer
}

func newManageSession(session *Session, initialPrefix string) *manageSession {
	return &manageSession{
		session:       session,
		org:           session.Settings.Org,
		initialPrefix: initialPrefix,
		filter:        session.Options.Filter,
		handin:        session.Options.Handin,
		sortKey:       session.Options.Sort,
		sortDesc:      session.Options.SortDesc,
		resolver:      identity.New(session.Client, session.Cache, session.Options.Jobs),
		prechargement: preload.New(),
	}
}

// precharger prépare en arrière-plan les deux lectures chères — les historiques
// et les accès — pour les dépôts des cours de la session la plus récente.
// L'assistant y passera : autant qu'elles soient déjà lues quand il y arrive.
//
// Sans mémoire, il n'y a rien à préparer : « --no-cache » demande précisément
// que tout soit redemandé au moment où on le regarde.
func (m *manageSession) precharger() {
	if m.session.Cache == nil || !m.session.Cache.Enabled {
		return
	}
	// Le registre n'entre pas ici : le préchargement ne s'intéresse qu'aux
	// dépôts, et les noms lui coûteraient une lecture pour rien.
	visibles := m.session.groupStore().Visible(m.org, m.repos,
		classroom.DefaultsFrom(m.session.Settings), registry.Empty())
	m.prechargement.Warm(m.resolver, m.org, visibles, m.repos)
}

// forget oublie l'inventaire retenu en mémoire, après une purge du cache.
func (m *manageSession) forget() {
	m.repos, m.loaded = nil, false
}

// suivre répercute sur l'inventaire retenu un changement que « groups » sait
// décrire, plutôt que de tout relire. Ce que le changement produit est décidé
// dans le domaine : le navigateur en tire le même inventaire.
//
// Sans inventaire chargé, il n'y a rien à corriger : le cache est oublié, et la
// prochaine lecture partira de GitHub.
func (m *manageSession) suivre(apply func([]groups.RepoInfo) []groups.RepoInfo) {
	if !m.loaded {
		m.session.Cache.Forget(cache.ReposKey(m.org))
		return
	}
	m.repos = apply(m.repos)
	m.session.Cache.Set(cache.ReposKey(m.org), m.repos)
}

// ------------------------------------------------------------------ inventaire

// loadRepos charge les dépôts de l'organisation : mémoire, puis cache, puis API.
func (m *manageSession) loadRepos(force bool) ([]groups.RepoInfo, error) {
	if m.loaded && !force {
		return m.repos, nil
	}
	repos, err := m.session.orgRepos(m.org, force)
	if err != nil {
		return nil, err
	}
	m.repos, m.loaded = repos, true
	return repos, nil
}

// chooseGroup propose les groupes détectés et retient celui demandé.
func (m *manageSession) chooseGroup() (*groups.Group, error) {
	console := m.session.Console
	repos, err := m.loadRepos(false)
	if err != nil {
		return nil, err
	}

	if m.initialPrefix != "" {
		// Préfixe fourni en ligne de commande : il ne sert qu'une fois.
		prefix := m.initialPrefix
		m.initialPrefix = ""
		group := groups.Build(prefix, repos)
		if group.Len() > 0 {
			return &group, nil
		}
		console.Failure("Aucun dépôt dans « %s ».", prefix)
		if !m.session.Interactive() {
			return nil, valid.Errorf("Groupe « %s » introuvable dans « %s ».", prefix, m.org)
		}
	}
	if len(repos) == 0 {
		console.Warning("Aucun dépôt dans cette organisation.")
		return nil, nil
	}
	if !m.session.Interactive() {
		return nil, valid.Errorf("Le mode gestion attend un préfixe : passez --manage PREFIXE.")
	}

	names := make([]string, 0, len(repos))
	for _, repo := range repos {
		names = append(names, repo.Name)
	}
	detected := groups.Detect(names, 2)

	console.Heading("Groupes existants")
	if len(detected) == 0 {
		console.Note("Aucun groupe détecté automatiquement.")
	} else {
		rows := make([][]string, 0, len(detected))
		for index, item := range detected {
			rows = append(rows, []string{itoa(index + 1), item.Prefix, valid.Plural(item.Count, "dépôt")})
		}
		console.Table([]string{"#", "Préfixe", "Taille"}, rows, 20)
	}

	for {
		options := make([]ui.Option, 0, len(detected)+2)
		for _, item := range detected {
			options = append(options, ui.Option{
				Value: "prefixe:" + item.Prefix,
				Label: item.Prefix + "  (" + valid.Plural(item.Count, "dépôt") + ")",
			})
		}
		options = append(options,
			ui.Option{Value: "saisir", Label: "Saisir un autre préfixe…"},
			ui.Option{Value: "revenir", Label: "Revenir"})

		defaultChoice := "saisir"
		if len(detected) > 0 {
			defaultChoice = "prefixe:" + detected[0].Prefix
		}
		choice, err := m.session.Prompt.Choose("Groupe à ouvrir", options, defaultChoice)
		if err != nil {
			return nil, err
		}
		if choice == "revenir" {
			return nil, nil
		}

		prefix := strings.TrimPrefix(choice, "prefixe:")
		if choice == "saisir" {
			answer, err := m.session.Prompt.Ask(ui.Question{
				Title:      "Préfixe du groupe (vide pour revenir)",
				AllowEmpty: true,
			})
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(answer) == "" {
				return nil, nil
			}
			// Le préfixe d'un travail porte des points — « a26.5n6.01.tp1 » : ses
			// niveaux se mettent en forme un à un, sinon le point deviendrait un
			// tiret et le préfixe ne retrouverait plus rien.
			cleaned, err := naming.Path(answer, "Préfixe")
			if err != nil {
				console.Failure("%v", err)
				continue
			}
			prefix = cleaned
		}

		group := groups.Build(prefix, repos)
		if group.Len() == 0 {
			console.Failure("Aucun dépôt dans « %s ».", prefix)
			continue
		}
		return &group, nil
	}
}

// names retrouve le nom complet de chaque dépôt du groupe : par la liste de son
// groupe et le registre, comme au navigateur et dans l'annuaire.
//
// Seuls les dépôts hors de toute place se nomment par le profil GitHub du
// compte que leur suffixe désigne. C'est alors le seul indice, et il vaut :
// GitHub Classroom nommait ses dépôts du compte de l'étudiant. Sous la
// nomenclature courante, le suffixe est un slug, et le profil du compte qui
// porterait ce slug serait celui d'un inconnu.
func (m *manageSession) names(group *groups.Group) map[string]string {
	groupes := m.groupesDe(group)
	names := make(map[string]string, group.Len())
	pairs := make([]identity.Pair, 0)
	for _, repo := range group.Repos {
		if cours, place := classroom.Of(groupes, repo.Name); place {
			names[repo.Name] = cours.NameOf(repo.Name, m.equipesDe(cours))
			continue
		}
		pairs = append(pairs, identity.Pair{Repo: repo.Name, Login: repo.Suffix})
	}
	if len(pairs) == 0 {
		return names
	}
	var resolus map[string]string
	if missing := m.resolver.Missing(pairs); len(missing) == 0 {
		resolus = m.resolver.Resolve(pairs, false, nil)
	} else {
		progress := ui.NewProgress(m.session.Console, "Noms complets", len(missing))
		resolus = m.resolver.Resolve(pairs, true, func(done, total int, repo string) {
			progress.Update(done, repo)
		})
		progress.Finish("")
	}
	for repo, nom := range resolus {
		names[repo] = nom
	}
	return names
}

// visible rend les dépôts que le filtre retient, dans l'ordre du tri. C'est ce
// que la liste montre et ce parmi quoi les sélections se font : on choisit ce
// qu'on voit.
func (m *manageSession) visible(group *groups.Group) []groups.Repo {
	// Le dernier envoi montré et filtré est celui des étudiants : ce que
	// l'enseignant a poussé n'en est pas, dès que l'historique le dit.
	date := group.Dated(m.session.activite(m.org, m.repos))
	group = &date
	parNom := make(map[string]groups.Repo, group.Len())
	for _, repo := range group.Repos {
		parNom[repo.Name] = repo
	}
	lignes := users.Apply(users.FromGroup(*group, m.names(group)),
		m.filter, m.sortKey, m.sortDesc)

	// L'état de la remise ne se lit pas dans une ligne d'annuaire : il vient
	// des historiques et des accès déjà relevés, et n'est donc demandé que
	// lorsqu'on filtre dessus.
	var etats map[string]classroom.HandinState
	if m.handin != classroom.AnyHandin {
		etats = m.etats(group)
	}
	retenus := make([]groups.Repo, 0, len(lignes))
	for _, ligne := range lignes {
		if len(ligne.Repos) == 0 {
			continue
		}
		repo, connu := parNom[ligne.Repos[0].Name]
		if !connu || !m.handin.Keep(etats[repo.Name]) {
			continue
		}
		retenus = append(retenus, repo)
	}
	return retenus
}

// criteria dit en une ligne ce que la liste retient et comment elle est
// ordonnée : un filtre qui ne se voit pas se retourne contre celui qui l'a posé.
func (m *manageSession) criteria() string {
	parts := []string{}
	if m.handin != classroom.AnyHandin {
		parts = append(parts, "remise : "+string(m.handin))
	}
	if m.filter.Text != "" {
		parts = append(parts, "« "+m.filter.Text+" »")
	}
	if m.filter.PushedAfter != "" {
		parts = append(parts, "envoi après le "+m.filter.PushedAfter)
	}
	if m.filter.PushedBefore != "" {
		parts = append(parts, "envoi avant le "+m.filter.PushedBefore)
	}
	if m.filter.Activity == users.Silent {
		parts = append(parts, "aucun envoi")
	}
	sens := "croissant"
	if m.sortDesc {
		sens = "décroissant"
	}
	tri := map[users.Key]string{
		users.ByName: "nom", users.ByUsername: "compte", users.ByPushed: "dernier envoi",
	}[m.sortKey]
	if tri == "" {
		tri = "nom"
	}
	parts = append(parts, "tri par "+tri+" ("+sens+")")
	return strings.Join(parts, " · ")
}

// show affiche le groupe : dépôt, nom complet, visibilité, dernier envoi. Les
// commits et l'état des remises s'y ajoutent dès qu'un relevé a eu lieu : deux
// colonnes de plus n'ont de sens que quand elles ont quelque chose à dire.
// L'invitation suit la même règle : elle paraît dès que des accès sont connus.
func (m *manageSession) show(group *groups.Group) {
	console := m.session.Console
	names := m.names(group)
	visibles := m.visible(group)
	bilans := m.bilans(group)
	etats := m.etats(group)
	invitations, aDebloquer := m.invitations(group)
	avecInvitations := false
	for _, etat := range invitations {
		if etat != identity.InvitationUnknown {
			avecInvitations = true
			break
		}
	}

	titre := "Groupe « " + group.Prefix + " » — " + valid.Plural(group.Len(), "dépôt")
	if len(visibles) != group.Len() {
		titre += ", " + itoa(len(visibles)) + " " + valid.Agree(len(visibles), "affiché")
	}
	console.Heading(titre)

	entetes := []string{"#", "Dépôt", "Nom complet", "Visibilité", "Dernier envoi"}
	if len(bilans) > 0 {
		entetes = append(entetes, "Commits", "Remise")
	}
	if avecInvitations {
		entetes = append(entetes, "Invitation")
	}
	rows := make([][]string, 0, len(visibles))
	for index, repo := range visibles {
		fullName := names[repo.Name]
		if fullName == "" {
			fullName = console.Dim(repo.Suffix)
		}
		pushed := repo.PushedAt
		if pushed == "" {
			pushed = console.Dim("jamais")
		}
		ligne := []string{itoa(index + 1), repo.Name, fullName, repo.Visibility(), pushed}
		if len(bilans) > 0 {
			commits, verdict := resumeDeRemise(console, bilans[repo.Name], etats[repo.Name])
			ligne = append(ligne, commits, verdict)
		}
		if avecInvitations {
			ligne = append(ligne, motDInvitation(console, invitations[repo.Name]))
		}
		rows = append(rows, ligne)
	}
	console.Table(entetes, rows, 40)
	if len(visibles) == 0 && group.Len() > 0 {
		console.Warning("Aucun dépôt ne répond aux critères.")
	}
	if aDebloquer > 0 {
		console.Warning("%s — expirée ou absente : « Envoyer les invitations manquantes » y remédie.",
			valid.Plural(aDebloquer, "dépôt sans invitation valable", "dépôts sans invitation valable"))
	}
	console.Note("%s", m.criteria())
}

// ---------------------------------------------------------------- sélections

// pickRepo choisit un dépôt parmi ceux que la liste montre.
func (m *manageSession) pickRepo(group *groups.Group, question string) (*groups.Repo, error) {
	names := m.names(group)
	visibles := m.visible(group)
	options := make([]ui.Option, 0, len(visibles)+1)
	for _, repo := range visibles {
		label := repo.Name
		if fullName := names[repo.Name]; fullName != "" {
			label += "  —  " + fullName
		}
		options = append(options, ui.Option{Value: repo.Name, Label: label})
	}
	options = append(options, ui.Option{Value: "annuler", Label: "Annuler"})

	choice, err := m.session.Prompt.Choose(question, options, "annuler")
	if err != nil {
		return nil, err
	}
	if choice == "annuler" {
		return nil, nil
	}
	repo, _, found := group.Find(choice)
	if !found {
		m.session.Console.Failure("« %s » ne correspond à aucun dépôt du groupe.", choice)
		return nil, nil
	}
	return &repo, nil
}

// pickMany choisit plusieurs dépôts, par cases à cocher ou par expression.
func (m *manageSession) pickMany(group *groups.Group, question string) ([]groups.Repo, error) {
	visibles := m.visible(group)
	options := make([]ui.Option, 0, len(visibles))
	selected := make([]bool, len(visibles))
	for index, repo := range visibles {
		options = append(options, ui.Option{Value: repo.Name, Label: repo.Name})
		selected[index] = true
	}
	indices, err := m.session.Prompt.MultiSelect(question, options, selected)
	if err != nil {
		return nil, err
	}
	chosen := make([]groups.Repo, 0, len(indices))
	for _, index := range indices {
		if index >= 0 && index < len(visibles) {
			chosen = append(chosen, visibles[index])
		}
	}
	return chosen, nil
}

// ------------------------------------------------------------------- filtres

// Menu du filtre et du tri.
var filterMenu = ui.Options(
	"chercher", "Chercher un nom ou un compte",
	"remise", "N'afficher qu'un état de remise",
	"apres", "Ne garder que les envois postérieurs à une date",
	"avant", "Ne garder que les envois antérieurs à une date",
	"muets", "N'afficher que les dépôts sans aucun envoi",
	"tri", "Changer le tri",
	"vider", "Tout effacer",
	"retour", "Revenir à la liste",
)

// États de remise proposés, dans l'ordre du domaine. « tous » dit qu'on ne
// retient rien : un menu sans porte de sortie enfermerait dans son dernier choix.
var handinMenu = handinOptions()

func handinOptions() []ui.Option {
	options := make([]ui.Option, 0, len(classroom.HandinStates))
	for _, etat := range classroom.HandinStates {
		libelle := string(etat)
		if etat == classroom.AnyHandin {
			libelle = "tous"
		}
		options = append(options, ui.Option{Value: string(etat), Label: libelle})
	}
	return options
}

// Colonnes de tri proposées.
var sortMenu = ui.Options(
	string(users.ByName), "Nom complet",
	string(users.ByUsername), "Compte GitHub",
	string(users.ByPushed), "Dernier envoi",
)

// filtrer règle ce que la liste montre et comment elle est ordonnée. Ce que
// signifient les critères est décidé dans « students » : le terminal ne fait
// que les recueillir, comme le navigateur les recueille dans sa barre.
func (m *manageSession) filtrer(group *groups.Group) error {
	for {
		m.session.Console.Note("%s", m.criteria())
		action, err := m.session.Prompt.Choose("Filtrer ou trier", filterMenu, "retour")
		if err != nil {
			return err
		}
		switch action {
		case "retour":
			return nil
		case "vider":
			m.filter = users.Filter{}
			m.handin = classroom.AnyHandin
			m.sortKey, m.sortDesc = users.ByName, false
		case "chercher":
			texte, err := m.session.Prompt.Ask(ui.Question{
				Title:      "Nom ou compte (vide pour tout afficher)",
				Default:    m.filter.Text,
				AllowEmpty: true,
			})
			if err != nil {
				return err
			}
			m.filter.Text = texte
		case "remise":
			if err := m.askHandin(); err != nil {
				return err
			}
		case "apres", "avant":
			if err := m.askDate(action); err != nil {
				return err
			}
		case "muets":
			// Un seul état à basculer : ou bien tout, ou bien ce qui n'a rien reçu.
			if m.filter.Activity == users.Silent {
				m.filter.Activity = users.AnyActivity
			} else {
				m.filter.Activity = users.Silent
			}
		case "tri":
			if err := m.askSort(); err != nil {
				return err
			}
		}
		if _, err := m.filter.Validate(); err != nil {
			m.session.Console.Failure("%v", err)
			continue
		}
		m.show(group)
	}
}

// askHandin recueille l'état de remise à ne garder. Les mots sont ceux du
// domaine : le navigateur propose les mêmes, et « en retard » y veut dire la
// même chose.
func (m *manageSession) askHandin() error {
	choix, err := m.session.Prompt.Choose("N'afficher que", handinMenu, string(m.handin))
	if err != nil {
		return err
	}
	etat, err := classroom.ParseHandinState(choix)
	if err != nil {
		return err
	}
	m.handin = etat
	return nil
}

// askDate recueille une des deux bornes du dernier envoi.
func (m *manageSession) askDate(borne string) error {
	titre, courant := "Dernier envoi après (AAAA-MM-JJ, vide pour aucune borne)", m.filter.PushedAfter
	if borne == "avant" {
		titre, courant = "Dernier envoi avant (AAAA-MM-JJ, vide pour aucune borne)",
			m.filter.PushedBefore
	}
	date, err := m.session.Prompt.Ask(ui.Question{
		Title: titre, Default: courant, AllowEmpty: true,
		Validate: func(value string) (string, error) {
			return users.ParseDate(value, "Dernier envoi")
		},
	})
	if err != nil {
		return err
	}
	if borne == "avant" {
		m.filter.PushedBefore = date
	} else {
		m.filter.PushedAfter = date
	}
	return nil
}

// askSort recueille la colonne de tri et son sens.
func (m *manageSession) askSort() error {
	choix, err := m.session.Prompt.Choose("Trier par", sortMenu, string(m.sortKey))
	if err != nil {
		return err
	}
	key, err := users.ParseKey(choix)
	if err != nil {
		return err
	}
	decroissant, err := m.session.Prompt.Confirm("Du plus grand au plus petit ?",
		key == users.ByPushed)
	if err != nil {
		return err
	}
	m.sortKey, m.sortDesc = key, decroissant
	return nil
}

// ------------------------------------------------------------------- actions

// detectTemplate retrouve le dépôt modèle utilisé par le groupe.
func (m *manageSession) detectTemplate(group *groups.Group) string {
	if group.Len() == 0 {
		return ""
	}
	var data *ghapi.Repo
	var err error
	ui.Await(m.session.Console, "Lecture du dépôt modèle…", func() {
		data, err = m.session.Client.GetRepo(m.org, group.Repos[0].Name)
	})
	if err != nil || data == nil || data.TemplateRepository == nil {
		return ""
	}
	return data.TemplateRepository.FullName
}

// addRepos ajoute des dépôts au groupe, pour les personnes non encore servies.
func (m *manageSession) addRepos(group *groups.Group) error {
	session, console := m.session, m.session.Console
	console.Heading("Ajouter des dépôts à « " + group.Prefix + " »")
	session.Settings.Assignment = group.Prefix

	template := m.detectTemplate(group)
	session.Settings.Template = template
	if template != "" {
		console.Printf("  Modèle réutilisé : %s %s", console.OK(template),
			console.Dim("(détecté sur les dépôts existants)"))
	} else {
		console.Note("Aucun dépôt modèle détecté : les dépôts seront créés neufs.")
	}

	// Un travail de la nomenclature nomme ses dépôts par elle, comme « creer »
	// et le navigateur : son gabarit ne se choisit pas. Seul un préfixe hérité
	// demande le sien, et c'est le seul qu'on retienne.
	if _, _, nomenclature := naming.SplitAssignment(group.Prefix); nomenclature {
		console.Printf("  Gabarit de nom : %s %s", console.OK(classroom.NamePattern),
			console.Dim("(celui de la nomenclature)"))
	} else {
		pattern, err := session.Prompt.Ask(ui.Question{
			Title:   "Gabarit de nom (doit reproduire celui du groupe)",
			Default: session.Settings.NamePattern,
			Validate: func(value string) (string, error) {
				return plan.ValidatePattern(value, "Gabarit de nom", true)
			},
		})
		if err != nil {
			return err
		}
		session.Settings.NamePattern = pattern
	}

	if template == "" {
		if err := m.askStarter(); err != nil {
			return err
		}
	}

	people, err := session.collectPeople()
	if err != nil {
		return err
	}
	people, err = session.verifyAccounts(people)
	if err != nil {
		return err
	}

	// Les personnes déjà servies sont écartées : leur dépôt existe déjà.
	set, _ := session.names(m.org)
	fresh, skipped := classroom.Pending(people, *group, set)
	if len(skipped) > 0 {
		console.Warning("%s dans le groupe :",
			valid.Plural(len(skipped), "personne déjà présente", "personnes déjà présentes"))
		for index, person := range skipped {
			if index == 10 {
				break
			}
			console.Printf("    %s %s", console.Dim("•"), nommer(person))
		}
	}
	if len(fresh) == 0 {
		console.Print("  " + console.Info("Rien à ajouter."))
		return nil
	}

	reglages := classroom.PlanSettings(session.Settings)
	items, err := plan.Build(fresh, reglages)
	if err != nil {
		return err
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Name, item.Person.FullName,
			colonneCompte(console, item.Person.Username)})
	}
	console.Table([]string{"Dépôt", "Personne", "Compte"}, rows, 20)

	confirmed, err := session.Prompt.Confirm(
		"Créer "+valid.Plural(len(items), "dépôt")+" dans « "+m.org+" » ?", false)
	if err != nil {
		return err
	}
	if !confirmed {
		console.Warning("Annulé.")
		return nil
	}
	// Comme pour « creer », la liste monte au registre une fois la création
	// confirmée et avant le premier dépôt : c'est lui qui dira, depuis
	// n'importe quel poste, à qui est chacun d'eux.
	if err := session.apprendre(m.org, people); err != nil {
		return err
	}

	width := 10
	for _, item := range items {
		if len(item.Name) > width {
			width = len(item.Name)
		}
	}
	// Un groupe cloisonné doit le rester : un dépôt ajouté va d'emblée à son
	// équipe enseignante, comme ceux que « creer » distribue.
	equipe, droit := session.teacherGrant(m.org, reglages.Assignment)
	executor := runner.New(session.Client, reglages, session.Starter).
		WithClock(session.Sleep, session.Now)
	// Les dépôts ajoutés sont signés comme les autres, et leurs marques montent
	// au même registre : sans lui, rien ne dirait jamais à qui elles sont.
	report, err := executor.Run(items, runner.Options{
		ForceStarter:      session.Options.ForceStarter,
		Sign:              !session.Settings.NoSign,
		Registry:          session.registryOf(m.org),
		TeacherTeam:       equipe,
		TeacherPermission: droit,
		OnProgress: func(index, total int, result runner.Result) {
			session.printProgress(index, total, result, width)
		},
	})
	if err != nil {
		return err
	}
	crees, presents := report.Count(runner.Created), report.Count(runner.Existing)
	console.Printf("  %s %s · %s %s · %s en échec",
		console.OK(itoa(crees)), valid.Agree(crees, "créé"),
		console.Info(itoa(presents)), valid.Agree(presents, "déjà présent", "déjà présents"),
		console.Err(itoa(len(report.Failures()))))
	session.direLesMarques(report)

	reportDir, err := roster.ExpandPath(session.Options.ReportDir)
	if err == nil {
		if jsonPath, _, err := report.Save(reportDir); err == nil {
			console.Note("Bilan : %s", jsonPath)
		}
	}
	_, err = m.loadRepos(true)
	return err
}

// askStarter propose de déposer des fichiers de départ dans les nouveaux dépôts.
func (m *manageSession) askStarter() error {
	session := m.session
	answer, err := session.Prompt.Ask(ui.Question{
		Title:      "Dossier de fichiers de départ (vide = aucun)",
		Default:    session.Settings.StarterDir,
		AllowEmpty: true,
		Complete:   complete.Dir,
	})
	if err != nil {
		return err
	}
	raw := strings.TrimSpace(answer)
	if raw == "" || clearKeywords[strings.ToLower(raw)] {
		session.Starter = nil
		return nil
	}
	bundle, err := starterLoad(raw)
	if err != nil {
		session.Console.Failure("%v", err)
		session.Starter = nil
		return nil
	}
	session.Starter = bundle
	session.Settings.StarterDir = bundle.Root
	session.Console.Printf("  Fichiers de départ : %s", session.Console.OK(bundle.Describe()))
	return nil
}

// accessOf relève les accès d'un dépôt. Un panneau ouvert sur un dépôt précis
// relit toujours : c'est là qu'on vient vérifier ce qu'on vient de changer.
func (m *manageSession) accessOf(repo groups.Repo) (identity.Access, error) {
	return m.resolver.AccessOf(m.org, repo.Name, identity.Refresh)
}

// showAccess affiche les accès de tous les dépôts du groupe. C'est deux
// requêtes par dépôt : le geste est explicite, il relit tout, et son résultat
// est mémorisé — l'interface web montre ensuite la même chose sans rien
// redemander.
func (m *manageSession) showAccess(group *groups.Group) error {
	console := m.session.Console
	console.Heading("Accès des dépôts de « " + group.Prefix + " »")
	noms := make([]string, 0, group.Len())
	for _, repo := range group.Repos {
		noms = append(noms, repo.Name)
	}

	progress := ui.NewProgress(console, "Dépôts", group.Len())
	lus := m.resolver.Accesses(m.org, noms, identity.Refresh,
		func(done, _ int, repo string) { progress.Update(done, repo) })
	progress.Finish("")

	rows := make([][]string, 0, len(noms))
	for _, nom := range noms {
		acces, inspecte := lus[nom]
		if !inspecte {
			continue
		}
		pending := make([]string, 0, len(acces.Invitations))
		for _, invitation := range acces.Invitations {
			if invitation.Expired {
				pending = append(pending, invitation.Login+" "+console.Err("(expirée)"))
				continue
			}
			pending = append(pending, invitation.Login+" (invité)")
		}
		rows = append(rows, []string{
			nom,
			orDim(console, strings.Join(acces.Collaborators, ", "), "aucun"),
			orDim(console, strings.Join(pending, ", "), "—"),
		})
	}
	// Un dépôt illisible — un jeton sans droit dessus — n'est pas inspecté. Le
	// taire ferait croire que personne n'y a accès.
	if manquants := len(noms) - len(rows); manquants > 0 {
		console.Warning("%s", valid.Plural(manquants,
			"dépôt n'a pas pu être lu : ses accès restent inconnus.",
			"dépôts n'ont pas pu être lus : leurs accès restent inconnus."))
	}
	console.Success("%s.", valid.Plural(len(rows), "dépôt inspecté", "dépôts inspectés"))
	console.Table([]string{"Dépôt", "Collaborateurs", "En attente"}, rows, 40)
	return nil
}

// manageCollaborators ajoute ou retire un accès sur un dépôt.
func (m *manageSession) manageCollaborators(group *groups.Group) error {
	repo, err := m.pickRepo(group, "Dépôt à gérer")
	if err != nil || repo == nil {
		return err
	}
	console := m.session.Console

	for {
		console.Heading("Accès de « " + repo.Name + " »")
		var acces identity.Access
		var err error
		ui.Await(console, "Lecture des accès de "+repo.Name+"…", func() {
			acces, err = m.accessOf(*repo)
		})
		if err != nil {
			console.Failure("%v", err)
			return nil
		}
		if len(acces.Collaborators) > 0 {
			coloured := make([]string, 0, len(acces.Collaborators))
			for _, login := range acces.Collaborators {
				coloured = append(coloured, console.OK(login))
			}
			console.Print("  Collaborateurs : " + strings.Join(coloured, ", "))
		} else {
			console.Note("Aucun collaborateur direct.")
		}
		for _, invitation := range acces.Invitations {
			if invitation.Expired {
				console.Warning("Invitation expirée : @%s", invitation.Login)
				continue
			}
			console.Note("Invitation en attente : @%s", invitation.Login)
		}

		actions := ui.Options(
			"ajouter", "Ajouter un collaborateur",
			"retirer", "Retirer un collaborateur ou annuler une invitation",
		)
		// L'envoi n'est proposé que s'il débloque quelque chose : une invitation
		// encore valable n'a pas besoin d'un second courriel.
		envois := m.envoisDuDepot(group, *repo, acces)
		if len(envois) > 0 {
			actions = append(actions, ui.Option{Value: "envoyer", Label: gesteDEnvoi(envois)})
		}
		actions = append(actions, ui.Option{Value: "revenir", Label: "Revenir au menu"})
		action, err := m.session.Prompt.Choose("Action", actions, "revenir")
		if err != nil {
			return err
		}
		switch action {
		case "revenir":
			return nil
		case "ajouter":
			if err := m.addCollaborator(*repo); err != nil {
				return err
			}
		case "envoyer":
			m.envoyerAuDepot(envois)
		default:
			if err := m.removeCollaborator(*repo, acces); err != nil {
				return err
			}
		}
	}
}

func (m *manageSession) addCollaborator(repo groups.Repo) error {
	username, err := m.session.Prompt.Ask(ui.Question{
		Title:      "Compte GitHub à inviter (vide pour annuler)",
		AllowEmpty: true,
		Validate:   func(value string) (string, error) { return valid.Login(value, "") },
	})
	if err != nil || username == "" {
		return err
	}
	if !m.compteExiste(username) {
		return nil
	}

	choices := make([]ui.Option, 0, len(config.Permissions))
	for _, value := range config.Permissions {
		choices = append(choices, ui.Option{Value: value, Label: config.PermissionLabels[value]})
	}
	permission, err := m.session.Prompt.Choose("Droit accordé", choices, m.session.Settings.Permission)
	if err != nil {
		return err
	}
	m.donnerAcces(repo, username, permission)
	return nil
}

// compteExiste vérifie qu'un compte existe avant de l'inviter : GitHub
// accepterait l'invitation d'un compte mal tapé, et personne ne la recevrait.
// Une vérification impossible n'arrête rien : elle se signale.
func (m *manageSession) compteExiste(username string) bool {
	console := m.session.Console
	var exists bool
	var err error
	ui.Await(console, "Vérification du compte @"+username+"…", func() {
		exists, err = m.session.Client.UserExists(username)
	})
	if err != nil {
		console.Warning("Vérification impossible : %v", err)
		return true
	}
	if !exists {
		console.Failure("Le compte « %s » n'existe pas sur GitHub.", username)
	}
	return exists
}

// donnerAcces invite un compte sur un dépôt, et dit ce que GitHub en a fait :
// un membre de l'organisation reçoit l'accès d'emblée, les autres une
// invitation. Rend faux quand l'invitation n'a pas pu partir.
func (m *manageSession) donnerAcces(repo groups.Repo, username, permission string) bool {
	console := m.session.Console
	var state string
	var err error
	ui.Await(console, "Envoi de l'invitation à @"+username+"…", func() {
		state, err = m.session.Client.AddCollaborator(m.org, repo.Name, username, permission)
	})
	if err != nil {
		console.Failure("Invitation impossible : %v", err)
		return false
	}
	// Ce qu'on savait des accès de ce dépôt vient de devenir faux.
	m.resolver.ForgetAccess(m.org, repo.Name)

	label := "accès accordé"
	if state == ghapi.CollaboratorInvited {
		label = "invitation envoyée"
	}
	console.Success("@%s : %s (%s).", username, label, permission)
	return true
}

// etiquetteDInvitation nomme une invitation dans un menu.
func etiquetteDInvitation(invitation identity.Invitation) string {
	if invitation.Expired {
		return "invitation expirée"
	}
	return "invitation en attente"
}

func (m *manageSession) removeCollaborator(repo groups.Repo, acces identity.Access) error {
	console := m.session.Console
	options := make([]ui.Option, 0, len(acces.Collaborators)+len(acces.Invitations)+1)
	for _, login := range acces.Collaborators {
		options = append(options, ui.Option{Value: "collaborateur:" + login, Label: login + " — collaborateur"})
	}
	for _, invitation := range acces.Invitations {
		options = append(options, ui.Option{
			Value: "invitation:" + strconv.FormatInt(invitation.ID, 10),
			Label: invitation.Login + " — " + etiquetteDInvitation(invitation),
		})
	}
	if len(options) == 0 {
		console.Note("Personne à retirer.")
		return nil
	}
	options = append(options, ui.Option{Value: "revenir", Label: "Revenir"})

	choice, err := m.session.Prompt.Choose("Qui retirer ?", options, "revenir")
	if err != nil || choice == "revenir" {
		return err
	}

	if strings.HasPrefix(choice, "invitation:") {
		identifier, _ := strconv.ParseInt(strings.TrimPrefix(choice, "invitation:"), 10, 64)
		confirmed, err := m.session.Prompt.Confirm("Annuler cette invitation ?", false)
		if err != nil || !confirmed {
			return err
		}
		m.annulerInvitation(repo, identifier)
		return nil
	}

	login := strings.TrimPrefix(choice, "collaborateur:")
	confirmed, err := m.session.Prompt.Confirm(
		"Retirer l'accès de @"+login+" à « "+repo.Name+" » ?", false)
	if err != nil || !confirmed {
		return err
	}
	m.retirerCollaborateur(repo, login)
	return nil
}

// annulerInvitation retire une invitation d'un dépôt. Rend faux quand GitHub a
// refusé.
func (m *manageSession) annulerInvitation(repo groups.Repo, identifier int64) bool {
	if err := m.session.Client.CancelInvitation(m.org, repo.Name, identifier); err != nil {
		m.session.Console.Failure("%v", err)
		return false
	}
	m.resolver.ForgetAccess(m.org, repo.Name)
	m.session.Console.Success("Invitation annulée.")
	return true
}

// retirerCollaborateur retire à un compte son accès à un dépôt. Rend faux quand
// GitHub a refusé.
func (m *manageSession) retirerCollaborateur(repo groups.Repo, login string) bool {
	if err := m.session.Client.RemoveCollaborator(m.org, repo.Name, login); err != nil {
		m.session.Console.Failure("%v", err)
		return false
	}
	m.resolver.ForgetAccess(m.org, repo.Name)
	m.session.Console.Success("@%s n'a plus accès à « %s ».", login, repo.Name)
	return true
}

// urls affiche les adresses des dépôts, prêtes à être copiées.
func (m *manageSession) urls(group *groups.Group) error {
	console := m.session.Console
	console.Heading("URL des dépôts de « " + group.Prefix + " »")
	chosen, err := m.pickMany(group, "Dépôts à lister")
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		console.Warning("Aucun dépôt sélectionné.")
		return nil
	}

	m.afficherURL(chosen)

	save, err := m.session.Prompt.Confirm("Enregistrer cette liste dans un fichier ?", false)
	if err != nil || !save {
		return err
	}
	target, err := m.session.Prompt.Ask(ui.Question{
		Title:    "Chemin du fichier",
		Default:  group.Prefix + "-urls.csv",
		Complete: complete.Path,
	})
	if err != nil {
		return err
	}
	if err := m.exporterCSV(group, chosen, target); err != nil {
		console.Failure("%v", err)
	}
	return nil
}

// afficherURL écrit l'adresse de chaque dépôt, sans décor ni indentation : la
// liste doit se copier telle quelle.
func (m *manageSession) afficherURL(depots []groups.Repo) {
	console := m.session.Console
	console.Blank()
	for _, repo := range depots {
		console.Print(m.session.urlOf(repo.Name, repo.URL))
	}
	console.Blank()
}

// exporterCSV enregistre les dépôts avec le nom de chacun et son adresse. Ce
// sont les colonnes de l'export du navigateur, et un dépôt dont on ne connaît
// pas la personne y porte son suffixe, comme là-bas.
func (m *manageSession) exporterCSV(group *groups.Group, depots []groups.Repo, cible string) error {
	path, err := roster.ExpandPath(cible)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return valid.Errorf("Enregistrement impossible : %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return valid.Errorf("Enregistrement impossible : %v", err)
	}
	defer file.Close()

	names := m.names(group)
	records := [][]string{{"nom_complet", "depot", "url"}}
	for _, repo := range depots {
		nom := names[repo.Name]
		if nom == "" {
			nom = repo.Suffix
		}
		records = append(records, []string{nom, repo.Name, m.session.urlOf(repo.Name, repo.URL)})
	}
	if err := csv.NewWriter(file).WriteAll(records); err != nil {
		return valid.Errorf("Enregistrement impossible : %v", err)
	}
	m.session.Console.Success("%s dans %s",
		valid.Plural(len(depots), "URL enregistrée", "URL enregistrées"), path)
	return nil
}

// cloneRepos récupère tout ou partie du groupe dans un dossier local.
func (m *manageSession) cloneRepos(group *groups.Group) error {
	session, console := m.session, m.session.Console
	console.Heading("Cloner des dépôts de « " + group.Prefix + " »")
	chosen, err := m.pickMany(group, "Dépôts à cloner")
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		console.Warning("Aucun dépôt sélectionné.")
		return nil
	}

	answer, err := session.Prompt.Ask(ui.Question{
		Title:      "Dossier de destination (« - » pour annuler)",
		Default:    m.dossierDesClones(group),
		AllowEmpty: true,
		Complete:   complete.Dir,
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(answer) == "" || clearKeywords[strings.ToLower(strings.TrimSpace(answer))] {
		console.Warning("Annulé.")
		return nil
	}
	destination, err := clone.PrepareDestination(answer)
	if err != nil {
		return err
	}

	console.Printf("  %s %s vers %s%s", console.Bold(itoa(len(chosen))), valid.Agree(len(chosen), "dépôt"),
		console.Info(destination), console.Dim(" · "+itoa(session.Options.Jobs)+" en parallèle"))
	confirmed, err := session.Prompt.Confirm("Lancer le clonage ?", true)
	if err != nil {
		return err
	}
	if !confirmed {
		console.Warning("Annulé.")
		return nil
	}
	_, err = m.cloner(chosen, destination)
	return err
}

// dossierDesClones est le dossier que l'assistant propose pour les clones d'un
// préfixe : celui des clones, réglé ou par défaut, suivi du préfixe.
func (m *manageSession) dossierDesClones(group *groups.Group) string {
	parent := m.session.Settings.CloneDir
	if parent == "" {
		parent = clone.DefaultParent()
	}
	return filepath.Join(parent, group.Prefix)
}

// cloner récupère des dépôts dans un dossier déjà préparé, dit ce qu'il en est
// advenu et rend le nombre d'échecs. Le dossier parent devient celui des
// clones : la fois suivante, c'est lui qu'on propose.
func (m *manageSession) cloner(chosen []groups.Repo, destination string) (int, error) {
	session, console := m.session, m.session.Console
	targets := make([]clone.Target, 0, len(chosen))
	for _, repo := range chosen {
		targets = append(targets, clone.Target{Name: repo.Name, URL: session.urlOf(repo.Name, repo.URL)})
	}
	progress := ui.NewProgress(console, "Clonage", len(targets))
	results, err := clone.New(session.Options.Jobs, session.Options.Depth).
		Run(targets, destination, func(done, total int, result clone.Result) {
			progress.Update(done, result.Name)
		})
	progress.Finish("")
	if err != nil {
		return 0, err
	}

	counts := map[string]int{}
	for _, result := range results {
		counts[result.Status]++
	}
	console.Printf("  %s %s · %s mis à jour · %s %s · %s en échec",
		console.OK(itoa(counts[clone.Cloned])), valid.Agree(counts[clone.Cloned], "cloné"),
		console.Info(itoa(counts[clone.Updated])),
		console.Dim(itoa(counts[clone.Skipped])), valid.Agree(counts[clone.Skipped], "ignoré"),
		console.Err(itoa(counts[clone.Failed])))
	for _, result := range results {
		if result.Status == clone.Failed || result.Status == clone.Skipped {
			icon := console.Warn("·")
			if result.IsFailed() {
				icon = console.Err("✗")
			}
			console.Printf("    %s %s : %s", icon, result.Name, result.Error)
		}
	}
	console.Printf("  Dossier : %s", console.Dim(destination))
	session.Settings.CloneDir = filepath.Dir(destination)
	return counts[clone.Failed], nil
}

// pullClones met à jour des clones existants, sans jamais écraser un travail local.
func (m *manageSession) pullClones(group *groups.Group) error {
	session, console := m.session, m.session.Console
	console.Heading("Mettre à jour des clones")

	var clones []clone.Clone
	var folder string
	for {
		answer, err := session.Prompt.Ask(ui.Question{
			Title:      "Dossier contenant les clones (« - » pour annuler)",
			Default:    m.dossierDesClones(group),
			AllowEmpty: true,
			Complete:   complete.Dir,
		})
		if err != nil {
			return err
		}
		trimmed := strings.TrimSpace(answer)
		if trimmed == "" || clearKeywords[strings.ToLower(trimmed)] {
			console.Warning("Annulé.")
			return nil
		}
		found, err := clone.FindClones(trimmed)
		if err != nil {
			console.Failure("%v", err)
			continue
		}
		if len(found) == 0 {
			console.Failure("Aucun dépôt git directement sous « %s ».", trimmed)
			continue
		}
		clones, folder = found, trimmed
		break
	}

	// Les clones étrangers au groupe restent visibles, mais signalés.
	options := make([]ui.Option, 0, len(clones))
	selected := make([]bool, len(clones))
	for index, item := range clones {
		label := item.Name
		if _, _, found := group.Find(item.Name); !found {
			label += "   (hors groupe)"
		}
		options = append(options, ui.Option{Value: item.Name, Label: label})
		selected[index] = true
	}
	indices, err := session.Prompt.MultiSelect("Clones à mettre à jour", options, selected)
	if err != nil {
		return err
	}
	chosen := make([]clone.Clone, 0, len(indices))
	for _, index := range indices {
		chosen = append(chosen, clones[index])
	}
	if len(chosen) == 0 {
		console.Warning("Aucun clone sélectionné.")
		return nil
	}
	confirmed, err := session.Prompt.Confirm("Mettre à jour "+valid.Plural(len(chosen), "clone")+" ?", true)
	if err != nil || !confirmed {
		if err == nil {
			console.Warning("Annulé.")
		}
		return err
	}
	_, err = m.mettreAJour(chosen, folder)
	return err
}

// mettreAJour met à jour des clones, sans jamais écraser un travail local, et
// rend le nombre d'échecs. Le dossier parent devient celui des clones.
func (m *manageSession) mettreAJour(chosen []clone.Clone, folder string) (int, error) {
	session, console := m.session, m.session.Console
	progress := ui.NewProgress(console, "Mise à jour", len(chosen))
	results, err := clone.New(session.Options.Jobs, 0).
		Update(chosen, func(done, total int, result clone.Result) {
			progress.Update(done, result.Name)
		})
	progress.Finish("")
	if err != nil {
		return 0, err
	}

	updated := 0
	var failed []clone.Result
	for _, result := range results {
		if result.Status == clone.Updated {
			updated++
			continue
		}
		failed = append(failed, result)
	}
	console.Printf("  %s mis à jour · %s en échec",
		console.OK(itoa(updated)), console.Err(itoa(len(failed))))
	for _, result := range failed {
		console.Printf("    %s %s : %s", console.Err("✗"), result.Name, result.Error)
	}
	if absolute, err := filepath.Abs(folder); err == nil {
		session.Settings.CloneDir = filepath.Dir(absolute)
	}
	return len(failed), nil
}

// deleteRepo supprime définitivement un dépôt, après confirmation renforcée.
func (m *manageSession) deleteRepo(group *groups.Group) error {
	console := m.session.Console
	repo, err := m.pickRepo(group, "Dépôt à supprimer")
	if err != nil || repo == nil {
		return err
	}

	// Une portée absente se répare avant la tentative plutôt qu'après : rien
	// n'est encore engagé, et la suppression échouerait de toute façon.
	if !m.session.ensureScope("delete_repo", "la suppression serait refusée") {
		console.Warning("Annulé : rien n'a été supprimé.")
		return nil
	}

	m.avertirDeLaSuppression(*repo)

	// Aucune option ne court-circuite cette confirmation : le nom exact du
	// dépôt doit être retapé.
	typed, err := m.session.Prompt.Ask(ui.Question{
		Title:      "Retapez « " + repo.Name + " » pour confirmer (vide pour annuler)",
		AllowEmpty: true,
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(typed) != repo.Name {
		console.Warning("Annulé : rien n'a été supprimé.")
		return nil
	}
	m.supprimer(*repo)
	return nil
}

// avertirDeLaSuppression dit ce qui va disparaître, avant qu'on le confirme.
func (m *manageSession) avertirDeLaSuppression(repo groups.Repo) {
	console := m.session.Console
	console.Print("  " + console.Err("⚠ Suppression définitive de "+m.org+"/"+repo.Name))
	console.Note("   Le contenu, les tickets et l'historique seront perdus.")
	if repo.URL != "" {
		console.Note("   %s", repo.URL)
	}
}

// supprimer supprime un dépôt dont la suppression vient d'être confirmée, et
// rend faux quand elle n'a pas eu lieu.
func (m *manageSession) supprimer(repo groups.Repo) bool {
	console := m.session.Console
	var removal error
	attempt := func() {
		ui.Await(console, "Suppression de "+repo.Name+"…", func() {
			removal = m.session.Client.DeleteRepo(m.org, repo.Name)
		})
	}
	attempt()
	// GitHub peut refuser sur une portée qu'aucune vérification préalable ne
	// pouvait connaître — un jeton « fine-grained » n'annonce rien. Le jeton se
	// refait alors sur place, et la suppression se reprend : elle n'a pas eu lieu.
	if scope := ghapi.MissingScope(removal); scope != "" && m.session.offerScope(scope) {
		attempt()
	}
	if removal != nil {
		console.Failure("Suppression impossible : %v", removal)
		return false
	}
	console.Success("« %s » supprimé.", repo.Name)
	m.suivre(func(repos []groups.RepoInfo) []groups.RepoInfo {
		return groups.WithoutRepo(repos, repo.Name)
	})
	return true
}

// -------------------------------------------------- déplacer vers un groupe

// Un travail est parfois rangé sous un préfixe qui n'est pas le sien : une
// organisation reprise en cours de route mélange sous « travail-de » ce qui
// appartient à plusieurs groupes et à plusieurs sessions. Le sortir de là, c'est
// renommer ses dépôts pour qu'ils tiennent à une place de la nomenclature
// courante — et l'assistant sait le faire, puisqu'un travail y est exactement ce
// dont il a besoin : un préfixe et ses dépôts.
//
// Le dernier niveau du nom est conservé tel quel : l'assistant ne tient pas de
// liste d'étudiants, et rien ne l'autoriserait à inventer un nom complet.

// relocate demande la place d'arrivée, puis y déplace le travail.
func (m *manageSession) relocate(group *groups.Group) (int, error) {
	if err := m.plusieursTravaux(group, "Un déplacement"); err != nil {
		return ExitOK, err
	}
	place, err := m.session.Prompt.Ask(ui.Question{
		Title:      "Place d'arrivée « session.cours.groupe » (vide pour annuler)",
		AllowEmpty: true,
	})
	if err != nil || strings.TrimSpace(place) == "" {
		return ExitOK, err
	}
	name, err := m.session.Prompt.Ask(ui.Question{
		Title:   "Nom du travail à l'arrivée",
		Default: shortAssignmentName(group.Prefix),
	})
	if err != nil {
		return ExitOK, err
	}
	return m.relocateTo(group, place, name)
}

// relocateTo renomme les dépôts du travail pour qu'ils tiennent à la place
// donnée. Le plan se montre en entier avant la première écriture : c'est la
// seule façon de vérifier que ce sont bien ces dépôts-là qu'on déplace.
func (m *manageSession) relocateTo(group *groups.Group, place, name string) (int, error) {
	if err := m.plusieursTravaux(group, "Un déplacement"); err != nil {
		return ExitOK, err
	}
	console := m.session.Console
	repos, err := m.loadRepos(false)
	if err != nil {
		return ExitOK, err
	}
	arrivee, err := classroom.AtScope(m.org, place,
		classroom.DefaultsFrom(m.session.Settings))
	if err != nil {
		return ExitOK, err
	}
	lignes, err := classroom.PlanRelocate(arrivee, name, group.Repos, nil, repos)
	if err != nil {
		return ExitOK, err
	}
	if len(lignes) == 0 {
		console.Warning("Ces dépôts sont déjà à cette place.")
		return ExitOK, nil
	}

	console.Heading("« " + group.Prefix + " » vers " + arrivee.Scope())
	console.Note("Le dernier niveau est conservé tel quel : l'assistant ne tient pas de " +
		"liste d'étudiants. L'interface web y met le nom complet quand elle le connaît.")
	return m.appliquerRenommage(lignes, func(faits int) string {
		return valid.Plural(faits, "dépôt déplacé", "dépôts déplacés") +
			" vers « " + arrivee.Scope() + " »."
	})
}

// appliquerRenommage montre le plan, le fait confirmer, puis renomme. Déplacer
// un travail et le renommer aboutissent tous deux ici : c'est la même écriture,
// et elle ne doit se raconter que d'une façon. Seule la phrase de succès
// distingue les deux ; elle reçoit le nombre de dépôts renommés, qui l'accorde.
func (m *manageSession) appliquerRenommage(lignes []classroom.Move,
	succes func(faits int) string) (int, error) {
	suivis, code, err := m.session.renommerDepots(m.org, lignes, succes)
	m.suivre(func(repos []groups.RepoInfo) []groups.RepoInfo {
		return groups.WithRenamed(repos, suivis)
	})
	return code, err
}

// renommerDepots est cette écriture-là, sans l'inventaire que la gestion tient
// à jour : déplacer une équipe la fait aussi, et depuis un autre écran.
func (s *Session) renommerDepots(org string, lignes []classroom.Move, succes func(int) string) (
	[]groups.Renamed, int, error) {
	console := s.Console
	s.montrerRenommages(lignes)
	if s.Options.DryRun {
		console.Warning("Simulation : aucun dépôt n'a été renommé.")
		return nil, ExitOK, nil
	}
	if !s.Options.Yes {
		suite, err := s.Prompt.Confirm(valid.Agree(len(lignes), "Renommer ce dépôt ?",
			"Renommer ces "+itoa(len(lignes))+" dépôts ?"), false)
		if err != nil || !suite {
			console.Warning("Annulé : rien n'a été renommé.")
			return nil, ExitOK, err
		}
	}
	return s.executerRenommages(org, lignes, succes)
}

// montrerRenommages écrit le plan : chaque dépôt, et le nom qu'il prendra.
func (s *Session) montrerRenommages(lignes []classroom.Move) {
	rows := make([][]string, 0, len(lignes))
	for index, ligne := range lignes {
		rows = append(rows, []string{itoa(index + 1), ligne.Repo, ligne.Target})
	}
	s.Console.Table([]string{"#", "Dépôt actuel", "Nouveau nom"}, rows, 40)
	s.Console.Note("GitHub garde une redirection depuis chaque ancien nom.")
}

// executerRenommages renomme, sans rien demander : le plan a déjà été montré et
// accepté. Elle rend ce qui a été renommé, pour que l'inventaire le suive.
func (s *Session) executerRenommages(org string, lignes []classroom.Move, succes func(int) string) (
	[]groups.Renamed, int, error) {
	console := s.Console
	progress := ui.NewProgress(console, "Renommage", len(lignes))
	renommes, echecs := 0, 0
	var suivis []groups.Renamed
	for index, ligne := range lignes {
		apres, err := s.Client.RenameRepo(org, ligne.Repo, ligne.Target)
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

	if echecs > 0 {
		console.Warning("%s, %d en échec.",
			valid.Plural(renommes, "dépôt renommé", "dépôts renommés"), echecs)
		return suivis, ExitFailure, nil
	}
	console.Success("%s", succes(renommes))
	return suivis, ExitOK, nil
}

// depotsRenommes dit combien de dépôts ont pris leur nouveau nom.
func depotsRenommes(faits int) string {
	return valid.Plural(faits, "dépôt renommé", "dépôts renommés") + "."
}

// ------------------------------------------------------ renommer le travail

// Un travail mal nommé — une faute de frappe distribuée à trente personnes — se
// corrige sans le déplacer. Son nom est un niveau du nom de chaque dépôt : il
// n'y a pas de fiche où le changer, les dépôts sont tout ce qu'un travail est.
//
// Cela demande que le préfixe géré dise déjà à quel groupe il appartient : sans
// place, un nom seul ne compose pas un nom de dépôt lisible. Un préfixe hérité
// se déplace donc d'abord — et il prend le nom qu'on veut au passage.

// rename demande le nouveau nom, puis renomme le travail.
func (m *manageSession) rename(group *groups.Group) (int, error) {
	name, err := m.session.Prompt.Ask(ui.Question{
		Title:   "Nouveau nom du travail",
		Default: shortAssignmentName(group.Prefix),
	})
	if err != nil {
		return ExitOK, err
	}
	return m.renameTo(group, name)
}

// renameTo renomme les dépôts du travail pour qu'ils portent le nom donné, sans
// changer de place. Le plan se montre en entier avant la première écriture.
func (m *manageSession) renameTo(group *groups.Group, name string) (int, error) {
	repos, err := m.loadRepos(false)
	if err != nil {
		return ExitOK, err
	}
	place, _, reconnu := naming.SplitAssignment(group.Prefix)
	if !reconnu {
		if err := m.plusieursTravaux(group, "Un renommage"); err != nil {
			return ExitOK, err
		}
		return ExitOK, valid.Errorf(
			"« %s » ne dit pas à quel groupe il appartient : renommer ce travail y "+
				"composerait un nom de dépôt illisible. Déplacez-le d'abord vers une place "+
				"« session.cours.groupe » — il prend le nom voulu au passage.", group.Prefix)
	}
	cours, err := classroom.AtScope(m.org, place,
		classroom.DefaultsFrom(m.session.Settings))
	if err != nil {
		return ExitOK, err
	}
	// Le nom est mis en forme ici plutôt que dans le plan seul : c'est lui qui
	// retrouvera le travail renommé, et il doit être celui des dépôts.
	fragment, err := naming.Fragment(name, "Travail")
	if err != nil {
		return ExitOK, err
	}
	lignes, err := classroom.PlanRenameAssignment(cours, group.Prefix, fragment, repos)
	if err != nil {
		return ExitOK, err
	}

	m.session.Console.Heading("« " + group.Prefix + " » vers « " + fragment + " »")
	code, err := m.appliquerRenommage(lignes, depotsRenommes)
	if err != nil || code != ExitOK {
		return code, err
	}

	// Le travail répond désormais à son nouveau nom : la session le suit, plutôt
	// que de retomber sur un préfixe qui ne désigne plus rien. Une simulation ou
	// un refus n'ont rien renommé : le préfixe reste alors celui qu'il était.
	repos, err = m.loadRepos(false)
	if err != nil {
		return ExitOK, err
	}
	if renomme := groups.Build(cours.AssignmentID(fragment), repos); renomme.Len() > 0 {
		*group = renomme
	}
	return ExitOK, nil
}

// shortAssignmentName propose le nom du travail à l'arrivée : le dernier segment
// du préfixe, qui est presque toujours ce qui le nomme — « a26-5n6-tp1 » donne
// « tp1 ». Ce n'est qu'une proposition ; c'est bien souvent ce nom-là qu'on
// voulait corriger.
func shortAssignmentName(prefix string) string {
	segments := strings.FieldsFunc(prefix, func(lettre rune) bool {
		return lettre == '-' || lettre == '.'
	})
	if len(segments) == 0 {
		return prefix
	}
	return segments[len(segments)-1]
}

// ------------------------------------------------------------------- pilote

// run enchaîne les actions de gestion jusqu'à la sortie.
func (m *manageSession) run() (int, error) {
	// Ce qui se préparait en arrière-plan n'a plus personne à servir.
	defer m.prechargement.Stop()

	for {
		group, err := m.chooseGroup()
		if err != nil {
			return ExitOK, err
		}
		if group == nil {
			return ExitOK, nil
		}

		// « --move-to » ne fait qu'une chose, et s'en va : le travail change de
		// place, et le préfixe par lequel on est entré n'existe plus.
		if strings.TrimSpace(m.session.Options.MoveTo) != "" {
			place := m.session.Options.MoveTo
			name := strings.TrimSpace(m.session.Options.RenameTo)
			if name == "" {
				name = shortAssignmentName(group.Prefix)
			}
			m.session.Options.MoveTo = ""
			return m.relocateTo(group, place, name)
		}

		// Sans place d'arrivée, « --rename-to » renomme le travail là où il est.
		// C'est le même drapeau : il dit le nom que le travail prend, que la
		// place change ou non.
		if name := strings.TrimSpace(m.session.Options.RenameTo); name != "" {
			m.session.Options.RenameTo = ""
			return m.renameTo(group, name)
		}

		// « --due » et « --handins » font une chose et s'en vont : l'un fixe la
		// date cible, l'autre relève les historiques. Les deux ensemble se
		// suivent — fixer une échéance puis voir qui la dépasse est un geste.
		if m.session.Options.DueSet {
			m.session.Options.DueSet = false
			if err := m.dueFromFlag(group); err != nil {
				return ExitValidation, err
			}
			if !m.session.Options.Handins {
				return ExitOK, nil
			}
		}
		if m.session.Options.Handins {
			m.session.Options.Handins = false
			if err := m.remises(group); err != nil {
				return ExitValidation, err
			}
			return ExitOK, nil
		}
		// « --send-invitations » fait une chose et s'en va, comme
		// « --handins » : le drapeau est la demande, il n'y a rien à confirmer.
		if m.session.Options.SendInvitations {
			m.session.Options.SendInvitations = false
			echecs, err := m.envoyerManquantes(group, false)
			if err != nil {
				return ExitValidation, err
			}
			if echecs > 0 {
				return ExitFailure, nil
			}
			return ExitOK, nil
		}
		// « --push-file » aussi : le fichier part, le bilan dit où, et les
		// échecs font le code de sortie.
		if m.session.Options.PushFile != "" {
			echecs, err := m.deposerDepuisDrapeaux(group)
			m.session.Options.PushFile = ""
			if err != nil {
				return ExitValidation, err
			}
			if echecs > 0 {
				return ExitFailure, nil
			}
			return ExitOK, nil
		}
		if m.session.Options.PublishIndex {
			m.session.Options.PublishIndex = false
			if err := m.publierIndex(group); err != nil {
				return ExitValidation, err
			}
			return ExitOK, nil
		}
		if m.session.Options.ExportZip != "" {
			destination := m.session.Options.ExportZip
			m.session.Options.ExportZip = ""
			if err := m.exporter(group, destination); err != nil {
				return ExitValidation, err
			}
			return ExitOK, nil
		}
		// « --plagiarism » fait une chose et s'en va : il compare les copies du
		// travail géré, écrit son rapport, et dit où il est.
		if m.session.Options.Plagiarism {
			m.session.Options.Plagiarism = false
			if err := m.plagiat(group); err != nil {
				return ExitValidation, err
			}
			return ExitOK, nil
		}

		// Les gestes que l'assistant range dans ses sections ont aussi leur
		// drapeau, et chacun fait une chose et s'en va.
		if code, fait, err := m.gesteDeDrapeau(group); fait {
			return code, err
		}

		// Passé les drapeaux qui font une chose et s'en vont, l'assistant reste
		// ouvert : ce qu'on va y regarder se prépare pendant qu'on choisit.
		m.precharger()

		showList := true
		for {
			// La liste n'est redonnée que si elle a changé : sinon elle chasserait
			// de l'écran le résultat de l'action qu'on vient de lancer.
			if showList {
				m.show(group)
			}
			action, err := m.choisirGeste()
			if err != nil {
				return ExitOK, err
			}
			if action == "quitter" {
				return ExitOK, nil
			}
			if action == "changer" {
				break
			}
			if action == "revenir" {
				showList = false
				continue
			}

			if err := m.dispatch(action, group); err != nil {
				if valid.IsValidation(err) || ghapi.IsGitHub(err) {
					m.session.Console.Failure("%v", err)
				} else {
					return ExitOK, err
				}
			}

			// « filtrer » a déjà remontré la liste à chaque changement.
			showList = action == "ajouter" || action == "supprimer" ||
				action == "rafraichir" || action == "deplacer" || action == "renommer" ||
				action == "etudiant"
			if showList {
				repos, err := m.loadRepos(false)
				if err != nil {
					return ExitOK, err
				}
				refreshed := groups.Build(group.Prefix, repos)
				// Un travail déplacé ne répond plus à son ancien préfixe : il
				// n'y a plus rien à gérer ici, on retourne au choix du groupe.
				if refreshed.Len() == 0 {
					m.session.Console.Note("Plus aucun dépôt dans « %s ».",
						group.Prefix)
					break
				}
				group = &refreshed
			}
		}
	}
}

func (m *manageSession) dispatch(action string, group *groups.Group) error {
	switch action {
	case "ajouter":
		return m.addRepos(group)
	case "inspecter":
		return m.showAccess(group)
	case "collaborateurs":
		return m.manageCollaborators(group)
	case "invitations":
		_, err := m.envoyerManquantes(group, true)
		return err
	case "urls":
		return m.urls(group)
	case "fichier":
		return m.deposerFichier(group)
	case "cloner":
		return m.cloneRepos(group)
	case "pull":
		return m.pullClones(group)
	case "supprimer":
		return m.deleteRepo(group)
	case "relever":
		return m.remises(group)
	case "reunir":
		return m.reunirEtranger(group)
	case "plagiat":
		return m.plagiat(group)
	case "envoi":
		return m.demanderEnvoi(group)
	case "publier":
		return m.publierIndex(group)
	case "demandes":
		return m.demandes()
	case "echeance":
		return m.echeance(group)
	case "renommer":
		_, err := m.rename(group)
		return err
	case "deplacer":
		_, err := m.relocate(group)
		return err
	case "enseignants":
		// Le groupe géré se désigne par son préfixe, et l'équipe enseignante
		// appartient à ce groupe-là.
		_, err := m.session.teachingMode(group.Prefix)
		return err
	case "etudiant":
		return m.corriger(group)
	case "filtrer":
		return m.filtrer(group)
	case "rafraichir":
		_, err := m.loadRepos(true)
		return err
	}
	return nil
}

func orDim(console *ui.Console, value, fallback string) string {
	if value == "" {
		return console.Dim(fallback)
	}
	return value
}
