package app

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Corriger la fiche d'un étudiant au terminal, c'est le « Renommer… » de la
// liste d'un groupe au navigateur : son nom complet, son compte GitHub, et, si
// on le demande, ses dépôts pour qu'ils portent le nouveau nom. Ce qui est
// permis et ce qui est refusé se décide dans « classroom.PlanCorrection » ; ce
// fichier ne fait que poser les questions et rendre compte.

// correctionMode corrige l'étudiant que « --student » désigne, dans le groupe
// que « --manage » désigne.
func (s *Session) correctionMode() (int, error) {
	place := strings.TrimSpace(s.Options.Manage)
	if place == "" {
		chosen, err := s.askPlace("Dans quel groupe ?")
		if err != nil {
			return ExitValidation, err
		}
		place = chosen
	}
	org := s.Settings.Org
	repos, err := s.orgRepos(org, false)
	if err != nil {
		return ExitFailure, err
	}
	cours, err := s.groupeEnrichi(placeDuGroupe(place), repos)
	if err != nil {
		return ExitValidation, err
	}
	voulu := roster.Person{FullName: s.Options.FullName, Username: s.Options.StudentAccount}
	suivis, code, err := s.correctStudent(cours, repos, s.Options.Student, voulu,
		s.Options.RenameRepos)
	// L'inventaire en cache suit les renommages plutôt que d'être relu : c'est
	// « groups » qui décide de ce qu'un renommage lui fait.
	if len(suivis) > 0 {
		s.Cache.Set(cache.ReposKey(org), groups.WithRenamed(repos, suivis))
	}
	return code, err
}

// placeDuGroupe accepte le travail géré aussi bien que son groupe : un étudiant
// appartient au groupe, et « a26.5n6.01.tp1 » dit lequel.
func placeDuGroupe(prefixe string) string {
	if place, _, travail := naming.SplitAssignment(prefixe); travail {
		return place
	}
	return strings.TrimSpace(prefixe)
}

// groupeEnrichi résout un groupe par sa place, tel que les autres interfaces le
// voient : sa liste s'il en a une ici, les noms du registre, et les personnes
// que seuls ses dépôts révèlent.
func (s *Session) groupeEnrichi(place string, repos []groups.RepoInfo) (classroom.Classroom, error) {
	cours, err := classroom.AtScope(s.Settings.Org, place, classroom.DefaultsFrom(s.Settings))
	if err != nil {
		return cours, err
	}
	if connu, existe := s.groupStore().Find(cours.Org, cours.Scope()); existe {
		cours = connu
	}
	set, _ := s.names(cours.Org)
	return cours.Enrich(set, repos), nil
}

// correctStudent corrige la fiche d'un étudiant du groupe, et renomme ses
// dépôts si on le demande. Tout est montré avant d'écrire, et une seule
// confirmation couvre la fiche et les dépôts : refuser le plan ne doit rien
// laisser à moitié fait.
//
// Elle rend ce qui a été renommé, pour que l'inventaire de l'appelant le suive.
func (s *Session) correctStudent(cours classroom.Classroom, repos []groups.RepoInfo,
	username string, voulu roster.Person, avecDepots bool) ([]groups.Renamed, int, error) {
	console := s.Console
	correction, err := classroom.PlanCorrection(cours, username, voulu, avecDepots, repos)
	if err != nil {
		return nil, ExitValidation, err
	}
	avant, apres := correction.Before, correction.After

	console.Heading("Fiche de @" + avant.Username + " dans « " + cours.Scope() + " »")
	console.Printf("  %s %s", console.Dim(pad("Nom complet", 18)),
		changement(console, avant.FullName, apres.FullName, "sans nom"))
	console.Printf("  %s %s", console.Dim(pad("Compte GitHub", 18)),
		changement(console, "@"+avant.Username, "@"+apres.Username, ""))
	switch {
	case len(correction.Moves) > 0:
		s.montrerRenommages(correction.Moves)
	case avecDepots:
		console.Note("Ses dépôts portent déjà ce nom : il n'y a rien à renommer.")
	case avant.FullName != apres.FullName:
		console.Note("Ses dépôts gardent leur nom, et restent les siens. Pour qu'ils " +
			"portent le nouveau, demandez aussi leur renommage.")
	}

	// Un compte qui n'existe pas sur GitHub ne sert à rien dans une liste :
	// aucun dépôt ne pourra lui être remis. Celui qui ne change pas a déjà été
	// vérifié à l'inscription.
	if !strings.EqualFold(apres.Username, avant.Username) {
		if existe, err := s.Client.UserExists(apres.Username); err == nil && !existe {
			return nil, ExitValidation, valid.Errorf(
				"Le compte « %s » n'existe pas sur GitHub.", apres.Username)
		}
	}

	if s.Options.DryRun {
		console.Warning("Simulation : ni la fiche ni les dépôts n'ont été touchés.")
		return nil, ExitOK, nil
	}
	if !s.Options.Yes {
		suite, err := s.Prompt.Confirm(questionDeCorrection(correction), false)
		if err != nil || !suite {
			console.Warning("Annulé : rien n'a été corrigé.")
			return nil, ExitOK, err
		}
	}

	if correction.Changed() {
		if err := s.ecrireCorrection(cours.Org, correction); err != nil {
			return nil, ExitFailure, err
		}
		console.Success("Fiche de @%s corrigée.", apres.Username)
	}
	if len(correction.Moves) == 0 {
		return nil, ExitOK, nil
	}
	return s.executerRenommages(cours.Org, correction.Moves, depotsRenommes)
}

// ecrireCorrection écrit la fiche corrigée : au registre d'abord, puis dans la
// liste du groupe. C'est l'ordre du navigateur, et pour la même raison : un
// registre qui refuse arrête tout avant qu'une liste n'ait bougé.
func (s *Session) ecrireCorrection(org string, correction classroom.Correction) error {
	listes := s.groupStore()
	// Le registre retient le nouveau nom sans oublier les anciens slugs — ceux
	// que les listes de ce poste lui donnaient compris : les dépôts déjà créés
	// restent rattachés à la personne, qu'on les renomme ou non.
	if _, err := s.registryOf(org).Apply(
		registry.Name(correction.After, listes.People(org)...)); err != nil {
		return err
	}
	if _, err := listes.Save(correction.Classroom); err != nil {
		return err
	}
	// Une autre liste qui la nommait encore autrement masquerait la correction.
	_, err := listes.Unname(org, correction.After.Username)
	return err
}

// questionDeCorrection dit ce que la confirmation engage, et rien de plus.
func questionDeCorrection(correction classroom.Correction) string {
	depots := len(correction.Moves)
	qui := "@" + correction.Before.Username
	switch {
	case correction.Changed() && depots > 0:
		return "Corriger la fiche de " + qui + " et renommer " +
			valid.Agree(depots, "son dépôt", "ses "+itoa(depots)+" dépôts") + " ?"
	case correction.Changed():
		return "Corriger la fiche de " + qui + " ?"
	default:
		return "Renommer " + valid.Agree(depots, "le dépôt", "les "+itoa(depots)+" dépôts") +
			" de " + qui + " ?"
	}
}

// changement écrit une valeur qui change, ou qui reste.
func changement(console *ui.Console, avant, apres, vide string) string {
	montrer := func(valeur string) string {
		if valeur == "" {
			return console.Dim(vide)
		}
		return valeur
	}
	if avant == apres {
		return montrer(avant)
	}
	return montrer(avant) + " → " + montrer(apres)
}

// ------------------------------------------------------------------- menu

// corriger mène la correction depuis la gestion d'un travail : l'étudiant
// appartient au groupe du travail, et ses dépôts de tous les travaux de ce
// groupe suivent.
func (m *manageSession) corriger(group *groups.Group) error {
	if _, err := m.loadRepos(false); err != nil {
		return err
	}
	cours, _, reconnu := m.travail(group)
	if !reconnu {
		return valid.Errorf("« %s » ne dit pas à quel groupe il appartient : ses "+
			"étudiants n'ont pas de liste où se corriger. Déplacez-le d'abord vers une "+
			"place « session.cours.groupe ».", group.Prefix)
	}

	options := make([]ui.Option, 0, len(cours.Students))
	for _, identite := range cours.Identities() {
		personne := identite.Person()
		// Sans compte, personne ne se désigne ici : c'est la liste du collège
		// qui l'a inscrite, et c'est son compte qu'il faut d'abord lui donner.
		if personne.Username == "" {
			continue
		}
		options = append(options, ui.Option{Value: personne.Username, Label: nommer(personne)})
	}
	if len(options) == 0 {
		return valid.Errorf("« %s » n'a aucun étudiant à corriger.", cours.Label())
	}
	prompt := m.session.Prompt
	compte, err := prompt.Choose("Quel étudiant ?", options, options[0].Value)
	if err != nil {
		return err
	}
	personne, _ := cours.Find(compte)

	nom, err := prompt.Ask(ui.Question{
		Title: "Nom complet", Default: personne.FullName, AllowEmpty: true,
	})
	if err != nil {
		return err
	}
	nouveau, err := prompt.Ask(ui.Question{
		Title: "Compte GitHub", Default: personne.Username, AllowEmpty: true,
	})
	if err != nil {
		return err
	}
	avecDepots, err := prompt.Confirm(
		"Renommer aussi ses dépôts pour qu'ils portent ce nom ?", true)
	if err != nil {
		return err
	}

	suivis, _, err := m.session.correctStudent(cours, m.repos, compte,
		roster.Person{FullName: nom, Username: nouveau}, avecDepots)
	m.suivre(func(repos []groups.RepoInfo) []groups.RepoInfo {
		return groups.WithRenamed(repos, suivis)
	})
	return err
}
