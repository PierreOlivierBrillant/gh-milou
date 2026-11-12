package app

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// L'invitation d'un étudiant, au terminal. GitHub la laisse expirer au bout de
// quelques jours, et un étudiant peut la refuser : dans les deux cas, il n'a
// plus de lien qui mène à son dépôt, et l'enseignant croit l'avoir invité. La
// liste le montre dès que les accès ont été relevés, et un envoi remplace
// l'invitation morte par une neuve — ou en fait partir une première.

// invitations dit où en est l'invitation de chaque dépôt du groupe, et combien
// de dépôts un envoi débloquerait.
//
// Rien n'est demandé à GitHub : ce sont les accès déjà mémorisés qui parlent,
// et un dépôt dont on ne les a pas relevés ne dit rien.
func (m *manageSession) invitations(group *groups.Group) (map[string]identity.InvitationState, int) {
	noms := nomsDuGroupe(group)
	acces := m.resolver.Accesses(m.org, noms, identity.Cached, nil)
	groupes := m.groupesDe(group)

	etats := make(map[string]identity.InvitationState, len(noms))
	for _, nom := range noms {
		var lus *identity.Access
		if connu, inspecte := acces[nom]; inspecte {
			lus = &connu
		}
		// Hors de toute place, le groupe vide ne vise personne : tous ceux qui
		// ont accès au dépôt comptent alors pour dire où il en est.
		cours, place := classroom.Of(groupes, nom)
		var equipes []teams.Team
		if place {
			equipes = m.equipesDe(cours)
		}
		etats[nom], _ = cours.InvitationOf(nom, equipes, lus)
	}
	depots := map[string]bool{}
	for _, envoi := range m.aInviter(groupes, noms, acces) {
		depots[envoi.Repo] = true
	}
	return etats, len(depots)
}

// aInviter décide des invitations à envoyer pour ces dépôts : chacun au nom de
// son groupe, qui dit qui il vise et à quel droit l'inviter — un groupe entier
// ou un cours en réunissent plusieurs. Un dépôt hors nomenclature ne vise
// personne : seules ses invitations expirées, qui nomment déjà quelqu'un, se
// renvoient, au droit des réglages.
func (m *manageSession) aInviter(groupes []classroom.Classroom, noms []string,
	acces map[string]identity.Access) []identity.Dispatch {
	return classroom.ToInviteAmong(groupes, m.equipesDe, noms, acces,
		m.session.Settings.Permission)
}

// motDInvitation colore l'état d'une invitation comme le navigateur le fait :
// une invitation expirée ou absente est celle qui demande qu'on agisse.
func motDInvitation(console *ui.Console, etat identity.InvitationState) string {
	switch etat {
	case identity.InvitationAccepted:
		return console.OK(string(etat))
	case identity.InvitationPending:
		return console.Warn(string(etat))
	case identity.InvitationExpired, identity.InvitationNone:
		return console.Err(string(etat))
	}
	return console.Dim("—")
}

// envoisDuDepot rend ce qu'il faut envoyer pour qu'un dépôt s'ouvre à celui
// qu'il vise, d'après des accès qu'on vient de relire.
func (m *manageSession) envoisDuDepot(group *groups.Group, repo groups.Repo,
	acces identity.Access) []identity.Dispatch {
	return m.aInviter(m.groupesDe(group), []string{repo.Name},
		map[string]identity.Access{repo.Name: acces})
}

// gesteDEnvoi nomme dans un menu ce qu'un envoi fera.
func gesteDEnvoi(envois []identity.Dispatch) string {
	gestes := make([]string, 0, len(envois))
	for _, envoi := range envois {
		if envoi.First() {
			gestes = append(gestes, "inviter @"+envoi.Invitation.Login)
			continue
		}
		gestes = append(gestes, "renvoyer l'invitation expirée de @"+envoi.Invitation.Login)
	}
	return "Envoyer l'invitation manquante (" + strings.Join(gestes, ", ") + ")"
}

// envoyerAuDepot fait partir ce qui manque à un seul dépôt.
func (m *manageSession) envoyerAuDepot(envois []identity.Dispatch) {
	console := m.session.Console
	var faits []identity.Dispatch
	ui.Await(console, "Envoi des invitations…", func() {
		faits = m.resolver.Send(m.org, envois, nil)
	})
	for _, fait := range faits {
		if fait.Error != "" {
			console.Failure("%s", fait.Summary())
			continue
		}
		console.Success("%s", fait.Summary())
	}
}

// envoyerManquantes relit les accès du groupe et envoie les invitations qui
// manquent : une neuve à la place de chaque invitation expirée, une première à
// qui n'en a aucune. « demander » fait confirmer avant d'agir : l'assistant le
// fait, la ligne de commande non — le drapeau est déjà la demande.
//
// Le second retour compte les envois qui ont échoué : la ligne de commande en
// fait son code de sortie.
func (m *manageSession) envoyerManquantes(group *groups.Group, demander bool) (int, error) {
	noms := nomsDuGroupe(group)
	if len(noms) == 0 {
		return 0, valid.Errorf("Aucun dépôt dans « %s ».", group.Prefix)
	}
	console := m.session.Console
	console.Heading("Invitations manquantes de « " + group.Prefix + " »")

	// Les accès sont relus : ce qui a expiré depuis le dernier relevé compte, et
	// ce qui a été accepté entre-temps n'a pas à l'être une seconde fois.
	progression := ui.NewProgress(console, "Accès", len(noms))
	lus := m.resolver.Accesses(m.org, noms, identity.Refresh,
		func(done, _ int, repo string) { progression.Update(done, repo) })
	progression.Clear()
	if manquants := len(noms) - len(lus); manquants > 0 {
		console.Warning("%s", valid.Plural(manquants,
			"dépôt n'a pas pu être lu : ses invitations restent inconnues.",
			"dépôts n'ont pas pu être lus : leurs invitations restent inconnues."))
	}

	envois := m.aInviter(m.groupesDe(group), noms, lus)
	if len(envois) == 0 {
		console.Success("Aucune invitation ne manque.")
		return 0, nil
	}
	rows := make([][]string, 0, len(envois))
	for _, envoi := range envois {
		geste := "renvoi (expirée)"
		if envoi.First() {
			geste = "première invitation"
		}
		rows = append(rows, []string{
			envoi.Repo, "@" + envoi.Invitation.Login, envoi.Invitation.Permission, geste,
		})
	}
	console.Table([]string{"Dépôt", "Compte", "Droit", "Envoi"}, rows, 40)

	if m.session.Options.DryRun {
		console.Note("Simulation : %s, rien n'est parti.",
			valid.Plural(len(envois), "invitation partirait", "invitations partiraient"))
		return 0, nil
	}
	if demander {
		console.Note("Une invitation expirée est annulée puis remplacée par une nouvelle, " +
			"au même droit ; qui n'en a aucune — jamais invité, invitation refusée ou " +
			"annulée — en reçoit une première, au droit du groupe. GitHub envoie un " +
			"courriel à chacun.")
		confirme, err := m.session.Prompt.Confirm(
			valid.Agree(len(envois), "Envoyer cette invitation ?",
				"Envoyer ces "+itoa(len(envois))+" invitations ?"), true)
		if err != nil || !confirme {
			return 0, err
		}
	}

	progression = ui.NewProgress(console, "Invitations", len(envois))
	faits := m.resolver.Send(m.org, envois,
		func(done, _ int, envoi identity.Dispatch) { progression.Update(done, envoi.Repo) })
	progression.Clear()
	echecs := 0
	for _, fait := range faits {
		if fait.Error != "" {
			echecs++
			console.Failure("%s : %s", fait.Repo, fait.Summary())
			continue
		}
		console.Success("%s : %s", fait.Repo, fait.Summary())
	}
	if echecs > 0 {
		console.Warning("%s sur %d %s.", valid.Plural(echecs, "envoi"), len(faits),
			valid.Agree(echecs, "a échoué", "ont échoué"))
	}
	return echecs, nil
}

// nomsDuGroupe rend les noms des dépôts du groupe, dans leur ordre.
func nomsDuGroupe(group *groups.Group) []string {
	noms := make([]string, 0, group.Len())
	for _, repo := range group.Repos {
		noms = append(noms, repo.Name)
	}
	return noms
}
