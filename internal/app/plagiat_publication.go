package app

import (
	"fmt"
	"github.com/PierreOlivierBrillant/gh-milou/internal/anonymize"
	"github.com/PierreOlivierBrillant/gh-milou/internal/brand"
	"github.com/PierreOlivierBrillant/gh-milou/internal/corpus"
	"github.com/PierreOlivierBrillant/gh-milou/internal/exchange"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plagiarism"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
	"strconv"
)

// Publier ce qu'on a donné.
//
// Deux choses partent, et elles ne disent pas la même chose. Le catalogue
// annonce qu'un travail existe : une place, un nom, un décompte. L'index
// d'empreintes permet de le comparer sans le lire — ce sont des hachés, on ne
// remonte pas au code depuis eux.
//
// Ni l'un ni l'autre ne nomme un étudiant, et la table qui relie les jetons aux
// personnes ne quitte pas ce poste.

// publierIndex analyse le travail géré et publie son index.
func (m *manageSession) publierIndex(group *groups.Group) error {
	console := m.session.Console
	cours, nom, reconnu := m.travail(group)
	if !reconnu {
		return valid.Errorf(
			"« %s » ne dit pas à quel groupe il appartient : seul un travail d'un "+
				"groupe déclaré se publie.", group.Prefix)
	}

	declarees, err := m.reglesDeComparaison()
	if err != nil {
		return err
	}
	// Ses propres copies, et rien d'autre : publier l'index d'un collègue
	// redistribuerait ce qu'on nous a confié.
	cibles, err := m.plagiatCibles(cours, nom, corpus.ReachGroup, declarees)
	if err != nil {
		return err
	}
	reglage, err := m.session.Options.inspection()
	if err != nil {
		return err
	}

	console.Heading("Publication — " + nom)
	console.Note("Ce qui part : des empreintes et des jetons. Ni code, ni nom. " +
		"La table qui relie les jetons aux personnes reste sur ce poste.")

	requete := plagiarism.Request{
		Assignment: cours.AssignmentID(nom), Org: m.org, Targets: cibles,
		Inspection: reglage, Rules: declarees,
		Kgram: m.session.Options.Kgram, Window: m.session.Options.Window,
		Jobs: m.session.Options.Jobs,
	}
	progression := ui.NewProgress(console, "Copies", len(cibles))
	rapport, err := plagiarism.RunFrom(plagiarism.Sources{
		Client: m.session.Client, Prior: anciensRapports(m.session.Options.ReportDir),
	}, requete, func(done, _ int, id string) { progression.Update(done, id) })
	progression.Clear()
	if err != nil {
		return err
	}
	if err := rapport.Barren(); err != nil {
		montrerSoucis(console, rapport)
		return err
	}

	publie, table, err := plagiarism.Publishable(rapport, m.session.Viewer,
		cours.Scope(), anonymize.Options{})
	if err != nil {
		return err
	}
	console.Printf("  %s, %s, sous l'étiquette « %s ».", valid.Plural(publie.Copies(), "copie"),
		valid.Plural(publie.Prints(), "empreinte"), publie.Origin)
	if !m.session.Options.Yes {
		suite, err := m.session.Prompt.Confirm(
			"Publier cet index dans l'organisation ?", true)
		if err != nil || !suite {
			return err
		}
	}

	if err := exchange.NewStore(m.session.Client, m.org).Publish(publie); err != nil {
		return err
	}
	chemin, err := plagiarism.WriteIndexTable(m.session.Options.ReportDir,
		rapport.Basename(), table)
	if err != nil {
		return err
	}

	ligne, err := rapport.Teaching(m.session.Viewer, true)
	if err != nil {
		return err
	}
	if _, err := m.session.registryOf(m.org).Apply(registry.Publish(ligne)); err != nil {
		return err
	}

	console.Success("Index publié dans « %s/%s ».", m.org, exchange.IndexRepo)
	console.Note("Catalogue mis à jour : vos collègues voient que vous avez donné "+
		"« %s » à %d personnes.", ligne.ID(), ligne.Copies)
	console.Note("Table de correspondance, restée ici : %s", chemin)
	return nil
}

// montrerCatalogue liste les travaux d'un enseignant, tels que le catalogue de
// l'organisation les annonce.
//
// C'est ce qui manquait pour comparer avec un collègue : les équipes disent
// déjà quels cours il a donnés, mais rien ne disait quels travaux — leurs noms
// ne se lisent que dans des dépôts qu'on ne voit pas.
func montrerCatalogue(console *ui.Console, catalogue exchange.Catalog, compte string) {
	siens := catalogue.Of(compte)
	if len(siens) == 0 {
		return
	}
	console.Heading("Travaux annoncés au catalogue")
	lignes := make([][]string, 0, len(siens))
	for _, ligne := range siens {
		empreintes := ""
		if ligne.Indexed {
			empreintes = "index publié"
		}
		lignes = append(lignes, []string{
			ligne.ID(), valid.Plural(ligne.Copies, "copie"),
			orDim(console, ligne.LastHandin, "—"), empreintes,
		})
	}
	console.Table([]string{"Travail", "Copies", "Dernière remise", ""}, lignes, 0)
	console.Note("Comparer vos copies aux siennes : "+brand.Command+" --plagiarism "+
		"--manage VOTRE-TRAVAIL --against %s — ce sont des empreintes qui "+
		"circulent, jamais du code.", siens[0].ID())
}

// Publier ce qui manque, d'un coup.
//
// Un travail annoncé au catalogue sans index publié ne sert à moitié : un
// collègue voit qu'il existe, et ne peut rien y mesurer — il ne peut que
// demander qu'on le publie. Les rattraper un par un est le genre de corvée
// qu'on remet, et c'est exactement ce qu'une passe automatisée sait faire à
// notre place.
//
// Ce qui est « manquant » se décide au catalogue, dans le domaine : le
// terminal, le navigateur et le workflow doivent en avoir la même idée.

// publierLesIndexManquants publie l'index de chaque travail annoncé qui n'en a
// pas encore.
func (s *Session) publierLesIndexManquants() (int, error) {
	org := s.Settings.Org
	set, avis := s.names(org)
	if avis != "" {
		s.Console.Print(s.Console.Warn(avis))
	}
	if set == nil {
		return ExitValidation, valid.Errorf(
			"Publication : le registre de « %s » n'a pas pu être lu.", org)
	}
	manquants := set.Catalog().Unindexed(s.Viewer)

	s.Console.Heading("Index manquants")
	if len(manquants) == 0 {
		s.Console.Success("Tous les travaux que vous avez annoncés ont leur index.")
		s.Console.Note("Pour en republier un après coup : « --publish-index " +
			"--manage a26.5n6.01.tp1 ».")
		return ExitOK, nil
	}
	lignes := make([][]string, 0, len(manquants))
	for _, ligne := range manquants {
		lignes = append(lignes, []string{
			ligne.ID(), strconv.Itoa(ligne.Copies), ligne.LastHandin,
		})
	}
	s.Console.Table([]string{"Travail", "Copies", "Dernière remise"}, lignes, 0)
	s.Console.Note("Ce qui part : des empreintes et des jetons. Ni code, ni nom. " +
		"Les tables qui relient les jetons aux personnes restent sur ce poste.")

	if !s.Options.Yes {
		suite, err := s.Prompt.Confirm(
			fmt.Sprintf("Publier %d index ?", len(manquants)), true)
		if err != nil || !suite {
			return ExitOK, err
		}
	}

	// Un travail qui échoue n'arrête pas les autres : une passe qui s'arrête au
	// premier dépôt supprimé ne publierait jamais rien.
	publies, echecs := 0, 0
	for _, ligne := range manquants {
		manager, _, groupe, err := s.travailOuvert(ligne.ID())
		if err == nil {
			err = manager.publierIndex(groupe)
		}
		if err != nil {
			echecs++
			s.Console.Failure("%s : %v", ligne.ID(), err)
			continue
		}
		publies++
	}
	if echecs > 0 {
		s.Console.Warning("%s, %d en échec.", valid.Plural(publies, "index publié", "index publiés"),
			echecs)
		return ExitFailure, nil
	}
	s.Console.Success("%s.", valid.Plural(publies, "index publié", "index publiés"))
	return ExitOK, nil
}
