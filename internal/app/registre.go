package app

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/rules"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Les noms accumulés sur ce poste ne montent pas d'eux-mêmes au registre : ils
// sont dans un fichier que plus rien ne lira. Cet écran les y verse, une fois,
// après avoir montré ce qu'il ferait — comme tout ce qui écrit dans cet outil.

// publishRegistry montre puis, si on l'accorde, publie.
func (s *Session) publishRegistry() (int, error) {
	org := s.Settings.Org
	s.Console.Heading("Registre des étudiants de « " + org + " »")

	set, avis := s.names(org)
	if avis != "" {
		s.Console.Print(s.Console.Warn(avis))
	}
	store := classroom.Open(classroom.PathNextTo(s.ConfigFile))
	locales := store.People(org)
	if len(locales) == 0 {
		s.Console.Note("Ce poste ne connaît aucun étudiant dans « %s » : "+
			"il n'y a rien à publier.", org)
		return ExitOK, nil
	}

	plan := registry.Plan(set, locales)
	s.showPublication(plan, set.Len())
	if avertissement := s.registryOf(org).Exposure(); avertissement != "" {
		s.Console.Blank()
		s.Console.Print(s.Console.Warn(avertissement))
	}

	if plan.Empty() {
		s.Console.Blank()
		s.Console.Note("Le registre connaît déjà tout ce que ce poste sait : rien à publier.")
		return ExitOK, nil
	}
	if s.Options.DryRun {
		s.Console.Blank()
		s.Console.Note("Simulation : rien n'a été écrit.")
		return ExitOK, nil
	}

	if !s.Options.Yes {
		suite, err := s.Prompt.Confirm(
			"Publier "+valid.Plural(plan.Count(), "fiche")+" dans « "+org+" » ?", false)
		if err != nil {
			return ExitOK, err
		}
		if !suite {
			s.Console.Warning("Annulé : rien n'a été publié.")
			return ExitAborted, nil
		}
	}

	publie, err := s.registryOf(org).Apply(plan.Apply(s.Options.PreferLocal))
	if err != nil {
		return ExitFailure, err
	}
	s.Console.Success("%s ; le registre en compte %d.",
		valid.Plural(plan.Count(), "fiche publiée", "fiches publiées"), publie.Len())

	// Ce qui vient de monter n'a plus à être redit ici. La sauvegarde précède
	// l'allègement : rien n'oblige à croire un outil sur parole.
	copie, err := store.Backup("avant-registre")
	if err != nil {
		return ExitFailure, err
	}
	allegees, err := store.Trim(org, publie)
	if err != nil {
		return ExitFailure, err
	}
	if allegees > 0 {
		s.Console.Note("%s du fichier des groupes : le registre en est désormais la source.",
			valid.Plural(allegees, "nom retiré", "noms retirés"))
		if copie != "" {
			s.Console.Note("Le fichier d'avant est recopié dans %s.", copie)
		}
	}
	return ExitOK, nil
}

// showPublication écrit ce que la publication ferait.
func (s *Session) showPublication(plan registry.Publication, connues int) {
	console := s.Console
	console.Printf("  Le registre connaît %s %s ; ce poste en apporte %s à écrire.",
		console.Info(itoa(connues)), valid.Agree(connues, "fiche"), console.OK(itoa(plan.Count())))

	if len(plan.New) > 0 {
		console.Blank()
		console.Printf("  %s", console.OK(valid.Plural(len(plan.New), "nouvelle fiche",
			"nouvelles fiches")))
		rows := make([][]string, 0, len(plan.New))
		for _, fiche := range plan.New {
			rows = append(rows, []string{fiche.FullName, "@" + fiche.Username})
		}
		console.Table([]string{"Nom complet", "Compte"}, rows, 15)
	}

	if len(plan.Renamed) > 0 {
		console.Blank()
		garde := "le registre garde le sien"
		if s.Options.PreferLocal {
			garde = "celui de ce poste l'emporte"
		}
		console.Printf("  %s — %s",
			console.Warn(valid.Plural(len(plan.Renamed), "désaccord de nom", "désaccords de nom")),
			garde)
		rows := make([][]string, 0, len(plan.Renamed))
		for _, desaccord := range plan.Renamed {
			rows = append(rows, []string{
				"@" + desaccord.Username, desaccord.Registry, desaccord.Local})
		}
		console.Table([]string{"Compte", "Au registre", "Sur ce poste"}, rows, 0)
	}

	if len(plan.Ambiguous) > 0 {
		console.Blank()
		console.Printf("  %s : ce poste %s nomme de plusieurs façons. Le premier nom "+
			"est retenu ; les autres restent rattachés par leur slug.",
			console.Warn(valid.Plural(len(plan.Ambiguous), "compte ambigu", "comptes ambigus")),
			valid.Agree(len(plan.Ambiguous), "le", "les"))
		rows := make([][]string, 0, len(plan.Ambiguous))
		for _, ambigu := range plan.Ambiguous {
			rows = append(rows, []string{
				"@" + ambigu.Username, ambigu.Chosen, strings.Join(ambigu.Names, " · ")})
		}
		console.Table([]string{"Compte", "Retenu", "Trouvés"}, rows, 0)
	}

	if len(plan.Nameless) > 0 {
		console.Blank()
		console.Printf("  %s : %s, rien ne peut être publié pour %s. %s",
			console.Warn(valid.Plural(len(plan.Nameless), "compte sans nom", "comptes sans nom")),
			valid.Agree(len(plan.Nameless), "son nom complet est inconnu",
				"leur nom complet est inconnu"),
			valid.Agree(len(plan.Nameless), "lui", "eux"),
			console.Dim("@"+strings.Join(plan.Nameless, ", @")))
	}
}

// ------------------------------------------------------------- effacement

// forgetRegistryHistory réécrit la branche du registre en un commit sans passé.
//
// C'est irréversible, et c'est demandé pour l'être : quelqu'un veut qu'un nom
// cesse d'être atteignable. Le nom complet du dépôt doit donc être retapé,
// comme pour une suppression — et « --yes » n'y change rien.
func (s *Session) forgetRegistryHistory() (int, error) {
	org := s.Settings.Org
	console := s.Console
	console.Heading("Historique du registre de « " + org + " »")

	cible := org + "/" + registry.RepoName
	console.Print("  " + console.Err("⚠ Réécriture définitive de "+cible))
	console.Note("   Le registre garde son contenu ; c'est son passé qui disparaît.")
	console.Note("   GitHub garde un temps les objets devenus inaccessibles, et un clone")
	console.Note("   déjà fait garde ce qu'il avait : rien de plus n'est promis ici.")

	if !s.Interactive() {
		return ExitValidation, valid.Errorf(
			"Effacer l'historique demande de retaper « %s » : impossible en mode script.", cible)
	}
	tape, err := s.Prompt.Ask(ui.Question{
		Title:      "Retapez « " + cible + " » pour confirmer",
		AllowEmpty: true,
	})
	if err != nil {
		return ExitAborted, err
	}
	if strings.TrimSpace(tape) != cible {
		console.Warning("Annulé : l'historique est intact.")
		return ExitAborted, nil
	}

	commit, err := s.registryOf(org).ForgetHistory()
	if err != nil {
		return ExitFailure, err
	}
	console.Success("Historique réécrit ; la branche « %s » repart de %s.",
		registry.Branch, commit[:min(7, len(commit))])
	return ExitOK, nil
}

// forgetHistoryFromMenu ouvre l'effacement depuis les options avancées, qui
// s'atteignent sans jeton : l'organisation et l'authentification sont donc à
// obtenir ici.
func (s *Session) forgetHistoryFromMenu() error {
	if err := s.authenticate(); err != nil {
		return err
	}
	if err := s.chooseOrg(); err != nil {
		return err
	}
	_, err := s.forgetRegistryHistory()
	return err
}

// ------------------------------------------------------------- accès d'équipe

// grantRegistryTeam donne à une équipe de l'organisation accès au registre.
func (s *Session) grantRegistryTeam(equipe string) (int, error) {
	org := s.Settings.Org
	if err := s.registryOf(org).Grant(equipe); err != nil {
		return ExitFailure, err
	}
	s.Console.Success("L'équipe « %s » a désormais accès à %s/%s.",
		equipe, org, registry.RepoName)
	// Le dire est ce qui empêche de croire le contraire : accorder n'est pas
	// restreindre.
	s.Console.Note("Cela ouvre un accès sans en fermer aucun : c'est la permission de " +
		"base de l'organisation qui restreint le reste.")
	if avertissement := s.registryOf(org).Exposure(); avertissement != "" {
		s.Console.Print(s.Console.Warn(avertissement))
	}
	return ExitOK, nil
}

// grantTeamFromMenu propose les équipes de l'organisation, puis en accorde une.
func (s *Session) grantTeamFromMenu() error {
	if err := s.authenticate(); err != nil {
		return err
	}
	if err := s.chooseOrg(); err != nil {
		return err
	}
	equipes, err := s.registryOf(s.Settings.Org).Teams()
	if err != nil {
		return err
	}
	if len(equipes) == 0 {
		// Un compte qui n'est pas membre n'en voit aucune : ce n'est pas une
		// panne, il n'y a rien à proposer.
		s.Console.Note("Aucune équipe visible dans « %s ».", s.Settings.Org)
		return nil
	}
	options := make([]ui.Option, 0, len(equipes)+1)
	for _, equipe := range equipes {
		options = append(options, ui.Option{Value: equipe.Slug, Label: equipe.Name})
	}
	options = append(options, ui.Option{Value: "revenir", Label: "Revenir"})
	choix, err := s.Prompt.Choose("Équipe à qui donner accès", options, "revenir")
	if err != nil || choix == "revenir" {
		return err
	}
	_, err = s.grantRegistryTeam(choix)
	return err
}

// publishRules écrit dans le registre de l'organisation les règles de
// comparaison d'un fichier.
//
// C'est le pendant scriptable de l'écran des règles : « 5N6 devient 5M6 » se
// déclare une fois, pour toute l'équipe, et les analyses de chacun retrouvent
// dès lors les copies d'il y a trois ans. Le fichier remplace ce qui s'y
// trouvait — retirer une équivalence doit pouvoir se faire —, et l'écran le dit
// avant d'écrire.
func (s *Session) publishRules(path string) (int, error) {
	org := s.Settings.Org
	s.Console.Heading("Règles de comparaison de « " + org + " »")

	nouvelles, err := rules.Load(path)
	if err != nil {
		return ExitValidation, err
	}
	set, avis := s.names(org)
	if avis != "" {
		s.Console.Print(s.Console.Warn(avis))
	}
	anciennes := set.Rules()

	s.Console.Printf("  Cours déclarés : %d, travaux : %d, profils : %d.",
		len(nouvelles.Courses), len(nouvelles.Assignments), len(nouvelles.Profiles))
	for _, cours := range nouvelles.Courses {
		s.Console.Printf("    %s : %s", orDim(s.Console, cours.Label, cours.ID),
			strings.Join(cours.Codes, ", "))
	}
	// Le remplacement est dit avant d'écrire : ce qui figurait au registre et
	// que le fichier ne redit pas disparaît, et ce n'est pas anodin.
	if perdus := len(anciennes.Courses) - len(nouvelles.Courses); perdus > 0 {
		s.Console.Warning("Le registre porte %d cours de plus : ils seront retirés.", perdus)
	}
	if s.Options.DryRun {
		s.Console.Note("Simulation : rien n'a été écrit.")
		return ExitOK, nil
	}
	if !s.Options.Yes {
		suite, err := s.Prompt.Confirm("Écrire ces règles dans le registre ?", true)
		if err != nil || !suite {
			return ExitOK, err
		}
	}
	if _, err := s.registryOf(org).Apply(registry.Declare(nouvelles)); err != nil {
		return ExitFailure, err
	}
	s.Console.Success("Règles écrites dans « %s/%s ».", org, registry.RepoName)
	return ExitOK, nil
}
