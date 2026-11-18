package web

import (
	"net/http"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/users"
)

// Rattacher les dépôts que personne ne réclame.
//
// Deux chemins y mènent, comme au terminal. L'annuaire les prend tous d'un
// coup : c'est là qu'on lit « 11 dépôts sans étudiant connu », et c'est à
// l'échelle de l'organisation que le registre écrit. La ligne d'un dépôt hors
// liste, dans l'écran de son travail, en prend un : c'est là qu'on le voit, et
// qu'on peut dire à qui il est quand GitHub ne le sait pas.
//
// Ce qui s'écrit est le registre, non les dépôts. Aucun nom de dépôt ne change ;
// le dernier niveau reste rattaché à sa personne pour toujours, et les
// prochains dépôts porteront son nom. Tout ce qui est décidé — ce qui est
// orphelin, ce que GitHub reconnaît, ce qu'un rattachement à la main exige —
// l'est dans « classroom ».

// orphansOf relève les dépôts sans étudiant de l'organisation, sur le même
// annuaire que la liste : ce qu'on rattache doit être ce qu'on voyait.
func (s *Server) orphansOf(org string) ([]classroom.Orphan, error) {
	annuaire, err := s.directoryOf(org, false)
	if err != nil {
		return nil, err
	}
	return users.Orphans(annuaire.visibles, annuaire.repos, annuaire.equipes), nil
}

// handleOrphans dit ce que GitHub sait des derniers niveaux sans étudiant :
// ceux qui sont des comptes, avec le nom de leur profil, et les autres.
func (s *Server) handleOrphans(writer http.ResponseWriter, request *http.Request) {
	org := s.org()
	orphelins, err := s.orphansOf(org)
	if err != nil {
		fail(writer, err)
		return
	}
	plan, err := classroom.PlanAdoption(orphelins, s.resolver(org))
	if err != nil {
		fail(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"found": plan.Found, "unknown": plan.Unknown, "repos": plan.Repos(),
	})
}

// handleAdoptOrphans rattache ceux des comptes reconnus que la page a gardés.
//
// Le plan est refait ici plutôt que reçu : la page ne dit que les derniers
// niveaux retenus, et ce qui s'écrit est ce que GitHub répond maintenant, non
// ce qu'elle affichait.
func (s *Server) handleAdoptOrphans(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Fragments []string `json:"fragments"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	org := s.org()
	orphelins, err := s.orphansOf(org)
	if err != nil {
		fail(writer, err)
		return
	}
	plan, err := classroom.PlanAdoption(orphelins, s.resolver(org))
	if err != nil {
		fail(writer, err)
		return
	}
	retenu := plan.Only(body.Fragments)
	if retenu.Ready() {
		if _, err := s.registryOf(org).Apply(
			registry.Attach(attachments(retenu.Found)...)); err != nil {
			fail(writer, err)
			return
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"repos": retenu.Repos(), "accounts": len(retenu.Found),
	})
}

// handleOrphan dit ce que GitHub sait du dernier niveau d'un dépôt sans
// étudiant : c'est ce qui permet de proposer le compte et le nom plutôt que de
// les faire saisir.
func (s *Server) handleOrphan(writer http.ResponseWriter, request *http.Request) {
	org := s.org()
	orphelins, err := s.orphansOf(org)
	if err != nil {
		fail(writer, err)
		return
	}
	orphelin, err := classroom.FindOrphan(orphelins, request.PathValue("fragment"))
	if err != nil {
		fail(writer, err)
		return
	}
	plan, err := classroom.PlanAdoption([]classroom.Orphan{orphelin}, s.resolver(org))
	if err != nil {
		fail(writer, err)
		return
	}
	var compte *roster.Person
	if plan.Ready() {
		compte = &plan.Found[0].Person
	}
	writeJSON(writer, http.StatusOK, map[string]any{"orphan": orphelin, "account": compte})
}

// handleAttachOrphan rattache un dépôt sans étudiant au compte que la page a
// donné — le dernier niveau lui-même, le plus souvent.
func (s *Server) handleAttachOrphan(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Username string `json:"username"`
		FullName string `json:"full_name"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	org := s.org()
	orphelins, err := s.orphansOf(org)
	if err != nil {
		fail(writer, err)
		return
	}
	set, _ := s.names(org)
	trouve, err := classroom.PlanAttach(orphelins, classroom.AttachRequest{
		Fragment: request.PathValue("fragment"), Login: body.Username,
		FullName: body.FullName,
	}, set, s.resolver(org))
	if err != nil {
		fail(writer, err)
		return
	}
	if _, err := s.registryOf(org).Apply(
		registry.Attach(attachments([]classroom.Found{trouve})...)); err != nil {
		fail(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"person": trouve.Person, "repos": len(trouve.Orphan.Repos),
	})
}

// attachments traduit ce que le domaine a trouvé en une écriture du registre.
// C'est le seul endroit où les deux vocabulaires se rencontrent : ni l'un ni
// l'autre paquet n'a besoin de connaître l'autre.
func attachments(trouves []classroom.Found) []registry.Attachment {
	liens := make([]registry.Attachment, 0, len(trouves))
	for _, trouve := range trouves {
		liens = append(liens, registry.Attachment{
			Fragment: trouve.Orphan.Fragment, Person: trouve.Person})
	}
	return liens
}
