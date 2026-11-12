package app

import (
	"sort"
	"strings"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Reprendre des dépôts qu'une autre convention a nommés — ceux de GitHub
// Classroom, « travail-compte ».
//
// L'écran suit l'ordre des questions : quels travaux sont là, qui est derrière
// chaque compte, où cela doit arriver. Rien n'est écrit avant le récapitulatif
// et l'accord.

// importSession tient l'écran d'importation.
type importSession struct {
	session *Session
	org     string
	// retenus nomme les dépôts que la reprise garde. Vide, elle les prend tous.
	retenus []string
	// proprietaires dit, pour un dépôt, le compte GitHub que ses accès
	// désignent. Relevé une fois le travail choisi, il sert à chaque plan
	// refait : un rapprochement corrigé ne doit pas rendre son compte au nom.
	proprietaires map[string]string
}

// importRepos déroule l'importation, de la découverte au renommage.
func (s *Session) importRepos() (int, error) {
	ecran := &importSession{session: s, org: s.Settings.Org}
	return ecran.run()
}

func (i *importSession) run() (int, error) {
	console := i.session.Console
	console.Heading("Reprendre des dépôts de « " + i.org + " »")

	repos, err := i.session.orgRepos(i.org, false)
	if err != nil {
		return ExitFailure, err
	}
	dehors := classroom.ForeignOf(repos)
	if len(dehors.Repos) == 0 {
		console.Note("Tous les dépôts de « %s » suivent déjà la nomenclature.", i.org)
		return ExitOK, nil
	}

	prefixe, err := i.choisirTravail(dehors)
	if err != nil || prefixe == "" {
		return ExitOK, err
	}
	i.retenus, err = i.choisirDepots(prefixe, repos)
	if err != nil {
		return ExitValidation, err
	}
	// Qui a accès à quoi se lit avant la liste : c'est ce qui dit le compte de
	// chaque dépôt, donc lesquels l'organisation nomme déjà — et si cette liste
	// a encore quelque chose à apprendre.
	i.proprietaires = i.acces(prefixe, i.retenus, repos)

	// Un travail d'équipe se reprend autrement : le dernier niveau du nom
	// désigne une équipe, il n'y a donc rien à rapprocher d'une liste — et pas
	// de liste à charger.
	equipe, err := i.enEquipeDemandee(prefixe, repos)
	if err != nil {
		return ExitOK, err
	}
	// La liste sert dans les deux cas : les équipes disent qui a fait le
	// travail, elle dit comment ces gens s'appellent.
	entrees, fichier, err := i.charger(prefixe, repos)
	if err != nil {
		return ExitOK, err
	}
	debut := classroom.AssignmentStart(prefixe, repos, func(depot string) (time.Time, error) {
		return i.session.Client.FirstCommit(i.org, depot)
	})
	place, err := i.choisirPlace(classroom.GuessPlace(fichier, entrees, debut))
	if err != nil {
		return ExitOK, err
	}
	nom := strings.TrimSpace(i.session.Options.RenameTo)
	if nom == "" {
		nom = prefixe
	}

	arrivee, err := classroom.AtScope(i.org, place,
		classroom.DefaultsFrom(i.session.Settings))
	if err != nil {
		return ExitValidation, err
	}
	if equipe {
		return i.enEquipe(arrivee, prefixe, nom, entrees, repos)
	}
	plan, err := classroom.PlanImport(arrivee, classroom.ImportRequest{
		Prefix: prefixe, Name: nom, Entries: entrees,
		Profiles: i.profils(prefixe, repos), Guess: true,
		NamedOnly: i.session.Options.NamedOnly, Owners: i.proprietaires,
		Known: i.connus(), Only: i.retenus,
	}, repos)
	if err != nil {
		return ExitValidation, err
	}
	if plan.Divided() {
		choisi, err := i.choisirParmiLesCaches(plan)
		if err != nil || choisi == "" {
			return ExitOK, err
		}
		if strings.TrimSpace(i.session.Options.RenameTo) == "" {
			nom = choisi
		}
		plan, err = classroom.PlanImport(arrivee, classroom.ImportRequest{
			Prefix: choisi, Name: nom, Entries: entrees,
			Profiles: i.profils(choisi, repos), Guess: true,
			NamedOnly: i.session.Options.NamedOnly, Owners: i.proprietaires,
			Known: i.connus(), Only: i.retenus,
		}, repos)
		if err != nil {
			return ExitValidation, err
		}
	}
	i.montrer(plan)

	// Une ressemblance douteuse se tranche ici, avant que quoi que ce soit ne
	// soit écrit : c'est le moment où cela ne coûte rien.
	if i.session.Interactive() && !i.session.Options.Yes {
		corrige, err := i.corriger(arrivee, plan, repos)
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
		suite, err := i.session.Prompt.Confirm(
			"Renommer "+valid.Plural(len(plan.Moves), "dépôt")+" et déclarer le groupe ?", false)
		if err != nil {
			return ExitOK, err
		}
		if !suite {
			console.Warning("Annulé : rien n'a été renommé.")
			return ExitAborted, nil
		}
	}
	return i.appliquer(arrivee, plan)
}

// enEquipeDemandee dit si le travail à reprendre a été fait en équipe.
//
// Le drapeau tranche s'il est là. Sinon la question ne se pose que lorsque les
// accès la rendent plausible : un dépôt individuel n'a qu'une personne, et
// poser la question devant une cohorte entière de dépôts à un collaborateur
// ajouterait une étape à tout le monde pour le cas d'un seul.
func (i *importSession) enEquipeDemandee(prefixe string,
	repos []groups.RepoInfo) (bool, error) {
	if i.session.Options.Teams {
		return true, nil
	}
	if !i.session.Interactive() || !plusieurs(i.membres(prefixe, i.retenus, repos)) {
		return false, nil
	}
	i.session.Console.Note(
		"Plusieurs personnes ont accès à un même dépôt : ce travail a peut-être " +
			"été fait en équipe.")
	choix, err := i.session.Prompt.Choose("Ce travail a-t-il été fait en équipe ?",
		ui.Options(
			"individuel", "Individuel — ce qui suit le préfixe nomme une personne",
			"equipe", "En équipe — ce qui suit le préfixe nomme une équipe",
		), "individuel")
	if err != nil {
		return false, err
	}
	return choix == "equipe", nil
}

// plusieurs dit qu'un dépôt au moins rassemble plus d'une personne.
func plusieurs(equipes map[string]identity.Crew) bool {
	for _, crew := range equipes {
		if len(crew.Members) > 1 {
			return true
		}
	}
	return false
}

// choisirTravail propose les travaux devinés, ou retient celui qu'on a nommé.
func (i *importSession) choisirTravail(dehors classroom.Foreign) (string, error) {
	console := i.session.Console
	if demande := strings.TrimSpace(i.session.Options.Import); demande != "" {
		return demande, nil
	}

	console.Printf("  %s %s hors nomenclature.", console.Info(itoa(len(dehors.Repos))),
		valid.Agree(len(dehors.Repos), "dépôt"))
	if len(dehors.Assignments) == 0 {
		return "", valid.Errorf(
			"Aucun préfixe commun dans ces dépôts : nommez-le avec --import.")
	}
	rows := make([][]string, 0, len(dehors.Assignments))
	for _, travail := range dehors.Assignments {
		rows = append(rows, []string{travail.Prefix, valid.Plural(travail.Count, "dépôt")})
	}
	console.Table([]string{"Travail", "Dépôts"}, rows, 15)

	if !i.session.Interactive() {
		return "", valid.Errorf("Travail manquant : passez --import en mode non interactif.")
	}
	options := make([]ui.Option, 0, len(dehors.Assignments)+1)
	for _, travail := range dehors.Assignments {
		options = append(options, ui.Option{
			Value: travail.Prefix,
			Label: travail.Prefix + " — " + valid.Plural(travail.Count, "dépôt"),
		})
	}
	options = append(options, ui.Option{Value: "", Label: "Revenir"})
	return i.session.Prompt.Choose("Travail à reprendre", options, "")
}

// connus rend le nom complet des comptes que l'organisation sait déjà nommer :
// son registre d'abord, puis les groupes déjà déclarés sur ce poste. Un compte
// qui s'y trouve n'a pas à repasser par un rapprochement — la réponse est
// écrite, et c'est autant de questions en moins.
func (i *importSession) connus() map[string]string {
	noms := map[string]string{}
	registre, _ := i.session.names(i.org)
	for _, etudiant := range registre.All() {
		if nom := strings.TrimSpace(etudiant.FullName); nom != "" {
			noms[strings.ToLower(etudiant.Username)] = nom
		}
	}
	// Ce que le poste retient et que le registre ignore encore vaut aussi :
	// une reprise faite avant la publication en est pleine.
	store := classroom.Open(classroom.PathNextTo(i.session.ConfigFile))
	for _, personne := range store.People(i.org) {
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

// acces relève, pour les dépôts d'un préfixe, le compte GitHub que leurs accès
// désignent. C'est un appel par dépôt la première fois, et rien ensuite : le
// cache les retient d'une reprise à l'autre.
func (i *importSession) acces(prefixe string, seulement []string,
	repos []groups.RepoInfo) map[string]string {
	groupe := groups.Build(prefixe, repos)
	if groupe.Len() == 0 {
		return nil
	}
	// Un dépôt écarté ne sera ni repris ni rapproché : lire ses accès coûterait
	// une requête pour rien.
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
	if len(noms) == 0 {
		return nil
	}
	resolveur := identity.New(i.session.Client, i.session.Cache, i.session.Options.Jobs)
	spin := ui.NewSpinner(i.session.Console, "Accès aux dépôts…")
	spin.Start()
	trouves := resolveur.Owners(i.org, noms, i.session.Viewer, nil)
	spin.Stop()

	comptes := make(map[string]string, len(trouves))
	for nom, proprietaire := range trouves {
		if proprietaire.Login != "" {
			comptes[nom] = proprietaire.Login
		}
	}
	return comptes
}

// choisirParmiLesCaches tranche quand le préfixe deviné couvrait plusieurs
// travaux. « kickmyb » n'en est pas un : « kickmyb-firebase » et
// « kickmyb-android » en sont deux, et se reprennent l'un après l'autre.
func (i *importSession) choisirParmiLesCaches(plan classroom.Import) (string, error) {
	console := i.session.Console
	console.Blank()
	console.Warning("« %s » n'est pas un travail : les accès aux dépôts en révèlent %d.",
		plan.Prefix, len(plan.Splits))
	rows := make([][]string, 0, len(plan.Splits))
	for _, travail := range plan.Splits {
		rows = append(rows, []string{travail.Prefix, valid.Plural(travail.Count, "dépôt")})
	}
	console.Table([]string{"Travail", "Dépôts"}, rows, 15)

	if !i.session.Interactive() {
		return "", valid.Errorf(
			"Plusieurs travaux sous « %s » : nommez celui à reprendre avec --import.",
			plan.Prefix)
	}
	options := make([]ui.Option, 0, len(plan.Splits)+1)
	for _, travail := range plan.Splits {
		options = append(options, ui.Option{
			Value: travail.Prefix,
			Label: travail.Prefix + " — " + valid.Plural(travail.Count, "dépôt"),
		})
	}
	options = append(options, ui.Option{Value: "", Label: "Revenir"})
	return i.session.Prompt.Choose("Travail à reprendre", options, "")
}

// choisirDepots retient les dépôts à reprendre. Un travail ne se reprend pas
// toujours en entier : un dépôt d'essai, celui d'une personne qui a abandonné,
// celui d'une équipe qui remettra ailleurs.
//
// Tout est retenu d'entrée — écarter est le geste rare —, et une sélection qui
// garde tout n'en est pas une : elle rend une liste vide, celle d'une reprise
// ordinaire.
func (i *importSession) choisirDepots(prefixe string, repos []groups.RepoInfo) (
	[]string, error) {
	groupe := groups.Build(prefixe, repos)
	if groupe.Len() == 0 {
		return nil, valid.Errorf("Aucun dépôt ne commence par « %s ».", prefixe)
	}
	if demande := strings.TrimSpace(i.session.Options.Repos); demande != "" {
		return depotsNommes(groupe, demande)
	}
	if !i.session.Interactive() {
		return nil, nil
	}

	console := i.session.Console
	console.Blank()
	console.Heading("Dépôts à reprendre")
	options := make([]ui.Option, 0, groupe.Len())
	coches := make([]bool, 0, groupe.Len())
	for _, depot := range groupe.Repos {
		envoi := depot.PushedAt
		if envoi == "" {
			envoi = "jamais"
		}
		options = append(options, ui.Option{
			Value: depot.Name, Label: depot.Name + "  " + console.Dim(envoi),
		})
		coches = append(coches, true)
	}
	indices, err := i.session.Prompt.MultiSelect("Dépôts à reprendre", options, coches)
	if err != nil {
		return nil, err
	}
	if len(indices) == 0 {
		return nil, valid.Errorf("Aucun dépôt retenu : il n'y a rien à reprendre.")
	}
	if len(indices) == groupe.Len() {
		return nil, nil
	}
	gardes := make([]string, 0, len(indices))
	for _, index := range indices {
		gardes = append(gardes, groupe.Repos[index].Name)
	}
	return gardes, nil
}

// depotsNommes lit la sélection passée en ligne de commande, et refuse un nom
// qui ne désigne aucun dépôt du travail : le taper de travers ne doit pas
// passer pour un choix.
func depotsNommes(groupe groups.Group, demande string) ([]string, error) {
	gardes := make([]string, 0, groupe.Len())
	for _, morceau := range strings.Split(demande, ",") {
		nom := strings.TrimSpace(morceau)
		if nom == "" {
			continue
		}
		depot, _, trouve := groupe.Find(nom)
		if !trouve {
			return nil, valid.Errorf("« %s » n'est pas un dépôt de « %s ».", nom, groupe.Prefix)
		}
		gardes = append(gardes, depot.Name)
	}
	if len(gardes) == 0 {
		return nil, valid.Errorf("--repos ne nomme aucun dépôt.")
	}
	return gardes, nil
}

// charger lit la liste des étudiants, et explique où la prendre. Le chemin est
// rendu avec elle : le nom du fichier dit le cours et le groupe.
func (i *importSession) charger(prefixe string, repos []groups.RepoInfo) (
	[]roster.Entry, string, error) {
	console := i.session.Console
	chemin := strings.TrimSpace(i.session.Options.Roster)
	if chemin == "" {
		// Sans liste, la reprise se fait quand même : ce que l'organisation
		// nomme déjà suffit souvent, et le reste gardera son compte. C'est un
		// choix, pas un manque — au terminal comme au drapeau.
		if !i.session.Interactive() {
			return nil, "", nil
		}
		console.Blank()
		console.Heading("Liste des étudiants")
		i.direDejaNommes(prefixe, repos)
		for _, ligne := range strings.Split(roster.OmnivoxHelp, "\n") {
			console.Note("%s", ligne)
		}
		// La question accepte une réponse vide, et le dit : sans liste, la
		// reprise se fait quand même. Un chemin mémorisé serait repris par une
		// réponse vide — il faut alors effacer le champ, et le dire aussi.
		retenu := strings.TrimSpace(i.session.Settings.RosterPath)
		if retenu == "" {
			console.Note("Laissez vide pour passer : les dépôts que rien ne nomme " +
				"garderont le compte qu'ils portent.")
		} else {
			console.Note("Entrée reprend « %s » ; effacez le champ pour passer "+
				"sans liste.", retenu)
		}
		reponse, err := i.session.Prompt.Ask(ui.Question{
			Title:      "Chemin du fichier",
			Default:    retenu,
			AllowEmpty: true,
			Complete:   complete.Path,
		})
		if err != nil {
			return nil, "", err
		}
		if chemin = strings.TrimSpace(reponse); chemin == "" {
			console.Note("Sans liste : seuls les noms déjà connus serviront.")
			return nil, "", nil
		}
	}
	liste, err := roster.Load(chemin)
	if err != nil {
		return nil, "", err
	}
	for _, souci := range liste.Issues {
		console.Warning("Ligne %d : %s", souci.Line, souci.Message)
	}
	if len(liste.Entries) == 0 {
		return nil, "", valid.Errorf("Aucun étudiant dans « %s ».", chemin)
	}
	console.Printf("  %s %s.", console.OK(itoa(len(liste.Entries))),
		valid.Agree(len(liste.Entries), "étudiant lu", "étudiants lus"))
	return liste.Entries, chemin, nil
}

// direDejaNommes dit ce que l'organisation sait déjà des dépôts retenus : c'est
// ce qui répond à la seule question du moment — cette liste a-t-elle encore
// quelque chose à apprendre ?
func (i *importSession) direDejaNommes(prefixe string, repos []groups.RepoInfo) {
	console := i.session.Console
	connus := i.connus()
	comptes := i.comptesRetenus(prefixe, repos)
	total, nommes := len(comptes), 0
	for _, compte := range comptes {
		if connus[strings.ToLower(compte)] != "" {
			nommes++
		}
	}
	if total == 0 {
		return
	}
	if nommes == total {
		console.Printf("  %s", console.OK(valid.Agree(total,
			"Le dépôt retenu est déjà nommé par l'organisation.",
			"Les "+itoa(total)+" dépôts retenus sont déjà nommés par l'organisation.")))
		return
	}
	console.Printf("  %s sur %s %s par l'organisation.", console.Info(itoa(nommes)),
		valid.Plural(total, "dépôt retenu", "dépôts retenus"),
		valid.Agree(nommes, "est déjà nommé", "sont déjà nommés"))
}

// comptesRetenus rend, pour chaque dépôt que la reprise garde, le compte que
// ses accès désignent — ou celui que son nom porte, faute de mieux.
func (i *importSession) comptesRetenus(prefixe string,
	repos []groups.RepoInfo) map[string]string {
	gardes := map[string]bool{}
	for _, nom := range i.retenus {
		gardes[strings.ToLower(strings.TrimSpace(nom))] = true
	}
	comptes := map[string]string{}
	for _, depot := range groups.Build(prefixe, repos).Repos {
		if len(gardes) > 0 && !gardes[strings.ToLower(depot.Name)] {
			continue
		}
		compte := depot.Suffix
		if login := i.proprietaires[depot.Name]; login != "" {
			compte = login
		}
		comptes[depot.Name] = compte
	}
	return comptes
}

// choisirPlace demande où les dépôts doivent arriver, en proposant ce qui a pu
// être deviné : le cours dans le nom du fichier, le groupe dans sa colonne, la
// session dans le premier commit du travail. Rien n'est imposé — la proposition
// s'efface d'un caractère.
func (i *importSession) choisirPlace(devinee classroom.Place) (string, error) {
	if place := strings.TrimSpace(i.session.Options.Into); place != "" {
		return place, nil
	}
	if !i.session.Interactive() {
		return "", valid.Errorf("Place manquante : passez --into en mode non interactif.")
	}
	console := i.session.Console
	if len(devinee.Groups) > 0 {
		console.Warning("La liste mêle les groupes %s : indiquez celui qui reçoit ces dépôts.",
			strings.Join(devinee.Groups, ", "))
	}
	return i.session.Prompt.Ask(ui.Question{
		Title:   "Place d'arrivée, par exemple « a26.5n6.1030 »",
		Default: devinee.Scope(),
		Validate: func(valeur string) (string, error) {
			return naming.Path(valeur, "Place")
		},
	})
}

// profils demande à GitHub le nom affiché des comptes qu'on va rapprocher.
// C'est l'indice le plus sûr après le numéro d'étudiant, et il ne coûte qu'une
// requête par compte inconnu.
func (i *importSession) profils(prefixe string, repos []groups.RepoInfo) map[string]string {
	groupe := groups.Build(prefixe, repos)
	pairs := make([]identity.Pair, 0, groupe.Len())
	for _, depot := range groupe.Repos {
		// Le compte des accès d'abord : demander le profil de ce que le nom
		// portait — « firebase-Walid7Akk » — ne ramènerait rien.
		compte := depot.Suffix
		if login := i.proprietaires[depot.Name]; login != "" {
			compte = login
		}
		pairs = append(pairs, identity.Pair{Repo: compte, Login: compte})
	}
	if len(pairs) == 0 {
		return nil
	}
	resolveur := identity.New(i.session.Client, i.session.Cache, i.session.Options.Jobs)
	spin := ui.NewSpinner(i.session.Console, "Noms des profils GitHub…")
	spin.Start()
	noms := resolveur.Resolve(pairs, true, nil)
	spin.Stop()

	profils := map[string]string{}
	for compte, nom := range noms {
		if nom != "" {
			profils[strings.ToLower(compte)] = nom
		}
	}
	return profils
}

// montrer écrit ce que l'importation ferait.
func (i *importSession) montrer(plan classroom.Import) {
	console := i.session.Console
	console.Blank()
	console.Heading("Rapprochement des comptes")

	rows := make([][]string, 0, len(plan.Pairings))
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
		rows = append(rows, []string{"@" + trouve.Login, nom, raison})
	}
	console.Table([]string{"Compte", "Étudiant", "Reconnu par"}, rows, 0)

	console.Blank()
	console.Heading("Renommage")
	moves := make([][]string, 0, len(plan.Moves))
	for _, ligne := range plan.Moves {
		moves = append(moves, []string{ligne.Repo, "→ " + ligne.Target})
	}
	console.Table([]string{"Dépôt", "Deviendra"}, moves, 20)

	if len(plan.Unmatched) > 0 {
		sort := valid.Agree(len(plan.Unmatched), "repris sous le compte qu'il porte, faute d'un nom",
			"repris sous le compte qu'ils portent, faute d'un nom")
		if plan.NamedOnly {
			sort = valid.Agree(len(plan.Unmatched), "laissé où il est", "laissés où ils sont")
		}
		console.Blank()
		console.Printf("  %s : %s",
			console.Warn(valid.Plural(len(plan.Unmatched), "dépôt sans étudiant connu",
				"dépôts sans étudiant connu")),
			console.Dim(sort))
	}
	if len(plan.Absent) > 0 {
		console.Blank()
		console.Printf("  %s : %s",
			console.Warn(valid.Plural(len(plan.Absent), "étudiant sans dépôt pour ce travail",
				"étudiants sans dépôt pour ce travail")),
			console.Dim(strings.Join(plan.Absent, ", ")))
	}
	// Ailleurs, le compte vient des accès ou du registre. Ici, il ne vient que
	// du nom : c'est le seul qui puisse encore être faux.
	if len(plan.Unconfirmed) > 0 {
		console.Blank()
		console.Printf("  %s : %s",
			console.Warn(valid.Plural(len(plan.Unconfirmed), "compte non confirmé",
				"comptes non confirmés")),
			console.Dim("ni accès ni registre ne les nomment ; ils sont lus dans le nom du dépôt"))
	}
}

// appliquer renomme, déclare le groupe, et confie les noms au registre.
func (i *importSession) appliquer(arrivee classroom.Classroom,
	plan classroom.Import) (int, error) {
	console := i.session.Console

	// Le registre passe en premier : un nom qui n'y monterait pas ne serait
	// connu que de ce poste, et c'est ce qu'on veut cesser.
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

	// L'inventaire suit les renommages plutôt que d'être relu : c'est « groups »
	// qui décide de ce qu'un renommage lui fait, et les deux interfaces s'y
	// tiennent.
	var connu []groups.RepoInfo
	if i.session.Cache.Get(cache.ReposKey(i.org), cache.ReposTTL, &connu) && len(connu) > 0 {
		i.session.Cache.Set(cache.ReposKey(i.org), groups.WithRenamed(connu, suivis))
	}

	store := classroom.Open(classroom.PathNextTo(i.session.ConfigFile))
	if _, err := store.Save(arrivee.With(plan.Students...)); err != nil {
		return ExitFailure, err
	}

	if echecs > 0 {
		console.Warning("%s, %d en échec.", valid.Plural(renommes, "dépôt repris", "dépôts repris"),
			echecs)
		return ExitFailure, nil
	}
	console.Success("%s dans « %s ».", valid.Plural(renommes, "dépôt repris", "dépôts repris"),
		arrivee.Scope())
	return ExitOK, nil
}

// --- corriger un rapprochement

// personneRetenue marque le choix de ne rapprocher personne. Une valeur vide
// dirait « revenir », et ce n'est pas la même décision.
const personneRetenue = "\x00personne"

// corriger laisse trancher à la main ce que le rapprochement a deviné.
//
// C'est le même jugement que l'interface web rend au même moment. Sans lui, le
// terminal ne saurait que tout accepter ou tout refuser, alors qu'une seule
// ressemblance douteuse suffit à gâcher un import.
func (i *importSession) corriger(arrivee classroom.Classroom, plan classroom.Import,
	repos []groups.RepoInfo) (classroom.Import, error) {
	console := i.session.Console

	// Tout ce que la liste portait : ceux qu'un dépôt a trouvés, et ceux
	// qu'aucun ne concerne. Les deux ensemble font le groupe.
	noms := append([]string{}, plan.Absent...)
	retenus := map[string]string{}
	for _, trouve := range plan.Pairings {
		if trouve.Found() {
			noms = append(noms, trouve.Entry.FullName)
			retenus[trouve.Login] = trouve.Entry.FullName
		}
	}
	sort.Strings(noms)

	for {
		suite, err := i.session.Prompt.Confirm("Corriger un rapprochement ?", false)
		if err != nil {
			return plan, err
		}
		if !suite {
			return i.laisserLesInconnus(arrivee, plan, noms, retenus, repos)
		}
		login, err := i.session.Prompt.Choose("Compte à corriger",
			comptesACorriger(plan, retenus), "")
		if err != nil || login == "" {
			return plan, err
		}
		nom, err := i.session.Prompt.Choose("Étudiant pour @"+login,
			etudiantsLibres(noms, retenus, login), retenus[login])
		if err != nil {
			return plan, err
		}
		if nom == "" {
			continue
		}

		delete(retenus, login)
		if nom != personneRetenue {
			// Un nom ne peut désigner qu'un compte : le donner ici le retire
			// de là où il était.
			for autre, porte := range retenus {
				if porte == nom {
					delete(retenus, autre)
				}
			}
			retenus[login] = nom
		}

		// Le plan est refait sans rien redeviner : le jugement rendu doit
		// tenir, y compris quand il consiste à ne rapprocher personne.
		refait, err := classroom.PlanImport(arrivee, classroom.ImportRequest{
			Prefix: plan.Prefix, Name: plan.Name,
			Entries: entreesRetenues(noms, retenus), NamedOnly: plan.NamedOnly,
			Owners: i.proprietaires, Known: i.connus(), Only: i.retenus,
		}, repos)
		if err != nil {
			return plan, err
		}
		plan = refait
		console.Blank()
		i.montrer(plan)
	}
}

// laisserLesInconnus propose de laisser derrière les dépôts dont personne n'a
// été reconnu. Sans cela ils entrent dans la nomenclature sous le compte qu'ils
// portent : un dernier niveau qui n'est pas un nom. Les deux se défendent, et
// la question ne se pose que lorsqu'il en reste.
func (i *importSession) laisserLesInconnus(arrivee classroom.Classroom,
	plan classroom.Import, noms []string, retenus map[string]string,
	repos []groups.RepoInfo) (classroom.Import, error) {
	if len(plan.Unmatched) == 0 || plan.NamedOnly {
		return plan, nil
	}
	laisser, err := i.session.Prompt.Confirm(valid.Agree(len(plan.Unmatched),
		"Laisser où il est le dépôt dont l'étudiant est inconnu ?",
		"Laisser où ils sont les "+itoa(len(plan.Unmatched))+
			" dépôts dont l'étudiant est inconnu ?"), false)
	if err != nil || !laisser {
		return plan, err
	}
	refait, err := classroom.PlanImport(arrivee, classroom.ImportRequest{
		Prefix: plan.Prefix, Name: plan.Name,
		Entries: entreesRetenues(noms, retenus), NamedOnly: true,
		Owners: i.proprietaires, Known: i.connus(), Only: i.retenus,
	}, repos)
	if err != nil {
		return plan, err
	}
	i.session.Console.Blank()
	i.montrer(refait)
	return refait, nil
}

// comptesACorriger énumère les dépôts et ce à quoi ils mènent pour l'instant.
func comptesACorriger(plan classroom.Import, retenus map[string]string) []ui.Option {
	options := make([]ui.Option, 0, len(plan.Pairings)+1)
	for _, trouve := range plan.Pairings {
		nom := retenus[trouve.Login]
		if nom == "" {
			nom = "personne"
		}
		options = append(options, ui.Option{
			Value: trouve.Login, Label: "@" + trouve.Login + " → " + nom,
		})
	}
	return append(options, ui.Option{Value: "", Label: "Terminer"})
}

// etudiantsLibres ne propose que ce qui reste : une personne déjà donnée à un
// autre compte ne peut pas l'être deux fois.
func etudiantsLibres(noms []string, retenus map[string]string, login string) []ui.Option {
	pris := map[string]bool{}
	for autre, nom := range retenus {
		if autre != login {
			pris[nom] = true
		}
	}
	options := []ui.Option{{Value: personneRetenue, Label: "— personne —"}}
	for _, nom := range noms {
		if !pris[nom] {
			options = append(options, ui.Option{Value: nom, Label: nom})
		}
	}
	return append(options, ui.Option{Value: "", Label: "Revenir"})
}

// entreesRetenues rend la liste telle que les corrections l'ont laissée : un
// nom par personne, et le compte qu'on lui a donné.
func entreesRetenues(noms []string, retenus map[string]string) []roster.Entry {
	parNom := map[string]string{}
	for login, nom := range retenus {
		parNom[nom] = login
	}
	liste := make([]roster.Entry, 0, len(noms))
	for _, nom := range noms {
		liste = append(liste, roster.Entry{FullName: nom, Username: parNom[nom]})
	}
	return liste
}
