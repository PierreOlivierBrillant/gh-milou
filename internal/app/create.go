package app

import (
	"strconv"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plan"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/runner"
	"github.com/PierreOlivierBrillant/gh-milou/internal/starter"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// create déroule le mode création, de la liste des personnes au bilan.
func (s *Session) create() (int, error) {
	// Un travail d'équipe ne se distribue pas à une liste : ses destinataires
	// sont les équipes du groupe, et elles vivent sur GitHub.
	if s.Options.Teams || s.Options.TeamShare {
		return s.createTeams()
	}
	people, err := s.collectPeople()
	if err != nil {
		return ExitOK, err
	}
	people, err = s.verifyAccounts(people)
	if err != nil {
		return ExitOK, err
	}
	if err := s.configure(); err != nil {
		return ExitOK, err
	}

	items, err := plan.Build(people, classroom.PlanSettings(s.Settings))
	if err != nil {
		return ExitOK, err
	}
	s.summarize(items)

	if !s.Options.Yes && !s.Options.DryRun {
		confirmed, err := s.Prompt.Confirm(
			"Confirmer et créer "+valid.Plural(len(items), "dépôt")+" dans « "+s.Settings.Org+" » ?",
			false)
		if err != nil {
			return ExitOK, err
		}
		if !confirmed {
			s.Console.Warning("Annulé : rien n'a été créé.")
			return ExitAborted, nil
		}
	}

	// La liste monte au registre juste après la confirmation, et avant le
	// premier dépôt : c'est lui qui dira, depuis n'importe quel poste, à qui
	// appartient chacun d'eux. Rien n'est écrit tant que la confirmation n'est
	// pas donnée — le registre ne fait pas exception.
	if !s.Options.DryRun {
		if err := s.apprendre(s.Settings.Org, people); err != nil {
			return ExitOK, err
		}
		if err := s.retenirEcheance(); err != nil {
			return ExitOK, err
		}
	}
	return s.execute(items)
}

// retenirEcheance porte au registre de l'organisation la date cible donnée en
// ligne de commande. Elle n'entre dans aucun dépôt, et la retenir sur ce poste
// la rendrait vraie d'une seule machine : c'est le registre qui la porte, comme
// il porte les noms.
//
// Un travail dont le nom ne dit pas à quel groupe il appartient — un préfixe
// hérité — n'a nulle part où la ranger. Le dire vaut mieux que de l'écrire
// quelque part d'invisible.
func (s *Session) retenirEcheance() error {
	if !s.Options.DueSet {
		return nil
	}
	place, nom, reconnu := naming.SplitAssignment(s.Settings.Assignment)
	if !reconnu {
		return valid.Errorf(
			"--due : « %s » ne dit pas à quel groupe il appartient. Une date cible "+
				"s'attache à un travail d'un groupe, nommé "+
				"« session%[2]scours%[2]sgroupe%[2]stravail ».",
			s.Settings.Assignment, naming.Separator)
	}
	cours, err := classroom.AtScope(s.Settings.Org, place,
		classroom.DefaultsFrom(s.Settings))
	if err != nil {
		return err
	}
	if connu, existe := s.groupStore().Find(cours.Org, cours.Scope()); existe {
		cours = connu
	}
	lignes, err := cours.SetDue(nom, s.Options.Due)
	if err != nil {
		return err
	}
	if _, err := s.registryOf(cours.Org).Apply(echeances(lignes)); err != nil {
		return err
	}
	if s.Options.Due != "" {
		s.Console.Note("« %s » est à remettre le %s.", nom, s.Options.Due)
	}
	return nil
}

// collectPeople charge la liste des personnes : fichier CSV ou saisie guidée.
func (s *Session) collectPeople() ([]roster.Person, error) {
	s.Console.Heading("Liste des personnes")
	path := s.Options.Roster

	if path == "" {
		if !s.Interactive() {
			return nil, valid.Errorf("Liste manquante : passez --roster en mode non interactif.")
		}
		defaultSource := "saisie"
		if s.Settings.RosterPath != "" {
			defaultSource = "fichier"
		}
		source, err := s.Prompt.Choose("Source de la liste", ui.Options(
			"fichier", "Fichier CSV existant",
			"saisie", "Saisie manuelle",
		), defaultSource)
		if err != nil {
			return nil, err
		}
		if source == "saisie" {
			return s.promptPeople()
		}
	}

	for {
		chosen := path
		if chosen == "" {
			answer, err := s.Prompt.Ask(ui.Question{
				Title:    "Chemin du fichier CSV",
				Default:  s.Settings.RosterPath,
				Complete: complete.Path,
			})
			if err != nil {
				return nil, err
			}
			chosen = answer
		}
		list, err := roster.Load(chosen)
		if err != nil {
			if !s.Interactive() {
				return nil, err
			}
			s.Console.Failure("%v", err)
			path = ""
			continue
		}
		people, err := s.reviewRoster(list, chosen)
		if err != nil {
			return nil, err
		}
		if people != nil {
			s.Settings.RosterPath = chosen
			return people, nil
		}
		path = ""
	}
}

// reviewRoster affiche le résultat du chargement et décide de la suite.
// Un retour nil sans erreur invite à redemander un fichier.
func (s *Session) reviewRoster(list roster.Roster, source string) ([]roster.Person, error) {
	// Tout le monde, compte GitHub ou non : une liste du collège n'en porte
	// aucun, et la compter sur les seuls comptes la disait vide.
	lues := list.Everyone()
	s.Console.Printf("  %s %s depuis %s.", s.Console.OK(itoa(len(lues))),
		valid.Agree(len(lues), "personne valide lue", "personnes valides lues"),
		s.Console.Dim(source))

	if len(list.Issues) > 0 {
		s.Console.Warning("%s :", valid.Plural(len(list.Issues), "ligne rejetée", "lignes rejetées"))
		for index, issue := range list.Issues {
			if index == 10 {
				s.Console.Note("… et %s.", valid.Plural(len(list.Issues)-10, "autre"))
				break
			}
			position := "fichier"
			if issue.Line > 0 {
				position = "ligne " + itoa(issue.Line)
			}
			s.Console.Printf("    %s %s : %s", s.Console.Err("•"), position, issue.Message)
		}
	}

	if len(lues) == 0 {
		if !s.Interactive() {
			return nil, valid.Errorf("Aucune personne valide dans la liste fournie.")
		}
		s.Console.Failure("Aucune personne exploitable.")
		return nil, nil
	}

	rows := make([][]string, 0, len(lues))
	sansCompte := 0
	for _, person := range lues {
		if strings.TrimSpace(person.Username) == "" {
			sansCompte++
		}
		rows = append(rows, []string{person.FullName, colonneCompte(s.Console, person.Username)})
	}
	s.Console.Table([]string{"Nom complet", "Compte GitHub"}, rows, 15)
	if sansCompte > 0 {
		s.Console.Note("%s", valid.Plural(sansCompte,
			"personne sans compte GitHub : son dépôt sera créé, mais personne n'y sera "+
				"invité tant que son compte n'est pas rattaché.",
			"personnes sans compte GitHub : leurs dépôts seront créés, mais personne n'y "+
				"sera invité tant que leurs comptes ne sont pas rattachés."))
	}

	if len(list.Issues) > 0 {
		if !s.Interactive() {
			if !s.Options.Yes {
				return nil, valid.Errorf(
					"%s", valid.Plural(len(list.Issues),
						"ligne invalide dans la liste : corrigez le fichier ou ajoutez "+
							"--yes pour l'ignorer.",
						"lignes invalides dans la liste : corrigez le fichier ou ajoutez "+
							"--yes pour les ignorer."))
			}
			return lues, nil
		}
		confirmed, err := s.Prompt.Confirm("Poursuivre en ignorant les lignes rejetées ?", false)
		if err != nil {
			return nil, err
		}
		if !confirmed {
			return nil, nil
		}
	}
	return lues, nil
}

// promptPeople guide une saisie manuelle, deux champs par personne.
func (s *Session) promptPeople() ([]roster.Person, error) {
	s.Console.Note("Laissez le nom vide pour terminer la saisie.")
	var people []roster.Person
	seen := map[string]bool{}

	for {
		name, err := s.Prompt.Ask(ui.Question{
			Title:      "Nom complet #" + itoa(len(people)+1),
			AllowEmpty: true,
			Validate:   func(value string) (string, error) { return valid.FullName(value) },
		})
		if err != nil {
			return nil, err
		}
		if name == "" {
			if len(people) > 0 {
				break
			}
			s.Console.Failure("Au moins une personne est nécessaire.")
			continue
		}
		for {
			username, err := s.Prompt.Ask(ui.Question{
				Title:    "  Compte GitHub",
				Validate: func(value string) (string, error) { return valid.Login(value, "") },
			})
			if err != nil {
				return nil, err
			}
			if seen[strings.ToLower(username)] {
				s.Console.Failure("« %s » figure déjà dans la liste.", username)
				continue
			}
			seen[strings.ToLower(username)] = true
			people = append(people, roster.Person{FullName: name, Username: username})
			break
		}
	}

	rows := make([][]string, 0, len(people))
	for _, person := range people {
		rows = append(rows, []string{person.FullName, "@" + person.Username})
	}
	s.Console.Table([]string{"Nom complet", "Compte GitHub"}, rows, 0)

	save, err := s.Prompt.Confirm("Enregistrer cette liste dans un fichier CSV ?", true)
	if err != nil {
		return nil, err
	}
	if save {
		target, err := s.Prompt.Ask(ui.Question{
			Title:    "Chemin du fichier",
			Default:  "cohorte.csv",
			Complete: complete.Path,
		})
		if err != nil {
			return nil, err
		}
		saved, err := roster.Write(target, people)
		if err != nil {
			s.Console.Failure("%v", err)
		} else {
			s.Settings.RosterPath = saved
			s.Console.Printf("  Liste enregistrée : %s", s.Console.OK(saved))
		}
	}
	return people, nil
}

// verifyAccounts confronte chaque compte GitHub à l'API.
func (s *Session) verifyAccounts(people []roster.Person) ([]roster.Person, error) {
	if s.Options.NoVerifyAccounts {
		return people, nil
	}
	// Seuls les comptes connus se vérifient : une personne qu'on n'a pas encore
	// rattachée n'a pas de compte introuvable, elle n'en a pas du tout.
	aVerifier := roster.WithAccounts(people)
	if len(aVerifier) == 0 {
		return people, nil
	}
	if s.Interactive() {
		wanted, err := s.Prompt.Confirm(
			valid.Agree(len(aVerifier), "Vérifier l'existence du compte GitHub ?",
				"Vérifier l'existence des "+itoa(len(aVerifier))+" comptes GitHub ?"),
			s.Settings.VerifyAccounts)
		if err != nil {
			return nil, err
		}
		s.Settings.VerifyAccounts = wanted
		if !wanted {
			return people, nil
		}
	}
	s.Settings.VerifyAccounts = true

	progress := ui.NewProgress(s.Console, "Comptes GitHub", len(aVerifier))
	var missing []roster.Person
	for index, person := range aVerifier {
		exists, err := s.Client.UserExists(person.Username)
		progress.Update(index+1, "@"+person.Username)
		if err != nil {
			progress.Clear()
			s.Console.Warning("Vérification interrompue : %v", err)
			return people, nil
		}
		if !exists {
			missing = append(missing, person)
		}
	}
	progress.Finish("")
	s.Console.Success("%s.", valid.Plural(len(aVerifier), "compte vérifié", "comptes vérifiés"))

	if len(missing) == 0 {
		return people, nil
	}
	s.Console.Failure("%s :", valid.Plural(len(missing), "compte inexistant", "comptes inexistants"))
	for _, person := range missing {
		s.Console.Printf("    • %s → @%s", person.FullName, person.Username)
	}
	if !s.Interactive() {
		return nil, valid.Errorf("Des comptes GitHub sont introuvables ; corrigez la liste.")
	}

	action, err := s.Prompt.Choose("Que faire ?", ui.Options(
		"retirer", "Retirer ces personnes et poursuivre",
		"garder", "Poursuivre malgré tout (les invitations échoueront)",
		"annuler", "Annuler",
	), "retirer")
	if err != nil {
		return nil, err
	}
	switch action {
	case "annuler":
		return nil, ui.ErrAborted
	case "garder":
		return people, nil
	}
	excluded := map[string]bool{}
	for _, person := range missing {
		excluded[person.Key()] = true
	}
	var kept []roster.Person
	for _, person := range people {
		if !excluded[person.Key()] {
			kept = append(kept, person)
		}
	}
	return kept, nil
}

// configure recueille les paramètres des dépôts à créer.
func (s *Session) configure() error {
	s.Console.Heading("Paramètres des dépôts")
	options := s.Options

	switch {
	case options.Assignment != "":
		assignment, err := naming.Assignment(options.Assignment, "Travail")
		if err != nil {
			return err
		}
		s.Settings.Assignment = assignment
	case s.Interactive():
		answer, err := s.Prompt.Ask(ui.Question{
			Title:   "Identifiant du travail (ex. tp1, projet-final)",
			Default: s.Settings.Assignment,
			Validate: func(value string) (string, error) {
				return naming.Assignment(value, "Travail")
			},
		})
		if err != nil {
			return err
		}
		s.Settings.Assignment = answer
	default:
		value, err := s.require(s.Settings.Assignment, "--assignment", "Travail")
		if err != nil {
			return err
		}
		assignment, err := naming.Assignment(value, "Travail")
		if err != nil {
			return err
		}
		s.Settings.Assignment = assignment
	}

	// Un travail de la nomenclature nomme ses dépôts par elle, comme au
	// navigateur : son gabarit ne se choisit pas, et la question n'a pas lieu
	// d'être posée. Un gabarit passé en drapeau ne vaut que pour un préfixe
	// hérité, et le dire évite de croire qu'il a servi.
	_, _, nomenclature := naming.SplitAssignment(s.Settings.Assignment)
	switch {
	case nomenclature:
		if strings.TrimSpace(options.Pattern) != "" &&
			strings.TrimSpace(options.Pattern) != classroom.NamePattern {
			s.Console.Note("--pattern ne vaut que pour un préfixe hérité : les dépôts de "+
				"« %s » se nomment « %s ».", s.Settings.Assignment, classroom.NamePattern)
		}
	case options.Pattern != "":
		pattern, err := plan.ValidatePattern(options.Pattern, "Gabarit de nom", true)
		if err != nil {
			return err
		}
		s.Settings.NamePattern = pattern
	case s.Interactive():
		s.Console.Note("Champs : %s", placeholderHint())
		answer, err := s.Prompt.Ask(ui.Question{
			Title:   "Gabarit de nom des dépôts",
			Default: s.Settings.NamePattern,
			Validate: func(value string) (string, error) {
				return plan.ValidatePattern(value, "Gabarit de nom", true)
			},
		})
		if err != nil {
			return err
		}
		s.Settings.NamePattern = answer
	}

	if err := s.configureTemplate(); err != nil {
		return err
	}
	if err := s.configureStarter(); err != nil {
		return err
	}

	switch {
	case options.Visibility != "":
		visibility, err := config.ValidateVisibility(options.Visibility)
		if err != nil {
			return err
		}
		s.Settings.Visibility = visibility
	case s.Interactive():
		choice, err := s.Prompt.Choose("Visibilité des dépôts", ui.Options(
			"private", "Privé", "public", "Public",
		), s.Settings.Visibility)
		if err != nil {
			return err
		}
		s.Settings.Visibility = choice
	}

	switch {
	case options.NoCollaborator:
		s.Settings.AddCollaborator = false
	case s.Interactive():
		invite, err := s.Prompt.Confirm("Inviter chaque personne sur son dépôt ?",
			s.Settings.AddCollaborator)
		if err != nil {
			return err
		}
		s.Settings.AddCollaborator = invite
	}

	if options.DelaySet {
		s.Settings.DelaySeconds = options.Delay
	}

	if s.Settings.AddCollaborator {
		switch {
		case options.Permission != "":
			permission, err := config.ValidatePermission(options.Permission)
			if err != nil {
				return err
			}
			s.Settings.Permission = permission
		case s.Interactive():
			choices := make([]ui.Option, 0, len(config.Permissions))
			for _, value := range config.Permissions {
				choices = append(choices, ui.Option{Value: value, Label: config.PermissionLabels[value]})
			}
			choice, err := s.Prompt.Choose("Droit accordé", choices, s.Settings.Permission)
			if err != nil {
				return err
			}
			s.Settings.Permission = choice
		}
	}
	return nil
}

// configureTemplate choisit le dépôt modèle et vérifie qu'il en est bien un.
func (s *Session) configureTemplate() error {
	options := s.Options
	switch {
	case options.TemplateSet:
		if strings.TrimSpace(options.Template) == "" {
			s.Settings.Template = ""
		} else {
			owner, repo, err := valid.RepoRef(options.Template)
			if err != nil {
				return err
			}
			s.Settings.Template = owner + "/" + repo
		}
	case s.Interactive():
		question := "Dépôt modèle « organisation/depot » (vide = dépôt neuf)"
		if s.Settings.Template != "" {
			question = "Dépôt modèle « organisation/depot » (vide = dépôt neuf, « - » pour retirer)"
		}
		answer, err := s.Prompt.Ask(ui.Question{
			Title:      question,
			Default:    s.Settings.Template,
			AllowEmpty: true,
			Validate: func(value string) (string, error) {
				if clearKeywords[strings.ToLower(strings.TrimSpace(value))] {
					return "", nil
				}
				owner, repo, err := valid.RepoRef(value)
				if err != nil {
					return "", err
				}
				return owner + "/" + repo, nil
			},
		})
		if err != nil {
			return err
		}
		s.Settings.Template = answer
	}

	if s.Settings.Template == "" {
		return nil
	}
	return s.checkTemplate()
}

// checkTemplate vérifie l'existence du modèle et son drapeau « is_template ».
func (s *Session) checkTemplate() error {
	owner, repo, err := valid.RepoRef(s.Settings.Template)
	if err != nil {
		return err
	}
	var data *ghapi.Repo
	ui.Await(s.Console, "Vérification du dépôt modèle "+owner+"/"+repo+"…", func() {
		data, err = s.Client.GetRepo(owner, repo)
	})
	if err != nil {
		s.Console.Warning("Modèle non vérifiable : %v", err)
		return nil
	}
	if data == nil {
		return valid.Errorf("Dépôt modèle « %s » introuvable pour @%s.", s.Settings.Template, s.Viewer)
	}
	if !data.IsTemplate {
		message := "« " + s.Settings.Template + " » n'est pas déclaré comme dépôt modèle " +
			"(réglage « Template repository » sur GitHub)."
		if !s.Interactive() {
			return valid.Errorf("%s", message)
		}
		s.Console.Warning("%s", message)
		confirmed, err := s.Prompt.Confirm("Poursuivre quand même ?", false)
		if err != nil {
			return err
		}
		if !confirmed {
			return ui.ErrAborted
		}
		return nil
	}
	s.Console.Printf("  Modèle : %s", s.Console.OK(s.Settings.Template))
	return nil
}

// configureStarter choisit et charge le dossier de fichiers de départ.
func (s *Session) configureStarter() error {
	options := s.Options
	candidate := ""
	asked := false
	if options.StarterSet {
		candidate = strings.TrimSpace(options.Starter)
		asked = true
	} else if !s.Interactive() {
		candidate = s.Settings.StarterDir
		asked = true
	}

	for {
		if !asked {
			question := "Dossier de fichiers de départ à commiter (vide = aucun)"
			if s.Settings.StarterDir != "" {
				question = "Dossier de fichiers de départ (vide = aucun, « - » pour retirer)"
			}
			answer, err := s.Prompt.Ask(ui.Question{
				Title:      question,
				Default:    s.Settings.StarterDir,
				AllowEmpty: true,
				Complete:   complete.Dir,
			})
			if err != nil {
				return err
			}
			candidate = strings.TrimSpace(answer)
		}
		if clearKeywords[strings.ToLower(candidate)] {
			candidate = ""
		}
		if candidate == "" {
			s.Starter = nil
			s.Settings.StarterDir = ""
			return nil
		}
		bundle, err := starter.Load(candidate)
		if err != nil {
			if !s.Interactive() {
				return err
			}
			s.Console.Failure("%v", err)
			asked = false
			continue
		}
		s.Starter = bundle
		break
	}

	s.Settings.StarterDir = s.Starter.Root
	s.Console.Printf("  Fichiers de départ : %s", s.Console.OK(s.Starter.Describe()))
	rows := make([][]string, 0, len(s.Starter.Files))
	for _, file := range s.Starter.Files {
		rows = append(rows, []string{file.Path, starter.HumanSize(file.Size())})
	}
	s.Console.Table([]string{"Fichier", "Taille"}, rows, 12)
	for index, skipped := range s.Starter.Skipped {
		if index == 5 {
			break
		}
		s.Console.Note("écarté : %s (%s)", skipped.Path, skipped.Reason)
	}
	if s.Starter.IsLarge() {
		s.Console.Warning("Envoi volumineux : un fichier par appel d'API. " +
			"Un dépôt modèle (--template) serait plus rapide.")
	}
	if s.Starter.NeedsWorkflowScope() {
		// Sans cette portée, GitHub refuse le commit entier des fichiers de
		// départ. Le dire — et le corriger — ici vaut mieux qu'à la dernière
		// étape ; un refus, lui, n'empêche pas de poursuivre la création.
		_ = s.ensureScope("workflow", "l'envoi des fichiers de départ serait refusé")
	}

	if s.Options.CommitMessage != "" {
		s.Settings.CommitMessage = strings.TrimSpace(s.Options.CommitMessage)
	} else if s.Interactive() {
		answer, err := s.Prompt.Ask(ui.Question{
			Title:   "Message du commit",
			Default: s.Settings.CommitMessage,
		})
		if err != nil {
			return err
		}
		s.Settings.CommitMessage = answer
	}
	return nil
}

// summarize récapitule ce qui sera fait, avant toute écriture.
func (s *Session) summarize(items []plan.PlannedRepo) {
	s.summarizeCommon(items, "Dépôts à traiter")
	preview := make([][]string, 0, len(items))
	for _, item := range items {
		preview = append(preview, []string{item.Name, item.Person.FullName,
			colonneCompte(s.Console, item.Person.Username)})
	}
	s.Console.Table([]string{"Dépôt", "Personne", "Compte"}, preview, 20)
}

// summarizeCommon dit ce qui ne change pas d'une distribution à l'autre : les
// réglages. Ce qu'elle produit — des dépôts de personnes ou d'équipes — se
// décrit ensuite, chacun avec ses colonnes.
func (s *Session) summarizeCommon(items []plan.PlannedRepo, compte string) {
	s.Console.Heading("Récapitulatif")
	mode := "création réelle"
	if s.Options.DryRun {
		mode = "SIMULATION (aucune écriture)"
	}
	source := s.Settings.Template
	if source == "" {
		source = "dépôt neuf initialisé"
	}
	starterLabel := "aucun"
	if s.Starter != nil {
		starterLabel = s.Starter.Describe() + " depuis " + s.Settings.StarterDir
	}
	visibility := "privé"
	if !s.Settings.Private() {
		visibility = "public"
	}
	invitations := "non"
	if s.Settings.AddCollaborator {
		invitations = "oui (" + s.Settings.Permission + ")"
	}

	rows := [][2]string{
		{"Organisation", s.Settings.Org},
		{"Travail", s.Settings.Assignment},
		{compte, itoa(len(items))},
		{"Source", source},
		{"Fichiers de départ", starterLabel},
		{"Visibilité", visibility},
		{"Invitations", invitations},
		{"Mode", mode},
	}
	for _, row := range rows {
		s.Console.Printf("  %s %s", s.Console.Dim(pad(row[0], 18)), row[1])
	}
}

// execute lance la génération et enregistre le bilan.
func (s *Session) execute(items []plan.PlannedRepo) (int, error) {
	s.Console.Heading("Exécution")
	width := 10
	for _, item := range items {
		width = max(width, ui.VisibleWidth(item.Name))
	}

	// Un groupe cloisonné doit le rester : chaque dépôt créé va d'emblée à son
	// équipe enseignante, faute de quoi la distribution suivante décloisonnerait
	// le groupe sans que personne ne s'en aperçoive.
	equipe, droit := s.teacherGrant(s.Settings.Org, s.Settings.Assignment)
	executor := runner.New(s.Client, s.Settings, s.Starter).WithClock(s.Sleep, s.Now)
	report, err := executor.Run(items, runner.Options{
		DryRun:            s.Options.DryRun,
		ForceStarter:      s.Options.ForceStarter,
		Sign:              !s.Settings.NoSign,
		Registry:          s.registryOf(s.Settings.Org),
		TeacherTeam:       equipe,
		TeacherPermission: droit,
		OnProgress: func(index, total int, result runner.Result) {
			s.printProgress(index, total, result, width)
		},
	})
	if err != nil {
		return ExitOK, err
	}
	// L'inventaire mis en cache n'est plus à jour dès qu'un dépôt est créé.
	if !s.Options.DryRun && report.Count(runner.Created) > 0 {
		s.Cache.Forget(cache.ReposKey(s.Settings.Org))
		s.invalidateCaches()
	}
	s.direLesMarques(report)

	s.Console.Heading("Bilan")
	crees, presents := report.Count(runner.Created), report.Count(runner.Existing)
	createdLabel := valid.Agree(crees, "créé")
	if s.Options.DryRun {
		createdLabel = "à créer"
	}
	s.Console.Printf("  %s %s · %s %s · %s en échec",
		s.Console.OK(itoa(crees)), createdLabel,
		s.Console.Info(itoa(presents)), valid.Agree(presents, "déjà présent", "déjà présents"),
		s.Console.Err(itoa(len(report.Failures()))))

	reportDir, err := roster.ExpandPath(s.Options.ReportDir)
	if err != nil {
		reportDir = s.Options.ReportDir
	}
	jsonPath, csvPath, err := report.Save(reportDir)
	if err != nil {
		s.Console.Warning("Bilan non enregistré : %v", err)
	} else {
		s.Console.Printf("  Bilan : %s et %s", s.Console.Dim(jsonPath), s.Console.Dim(csvPath))
	}

	if len(report.Failures()) > 0 {
		s.Console.Warning("Relancez la commande : les dépôts déjà créés seront ignorés.")
		return ExitFailure, nil
	}
	if s.Options.DryRun {
		s.Console.Print("  " + s.Console.Info("Simulation terminée : rien n'a été créé sur GitHub."))
	}
	return ExitOK, nil
}

// printProgress affiche une ligne par dépôt traité.
func (s *Session) printProgress(index, total int, result runner.Result, width int) {
	icon, colour := "·", s.Console.Dim
	switch result.Status {
	case runner.Created:
		icon, colour = "✓", s.Console.OK
	case runner.Existing:
		icon, colour = "=", s.Console.Info
	case runner.Failed:
		icon, colour = "✗", s.Console.Err
	}
	label := result.Status
	// En simulation, aucun dépôt n'est écrit : le libellé doit le refléter.
	if s.Options.DryRun && result.Status == runner.Created {
		label = "à créer"
	}
	line := "  " + s.Console.Dim("["+itoa(index)+"/"+itoa(total)+"]") + " " +
		colour(icon) + " " + pad(result.Repo, width) + "  " + label
	if s.Starter != nil && result.Starter != runner.StarterNone && result.Starter != "" {
		line += s.Console.Dim(" · " + result.Starter)
	}
	if s.Settings.AddCollaborator && result.Collaborator != runner.CollaboratorNo && result.Collaborator != "" {
		// Sans compte, l'état se suffit — « compte à rattacher » : un « @ » tout
		// seul devant lui ne désignerait personne.
		qui := arobase(result.Username)
		if qui != "" {
			qui += " : "
		}
		line += s.Console.Dim(" · " + qui + result.Collaborator)
	}
	s.Console.Print(line)
	if result.Error != "" {
		s.Console.Printf("      %s", s.Console.Err(result.Error))
	}
}

// urlOf reconstitue l'adresse d'un dépôt de l'organisation courante.
func (s *Session) urlOf(name, known string) string {
	if known != "" {
		return known
	}
	return "https://github.com/" + s.Settings.Org + "/" + name
}

// pad complète un libellé jusqu'à width colonnes, comptées comme celles des
// tableaux : « Dépôts à traiter » en occupe seize, pas les dix-neuf octets
// qui l'écrivent.
func pad(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-ui.VisibleWidth(text)))
}

func itoa(value int) string { return strconv.Itoa(value) }

// direLesMarques dit ce qu'il est advenu des marques invisibles déposées.
//
// C'est l'exécuteur qui les verse au registre ; il reste à dire combien de
// dépôts en portent une, et pourquoi celles qui n'y sont pas n'y ont pas
// monté. Une marque absente du registre ne fait pas échouer la distribution :
// les dépôts sont créés, les étudiants peuvent travailler. Cela mérite un
// avertissement, pas un refus.
func (s *Session) direLesMarques(report *runner.Report) {
	if report.Marks.Recorded > 0 {
		s.Console.Printf("  %s d'une marque invisible.",
			valid.Plural(report.Marks.Signed, "dépôt signé", "dépôts signés"))
	}
	if report.Marks.Problem != "" {
		s.Console.Warning("%s", report.Marks.Problem)
	}
}
