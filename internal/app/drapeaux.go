package app

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/clone"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Les gestes que l'assistant range dans ses sections, au drapeau. Chacun porte
// sur le préfixe géré — sur ce que la liste en retient, quand « --filter » ou
// « --handin » la restreignent : on agit sur ce qu'on voit, comme au
// navigateur. Chacun fait une chose et s'en va, et le drapeau est la demande :
// il n'y a rien à confirmer. Seule la suppression demande davantage, le nom
// complet du dépôt, écrit exactement.
//
// Ce que ces gestes font, l'assistant le fait par les mêmes fonctions : seule
// la conversation qui y mène diffère.

// gesteDeDrapeau fait le geste que les drapeaux demandent, s'il y en a un. Le
// deuxième retour dit qu'il y en avait un : la séance s'arrête alors là.
func (m *manageSession) gesteDeDrapeau(group *groups.Group) (int, bool, error) {
	options := m.session.Options
	switch {
	case options.Access:
		options.Access = false
		return ExitOK, true, m.showAccess(group)
	case options.URLs || strings.TrimSpace(options.ExportCSV) != "":
		code, err := m.urlsDuDrapeau(group)
		options.URLs, options.ExportCSV = false, ""
		return code, true, err
	case options.CloneSet:
		options.CloneSet = false
		code, err := m.clonerDuDrapeau(group, options.Clone)
		return code, true, err
	case options.UpdateClonesSet:
		options.UpdateClonesSet = false
		code, err := m.mettreAJourDuDrapeau(group, options.UpdateClones)
		return code, true, err
	case strings.TrimSpace(options.AddCollaborator) != "" ||
		strings.TrimSpace(options.RemoveCollaborator) != "":
		code, err := m.collaborateurDuDrapeau(group)
		options.AddCollaborator, options.RemoveCollaborator = "", ""
		return code, true, err
	case strings.TrimSpace(options.DeleteRepo) != "":
		code, err := m.supprimerDuDrapeau(group)
		options.DeleteRepo = ""
		return code, true, err
	}
	return ExitOK, false, nil
}

// retenus rend les dépôts que la liste montre, ou refuse s'il n'y en a aucun :
// un geste sans objet ne doit pas passer pour un geste réussi.
func (m *manageSession) retenus(group *groups.Group, geste string) ([]groups.Repo, error) {
	depots := m.visible(group)
	if len(depots) == 0 {
		return nil, valid.Errorf("Aucun dépôt de « %s » ne répond aux critères : rien à %s.",
			group.Prefix, geste)
	}
	return depots, nil
}

// urlsDuDrapeau écrit les adresses des dépôts, les enregistre en CSV, ou les
// deux. Avec « --dry-run », le fichier n'est pas écrit.
func (m *manageSession) urlsDuDrapeau(group *groups.Group) (int, error) {
	options, console := m.session.Options, m.session.Console
	depots, err := m.retenus(group, "lister")
	if err != nil {
		return ExitValidation, err
	}
	console.Heading("URL des dépôts de « " + group.Prefix + " »")
	if options.URLs {
		m.afficherURL(depots)
	}
	cible := strings.TrimSpace(options.ExportCSV)
	if cible == "" {
		return ExitOK, nil
	}
	if options.DryRun {
		console.Warning("Simulation : %s, rien n'a été écrit.", valid.Agree(len(depots),
			"l'URL serait enregistrée dans "+cible,
			"les "+itoa(len(depots))+" URL seraient enregistrées dans "+cible))
		return ExitOK, nil
	}
	if err := m.exporterCSV(group, depots, cible); err != nil {
		return ExitFailure, err
	}
	return ExitOK, nil
}

// clonerDuDrapeau récupère les dépôts dans le dossier donné, ou dans celui que
// l'assistant proposerait.
func (m *manageSession) clonerDuDrapeau(group *groups.Group, dossier string) (int, error) {
	console := m.session.Console
	depots, err := m.retenus(group, "cloner")
	if err != nil {
		return ExitValidation, err
	}
	if strings.TrimSpace(dossier) == "" {
		dossier = m.dossierDesClones(group)
	}
	console.Heading("Cloner des dépôts de « " + group.Prefix + " »")
	if m.session.Options.DryRun {
		// Le dossier n'est pas même créé : une simulation ne laisse rien derrière
		// elle.
		chemin, err := roster.ExpandPath(dossier)
		if err != nil {
			return ExitValidation, err
		}
		console.Printf("  %s vers %s", valid.Plural(len(depots), "dépôt"), console.Info(chemin))
		for _, repo := range depots {
			console.Printf("    %s", repo.Name)
		}
		console.Warning("Simulation : rien n'a été cloné.")
		return ExitOK, nil
	}
	destination, err := clone.PrepareDestination(dossier)
	if err != nil {
		return ExitValidation, err
	}
	echecs, err := m.cloner(depots, destination)
	if err != nil {
		return ExitFailure, err
	}
	if echecs > 0 {
		return ExitFailure, nil
	}
	return ExitOK, nil
}

// mettreAJourDuDrapeau met à jour tous les clones que le dossier contient : le
// dossier est la sélection, et c'est aussi ce que l'assistant coche d'office.
func (m *manageSession) mettreAJourDuDrapeau(group *groups.Group, dossier string) (int, error) {
	console := m.session.Console
	if strings.TrimSpace(dossier) == "" {
		dossier = m.dossierDesClones(group)
	}
	clones, err := clone.FindClones(dossier)
	if err != nil {
		return ExitValidation, err
	}
	if len(clones) == 0 {
		return ExitValidation, valid.Errorf("Aucun dépôt git directement sous « %s ».", dossier)
	}
	console.Heading("Mettre à jour des clones")
	if m.session.Options.DryRun {
		console.Printf("  %s dans %s", valid.Plural(len(clones), "clone"), console.Info(dossier))
		for _, item := range clones {
			console.Printf("    %s", item.Name)
		}
		console.Warning("Simulation : aucun clone n'a été mis à jour.")
		return ExitOK, nil
	}
	echecs, err := m.mettreAJour(clones, dossier)
	if err != nil {
		return ExitFailure, err
	}
	if echecs > 0 {
		return ExitFailure, nil
	}
	return ExitOK, nil
}

// collaborateurDuDrapeau donne ou retire à un compte l'accès au dépôt de
// « --repo ». Retirer un compte qui n'a encore qu'une invitation l'annule :
// pour qui lance la commande, c'est le même geste.
func (m *manageSession) collaborateurDuDrapeau(group *groups.Group) (int, error) {
	options, console := m.session.Options, m.session.Console
	repo, _, trouve := group.Find(options.Repo)
	if !trouve {
		return ExitValidation, valid.Errorf("--repo : « %s » n'est pas un dépôt de « %s ».",
			strings.TrimSpace(options.Repo), group.Prefix)
	}
	console.Heading("Accès de « " + repo.Name + " »")

	if login := strings.TrimSpace(options.AddCollaborator); login != "" {
		permission := strings.TrimSpace(options.Permission)
		if permission == "" {
			permission = m.session.Settings.Permission
		}
		if _, err := config.ValidatePermission(permission); err != nil {
			return ExitValidation, err
		}
		if options.DryRun {
			console.Warning("Simulation : @%s serait invité sur « %s » (%s).",
				login, repo.Name, permission)
			return ExitOK, nil
		}
		if !m.compteExiste(login) {
			return ExitValidation, nil
		}
		if !m.donnerAcces(repo, login, permission) {
			return ExitFailure, nil
		}
		return ExitOK, nil
	}

	login := strings.TrimSpace(options.RemoveCollaborator)
	acces, err := m.accessOf(repo)
	if err != nil {
		return ExitFailure, err
	}
	for _, collaborateur := range acces.Collaborators {
		if !strings.EqualFold(collaborateur, login) {
			continue
		}
		if options.DryRun {
			console.Warning("Simulation : @%s perdrait son accès à « %s ».", collaborateur, repo.Name)
			return ExitOK, nil
		}
		if !m.retirerCollaborateur(repo, collaborateur) {
			return ExitFailure, nil
		}
		return ExitOK, nil
	}
	annulees, echecs := 0, 0
	for _, invitation := range acces.Invitations {
		if !strings.EqualFold(invitation.Login, login) {
			continue
		}
		annulees++
		if options.DryRun {
			console.Warning("Simulation : l'%s de @%s serait annulée.",
				etiquetteDInvitation(invitation), invitation.Login)
			continue
		}
		if !m.annulerInvitation(repo, invitation.ID) {
			echecs++
		}
	}
	switch {
	case annulees == 0:
		return ExitValidation, valid.Errorf("@%s n'a ni accès ni invitation sur « %s ».",
			login, repo.Name)
	case echecs > 0:
		return ExitFailure, nil
	}
	return ExitOK, nil
}

// supprimerDuDrapeau supprime le dépôt que « --delete-repo » nomme. Le nom
// doit être exactement le sien, en entier : c'est ce que l'assistant et le
// navigateur font retaper, et un suffixe ou une autre casse — qui suffisent
// partout ailleurs à désigner un dépôt — n'en tiennent pas lieu ici.
func (m *manageSession) supprimerDuDrapeau(group *groups.Group) (int, error) {
	console := m.session.Console
	nom := strings.TrimSpace(m.session.Options.DeleteRepo)
	repo, _, trouve := group.Find(nom)
	if !trouve || repo.Name != nom {
		return ExitValidation, valid.Errorf("--delete-repo : « %s » n'est le nom exact "+
			"d'aucun dépôt de « %s ». Son nom complet, écrit tel quel, vaut confirmation.",
			nom, group.Prefix)
	}
	console.Heading("Supprimer « " + repo.Name + " »")
	m.avertirDeLaSuppression(repo)
	if m.session.Options.DryRun {
		console.Warning("Simulation : « %s » n'a pas été supprimé.", repo.Name)
		return ExitOK, nil
	}
	if !m.session.ensureScope("delete_repo", "la suppression serait refusée") {
		console.Warning("Annulé : rien n'a été supprimé.")
		return ExitFailure, nil
	}
	if !m.supprimer(repo) {
		return ExitFailure, nil
	}
	return ExitOK, nil
}
